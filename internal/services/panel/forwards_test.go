package panel

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"

	"github.com/hann0w0/singbox-panel/internal/domain/model"
	"github.com/hann0w0/singbox-panel/internal/domain/protocol"
	"github.com/hann0w0/singbox-panel/internal/domain/singbox"
)

func portForwardTestRouter(db *gorm.DB) (*App, *gin.Engine) {
	hub := NewHub(db)
	app := &App{
		db:   db,
		hub:  hub,
		orch: NewOrchestrator(db, hub),
	}
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.PUT("/servers/:id", app.updateServer)
	r.DELETE("/servers/:id", app.deleteServer)
	r.POST("/servers/:id/import", app.importRemoteConfig)
	r.PUT("/servers/:id/raw", app.applyRawConfig)
	r.GET("/servers/:id/traffic", app.serverTraffic)
	r.POST("/servers/:id/inbounds", app.createInbound)
	r.PUT("/servers/:id/inbounds/:inboundID", app.updateInbound)
	r.DELETE("/servers/:id/inbounds/:inboundID", app.deleteInbound)
	r.PUT("/servers/:id/outbounds/:outboundID", app.updateOutbound)
	r.DELETE("/servers/:id/outbounds/:outboundID", app.deleteOutbound)
	r.DELETE("/custom-nodes/:id", app.deleteCustomNode)
	r.GET("/servers/:id/forwards", app.listForwards)
	r.POST("/servers/:id/forwards", app.createForward)
	r.PUT("/servers/:id/forwards/:forwardID", app.updateForward)
	r.DELETE("/servers/:id/forwards/:forwardID", app.deleteForward)
	return app, r
}

func checkSingBoxBinary(t *testing.T, raw []byte) {
	t.Helper()
	bin := os.Getenv("SING_BOX_BINARY")
	if bin == "" {
		if path, err := exec.LookPath("sing-box"); err == nil {
			bin = path
		}
	}
	if bin == "" {
		t.Skip("sing-box binary unavailable in environment")
	}
	cfgPath := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(cfgPath, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command(bin, "check", "-c", cfgPath).CombinedOutput()
	if err != nil {
		t.Fatalf("sing-box check failed: %v\noutput:\n%s\nconfig:\n%s", err, string(out), string(raw))
	}
}

func setupForwardClusterFixture(t *testing.T) (*gorm.DB, *App, *gin.Engine, model.Server, model.Server, model.Server, model.Inbound, model.Inbound, model.Inbound) {
	t.Helper()
	db := testDB(t)
	app, router := portForwardTestRouter(db)

	relay := model.Server{Name: "relay-hk", AgentToken: "tok-relay", ConfigMode: model.ConfigModeManaged, Address: "relay.example.com"}
	target := model.Server{Name: "target-jp", AgentToken: "tok-target", ConfigMode: model.ConfigModeManaged, Address: "jp.example.com"}
	landing := model.Server{Name: "landing-us", AgentToken: "tok-landing", ConfigMode: model.ConfigModeManaged, Address: "us.example.com"}
	mustCreate(t, db, &relay)
	mustCreate(t, db, &target)
	mustCreate(t, db, &landing)

	relayIb := model.Inbound{
		ServerID: relay.ID, Tag: "relay-socks", Type: model.InboundSocks, ListenPort: 1080, Enabled: true,
		Settings: model.JSONText(`{"single_user":true,"username":"r","password":"p"}`),
	}
	targetVless := model.Inbound{
		ServerID: target.ID, Tag: "jp-socks", Type: model.InboundSocks, ListenPort: 2443, Enabled: true,
		Settings: model.JSONText(`{"single_user":true,"username":"t","password":"p"}`),
	}
	targetHy2 := model.Inbound{
		ServerID: target.ID, Tag: "jp-hy2", Type: model.InboundHysteria2, ListenPort: 8443, Enabled: true,
		Settings: model.JSONText(`{"password":"secret"}`),
	}
	targetDisabled := model.Inbound{
		ServerID: target.ID, Tag: "jp-disabled", Type: model.InboundSocks, ListenPort: 2444, Enabled: false,
		Settings: model.JSONText(`{"single_user":true,"username":"d","password":"p"}`),
	}
	landingSocks := model.Inbound{
		ServerID: landing.ID, Tag: "us-socks", Type: model.InboundSocks, ListenPort: 3080, Enabled: true,
		Settings: model.JSONText(`{"single_user":true,"username":"u","password":"p"}`),
	}
	mustCreate(t, db, &relayIb)
	mustCreate(t, db, &targetVless)
	mustCreate(t, db, &targetHy2)
	mustCreate(t, db, &targetDisabled)
	mustCreate(t, db, &landingSocks)
	_ = landing

	return db, app, router, relay, target, landing, relayIb, targetVless, landingSocks
}

func TestMigrationV34PortForwardsIsReplaySafe(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "mig.db")), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := migratePortForwards(db); err != nil {
		t.Fatalf("first migratePortForwards failed: %v", err)
	}
	if err := migratePortForwards(db); err != nil {
		t.Fatalf("replay migratePortForwards failed: %v", err)
	}
	for _, tbl := range []any{&model.PortForward{}, &model.TrafficForwardRecord{}, &model.TrafficForwardHourly{}, &model.ForwardTrafficIdentity{}} {
		if !db.Migrator().HasTable(tbl) {
			t.Fatalf("missing table for %T", tbl)
		}
	}
}

func TestPortForwardCRUDAndValidation(t *testing.T) {
	db, _, router, relay, target, _, relayIb, targetSocks, landingSocks := setupForwardClusterFixture(t)
	_ = landingSocks

	// Find jp-hy2 & jp-disabled IDs.
	var jpHy2, jpDisabled model.Inbound
	if err := db.Where("server_id = ? AND tag = ?", target.ID, "jp-hy2").First(&jpHy2).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Where("server_id = ? AND tag = ?", target.ID, "jp-disabled").First(&jpDisabled).Error; err != nil {
		t.Fatal(err)
	}

	relayBase := fmt.Sprintf("/servers/%d/forwards", relay.ID)

	// 1. Create custom forward (default tag + dual stack network "").
	res := serveJSONRequest(t, router, http.MethodPost, relayBase, `{
		"listen_port": 9001,
		"target_type": "custom",
		"target_address": "1.2.3.4",
		"target_port": 443,
		"outbound": "direct",
		"remark": "custom route"
	}`)
	if res.Code != http.StatusOK {
		t.Fatalf("create custom forward status = %d: %s", res.Code, res.Body.String())
	}
	var customResp struct {
		Forward model.PortForward `json:"forward"`
	}
	if err := json.Unmarshal(res.Body.Bytes(), &customResp); err != nil {
		t.Fatal(err)
	}
	if customResp.Forward.Tag != "fwd-9001" || customResp.Forward.ResolvedAddress != "1.2.3.4" || customResp.Forward.ResolvedPort != 443 {
		t.Fatalf("unexpected custom forward response: %+v", customResp.Forward)
	}

	// 2. Create inbound target forward with proxy outbound.
	landingTag := managedRoutingNodeTag(landingSocks.ID)
	res = serveJSONRequest(t, router, http.MethodPost, relayBase, fmt.Sprintf(`{
		"tag": "fwd-to-jp",
		"listen_port": 9002,
		"network": "tcp",
		"target_type": "inbound",
		"target_inbound_id": %d,
		"outbound": %q
	}`, targetSocks.ID, landingTag))
	if res.Code != http.StatusOK {
		t.Fatalf("create inbound forward status = %d: %s", res.Code, res.Body.String())
	}
	var ibFwdResp struct {
		Forward model.PortForward `json:"forward"`
	}
	if err := json.Unmarshal(res.Body.Bytes(), &ibFwdResp); err != nil {
		t.Fatal(err)
	}
	if ibFwdResp.Forward.TargetServerID != target.ID || ibFwdResp.Forward.TargetServerName != "target-jp" ||
		ibFwdResp.Forward.TargetInboundTag != "jp-socks" || ibFwdResp.Forward.ResolvedAddress != "jp.example.com" ||
		ibFwdResp.Forward.ResolvedPort != 2443 {
		t.Fatalf("unexpected hydrated inbound forward: %+v", ibFwdResp.Forward)
	}

	// 3. List forwards.
	res = serveJSONRequest(t, router, http.MethodGet, relayBase, "")
	if res.Code != http.StatusOK {
		t.Fatalf("list forwards status = %d: %s", res.Code, res.Body.String())
	}
	var listResp struct {
		Forwards []model.PortForward `json:"forwards"`
	}
	if err := json.Unmarshal(res.Body.Bytes(), &listResp); err != nil || len(listResp.Forwards) != 2 {
		t.Fatalf("unexpected list response: %s", res.Body.String())
	}

	// 4. Reject invalid / conflicting requests (400).
	badCases := []struct {
		name string
		body string
	}{
		{"port conflicts with inbound", fmt.Sprintf(`{"tag":"c1","listen_port":%d,"target_type":"custom","target_address":"1.2.3.4","target_port":443}`, relayIb.ListenPort)},
		{"tag conflicts with inbound", fmt.Sprintf(`{"tag":%q,"listen_port":9010,"target_type":"custom","target_address":"1.2.3.4","target_port":443}`, relayIb.Tag)},
		{"reserved stats port 29091", `{"tag":"c2","listen_port":29091,"target_type":"custom","target_address":"1.2.3.4","target_port":443}`},
		{"reserved stats port 29092", `{"tag":"c3","listen_port":29092,"target_type":"custom","target_address":"1.2.3.4","target_port":443}`},
		{"duplicate port on same server", `{"tag":"c4","listen_port":9001,"target_type":"custom","target_address":"1.2.3.4","target_port":443}`},
		{"duplicate tag on same server", `{"tag":"fwd-9001","listen_port":9011,"target_type":"custom","target_address":"1.2.3.4","target_port":443}`},
		{"self server target inbound", fmt.Sprintf(`{"tag":"c5","listen_port":9012,"target_type":"inbound","target_inbound_id":%d}`, relayIb.ID)},
		{"disabled target inbound", fmt.Sprintf(`{"tag":"c6","listen_port":9013,"target_type":"inbound","target_inbound_id":%d}`, jpDisabled.ID)},
		{"quic inbound with tcp-only forward", fmt.Sprintf(`{"tag":"c7","listen_port":9014,"network":"tcp","target_type":"inbound","target_inbound_id":%d}`, jpHy2.ID)},
		{"loopback ipv4 target", `{"tag":"c8","listen_port":9015,"target_type":"custom","target_address":"127.0.0.1","target_port":443}`},
		{"localhost target", `{"tag":"c9","listen_port":9016,"target_type":"custom","target_address":"localhost","target_port":443}`},
		{"metadata target", `{"tag":"c10","listen_port":9017,"target_type":"custom","target_address":"169.254.169.254","target_port":80}`},
		{"ipv4-mapped ipv6 loopback", `{"tag":"c11","listen_port":9018,"target_type":"custom","target_address":"::ffff:127.0.0.1","target_port":443}`},
		{"disallowed outbound block", `{"tag":"c12","listen_port":9019,"target_type":"custom","target_address":"1.2.3.4","target_port":443,"outbound":"block"}`},
		{"self outbound inbound", fmt.Sprintf(`{"tag":"c13","listen_port":9020,"target_type":"custom","target_address":"1.2.3.4","target_port":443,"outbound":%q}`, managedRoutingNodeTag(relayIb.ID))},
	}
	for _, tc := range badCases {
		res := serveJSONRequest(t, router, http.MethodPost, relayBase, tc.body)
		if res.Code != http.StatusBadRequest {
			t.Fatalf("%s: status = %d, want 400: %s", tc.name, res.Code, res.Body.String())
		}
	}

	// 5. Mutual protection: inbound cannot reuse active forward's tag or port, nor deleted forward's historical tag.
	inboundBase := fmt.Sprintf("/servers/%d/inbounds", relay.ID)
	res = serveJSONRequest(t, router, http.MethodPost, inboundBase, `{"type":"socks","tag":"fwd-9001","listen_port":9050,"settings":{"single_user":true,"username":"a","password":"b"}}`)
	if res.Code != http.StatusBadRequest {
		t.Fatalf("creating inbound with forward tag status = %d, want 400: %s", res.Code, res.Body.String())
	}
	res = serveJSONRequest(t, router, http.MethodPost, inboundBase, `{"type":"socks","tag":"ib-conflict-port","listen_port":9001,"settings":{"single_user":true,"username":"a","password":"b"}}`)
	if res.Code != http.StatusBadRequest {
		t.Fatalf("creating inbound on forward port status = %d, want 400: %s", res.Code, res.Body.String())
	}

	// Update and delete custom forward, then verify historical tag cannot be reused by a new inbound or forward.
	res = serveJSONRequest(t, router, http.MethodPut, fmt.Sprintf("%s/%d", relayBase, customResp.Forward.ID), `{"listen_port":9003,"network":"udp"}`)
	if res.Code != http.StatusOK {
		t.Fatalf("update forward status = %d: %s", res.Code, res.Body.String())
	}
	res = serveJSONRequest(t, router, http.MethodDelete, fmt.Sprintf("%s/%d", relayBase, customResp.Forward.ID), "")
	if res.Code != http.StatusOK {
		t.Fatalf("delete forward status = %d: %s", res.Code, res.Body.String())
	}
	res = serveJSONRequest(t, router, http.MethodPost, inboundBase, `{"type":"socks","tag":"fwd-9001","listen_port":9051,"settings":{"single_user":true,"username":"a","password":"b"}}`)
	if res.Code != http.StatusBadRequest {
		t.Fatalf("reusing deleted forward tag for inbound status = %d, want 400: %s", res.Code, res.Body.String())
	}
	res = serveJSONRequest(t, router, http.MethodPost, relayBase, `{"tag":"fwd-9001","listen_port":9052,"target_type":"custom","target_address":"1.2.3.4","target_port":443}`)
	if res.Code != http.StatusBadRequest {
		t.Fatalf("reusing deleted forward tag for another forward status = %d, want 400: %s", res.Code, res.Body.String())
	}
}

func TestPortForwardOrchestratorBuildAndSingBoxCheck(t *testing.T) {
	db, _, router, relay, _, _, _, targetSocks, landingSocks := setupForwardClusterFixture(t)
	app := &App{db: db, orch: NewOrchestrator(db, NewHub(db))}

	node := model.CustomNode{
		Name: "proxy-node", Group: "g", Protocol: "socks", Address: "198.51.100.10", Port: 1080,
		Params: model.JSONText(`{"username":"u","password":"p"}`), Enabled: true, UserEnabled: true,
	}
	mustCreate(t, db, &node)

	mustCreate(t, db, &model.PortForward{
		ServerID: relay.ID, Tag: "fwd-direct", ListenPort: 11001, Network: "tcp",
		TargetType: model.ForwardTargetCustom, TargetAddress: "203.0.113.10", TargetPort: 443,
		Outbound: "direct", Enabled: true,
	})
	mustCreate(t, db, &model.PortForward{
		ServerID: relay.ID, Tag: "fwd-landing", ListenPort: 11002, Network: "",
		TargetType: model.ForwardTargetInbound, TargetServerID: targetSocks.ServerID, TargetInboundID: targetSocks.ID,
		Outbound: managedRoutingNodeTag(landingSocks.ID), Enabled: true,
	})
	mustCreate(t, db, &model.PortForward{
		ServerID: relay.ID, Tag: "fwd-custom-node", ListenPort: 11003, Network: "udp",
		TargetType: model.ForwardTargetCustom, TargetAddress: "203.0.113.20", TargetPort: 8443,
		Outbound: routingNodeTag(node.ID), Enabled: true,
	})
	mustCreate(t, db, &model.PortForward{
		ServerID: relay.ID, Tag: "fwd-disabled", ListenPort: 11004, Network: "tcp",
		TargetType: model.ForwardTargetCustom, TargetAddress: "203.0.113.30", TargetPort: 443,
		Outbound: "direct", Enabled: false,
	})
	_ = router

	raw, err := app.orch.BuildServerConfig(&relay)
	if err != nil {
		t.Fatalf("BuildServerConfig failed: %v", err)
	}
	parsed, err := singbox.ParseServerConfig(raw)
	if err != nil {
		t.Fatalf("ParseServerConfig failed: %v", err)
	}
	if len(parsed.Forwards) != 3 {
		t.Fatalf("parsed forwards = %d, want 3 (disabled omitted): %+v", len(parsed.Forwards), parsed.Forwards)
	}
	if parsed.Forwards[1].OverrideAddress != "jp.example.com" || parsed.Forwards[1].OverridePort != 2443 {
		t.Fatalf("resolved inbound forward mismatch: %+v", parsed.Forwards[1])
	}
	checkSingBoxBinary(t, raw)
}

func TestPortForwardCrossEntityProtectionAndCascades(t *testing.T) {
	db, _, router, relay, target, landing, _, targetSocks, landingSocks := setupForwardClusterFixture(t)

	node := model.CustomNode{
		Name: "fwd-node", Group: "g", Protocol: "socks", Address: "198.51.100.11", Port: 1080,
		Params: model.JSONText(`{"username":"u","password":"p"}`), Enabled: true, UserEnabled: true,
	}
	mustCreate(t, db, &node)
	nodeTag := routingNodeTag(node.ID)

	fwd := model.PortForward{
		ServerID: relay.ID, Tag: "fwd-guard", ListenPort: 12001, Network: "tcp",
		TargetType: model.ForwardTargetInbound, TargetServerID: target.ID, TargetInboundID: targetSocks.ID,
		Outbound: managedRoutingNodeTag(landingSocks.ID), Enabled: true,
	}
	mustCreate(t, db, &fwd)
	fwdCustom := model.PortForward{
		ServerID: relay.ID, Tag: "fwd-uses-node", ListenPort: 12002, Network: "",
		TargetType: model.ForwardTargetCustom, TargetAddress: "1.1.1.1", TargetPort: 443,
		Outbound: nodeTag, Enabled: true,
	}
	mustCreate(t, db, &fwdCustom)

	// 1. Delete target inbound -> 409 Conflict.
	res := serveJSONRequest(t, router, http.MethodDelete, fmt.Sprintf("/servers/%d/inbounds/%d", target.ID, targetSocks.ID), "")
	if res.Code != http.StatusConflict {
		t.Fatalf("delete referenced target inbound status = %d, want 409: %s", res.Code, res.Body.String())
	}

	// 2. Disable target inbound while forward is enabled -> 409 Conflict.
	res = serveJSONRequest(t, router, http.MethodPut, fmt.Sprintf("/servers/%d/inbounds/%d", target.ID, targetSocks.ID), `{"enabled":false}`)
	if res.Code != http.StatusConflict {
		t.Fatalf("disable referenced target inbound status = %d, want 409: %s", res.Code, res.Body.String())
	}

	// 3. Delete landing inbound referenced as forward outbound -> 409 Conflict.
	res = serveJSONRequest(t, router, http.MethodDelete, fmt.Sprintf("/servers/%d/inbounds/%d", landing.ID, landingSocks.ID), "")
	if res.Code != http.StatusConflict {
		t.Fatalf("delete referenced outbound inbound status = %d, want 409: %s", res.Code, res.Body.String())
	}

	// 4. Delete target server while another server's forward targets it -> 409 Conflict.
	res = serveJSONRequest(t, router, http.MethodDelete, fmt.Sprintf("/servers/%d", target.ID), "")
	if res.Code != http.StatusConflict {
		t.Fatalf("delete referenced target server status = %d, want 409: %s", res.Code, res.Body.String())
	}

	// 5. Switch target server to raw config -> 409 Conflict.
	rawPayload := `{"config":"{\"inbounds\":[{\"type\":\"socks\",\"tag\":\"jp-socks\",\"listen\":\"::\",\"listen_port\":2443,\"users\":[{\"username\":\"t\",\"password\":\"p\"}]}],\"outbounds\":[{\"type\":\"direct\",\"tag\":\"direct\"}],\"route\":{\"final\":\"direct\"}}"}`
	res = serveJSONRequest(t, router, http.MethodPut, fmt.Sprintf("/servers/%d/raw", target.ID), rawPayload)
	if res.Code != http.StatusConflict {
		t.Fatalf("raw mode on referenced target server status = %d, want 409: %s", res.Code, res.Body.String())
	}

	// 6. Set target server address to loopback -> 409 Conflict.
	res = serveJSONRequest(t, router, http.MethodPut, fmt.Sprintf("/servers/%d", target.ID), `{"address":"127.0.0.1"}`)
	if res.Code != http.StatusConflict {
		t.Fatalf("setting target server address to 127.0.0.1 status = %d, want 409: %s", res.Code, res.Body.String())
	}

	// 7. Delete CustomNode referenced by forward -> 409 Conflict.
	res = serveJSONRequest(t, router, http.MethodDelete, fmt.Sprintf("/custom-nodes/%d", node.ID), "")
	if res.Code != http.StatusConflict {
		t.Fatalf("delete custom node referenced by forward status = %d, want 409: %s", res.Code, res.Body.String())
	}

	// 8. Legacy Outbound delete blocked (409) & tag rename cascades to PortForward.
	ob := model.Outbound{ServerID: relay.ID, Tag: "legacy-sock", Type: "socks", Settings: model.JSONText(`{"server":"198.51.100.12","server_port":1080}`)}
	mustCreate(t, db, &ob)
	if err := db.Model(&fwdCustom).Update("outbound", "legacy-sock").Error; err != nil {
		t.Fatal(err)
	}
	res = serveJSONRequest(t, router, http.MethodDelete, fmt.Sprintf("/servers/%d/outbounds/%d", relay.ID, ob.ID), "")
	if res.Code != http.StatusConflict {
		t.Fatalf("delete legacy outbound referenced by forward status = %d, want 409: %s", res.Code, res.Body.String())
	}
	res = serveJSONRequest(t, router, http.MethodPut, fmt.Sprintf("/servers/%d/outbounds/%d", relay.ID, ob.ID), `{"tag":"legacy-sock-v2"}`)
	if res.Code != http.StatusOK {
		t.Fatalf("rename legacy outbound status = %d: %s", res.Code, res.Body.String())
	}
	var updatedFwd model.PortForward
	if err := db.First(&updatedFwd, fwdCustom.ID).Error; err != nil || updatedFwd.Outbound != "legacy-sock-v2" {
		t.Fatalf("forward outbound rename did not cascade: %+v, err=%v", updatedFwd, err)
	}

	// 9. Delete relay server cleans up its own forward state (when nothing external references relay).
	mustCreate(t, db, &model.TrafficForwardRecord{ServerID: relay.ID, ForwardID: fwd.ID, Bucket: time.Now(), Upload: 10})
	res = serveJSONRequest(t, router, http.MethodDelete, fmt.Sprintf("/servers/%d", relay.ID), "")
	if res.Code != http.StatusOK {
		t.Fatalf("delete relay server status = %d: %s", res.Code, res.Body.String())
	}
	var rem int64
	if err := db.Model(&model.PortForward{}).Where("server_id = ?", relay.ID).Count(&rem).Error; err != nil || rem != 0 {
		t.Fatalf("relay forwards not deleted: %d, %v", rem, err)
	}
	if err := db.Model(&model.TrafficForwardRecord{}).Where("server_id = ?", relay.ID).Count(&rem).Error; err != nil || rem != 0 {
		t.Fatalf("relay forward traffic records not deleted: %d, %v", rem, err)
	}
}

func TestPortForwardImportRoundTripAndTagProtection(t *testing.T) {
	db, app, router, relay, target, _, _, targetSocks, _ := setupForwardClusterFixture(t)

	customFwd := model.PortForward{
		ServerID: relay.ID, Tag: "fwd-c", ListenPort: 13001, Network: "tcp",
		TargetType: model.ForwardTargetCustom, TargetAddress: "203.0.113.50", TargetPort: 443,
		Outbound: "direct", Enabled: true, Remark: "keep me",
	}
	inboundFwd := model.PortForward{
		ServerID: relay.ID, Tag: "fwd-i", ListenPort: 13002, Network: "",
		TargetType: model.ForwardTargetInbound, TargetServerID: target.ID, TargetInboundID: targetSocks.ID,
		Outbound: "direct", Enabled: true, Remark: "linked",
	}
	mustCreate(t, db, &customFwd)
	mustCreate(t, db, &inboundFwd)
	if err := rememberForwardTrafficTag(db, relay.ID, customFwd.ID, customFwd.Tag); err != nil {
		t.Fatal(err)
	}
	if err := rememberForwardTrafficTag(db, relay.ID, inboundFwd.ID, inboundFwd.Tag); err != nil {
		t.Fatal(err)
	}

	raw, err := app.orch.BuildServerConfig(&relay)
	if err != nil {
		t.Fatal(err)
	}

	// Re-import the panel-generated config via /import.
	body, _ := json.Marshal(gin.H{"config": string(raw)})
	res := serveJSONRequest(t, router, http.MethodPost, fmt.Sprintf("/servers/%d/import", relay.ID), string(body))
	if res.Code != http.StatusOK {
		t.Fatalf("re-import managed config with forwards status = %d: %s", res.Code, res.Body.String())
	}
	var importResp struct {
		OK      bool          `json:"ok"`
		Summary importSummary `json:"summary"`
	}
	if err := json.Unmarshal(res.Body.Bytes(), &importResp); err != nil {
		t.Fatal(err)
	}
	if len(importResp.Summary.Forwards) != 2 {
		t.Fatalf("import summary forwards = %d, want 2: %+v", len(importResp.Summary.Forwards), importResp.Summary.Forwards)
	}
	var reloadedRelay model.Server
	if err := db.First(&reloadedRelay, relay.ID).Error; err != nil {
		t.Fatal(err)
	}
	if reloadedRelay.ConfigMode != model.ConfigModeManaged {
		t.Fatalf("relay ConfigMode after lossless import = %q, want managed", reloadedRelay.ConfigMode)
	}
	var reloadedLinked model.PortForward
	if err := db.Where("server_id = ? AND tag = ?", relay.ID, "fwd-i").First(&reloadedLinked).Error; err != nil {
		t.Fatal(err)
	}
	if reloadedLinked.TargetType != model.ForwardTargetInbound || reloadedLinked.TargetInboundID != targetSocks.ID {
		t.Fatalf("re-import broke inbound target link: %+v", reloadedLinked)
	}

	// Target server import removing targetSocks -> 409 Conflict.
	emptyRaw, err := singbox.BuildServerConfig(singbox.ServerConfigInput{})
	if err != nil {
		t.Fatal(err)
	}
	emptyBody, _ := json.Marshal(gin.H{"config": string(emptyRaw)})
	res = serveJSONRequest(t, router, http.MethodPost, fmt.Sprintf("/servers/%d/import", target.ID), string(emptyBody))
	if res.Code != http.StatusConflict {
		t.Fatalf("import removing forward-referenced inbound status = %d, want 409: %s", res.Code, res.Body.String())
	}

	// Deleting customFwd records tombstone, then raw import trying to reuse tag as a socks inbound -> 400 BadRequest.
	res = serveJSONRequest(t, router, http.MethodDelete, fmt.Sprintf("/servers/%d/forwards/%d", relay.ID, customFwd.ID), "")
	if res.Code != http.StatusOK {
		t.Fatal(res.Body.String())
	}
	conflictRaw := `{"inbounds":[{"type":"socks","tag":"fwd-c","listen":"::","listen_port":14001,"users":[{"username":"a","password":"b"}]}],"outbounds":[{"type":"direct","tag":"direct"}],"route":{"final":"direct"}}`
	conflictBody, _ := json.Marshal(gin.H{"config": conflictRaw})
	res = serveJSONRequest(t, router, http.MethodPost, fmt.Sprintf("/servers/%d/import", relay.ID), string(conflictBody))
	if res.Code != http.StatusBadRequest || !strings.Contains(res.Body.String(), errForwardTrafficTagReserved.Error()) {
		t.Fatalf("raw import reusing deleted forward tag status = %d: %s", res.Code, res.Body.String())
	}

	// Import with disallowed forward target stays raw and does NOT insert invalid forward row.
	loopRaw := `{"inbounds":[{"type":"direct","tag":"bad-loop","listen":"::","listen_port":14002,"override_address":"127.0.0.1","override_port":80}],"outbounds":[{"type":"direct","tag":"direct"}],"route":{"rules":[{"inbound":["bad-loop"],"outbound":"direct"}],"final":"direct"}}`
	loopBody, _ := json.Marshal(gin.H{"config": loopRaw})
	res = serveJSONRequest(t, router, http.MethodPost, fmt.Sprintf("/servers/%d/import", target.ID), string(loopBody))
	// target is still referenced by relay's fwd-i; use a fresh server for raw mode test.
	if res.Code != http.StatusConflict {
		t.Fatalf("expected target server protected from becoming raw: %d %s", res.Code, res.Body.String())
	}
	unlinked := model.Server{Name: "unlinked", AgentToken: "tok-unlinked"}
	mustCreate(t, db, &unlinked)
	res = serveJSONRequest(t, router, http.MethodPost, fmt.Sprintf("/servers/%d/import", unlinked.ID), string(loopBody))
	if res.Code != http.StatusOK {
		t.Fatalf("unlinked import status = %d: %s", res.Code, res.Body.String())
	}
	var gotUnlinked model.Server
	if err := db.First(&gotUnlinked, unlinked.ID).Error; err != nil || gotUnlinked.ConfigMode != model.ConfigModeRaw {
		t.Fatalf("expected raw mode for disallowed forward target, got %+v, err=%v", gotUnlinked, err)
	}
	var badCount int64
	if err := db.Model(&model.PortForward{}).Where("server_id = ?", unlinked.ID).Count(&badCount).Error; err != nil || badCount != 0 {
		t.Fatalf("disallowed raw forward poisoned port_forwards: %d, %v", badCount, err)
	}

	// Sanity check error type wrapping.
	if err := checkForwardTrafficTag(db, relay.ID, 0, "fwd-c"); !errors.Is(err, errForwardTrafficTagReserved) {
		t.Fatalf("expected errForwardTrafficTagReserved, got %v", err)
	}
}

func TestPortForwardTrafficAccountingAndPrune(t *testing.T) {
	db := testDB(t)
	_, router := portForwardTestRouter(db)

	srv := model.Server{Name: "node-traffic", AgentToken: "tok-tr", ConfigMode: model.ConfigModeManaged, TrafficResetDay: 1}
	mustCreate(t, db, &srv)
	ib := model.Inbound{ServerID: srv.ID, Tag: "vless-in", Type: model.InboundVLESS, ListenPort: 443, Enabled: true}
	mustCreate(t, db, &ib)
	fwd := model.PortForward{
		ServerID: srv.ID, Tag: "fwd-9001", ListenPort: 9001, Network: "tcp",
		TargetType: model.ForwardTargetCustom, TargetAddress: "1.2.3.4", TargetPort: 443,
		Outbound: "direct", Enabled: true,
	}
	mustCreate(t, db, &fwd)
	if err := rememberForwardTrafficTag(db, srv.ID, fwd.ID, fwd.Tag); err != nil {
		t.Fatal(err)
	}
	// Historical tombstone for a deleted forward.
	if err := rememberForwardTrafficTag(db, srv.ID, 9999, "fwd-old"); err != nil {
		t.Fatal(err)
	}

	now := time.Now().UTC().Truncate(time.Minute)
	snap := &protocol.TrafficSnapshot{
		AccountingVersion: 2,
		SampledAt:         now.Unix(),
		UploadTotal:       6400,
		DownloadTotal:     10600,
		UploadRate:        640,
		DownloadRate:      1060,
		TCPConnections:    5,
		UDPConnections:    2,
		Report: &protocol.TrafficReport{
			Session:  "7795c26e-d04c-4f36-aa76-fc891179f361",
			Sequence: 1,
			Buckets: []protocol.TrafficTimeBucket{{
				Time:           now.Unix(),
				Upload:         6400,
				Download:       10600,
				UploadRate:     640,
				DownloadRate:   1060,
				TCPConnections: 5,
				UDPConnections: 2,
				Ports: []protocol.PortTrafficSnapshot{
					{Inbound: "vless-in", Upload: 1000, Download: 2000},
					{Inbound: "fwd-9001", Upload: 5000, Download: 8000, UploadRate: 500, DownloadRate: 800},
					{Inbound: "fwd-old", Upload: 300, Download: 400},
					{Inbound: "unknown-in", Upload: 100, Download: 200},
				},
			}},
		},
	}
	deliverInboundTrafficReport(t, db, srv.ID, snap)

	res := serveJSONRequest(t, router, http.MethodGet, fmt.Sprintf("/servers/%d/traffic?range=24h", srv.ID), "")
	if res.Code != http.StatusOK {
		t.Fatalf("serverTraffic status = %d: %s", res.Code, res.Body.String())
	}
	var tr struct {
		Upload   uint64 `json:"upload"`
		Download uint64 `json:"download"`
		Ports    []struct {
			InboundID uint   `json:"inbound_id"`
			Upload    uint64 `json:"upload"`
			Download  uint64 `json:"download"`
		} `json:"ports"`
		Forwards []struct {
			ForwardID uint   `json:"forward_id"`
			Tag       string `json:"tag"`
			Port      int    `json:"port"`
			Upload    uint64 `json:"upload"`
			Download  uint64 `json:"download"`
		} `json:"forwards"`
		UnattributedUpload   uint64 `json:"unattributed_upload"`
		UnattributedDownload uint64 `json:"unattributed_download"`
	}
	if err := json.Unmarshal(res.Body.Bytes(), &tr); err != nil {
		t.Fatal(err)
	}
	if tr.Upload != 6400 || tr.Download != 10600 {
		t.Fatalf("unexpected total upload/download: %+v", tr)
	}
	if len(tr.Forwards) != 1 || tr.Forwards[0].ForwardID != fwd.ID || tr.Forwards[0].Port != 9001 || tr.Forwards[0].Upload != 5000 || tr.Forwards[0].Download != 8000 {
		t.Fatalf("unexpected forwards series: %+v", tr.Forwards)
	}
	// Total 6400 - inbound 1000 - fwd 5000 - deleted fwd 300 = 100 unknown.
	if tr.UnattributedUpload != 100 || tr.UnattributedDownload != 200 {
		t.Fatalf("unattributed mismatch: got (%d, %d), want (100, 200)", tr.UnattributedUpload, tr.UnattributedDownload)
	}

	// Verify pruneTrafficRecords deletes old forward buckets while keeping recent ones.
	nowUTC := time.Now().UTC()
	staleMinute := nowUTC.Add(-5 * 24 * time.Hour).Truncate(time.Minute)
	staleHourly := nowUTC.Add(-410 * 24 * time.Hour).Truncate(time.Hour)
	mustCreate(t, db, &model.TrafficForwardRecord{ServerID: srv.ID, ForwardID: fwd.ID, Bucket: staleMinute, Upload: 123})
	mustCreate(t, db, &model.TrafficForwardHourly{ServerID: srv.ID, ForwardID: fwd.ID, Bucket: staleHourly, Upload: 123})
	if err := pruneTrafficRecords(db, nowUTC); err != nil {
		t.Fatal(err)
	}
	var recentMinuteCount, recentHourlyCount, staleHourlyCount int64
	if err := db.Model(&model.TrafficForwardRecord{}).Where("server_id = ?", srv.ID).Count(&recentMinuteCount).Error; err != nil || recentMinuteCount != 2 {
		t.Fatalf("expected 2 recent forward minute records (fwd + tombstone) after prune, got %d (%v)", recentMinuteCount, err)
	}
	if err := db.Model(&model.TrafficForwardHourly{}).Where("server_id = ?", srv.ID).Count(&recentHourlyCount).Error; err != nil || recentHourlyCount != 2 {
		t.Fatalf("expected 2 recent forward hourly records after prune, got %d (%v)", recentHourlyCount, err)
	}
	if err := db.Model(&model.TrafficForwardHourly{}).Where("server_id = ? AND bucket = ?", srv.ID, staleHourly).Count(&staleHourlyCount).Error; err != nil || staleHourlyCount != 0 {
		t.Fatalf("expected stale forward hourly record pruned, got %d (%v)", staleHourlyCount, err)
	}
}
