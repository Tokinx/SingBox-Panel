package panel

import (
	"testing"
	"time"

	"github.com/tokinx/singbox-panel/internal/domain/model"
)

func TestOrchestratorCloseStopsRetriesAndReleasesDB(t *testing.T) {
	db := testDB(t)
	srv := model.Server{
		Name:       "close-test",
		AgentToken: "close-test-token",
		ConfigMode: model.ConfigModeManaged,
	}
	if err := db.Create(&srv).Error; err != nil {
		t.Fatal(err)
	}
	// Missing TLS makes BuildServerConfig fail so PushConfigAsync enters its
	// multi-second retry sleep unless Close cancels it promptly.
	ib := model.Inbound{
		ServerID:   srv.ID,
		Tag:        "trojan-invalid",
		Type:       model.InboundTrojan,
		ListenPort: 443,
		Enabled:    true,
	}
	if err := db.Create(&ib).Error; err != nil {
		t.Fatal(err)
	}

	orch := NewOrchestrator(db, NewHub(db))
	orch.PushConfigAsync(srv.ID)

	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		orch.asyncMu.Lock()
		running := orch.asyncRunning[srv.ID]
		orch.asyncMu.Unlock()
		if running {
			break
		}
		time.Sleep(time.Millisecond)
	}

	start := time.Now()
	orch.Close()
	if elapsed := time.Since(start); elapsed >= asyncPushRetryDelay {
		t.Fatalf("Close waited %v, expected prompt cancellation below %v", elapsed, asyncPushRetryDelay)
	}

	orch.PushConfigAsync(srv.ID)
	orch.asyncMu.Lock()
	runningAfterClose := len(orch.asyncRunning)
	pendingAfterClose := len(orch.asyncPending)
	orch.asyncMu.Unlock()
	if runningAfterClose != 0 || pendingAfterClose != 0 {
		t.Fatalf("async maps not empty after Close: running=%d pending=%d", runningAfterClose, pendingAfterClose)
	}
}
