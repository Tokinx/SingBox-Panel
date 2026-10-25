package panel

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/hann0w0/singbox-panel/internal/domain/model"
	"github.com/hann0w0/singbox-panel/internal/domain/singbox"
)

func panelGuardSettings() singbox.InboundSettings {
	return singbox.InboundSettings{SingleUser: true, UUID: "11111111-2222-4333-8444-555555555555", Flow: "xtls-rprx-vision",
		TLS: singbox.TLSSettings{Enabled: true, ServerName: "music.apple.com", Reality: singbox.RealitySettings{
			Enabled: true, HandshakeServer: "music.apple.com", PrivateKey: "UuMBgl7MXTPx9inmQp2UC7Jcnwc6XYbwDNebonM-FCc",
			ShortID: []string{"abcd"}, Guard: &singbox.RealityGuardSettings{Enabled: true, InternalPort: 31736},
		}},
	}
}

func TestRealityGuardPanelReservesVirtualPortsAndTags(t *testing.T) {
	db := testDB(t)
	a := &App{db: db}
	st := panelGuardSettings()
	raw, _ := json.Marshal(st)
	row := model.Inbound{ServerID: 1, Tag: "reality", Type: "vless", ListenPort: 31735, Settings: model.JSONText(raw), Enabled: false}
	if err := db.Create(&row).Error; err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		tag  string
		port int
	}{{"normal", 31736}, {"reality-guard", 31737}} {
		if err := a.validateInboundConfiguration(db, 1, 0, tc.tag, "socks", tc.port, singbox.InboundSettings{}); err == nil {
			t.Fatalf("virtual listener collision accepted: %+v", tc)
		}
	}
	if err := a.validateInboundConfiguration(db, 1, row.ID, row.Tag, "vless", 31735, st); err != nil {
		t.Fatalf("unchanged edit: %v", err)
	}
	for _, port := range []int{29091, 29092} {
		st.TLS.Reality.Guard.InternalPort = port
		if err := a.validateInboundConfiguration(db, 1, row.ID, row.Tag, "vless", 31735, st); err == nil {
			t.Fatalf("reserved internal port accepted: %d", port)
		}
	}
}

func TestRealityGuardAPICreateEditAndSubscriptions(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := testDB(t)
	srv := model.Server{Name: "guard-node", Address: "node.example.com", AgentToken: "guard-node-token", ConfigMode: model.ConfigModeManaged, SingboxVersion: "1.14.4"}
	if err := db.Create(&srv).Error; err != nil {
		t.Fatal(err)
	}
	hub := NewHub(db)
	a := &App{db: db, hub: hub, orch: NewOrchestrator(db, hub), serverOperations: newKeyedMutex[uint]()}
	router := gin.New()
	router.POST("/servers/:id/inbounds", a.createInbound)
	router.PUT("/servers/:id/inbounds/:inboundID", a.updateInbound)
	basePath := fmt.Sprintf("/servers/%d/inbounds", srv.ID)
	st := panelGuardSettings()
	body, _ := json.Marshal(map[string]any{"type": "vless", "tag": "reality", "listen_port": 31735, "settings": st})
	res := serveJSONRequest(t, router, http.MethodPost, basePath, string(body))
	if res.Code != http.StatusOK {
		t.Fatalf("create: %d %s", res.Code, res.Body.String())
	}
	var in model.Inbound
	if err := db.Where("server_id = ?", srv.ID).First(&in).Error; err != nil {
		t.Fatal(err)
	}
	if in.ListenPort != 31735 {
		t.Fatalf("stored public port = %d", in.ListenPort)
	}
	nodes, err := a.gatherNodesWithError(&model.User{ServerIDs: []uint{srv.ID}})
	if err != nil || len(nodes) != 1 || nodes[0].port != 31735 {
		t.Fatalf("subscription nodes=%+v err=%v", nodes, err)
	}
	raw, err := a.orch.BuildServerConfig(&srv)
	if err != nil || !strings.Contains(string(raw), `"listen": "127.0.0.1"`) {
		t.Fatalf("generate: %v %s", err, raw)
	}
	path := fmt.Sprintf("%s/%d", basePath, in.ID)
	for _, payload := range []string{`{"listen_port":31736}`, `{"tag":"reality-guard","settings":{"tls":{"enabled":true,"server_name":"music.apple.com","reality":{"enabled":true,"handshake_server":"music.apple.com","guard":{"enabled":true,"tag":"reality-guard","internal_port":31736}}}}}`} {
		r := serveJSONRequest(t, router, http.MethodPut, path, payload)
		if r.Code != http.StatusBadRequest {
			t.Fatalf("invalid edit accepted: %d %s", r.Code, r.Body.String())
		}
	}
	var unchanged model.Inbound
	if err := db.First(&unchanged, in.ID).Error; err != nil {
		t.Fatal(err)
	}
	if unchanged.ListenPort != 31735 || unchanged.Tag != "reality" {
		t.Fatal("rejected edit persisted")
	}
	st.TLS.Reality.Guard = nil
	body, _ = json.Marshal(map[string]any{"settings": st})
	res = serveJSONRequest(t, router, http.MethodPut, path, string(body))
	if res.Code != http.StatusOK {
		t.Fatalf("disable: %d %s", res.Code, res.Body.String())
	}
	raw, err = a.orch.BuildServerConfig(&srv)
	if err != nil || strings.Contains(string(raw), "31736") || strings.Contains(string(raw), "reality-guard") {
		t.Fatalf("stale guard on disable: %v %s", err, raw)
	}
}
