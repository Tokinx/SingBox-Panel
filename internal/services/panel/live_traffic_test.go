package panel

import (
	"encoding/json"
	"testing"

	"github.com/hann0w0/singbox-panel/internal/domain/protocol"
)

func TestUnavailableTrafficIsBroadcastAndReplayed(t *testing.T) {
	live := newLiveHub()
	sub, _ := live.subscribe(1)
	defer live.unsubscribe(1, sub)
	live.publishTraffic(1, &protocol.TrafficSnapshot{DownloadRate: 1024})
	live.publishTraffic(1, nil)
	<-sub.ch
	event := <-sub.ch
	encoded, err := json.Marshal(event)
	if err != nil {
		t.Fatal(err)
	}
	var payload struct {
		Data *protocol.TrafficSnapshot `json:"data"`
	}
	if err := json.Unmarshal(encoded, &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Data != nil {
		t.Fatal("unavailable source still has a live rate")
	}
	reconnected, backlog := live.subscribe(1)
	defer live.unsubscribe(1, reconnected)
	if len(backlog) != 2 || backlog[1].SSE() != event.SSE() {
		t.Fatal("reconnect lost unavailable state")
	}
}
