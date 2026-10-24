package panel

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/hann0w0/singbox-panel/internal/config"
	"github.com/hann0w0/singbox-panel/internal/domain/model"
)

func TestTrafficCycleUsesUTCCalendarAndClampsEachMonth(t *testing.T) {
	cases := []struct {
		now, start, end string
		day             int
	}{
		{"2026-10-05T00:00:00Z", "2026-10-05T00:00:00Z", "2026-11-05T00:00:00Z", 5},
		{"2026-10-04T23:59:59Z", "2026-09-05T00:00:00Z", "2026-10-05T00:00:00Z", 5},
		{"2026-10-05T00:30:00+08:00", "2026-09-05T00:00:00Z", "2026-10-05T00:00:00Z", 5},
		{"2026-02-27T23:59:59Z", "2026-01-31T00:00:00Z", "2026-02-28T00:00:00Z", 31},
		{"2026-02-28T00:00:00Z", "2026-02-28T00:00:00Z", "2026-03-31T00:00:00Z", 31},
		{"2028-02-29T00:00:00Z", "2028-02-29T00:00:00Z", "2028-03-31T00:00:00Z", 31},
		{"2026-01-01T01:00:00Z", "2025-12-15T00:00:00Z", "2026-01-15T00:00:00Z", 15},
		{"2026-10-05T00:00:00Z", "2026-10-01T00:00:00Z", "2026-11-01T00:00:00Z", 0},
	}
	for _, tc := range cases {
		t.Run(tc.now+fmt.Sprint(tc.day), func(t *testing.T) {
			now, err := time.Parse(time.RFC3339, tc.now)
			if err != nil {
				t.Fatal(err)
			}
			start, end := trafficCycleBounds(now, tc.day)
			if start.Format(time.RFC3339) != tc.start || end.Format(time.RFC3339) != tc.end || start.Location() != time.UTC || end.Location() != time.UTC {
				t.Fatalf("cycle = %v / %v, want %s / %s", start, end, tc.start, tc.end)
			}
		})
	}
}

func TestServerTrafficWindowKeepsSelectedRangeAndResetsAtUTCMidnight(t *testing.T) {
	reset := time.Date(2026, 10, 5, 0, 0, 0, 0, time.UTC)
	for _, rangeName := range []string{"1h", "12h", "24h", "7d", "30d"} {
		for _, now := range []time.Time{reset.Add(-time.Second), reset, reset.Add(36 * time.Hour)} {
			cfg, start, end, usageStart, ok := serverTrafficWindow(now, rangeName, 5)
			wantStart, wantEnd := trafficWindow(now, trafficRanges[rangeName], time.UTC)
			wantUsageStart, _ := trafficCycleBounds(now, 5)
			if wantUsageStart.Before(wantStart) {
				wantUsageStart = wantStart
			}
			if !ok || cfg != trafficRanges[rangeName] || !start.Equal(wantStart) || !end.Equal(wantEnd) || !usageStart.Equal(wantUsageStart) {
				t.Fatalf("%s at %v: chart=%v..%v usage=%v", rangeName, now, start, end, usageStart)
			}
		}
	}
	if _, _, _, _, ok := serverTrafficWindow(reset, "cycle", 5); ok {
		t.Fatal("removed cycle range is still accepted")
	}
}

func TestTrafficResetExcludesPreviousUsageWithoutDeletingHistory(t *testing.T) {
	db := testDB(t)
	now := time.Date(2026, 10, 6, 13, 0, 0, 0, time.UTC)
	cfg, start, end, usageStart, ok := serverTrafficWindow(now, "7d", 5)
	if !ok || !end.Equal(time.Date(2026, 10, 7, 0, 0, 0, 0, time.UTC)) {
		t.Fatal("invalid history window")
	}
	rows := []model.TrafficHourly{
		{ServerID: 1, Bucket: usageStart.Add(-time.Hour), Download: 1000},
		{ServerID: 1, InboundID: 9, Bucket: usageStart.Add(-time.Hour), Download: 900},
		{ServerID: 1, Bucket: usageStart, Upload: 20, Download: 100},
		{ServerID: 1, InboundID: 9, Bucket: usageStart, Upload: 10, Download: 60},
		{ServerID: 2, Bucket: usageStart, Download: 5000},
	}
	if err := db.Create(&rows).Error; err != nil {
		t.Fatal(err)
	}
	records, err := queryTrafficBuckets(db, trafficQuery{hourly: cfg.hourly, start: usageStart, end: end, step: cfg.step, serverID: 1})
	if err != nil {
		t.Fatal(err)
	}
	total, inbounds := buildAggregatedTrafficPoints(records, start, end, cfg.step)
	up, down := sumTraffic(total)
	ibUp, ibDown := sumTraffic(inbounds[9])
	if up != 20 || down != 100 || ibUp != 10 || ibDown != 60 || len(total) != 7 {
		t.Fatalf("reset node=%d/%d inbound=%d/%d points=%d", up, down, ibUp, ibDown, len(total))
	}
	for _, point := range total {
		if point.Time.Before(usageStart) && (point.Upload != 0 || point.Download != 0) {
			t.Fatalf("pre-reset chart point contains usage: %+v", point)
		}
	}
	var count int64
	if err := db.Model(&model.TrafficHourly{}).Count(&count).Error; err != nil || count != int64(len(rows)) {
		t.Fatalf("history was removed: count=%d error=%v", count, err)
	}
	// A shorter selected range must not retain the whole reset period's total.
	cfg, start, end, usageStart, _ = serverTrafficWindow(now, "24h", 5)
	records, err = queryTrafficBuckets(db, trafficQuery{hourly: cfg.hourly, start: usageStart, end: end, step: cfg.step, serverID: 1})
	if err != nil {
		t.Fatal(err)
	}
	total, _ = buildAggregatedTrafficPoints(records, start, end, cfg.step)
	if up, down = sumTraffic(total); up != 0 || down != 0 || len(total) != 24 {
		t.Fatalf("range totals did not follow 24h: %d/%d, points=%d", up, down, len(total))
	}
}

func TestServerTrafficResetDayUpdateAndDefault24Hours(t *testing.T) {
	db := testDB(t)
	srv := model.Server{Name: "cycle", AgentToken: "cycle-token", TrafficResetDay: 20}
	if err := db.Create(&srv).Error; err != nil {
		t.Fatal(err)
	}
	app := &App{db: db, cfg: config.PanelConfig{Timezone: "Asia/Shanghai"}, serverOperations: newKeyedMutex[uint]()}
	router := gin.New()
	router.PUT("/servers/:id", app.updateServer)
	router.GET("/servers/:id/traffic", app.serverTraffic)
	path := fmt.Sprintf("/servers/%d", srv.ID)
	for _, day := range []int{-1, 0, 32} {
		res := serveJSONRequest(t, router, http.MethodPut, path, fmt.Sprintf(`{"name":"must-not-save","traffic_reset_day":%d}`, day))
		if res.Code != http.StatusBadRequest {
			t.Fatalf("invalid day %d: %d", day, res.Code)
		}
	}
	if err := db.First(&srv, srv.ID).Error; err != nil {
		t.Fatal(err)
	}
	if srv.Name != "cycle" || srv.TrafficResetDay != 20 {
		t.Fatalf("invalid update changed server: %+v", srv)
	}
	res := serveJSONRequest(t, router, http.MethodPut, path, `{"name":"renamed"}`)
	if res.Code != http.StatusOK {
		t.Fatal(res.Body.String())
	}
	if err := db.First(&srv, srv.ID).Error; err != nil {
		t.Fatal(err)
	}
	if srv.TrafficResetDay != 20 {
		t.Fatal("omitted reset day changed")
	}
	res = serveJSONRequest(t, router, http.MethodPut, path, `{"traffic_reset_day":31}`)
	if res.Code != http.StatusOK {
		t.Fatal(res.Body.String())
	}
	cycleStart, _ := trafficCycleBounds(time.Now(), 31)
	if err := db.Create(&[]model.TrafficHourly{
		{ServerID: srv.ID, Bucket: time.Now().UTC().Truncate(time.Hour), Upload: 20, Download: 100},
		{ServerID: srv.ID, Bucket: cycleStart.Add(-time.Hour), Upload: 500, Download: 1000},
	}).Error; err != nil {
		t.Fatal(err)
	}
	res = httptest.NewRecorder()
	router.ServeHTTP(res, httptest.NewRequest(http.MethodGet, path+"/traffic", nil))
	if res.Code != http.StatusOK {
		t.Fatal(res.Body.String())
	}
	var result trafficSeries
	if err := json.Unmarshal(res.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.Range != "24h" || result.TrafficResetDay != 31 || result.Timezone != "UTC" || len(result.Points) != 24 {
		t.Fatalf("24h response = %+v", result)
	}
	if result.Upload != 20 || result.Download != 100 {
		t.Fatalf("unexpected range totals: %+v", result)
	}
	res = httptest.NewRecorder()
	router.ServeHTTP(res, httptest.NewRequest(http.MethodGet, path+"/traffic?range=7d", nil))
	if res.Code != http.StatusOK {
		t.Fatal(res.Body.String())
	}
	if err := json.Unmarshal(res.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.Timezone != "UTC" || len(result.Points) != 7 || result.Points[0].Time.Hour() != 0 {
		t.Fatalf("relative range not aligned to UTC: %+v", result)
	}
	if result.Upload != 20 || result.Download != 100 {
		t.Fatalf("history range included pre-reset traffic: %+v", result)
	}
}
