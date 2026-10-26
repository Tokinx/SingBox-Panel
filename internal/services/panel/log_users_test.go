package panel

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/hann0w0/singbox-panel/internal/domain/model"
	"github.com/hann0w0/singbox-panel/internal/domain/protocol"
	"github.com/hann0w0/singbox-panel/internal/domain/singbox"
)

func newLogUserFixture(t *testing.T) userTrafficFixture {
	t.Helper()
	f := newUserTrafficFixture(t, model.ConfigModeManaged)
	if _, err := proxyUsersForInbound(f.db, &f.multi, singbox.InboundSettings{MultiUser: true}); err != nil {
		t.Fatal(err)
	}
	return f
}

func userLogLine(name string) string {
	return "Oct 26 13:05:12 node sing-box[123]: +0800 2026-10-26 13:05:12 INFO [42 0ms] inbound/anytls[AnyTLS]: [" + name + "] inbound connection to example.com:443"
}

func TestLogUserResolverRestoresNamesWithoutChangingOtherLogText(t *testing.T) {
	f := newLogUserFixture(t)
	identity := proxyTrafficName(&f.alice)
	const renamed = "Alice \u4e3b\u8d26\u6237"
	// Past logs still belong to the same account after renaming or revocation.
	if err := f.db.Model(&f.alice).Updates(map[string]any{
		"email": renamed, "enabled": false, "server_ids": "[]", "inbound_ids": "[]",
	}).Error; err != nil {
		t.Fatal(err)
	}
	other := proxyTrafficName(&f.namedUser)
	unknown := proxyTrafficName(&model.User{ID: 999, ProxyToken: "unknown"})
	text := strings.Join([]string{
		userLogLine(identity), userLogLine(other), userLogLine(identity),
		userLogLine(unknown), userLogLine("legacy-user"), userLogLine(lockoutProxyUserName),
		userLogLine(identity + "-not-an-identity"),
		"INFO outbound/direct[direct]: outbound connection to example.com:443",
		"ERROR network unreachable", "",
	}, "\n")
	want := strings.ReplaceAll(text, "["+identity+"]", "["+renamed+"]")
	want = strings.ReplaceAll(want, "["+other+"]", "["+f.namedUser.Email+"]")
	r := logUserResolver{db: f.db, serverID: f.server.ID}
	if got := r.resolve(context.Background(), text); got != want {
		t.Fatalf("resolved logs:\n%s\nwant:\n%s", got, want)
	}
	foreign := logUserResolver{db: f.db, serverID: f.server.ID + 1}
	if got := foreign.resolve(context.Background(), text); got != text {
		t.Fatalf("another server's identities were resolved: %s", got)
	}
}

func TestLogUserResolverNeverRelabelsDeletedOrRecreatedAccounts(t *testing.T) {
	f := newLogUserFixture(t)
	old := proxyTrafficName(&f.alice)
	if err := f.db.Delete(&f.alice).Error; err != nil {
		t.Fatal(err)
	}
	r := logUserResolver{db: f.db, serverID: f.server.ID}
	if got := r.resolve(context.Background(), userLogLine(old)); got != userLogLine(old) {
		t.Fatalf("deleted user was relabelled: %s", got)
	}
	reused := model.User{ID: f.alice.ID, Email: "new-owner", Enabled: true, SubToken: "new-sub", ProxyToken: "new-proxy", ServerIDs: []uint{f.server.ID}}
	mustCreate(t, f.db, &reused)
	if _, err := proxyUsersForInbound(f.db, &f.multi, singbox.InboundSettings{MultiUser: true}); err != nil {
		t.Fatal(err)
	}
	r.refreshAt = time.Time{}
	text := userLogLine(old) + "\n" + userLogLine(proxyTrafficName(&reused))
	want := userLogLine(old) + "\n" + userLogLine(reused.Email)
	if got := r.resolve(context.Background(), text); got != want {
		t.Fatalf("recreated account inherited another owner's logs:\n%s", got)
	}
}

func TestLogUserResolverRefreshesSnapshotAndRecoversFromLookupFailure(t *testing.T) {
	f := newLogUserFixture(t)
	originalName := f.alice.Email
	text := userLogLine(proxyTrafficName(&f.alice))
	r := logUserResolver{db: f.db, serverID: f.server.ID}
	if got := r.resolve(context.Background(), text); got != userLogLine(f.alice.Email) {
		t.Fatalf("initial lookup: %s", got)
	}
	if err := f.db.Model(&f.alice).Update("email", "renamed").Error; err != nil {
		t.Fatal(err)
	}
	if got := r.resolve(context.Background(), text); got != userLogLine(originalName) {
		t.Fatalf("snapshot was not cached: %s", got)
	}
	r.refreshAt = time.Time{}
	if got := r.resolve(context.Background(), text); got != userLogLine("renamed") {
		t.Fatalf("snapshot did not refresh the username: %s", got)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	r.refreshAt = time.Time{}
	if got := r.resolve(ctx, text); got != text {
		t.Fatalf("failed lookup changed or hid the original log: %s", got)
	}
	r.refreshAt = time.Time{}
	if got := r.resolve(context.Background(), text); got != userLogLine("renamed") {
		t.Fatalf("lookup did not recover: %s", got)
	}
}

func TestLogUserResolverPreservesLogStructure(t *testing.T) {
	f := newLogUserFixture(t)
	if err := f.db.Model(&f.alice).Update("email", "legacy[owner]\nname").Error; err != nil {
		t.Fatal(err)
	}
	r := logUserResolver{db: f.db, serverID: f.server.ID}
	if got, want := r.resolve(context.Background(), userLogLine(proxyTrafficName(&f.alice))), userLogLine(`legacy\u005bowner\u005d\nname`); got != want {
		t.Fatalf("username broke the log structure: %s, want %s", got, want)
	}
	// Ordinary logs and non-log SSE events must not even need a database lookup.
	empty := logUserResolver{}
	if got := empty.resolve(context.Background(), "INFO [legacy-user] connected"); got != "INFO [legacy-user] connected" {
		t.Fatalf("ordinary log changed: %s", got)
	}
	progress := liveEvent{Kind: "progress", TS: 123, Data: protocol.LogEvt{Msg: userLogLine(proxyTrafficName(&f.alice))}}
	if got := empty.event(context.Background(), progress); got != progress {
		t.Fatalf("non-log SSE event changed: %+v", got)
	}
	event := progress
	event.Kind = "log"
	resolved := r.event(context.Background(), event)
	if resolved.Kind != event.Kind || resolved.TS != event.TS || event.Data != progress.Data || resolved.Data == event.Data {
		t.Fatalf("SSE resolution mutated the original event or its metadata: %+v, %+v", event, resolved)
	}
}

func TestServerLogsReturnsResolvedUsernames(t *testing.T) {
	gin.SetMode(gin.TestMode)
	f := newLogUserFixture(t)
	text := userLogLine(proxyTrafficName(&f.alice)) + "\n" + userLogLine("legacy-user")
	hub := NewHub(f.db)
	conn := &agentConn{serverID: f.server.ID, send: make(chan protocol.Envelope, 1), done: make(chan struct{}), pending: make(map[string]chan protocol.CommandResultEvt)}
	hub.conns[f.server.ID] = conn
	go func() {
		env := <-conn.send
		conn.mu.Lock()
		result := conn.pending[env.ID]
		conn.mu.Unlock()
		data, _ := json.Marshal(protocol.LogsData{Text: text})
		result <- protocol.CommandResultEvt{ID: env.ID, OK: true, Data: data}
	}()
	a := &App{db: f.db, hub: hub}
	router := gin.New()
	router.GET("/servers/:id/logs", a.serverLogs)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	req := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/servers/%d/logs", f.server.ID), nil).WithContext(ctx)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, req)
	if response.Code != http.StatusOK {
		t.Fatalf("history response: %d %s", response.Code, response.Body.String())
	}
	var data protocol.LogsData
	if err := json.Unmarshal(response.Body.Bytes(), &data); err != nil {
		t.Fatal(err)
	}
	if want := userLogLine(f.alice.Email) + "\n" + userLogLine("legacy-user"); data.Text != want {
		t.Fatalf("history still exposes an internal identity: %s", data.Text)
	}
}

func TestTrafficSSEReturnsResolvedLogUsernames(t *testing.T) {
	gin.SetMode(gin.TestMode)
	f := newLogUserFixture(t)
	f.namedUser.Role = model.RoleAdmin
	if err := f.db.Model(&f.namedUser).Update("role", f.namedUser.Role).Error; err != nil {
		t.Fatal(err)
	}
	a := &App{db: f.db, auth: NewAuth("0123456789abcdef0123456789abcdef", f.db), hub: NewHub(f.db)}
	router := gin.New()
	router.GET("/servers/:id/live", a.auth.Middleware(), AdminOnly(), a.handleTrafficSSE)
	server := httptest.NewServer(router)
	defer server.Close()
	token, err := a.auth.Issue(&f.namedUser, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf("%s/servers/%d/live", server.URL, f.server.ID), nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	response, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("SSE status: %d", response.StatusCode)
	}
	a.hub.live.publishLog(f.server.ID, protocol.LogEvt{Level: "INFO", Msg: userLogLine(proxyTrafficName(&f.alice))})
	line, err := bufio.NewReader(response.Body).ReadString('\n')
	if err != nil || !strings.HasPrefix(line, "data: ") {
		t.Fatalf("log event: %q, %v", line, err)
	}
	var event struct {
		Kind string          `json:"kind"`
		Data protocol.LogEvt `json:"data"`
	}
	if err := json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &event); err != nil {
		t.Fatal(err)
	}
	if event.Kind != "log" || event.Data.Level != "INFO" || event.Data.Msg != userLogLine(f.alice.Email) {
		t.Fatalf("live log still exposes an internal identity: %+v", event)
	}
}
