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

func TestTrafficCycleExcludesPreviousCycleAndUsesSameInboundWindow(t *testing.T) {
	db := testDB(t)
	now := time.Date(2026, 10, 6, 13, 0, 0, 0, time.UTC)
	cfg, start, end, _, _, ok := serverTrafficWindow(now, "cycle", 5)
	if !ok || !end.Equal(time.Date(2026, 10, 7, 0, 0, 0, 0, time.UTC)) {
		t.Fatal("invalid cycle window")
	}
	rows := []model.TrafficHourly{
		{ServerID: 1, Bucket: start.Add(-time.Hour), Download: 1000},
		{ServerID: 1, InboundID: 9, Bucket: start.Add(-time.Hour), Download: 900},
		{ServerID: 1, Bucket: start, Upload: 20, Download: 100},
		{ServerID: 1, InboundID: 9, Bucket: start, Upload: 10, Download: 60},
		{ServerID: 2, Bucket: start, Download: 5000},
	}
	if err := db.Create(&rows).Error; err != nil {
		t.Fatal(err)
	}
	records, err := queryTrafficBuckets(db, trafficQuery{hourly: cfg.hourly, start: start, end: end, step: cfg.step, serverID: 1})
	if err != nil {
		t.Fatal(err)
	}
	total, inbounds := buildAggregatedTrafficPoints(records, start, end, cfg.step)
	up, down := sumTraffic(total)
	ibUp, ibDown := sumTraffic(inbounds[9])
	if up != 20 || down != 100 || ibUp != 10 || ibDown != 60 || len(total) != 2 {
		t.Fatalf("cycle node=%d/%d inbound=%d/%d points=%d", up, down, ibUp, ibDown, len(total))
	}
}

func TestServerTrafficResetDayUpdateAndDefaultCycle(t *testing.T) {
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
		{ServerID: srv.ID, Bucket: cycleStart, Upload: 20, Download: 100},
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
	if result.Range != "cycle" || result.TrafficResetDay != 31 || result.Timezone != "UTC" || result.CycleStart.IsZero() || !result.CycleEnd.After(result.CycleStart) {
		t.Fatalf("cycle response = %+v", result)
	}
	if result.CycleUpload != 20 || result.CycleDownload != 100 || result.Upload != 20 || result.Download != 100 {
		t.Fatalf("unexpected cycle totals: %+v", result)
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
	if result.CycleUpload != 20 || result.CycleDownload != 100 {
		t.Fatalf("history range changed cycle totals: %+v", result)
	}
}
