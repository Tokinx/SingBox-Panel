package panel

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/hann0w0/singbox-panel/internal/domain/model"
	"github.com/hann0w0/singbox-panel/internal/domain/singbox"
)

func TestNodeJSONEditorRoundTripAndValidation(t *testing.T) {
	db := testDB(t)
	srv := model.Server{Name: "json", AgentToken: "json-token", ConfigMode: model.ConfigModeManaged}
	if err := db.Create(&srv).Error; err != nil {
		t.Fatal(err)
	}
	app := &App{db: db}
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.POST("/servers/:id/node-json", app.convertNodeJSON)
	call := func(body any, status int) map[string]json.RawMessage {
		t.Helper()
		data, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		req := httptest.NewRequest("POST", fmt.Sprintf("/servers/%d/node-json", srv.ID), bytes.NewReader(data))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		if w.Code != status {
			t.Fatalf("status=%d want=%d response=%s", w.Code, status, w.Body.String())
		}
		if w.Header().Get("Cache-Control") != "no-store" {
			t.Fatal("missing no-store")
		}
		var result map[string]json.RawMessage
		if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		return result
	}
	for _, kind := range []string{"inbound", "outbound"} {
		for _, typ := range []string{"shadowsocks", "socks", "vless", "vmess", "trojan", "hysteria2", "tuic", "anytls", "snell"} {
			t.Run(kind+"/"+typ, func(t *testing.T) {
				st := singbox.InboundSettings{SingleUser: true, Method: "aes-128-gcm", SSServerPSK: "ss-password", UUID: "11111111-2222-4333-8444-555555555555", Password: "test-password", SnellPSK: "test-password", SnellVersion: 5}
				if typ == "trojan" || typ == "hysteria2" || typ == "tuic" || typ == "anytls" {
					st.TLS = singbox.TLSSettings{Enabled: true, SelfSigned: true, ServerName: "node.example.com"}
				}
				if typ == "socks" {
					st.Username = "alice"
				}
				var body any = gin.H{"type": typ, "tag": "node", "listen_port": 12345, "settings": st}
				if kind == "outbound" {
					st.SnellVersion = 4
					body = gin.H{"type": typ, "tag": "node", "settings": outboundStoredSettings{Server: "node.example.com", ServerPort: 443, Username: st.Username, UUID: st.UUID, Password: st.Password, Settings: st}}
				}
				encoded := call(gin.H{"kind": kind, "direction": "encode", "body": body}, 200)
				decoded := call(gin.H{"kind": kind, "direction": "decode", "body": encoded["body"], "config": encoded["config"]}, 200)
				if err := checkNodeJSONLoss(encoded["config"], decoded["config"]); err != nil {
					t.Fatal(err)
				}
			})
		}
	}
	body := gin.H{"type": "socks", "tag": "node", "listen_port": 12345, "settings": gin.H{"single_user": true}}
	encoded := call(gin.H{"kind": "inbound", "direction": "encode", "body": body}, 200)
	var native map[string]any
	json.Unmarshal(encoded["config"], &native)
	native["listen_port"] = 23456
	result := call(gin.H{"kind": "inbound", "direction": "decode", "body": encoded["body"], "config": native}, 200)
	var decoded inboundReq
	json.Unmarshal(result["body"], &decoded)
	if decoded.ListenPort != 23456 {
		t.Fatal("JSON edit was not mapped back to form")
	}
	native["sniff"] = true
	rejected := call(gin.H{"kind": "inbound", "direction": "decode", "body": encoded["body"], "config": native}, 400)
	if !strings.Contains(string(rejected["error"]), "$.sniff") {
		t.Fatalf("missing field path: %s", rejected["error"])
	}
	delete(native, "sniff")
	native["listen"] = "127.0.0.1"
	call(gin.H{"kind": "inbound", "direction": "decode", "body": encoded["body"], "config": native}, 400)
	call(gin.H{"kind": "inbound", "direction": "decode", "body": body, "config": []any{}}, 400)
	call(gin.H{"kind": "inbound", "direction": "encode", "id": 99999, "body": body}, 404)
	multi := gin.H{"type": "vless", "tag": "multi", "listen_port": 12345, "settings": gin.H{"multi_user": true}}
	encoded = call(gin.H{"kind": "inbound", "direction": "encode", "body": multi}, 200)
	if string(encoded["managed_users"]) != "true" || strings.Contains(string(encoded["config"]), "users") {
		t.Fatal("managed users must be kept outside JSON")
	}
	call(gin.H{"kind": "inbound", "direction": "decode", "body": encoded["body"], "config": encoded["config"]}, 200)
	native = nil
	json.Unmarshal(encoded["config"], &native)
	native["users"] = []any{gin.H{"uuid": "fake"}}
	call(gin.H{"kind": "inbound", "direction": "decode", "body": encoded["body"], "config": native}, 400)
	var count int64
	db.Model(&model.Inbound{}).Count(&count)
	if count != 0 {
		t.Fatal("conversion created an inbound")
	}
	db.Model(&model.Outbound{}).Count(&count)
	if count != 0 {
		t.Fatal("conversion created an outbound")
	}
}
