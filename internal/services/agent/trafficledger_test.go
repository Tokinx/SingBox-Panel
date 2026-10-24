package agent

import (
	"testing"
	"time"
)

func TestUnknownUserAttributionKeepsOnly24UTCHours(t *testing.T) {
	s := newTrafficSampler()
	start := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	s.ingest("api", clashTrafficResponse{}, start.Add(-time.Minute))
	var total [2]uint64
	for hour := 0; hour < 48; hour++ {
		// Two observations in one hour must share one deferred attribution row.
		for _, minute := range []int{0, 30} {
			total[0] += 10
			total[1] += 20
			s.observeConnection("long", "in", "", total, false, start.Add(time.Duration(hour)*time.Hour+time.Duration(minute)*time.Minute))
		}
		if n := len(s.ledger["long"].unassigned); n > maxUnassignedTrafficHours {
			t.Fatalf("deferred identity history grew to %d hours", n)
		}
	}
	entry := s.ledger["long"]
	if len(entry.unassigned) != 24 || entry.userCounted != [2]uint64{480, 960} {
		t.Fatalf("retained=%d excluded=%v", len(entry.unassigned), entry.userCounted)
	}
	s.closeConnection("long")
	if len(s.tombstones["long"].unassigned) != 24 {
		t.Fatal("closed connection lost its bounded attribution history")
	}
	s.observeConnection("long", "in", "alice", total, false, start.Add(47*time.Hour+45*time.Minute))
	if user := s.pendingUser("in", "alice"); user.Upload != 480 || user.Download != 960 {
		t.Fatalf("late identity reattributed excluded usage: %+v", user)
	}
	for _, bucket := range s.buckets {
		for _, user := range bucket.users {
			if user.User == "alice" && bucket.Time < start.Add(24*time.Hour).Unix() {
				t.Fatal("expired identity history was retained")
			}
		}
	}
}

func TestExpiredDeferredUserUsageIsNotMovedToDiscoveryTime(t *testing.T) {
	s := newTrafficSampler()
	start := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	s.ingest("api", clashTrafficResponse{}, start.Add(-time.Minute))
	s.observeConnection("closed", "in", "", [2]uint64{100, 200}, false, start)
	s.closeConnection("closed")
	s.observeConnection("closed", "in", "alice", [2]uint64{105, 210}, false, start.Add(25*time.Hour))
	if user := s.pendingUser("in", "alice"); user.Upload != 5 || user.Download != 10 {
		t.Fatalf("expired bytes were attributed at discovery: %+v", user)
	}
	s.observeConnection("closed", "in", "alice", [2]uint64{105, 210}, false, start.Add(26*time.Hour))
	if user := s.pendingUser("in", "alice"); user.Upload != 5 || user.Download != 10 {
		t.Fatalf("replay credited usage twice: %+v", user)
	}
}

func TestDeferredUserWindowHasHardBoundAfterClockRollback(t *testing.T) {
	start := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	entry := &ledgerEntry{unassigned: make(map[int64][2]uint64)}
	for hour := 0; hour < 30; hour++ {
		entry.unassigned[start.Add(time.Duration(hour)*time.Hour).Unix()] = [2]uint64{1, 2}
	}
	entry.pruneUnassigned(start)
	if len(entry.unassigned) != 24 || entry.userCounted != [2]uint64{6, 12} {
		t.Fatalf("clock rollback broke bound: hours=%d excluded=%v", len(entry.unassigned), entry.userCounted)
	}
	entry.pruneUnassigned(start)
	if entry.userCounted != [2]uint64{6, 12} {
		t.Fatal("pruning credited an exclusion more than once")
	}
}
