package agent

import (
	"testing"
	"time"

	"github.com/hann0w0/singbox-panel/internal/domain/protocol"
)

func reportUploadTotals(report *protocol.TrafficReport) (node, ports, users uint64) {
	if report == nil {
		return
	}
	for _, bucket := range report.Buckets {
		node += bucket.Upload
		for _, port := range bucket.Ports {
			ports += port.Upload
		}
		for _, user := range bucket.Users {
			users += user.Upload
		}
	}
	return
}

func TestFirstNodeSampleAlsoBaselinesNewlyCreatedConnections(t *testing.T) {
	s := newTrafficSampler()
	start := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	s.startedAt = start
	created := start.Add(time.Second)
	s.ingest("api", clashTrafficResponse{UploadTotal: 100, Connections: []clashConnection{
		clashConn("new", "in", 100, 0, created),
	}}, start.Add(2*time.Second))
	initial := s.accountingSnapshot().Report
	if node, ports, users := reportUploadTotals(initial); node != 0 || ports != 0 || users != 0 {
		t.Fatalf("first baseline charged node/ports/users = %d/%d/%d", node, ports, users)
	}
	s.acknowledgeReport(protocol.TrafficAck{Session: initial.Session, Sequence: initial.Sequence})
	s.observeConnection("new", "in", "alice", [2]uint64{100, 0}, false, start.Add(3*time.Second))
	s.ingest("api", clashTrafficResponse{UploadTotal: 140, Connections: []clashConnection{
		clashConn("new", "in", 140, 0, created),
	}}, start.Add(4*time.Second))
	if node, ports, users := reportUploadTotals(s.accountingSnapshot().Report); node != 40 || ports != 40 || users != 40 {
		t.Fatalf("post-baseline usage = %d/%d/%d", node, ports, users)
	}
}

func TestStreamBeforeClashContinuouslyAdvancesConnectionBaseline(t *testing.T) {
	s := newTrafficSampler()
	start := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	s.startedAt = start
	session := newStreamSession(start)
	s.applyStreamEvents(session, connectionEvents{Events: []connectionEvent{{
		Type: connectionEventNew, ID: "active", Connection: &streamConnection{
			ID: "active", Inbound: "in", User: "alice", CreatedAt: start.Add(time.Second).UnixMilli(), UplinkTotal: 10,
		},
	}}}, start.Add(time.Second))
	s.applyStreamEvents(session, connectionEvents{Events: []connectionEvent{
		{Type: connectionEventUpdate, ID: "active", UplinkDelta: 30},
	}}, start.Add(2*time.Second))
	if snap := s.accountingSnapshot(); snap.Report != nil {
		t.Fatal("stream bytes were reported before the first node baseline")
	}
	s.ingest("api", clashTrafficResponse{UploadTotal: 100, Connections: []clashConnection{
		clashConn("active", "in", 100, 0, start.Add(time.Second)),
	}}, start.Add(3*time.Second))
	entry := s.ledger["active"]
	if entry.baseline[0] != 100 || entry.counted[0] != 100 || len(entry.unassigned) != 0 {
		t.Fatalf("existing stream entry was not baselined through Clash: %+v", entry)
	}
	// The stream's own counter may lag the poll. The ledger must count only
	// the new delta once either source finally passes the shared baseline.
	s.applyStreamEvents(session, connectionEvents{Events: []connectionEvent{
		{Type: connectionEventUpdate, ID: "active", UplinkDelta: 100},
	}}, start.Add(4*time.Second))
	s.ingest("api", clashTrafficResponse{UploadTotal: 140, Connections: []clashConnection{
		clashConn("active", "in", 140, 0, start.Add(time.Second)),
	}}, start.Add(5*time.Second))
	if node, ports, users := reportUploadTotals(s.accountingSnapshot().Report); node != 40 || ports != 40 || users != 40 {
		t.Fatalf("stream-first startup charged old bytes: %d/%d/%d", node, ports, users)
	}
}

func TestClosedConnectionReplaysRespectFirstNodeBaseline(t *testing.T) {
	s := newTrafficSampler()
	start := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	s.startedAt = start
	cutoff := start.Add(5 * time.Second)
	s.ingest("api", clashTrafficResponse{UploadTotal: 500}, cutoff)
	session := newStreamSession(cutoff.Add(time.Second))
	for _, typ := range []int{connectionEventNew, connectionEventClosed} {
		id := "old-new"
		if typ == connectionEventClosed {
			id = "old-closed"
		}
		s.applyStreamEvents(session, connectionEvents{Events: []connectionEvent{{
			Type: typ, ID: id, Connection: &streamConnection{
				ID: id, Inbound: "in", User: "alice", CreatedAt: start.Add(time.Second).UnixMilli(),
				ClosedAt: start.Add(2 * time.Second).UnixMilli(), UplinkTotal: 100,
			},
		}}}, cutoff.Add(time.Second))
	}
	if node, ports, users := reportUploadTotals(s.accountingSnapshot().Report); node != 0 || ports != 0 || users != 0 {
		t.Fatalf("pre-baseline closed replays charged %d/%d/%d", node, ports, users)
	}
	s.acknowledgeReport(protocol.TrafficAck{Session: s.report.Session, Sequence: s.report.Sequence})
	// A legitimate post-baseline connection may close before its NEW arrives.
	s.applyStreamEvents(session, connectionEvents{Events: []connectionEvent{{
		Type: connectionEventClosed, ID: "new-closed", Connection: &streamConnection{
			ID: "new-closed", Inbound: "in", User: "alice", CreatedAt: cutoff.Add(time.Second).UnixMilli(),
			ClosedAt: cutoff.Add(2 * time.Second).UnixMilli(), UplinkTotal: 70,
		},
	}}}, cutoff.Add(3*time.Second))
	s.ingest("api", clashTrafficResponse{UploadTotal: 570}, cutoff.Add(4*time.Second))
	if node, ports, users := reportUploadTotals(s.accountingSnapshot().Report); node != 70 || ports != 70 || users != 70 {
		t.Fatalf("new closed connection lost usage: %d/%d/%d", node, ports, users)
	}
}
