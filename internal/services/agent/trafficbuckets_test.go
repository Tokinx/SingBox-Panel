package agent

import (
	"testing"
	"time"
)

func TestOfflineTrafficKeepsUTCResetBoundary(t *testing.T) {
	s := newTrafficSampler()
	reset := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	s.ingest("api", clashTrafficResponse{}, reset.Add(-2*time.Minute))
	s.ingest("api", clashTrafficResponse{UploadTotal: 1000}, reset.Add(-time.Minute))
	s.ingest("api", clashTrafficResponse{UploadTotal: 1200}, reset.Add(time.Minute))
	report := s.accountingSnapshot().Report
	var before, after uint64
	for _, b := range report.Buckets {
		if b.Time < reset.Unix() {
			before += b.Upload
		} else {
			after += b.Upload
		}
	}
	if before != 1000 || after != 200 {
		t.Fatalf("usage before/after reset = %d/%d", before, after)
	}
	s.available = false
	if snap := s.accountingSnapshot(); !snap.Unavailable || snap.Report != report {
		t.Fatal("API failure lost the pending batch")
	}
}

func TestLateUserIdentityPreservesOriginalBucket(t *testing.T) {
	s := newTrafficSampler()
	reset := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	s.ingest("api", clashTrafficResponse{}, reset.Add(-2*time.Minute))
	s.observeConnection("c", "in", "", [2]uint64{100, 200}, false, reset.Add(-time.Minute))
	s.observeConnection("c", "in", "alice", [2]uint64{120, 240}, false, reset.Add(time.Minute))
	report := s.accountingSnapshot().Report
	var before, after uint64
	for _, b := range report.Buckets {
		for _, u := range b.Users {
			if b.Time < reset.Unix() {
				before += u.Upload
			} else {
				after += u.Upload
			}
		}
	}
	if before != 100 || after != 20 {
		t.Fatalf("user usage before/after reset = %d/%d", before, after)
	}
}
