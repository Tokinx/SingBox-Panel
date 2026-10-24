package panel

import (
	"testing"
	"time"

	"github.com/hann0w0/singbox-panel/internal/domain/model"
	"github.com/hann0w0/singbox-panel/internal/domain/protocol"
)

func timestampedFixtureReport(f userTrafficFixture, reset time.Time) *protocol.TrafficSnapshot {
	bucket := func(at time.Time, amount uint64) protocol.TrafficTimeBucket {
		return protocol.TrafficTimeBucket{Time: at.Unix(), Upload: amount,
			Ports: []protocol.PortTrafficSnapshot{{Inbound: f.multi.Tag, Upload: amount}},
			Users: []protocol.UserTrafficSnapshot{{Inbound: f.multi.Tag, User: f.alice.Email, Upload: amount}},
		}
	}
	return &protocol.TrafficSnapshot{AccountingVersion: 2, SampledAt: reset.Add(time.Minute).Unix(), UploadTotal: 999999,
		Report: &protocol.TrafficReport{Session: "7795c26e-d04c-4f36-aa76-fc891179f361", Sequence: 1,
			Buckets: []protocol.TrafficTimeBucket{bucket(reset.Add(-time.Minute), 1000), bucket(reset.Add(time.Minute), 200)}},
	}
}

func TestTimestampedReportsPreserveResetAndDeduplicateAcrossRestart(t *testing.T) {
	f := newUserTrafficFixture(t, model.ConfigModeManaged)
	reset := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	snapshot := timestampedFixtureReport(f, reset)
	now := reset.Add(time.Hour)
	for i := 0; i < 2; i++ {
		// The deduplication state is in the DB, not a previous writer instance.
		if err := recordTimestampedTraffic(f.db, f.server.ID, snapshot, now); err != nil {
			t.Fatal(err)
		}
	}
	_, start, end, usageStart, _ := serverTrafficWindow(now, "7d", 1)
	rows, err := queryTrafficBuckets(f.db, trafficQuery{hourly: true, start: usageStart, end: end, step: 24 * time.Hour, serverID: f.server.ID})
	if err != nil {
		t.Fatal(err)
	}
	points, ports := buildAggregatedTrafficPoints(rows, start, end, 24*time.Hour)
	if upload, _ := sumTraffic(points); upload != 200 {
		t.Fatalf("new-cycle node usage = %d", upload)
	}
	if upload, _ := sumTraffic(ports[f.multi.ID]); upload != 200 {
		t.Fatalf("new-cycle inbound usage = %d", upload)
	}
	if usage := userRows(t, f.db)[f.alice.ID]; usage[0] != 1200 {
		t.Fatalf("user history duplicated or lost: %v", usage)
	}
	var server model.Server
	if err := f.db.First(&server, f.server.ID).Error; err != nil {
		t.Fatal(err)
	}
	if server.TrafficUpload != 1200 {
		t.Fatalf("lifetime total = %d", server.TrafficUpload)
	}
	// A heartbeat's absolute counters do not add bytes a second time.
	if err := newTrafficWriter(f.db).record(f.server.ID, &protocol.TrafficSnapshot{AccountingVersion: 2, UploadTotal: 2000000, SampledAt: now.Unix()}); err != nil {
		t.Fatal(err)
	}
	if err := f.db.First(&server, f.server.ID).Error; err != nil {
		t.Fatal(err)
	}
	if server.TrafficUpload != 1200 {
		t.Fatal("heartbeat duplicated accounting")
	}
}

func TestTrafficReceiptRollsBackWithFailedHistoryWrite(t *testing.T) {
	f := newUserTrafficFixture(t, model.ConfigModeManaged)
	reset := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	snapshot := timestampedFixtureReport(f, reset)
	if err := f.db.Exec("CREATE TRIGGER reject_usage BEFORE INSERT ON traffic_user_hourly BEGIN SELECT RAISE(ABORT, 'simulated write failure'); END").Error; err != nil {
		t.Fatal(err)
	}
	if err := recordTimestampedTraffic(f.db, f.server.ID, snapshot, reset.Add(time.Hour)); err == nil {
		t.Fatal("expected write error")
	}
	for _, table := range []any{&model.TrafficRecord{}, &model.TrafficHourly{}, &model.TrafficReportCursor{}} {
		var count int64
		if err := f.db.Model(table).Count(&count).Error; err != nil {
			t.Fatal(err)
		}
		if count != 0 {
			t.Fatal("failed transaction retained history or receipt")
		}
	}
	if err := f.db.Exec("DROP TRIGGER reject_usage").Error; err != nil {
		t.Fatal(err)
	}
	if err := recordTimestampedTraffic(f.db, f.server.ID, snapshot, reset.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if usage := userRows(t, f.db)[f.alice.ID]; usage[0] != 1200 {
		t.Fatalf("retry lost usage: %v", usage)
	}
}
