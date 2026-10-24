package agent

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/hann0w0/singbox-panel/internal/domain/protocol"
)

func TestCountProcSocketsAcceptsSequenceColon(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tcp")
	contents := strings.Join([]string{
		"  sl  local_address rem_address   st tx_queue rx_queue tr tm->when retrnsmt   uid  timeout inode",
		"   0: 0100007F:1F90 0200007F:C350 01 00000000:00000000 00:00000000 00000000 1000 0 12345 1",
		"   1: 00000000:1F91 00000000:0000 0A 00000000:00000000 00:00000000 00000000 1000 0 12346 1",
	}, "\n")
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := countProcSockets(path, false); got != 2 {
		t.Fatalf("all sockets = %d, want 2", got)
	}
	if got := countProcSockets(path, true); got != 1 {
		t.Fatalf("non-listening sockets = %d, want 1", got)
	}
}

func TestCappedCommandOutputStopsGrowing(t *testing.T) {
	var output cappedCommandOutput
	payload := []byte(strings.Repeat("x", maxCommandOutputBytes+1024))
	n, err := output.Write(payload)
	if err != nil || n != len(payload) {
		t.Fatalf("Write = %d, %v", n, err)
	}
	got := output.String()
	if !strings.HasSuffix(got, "[command output truncated]") {
		t.Fatal("truncation marker missing")
	}
	if len(got) > maxCommandOutputBytes+64 {
		t.Fatalf("buffer grew past cap: %d", len(got))
	}
}

func TestTrafficSnapshotRequiresAcknowledgementBeforeDraining(t *testing.T) {
	sampler := newTrafficSampler()
	sampler.available = true
	sampler.haveSample = true
	sampler.lastSample = time.Now()
	sampler.pendingPorts["vless-in"] = protocol.PortTrafficSnapshot{
		Inbound: "vless-in", Upload: 120, Download: 80, UploadRate: 50, DownloadRate: 40,
	}

	first := sampler.snapshot()
	second := sampler.snapshot()
	if len(first.Ports) != 1 || len(second.Ports) != 1 || second.Ports[0].Upload != 120 {
		t.Fatalf("unacknowledged traffic was drained: first=%+v second=%+v", first.Ports, second.Ports)
	}

	sampler.mu.Lock()
	pending := sampler.pendingPorts["vless-in"]
	pending.Upload += 30
	pending.Download += 20
	pending.UploadRate = 75
	sampler.pendingPorts["vless-in"] = pending
	sampler.mu.Unlock()
	sampler.acknowledge(first)

	remaining := sampler.snapshot()
	if len(remaining.Ports) != 1 || remaining.Ports[0].Upload != 30 || remaining.Ports[0].Download != 20 || remaining.Ports[0].UploadRate != 75 {
		t.Fatalf("acknowledgement removed newer traffic: %+v", remaining.Ports)
	}
	sampler.acknowledge(remaining)
	if got := sampler.snapshot(); len(got.Ports) != 0 {
		t.Fatalf("acknowledged traffic still pending: %+v", got.Ports)
	}
}

func TestTrafficSummaryDoesNotCarryPortDeltas(t *testing.T) {
	sampler := newTrafficSampler()
	sampler.available = true
	sampler.haveSample = true
	sampler.lastSample = time.Now()
	sampler.pendingPorts["in"] = protocol.PortTrafficSnapshot{Inbound: "in", Upload: 10}
	if summary := sampler.summarySnapshot(); summary == nil || len(summary.Ports) != 0 {
		t.Fatalf("summary snapshot = %+v", summary)
	}
	if full := sampler.snapshot(); len(full.Ports) != 1 {
		t.Fatal("summary snapshot consumed pending port traffic")
	}
}

func TestTrafficReportsWaitForWriteCompletionAndPreserveNewSamples(t *testing.T) {
	sampler := newTrafficSampler()
	sampler.available, sampler.haveSample = true, true
	sampler.lastSample = time.Now()
	sampler.pendingPorts["in"] = protocol.PortTrafficSnapshot{Inbound: "in", Upload: 100}
	userKey := userTrafficKey{inbound: "in", user: "alice"}
	sampler.pendingUsers[userKey] = protocol.UserTrafficSnapshot{Inbound: "in", User: "alice", Upload: 100}
	client := NewClient("https://panel.example.com", "token", false)
	client.activeSession.Store(1)
	a := &Agent{client: client, traffic: sampler}
	nextReport := func() sessionEnvelope {
		t.Helper()
		select {
		case report := <-client.send:
			return report
		default:
			t.Fatal("traffic report was not queued")
			return sessionEnvelope{}
		}
	}

	a.reportTraffic()
	first := nextReport()
	if snapshot := sampler.snapshot(); snapshot.Ports[0].Upload != 100 || snapshot.Users[0].Upload != 100 {
		t.Fatalf("enqueue consumed unsent deltas: %+v", snapshot)
	}
	a.reportTraffic()
	if len(client.send) != 0 {
		t.Fatal("a second report duplicated the in-flight deltas")
	}
	first.complete(false)
	a.reportTraffic()
	retry := nextReport()
	var event protocol.TrafficEvt
	if err := json.Unmarshal(retry.env.Payload, &event); err != nil {
		t.Fatal(err)
	}
	if event.Traffic.Ports[0].Upload != 100 || event.Traffic.Users[0].Upload != 100 {
		t.Fatalf("failed write lost pending deltas: %+v", event.Traffic)
	}
	sampler.mu.Lock()
	sampler.pendingPorts["in"] = protocol.PortTrafficSnapshot{Inbound: "in", Upload: 150}
	sampler.pendingUsers[userKey] = protocol.UserTrafficSnapshot{Inbound: "in", User: "alice", Upload: 150}
	sampler.mu.Unlock()
	retry.complete(true)
	if snapshot := sampler.snapshot(); snapshot.Ports[0].Upload != 50 || snapshot.Users[0].Upload != 50 {
		t.Fatalf("write receipt consumed newer samples: %+v", snapshot)
	}

	client.activeSession.Store(0)
	a.reportTraffic()
	if a.trafficReportInFlight.Load() {
		t.Fatal("offline enqueue left the report permanently in flight")
	}
}

func trafficConn(id, inbound string, up, down uint64) clashConnection {
	var c clashConnection
	c.ID = id
	c.Upload = up
	c.Download = down
	c.Metadata.Type = "vless/" + inbound
	return c
}

func TestTrafficSamplerCountsNewAndShortLivedConnections(t *testing.T) {
	sampler := newTrafficSampler()
	start := time.Unix(1_700_000_000, 0)
	// Baseline: a connection that existed before the Agent started is not
	// counted (its history predates the accounting window).
	sampler.ingest("e", clashTrafficResponse{UploadTotal: 100, DownloadTotal: 1000, Connections: []clashConnection{
		trafficConn("old", "in", 100, 1000),
	}}, start)
	// One second later: the old connection grew, and a new one appeared with
	// bytes already transferred. Both must be attributed in full.
	sampler.ingest("e", clashTrafficResponse{UploadTotal: 160, DownloadTotal: 1600, Connections: []clashConnection{
		trafficConn("old", "in", 110, 1100),
		trafficConn("new", "in", 50, 500),
	}}, start.Add(time.Second))
	snap := sampler.snapshot()
	if len(snap.Ports) != 1 {
		t.Fatalf("ports = %+v", snap.Ports)
	}
	port := snap.Ports[0]
	if port.Upload != 60 || port.Download != 600 {
		t.Fatalf("port delta = %d/%d, want 60/600", port.Upload, port.Download)
	}
	// Rates are the port sum for that second, not the fastest connection.
	if port.UploadRate != 60 || port.DownloadRate != 600 {
		t.Fatalf("port rate = %d/%d, want 60/600", port.UploadRate, port.DownloadRate)
	}
	if snap.PeakUploadRate != 60 || snap.PeakDownloadRate != 600 {
		t.Fatalf("peak = %d/%d", snap.PeakUploadRate, snap.PeakDownloadRate)
	}
}

func TestTrafficSamplerPeakSurvivesUntilAcknowledged(t *testing.T) {
	sampler := newTrafficSampler()
	start := time.Unix(1_700_000_000, 0)
	sampler.ingest("e", clashTrafficResponse{UploadTotal: 0, DownloadTotal: 0}, start)
	sampler.ingest("e", clashTrafficResponse{UploadTotal: 0, DownloadTotal: 5000}, start.Add(time.Second))
	sampler.ingest("e", clashTrafficResponse{UploadTotal: 0, DownloadTotal: 5100}, start.Add(2*time.Second))
	snap := sampler.snapshot()
	if snap.DownloadRate != 100 || snap.PeakDownloadRate != 5000 {
		t.Fatalf("instant/peak = %d/%d, want 100/5000", snap.DownloadRate, snap.PeakDownloadRate)
	}
	if summary := sampler.summarySnapshot(); summary.PeakDownloadRate != 0 {
		t.Fatal("heartbeat summary must not carry or consume the peak")
	}
	sampler.acknowledge(snap)
	if got := sampler.snapshot().PeakDownloadRate; got != 0 {
		t.Fatalf("peak after ack = %d", got)
	}
}
