package panel

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/hann0w0/singbox-panel/internal/domain/model"
)

func TestServerNodeFormatsExportsAuthorizedUserCredentials(t *testing.T) {
	db := testDB(t)
	server := model.Server{Name: "Frankfurt", Address: "fra.example.com", AgentToken: "agent-secret"}
	if err := db.Create(&server).Error; err != nil {
		t.Fatal(err)
	}
	inbounds := []model.Inbound{
		{ServerID: server.ID, Tag: "fixed", Type: model.InboundSocks, ListenPort: 1080, Enabled: true, Settings: model.JSONText(`{"single_user":true,"username":"fixed","password":"fixed-password"}`)},
		{ServerID: server.ID, Tag: "vless", Type: model.InboundVLESS, ListenPort: 443, Enabled: true, Settings: model.JSONText(`{"multi_user":true}`)},
		{ServerID: server.ID, Tag: "trojan", Type: model.InboundTrojan, ListenPort: 444, Enabled: true, Settings: model.JSONText(`{"multi_user":true}`)},
		{ServerID: server.ID, Tag: "ss2022", Type: model.InboundShadowsocks, ListenPort: 445, Enabled: true, Settings: model.JSONText(`{"multi_user":true,"method":"2022-blake3-aes-128-gcm","ss_server_psk":"dGVzdC1zZXJ2ZXIta2V5IQ=="}`)},
		{ServerID: server.ID, Tag: "disabled", Type: model.InboundVLESS, ListenPort: 446, Enabled: false, Settings: model.JSONText(`{"multi_user":true}`)},
	}
	for i := range inbounds {
		if err := db.Create(&inbounds[i]).Error; err != nil {
			t.Fatal(err)
		}
	}
	past := time.Now().Add(-time.Hour)
	users := []model.User{
		{Email: "alice", Enabled: true, ServerIDs: []uint{server.ID}},
		{Email: "bob", Enabled: true, ServerIDs: []uint{server.ID}, InboundIDs: []uint{inbounds[1].ID}},
		{Email: "disabled", Enabled: false, ServerIDs: []uint{server.ID}},
		{Email: "expired", Enabled: true, ExpireAt: &past, ServerIDs: []uint{server.ID}},
		{Email: "unassigned", Enabled: true},
		{Email: "fixed-only", Enabled: true, ServerIDs: []uint{server.ID}, InboundIDs: []uint{inbounds[0].ID}},
		{Email: "disabled-only", Enabled: true, ServerIDs: []uint{server.ID}, InboundIDs: []uint{inbounds[4].ID}},
	}
	for i := range users {
		users[i].SubToken = "sub-secret-" + users[i].Email
		users[i].ProxyToken = "proxy-secret-" + users[i].Email
		if err := db.Create(&users[i]).Error; err != nil {
			t.Fatal(err)
		}
	}
	app := &App{db: db}
	router := gin.New()
	router.GET("/servers/:id/node-formats", app.serverNodeFormats)
	request := func(query string) *httptest.ResponseRecorder {
		t.Helper()
		res := httptest.NewRecorder()
		router.ServeHTTP(res, httptest.NewRequest(http.MethodGet, fmt.Sprintf("/servers/%d/node-formats%s", server.ID, query), nil))
		if res.Header().Get("Cache-Control") != "no-store" {
			t.Fatal("credential response must not be cached")
		}
		if strings.Contains(res.Body.String(), "proxy-secret-") || strings.Contains(res.Body.String(), "sub-secret-") {
			t.Fatal("export leaked an account seed")
		}
		return res
	}
	type response struct {
		Items      []NodeFormatItem `json:"items"`
		Users      []nodeExportUser `json:"users"`
		MultiCount int              `json:"multi_user_count"`
		UserID     uint             `json:"user_id"`
	}
	decode := func(res *httptest.ResponseRecorder) response {
		t.Helper()
		if res.Code != http.StatusOK {
			t.Fatalf("status = %d: %s", res.Code, res.Body.String())
		}
		var got response
		if err := json.Unmarshal(res.Body.Bytes(), &got); err != nil {
			t.Fatal(err)
		}
		return got
	}
	initial := decode(request(""))
	if len(initial.Items) != 1 || initial.Items[0].Tag != "fixed" || initial.MultiCount != 3 || len(initial.Users) != 2 || initial.UserID != 0 {
		t.Fatalf("initial export = %+v", initial)
	}
	for i := range initial.Users {
		if initial.Users[i].ID != users[i].ID {
			t.Fatalf("ineligible user offered: %+v", initial.Users)
		}
	}
	all := decode(request(fmt.Sprintf("?user_id=%d", users[0].ID)))
	want := buildNodeFormatItems(app.gatherNodes(&users[0]))
	if len(all.Items) != 4 || len(want) != 4 || all.UserID != users[0].ID {
		t.Fatalf("full export = %+v", all)
	}
	for i, item := range all.Items {
		if item.URI != want[i].URI || item.Clash != want[i].Clash || item.Surge != want[i].Surge {
			t.Errorf("%s export disagrees with the user's subscription", item.Tag)
		}
	}
	restricted := decode(request(fmt.Sprintf("?user_id=%d", users[1].ID)))
	if len(restricted.Items) != 2 || restricted.Items[0].Tag != "fixed" || restricted.Items[1].Tag != "vless" {
		t.Fatalf("restricted export included ungranted inbounds: %+v", restricted)
	}
	uuid := proxyIdentity(&users[1], inbounds[1].ID).UUID
	if !strings.Contains(restricted.Items[1].URI, uuid) || strings.Contains(all.Items[1].URI, uuid) {
		t.Fatal("multi-user export did not use independent account credentials")
	}
	for _, user := range users[2:] {
		if res := request(fmt.Sprintf("?user_id=%d", user.ID)); res.Code != http.StatusForbidden {
			t.Errorf("%s: status = %d, body = %s", user.Email, res.Code, res.Body.String())
		}
	}
	for _, invalid := range []string{"0", "-1", "abc", "18446744073709551616"} {
		if res := request("?user_id=" + invalid); res.Code != http.StatusBadRequest {
			t.Errorf("invalid user %s: status = %d", invalid, res.Code)
		}
	}
	if res := request("?user_id=99999"); res.Code != http.StatusForbidden {
		t.Fatalf("nonexistent user: %d", res.Code)
	}
	if err := db.Model(&model.User{}).Where("id IN ?", []uint{users[0].ID, users[1].ID}).Update("enabled", false).Error; err != nil {
		t.Fatal(err)
	}
	if res := request(fmt.Sprintf("?user_id=%d", users[0].ID)); res.Code != http.StatusForbidden {
		t.Fatal("revoked user still exports credentials")
	}
	if got := decode(request("")); len(got.Users) != 0 || got.MultiCount != 3 || len(got.Items) != 1 {
		t.Fatalf("empty eligible users = %+v", got)
	}
	if err := db.Migrator().DropTable(&model.User{}); err != nil {
		t.Fatal(err)
	}
	if res := request(""); res.Code != http.StatusInternalServerError {
		t.Fatal("user query failure silently returned a partial export")
	}
}
