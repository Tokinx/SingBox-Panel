package panel

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

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
	if len(backlog) != 2 || backlog[1].Kind != event.Kind || backlog[1].TS != event.TS {
		t.Fatal("reconnect lost unavailable state")
	}
	var replay struct {
		Data  *protocol.TrafficSnapshot `json:"data"`
		AgeMS int64                     `json:"age_ms"`
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(strings.TrimPrefix(backlog[1].SSE(), "data: "))), &replay); err != nil {
		t.Fatal(err)
	}
	if replay.Data != nil || replay.AgeMS < 0 {
		t.Fatal("unavailable replay is invalid")
	}
}

func TestTrafficSSEReportsBacklogAgeAtEncodingTime(t *testing.T) {
	old := time.Now().Add(-time.Minute).UnixMilli()
	live := newLiveHub()
	live.recentTraffic[1] = []liveEvent{{Kind: "traffic", TS: old, Data: &protocol.TrafficSnapshot{DownloadRate: 1024}}}
	sub, backlog := live.subscribe(1)
	defer live.unsubscribe(1, sub)
	before := time.Now().UnixMilli()
	encoded := backlog[0].SSE()
	after := time.Now().UnixMilli()
	var replay struct {
		Kind  string                    `json:"kind"`
		TS    int64                     `json:"ts"`
		AgeMS int64                     `json:"age_ms"`
		Data  *protocol.TrafficSnapshot `json:"data"`
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(strings.TrimPrefix(encoded, "data: "))), &replay); err != nil {
		t.Fatal(err)
	}
	if replay.Kind != "traffic" || replay.TS != old || replay.Data == nil || replay.Data.DownloadRate != 1024 {
		t.Fatal("backlog payload changed")
	}
	if replay.AgeMS < before-old || replay.AgeMS > after-old {
		t.Fatalf("backlog age = %d", replay.AgeMS)
	}

	future := liveEvent{Kind: "traffic", TS: time.Now().Add(time.Hour).UnixMilli()}
	if err := json.Unmarshal([]byte(strings.TrimSpace(strings.TrimPrefix(future.SSE(), "data: "))), &replay); err != nil {
		t.Fatal(err)
	}
	if replay.AgeMS != 0 {
		t.Fatalf("future event age = %d", replay.AgeMS)
	}
}
