package panel

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/tokinx/singbox-panel/internal/domain/model"
	"github.com/tokinx/singbox-panel/internal/domain/singbox"
)

func TestProxyIdentityIsStableAndPerInbound(t *testing.T) {
	user := model.User{ID: 7, ProxyToken: "stable-seed"}
	one := proxyIdentity(&user, 10)
	again := proxyIdentity(&user, 10)
	other := proxyIdentity(&user, 11)
	if one != again {
		t.Fatalf("identity changed: %+v != %+v", one, again)
	}
	if one.UUID == other.UUID || one.Password == other.Password {
		t.Fatalf("different inbounds reused credentials: %+v %+v", one, other)
	}
	if one.Name != "u7" || one.Username != "u7" {
		t.Fatalf("unexpected stable name: %+v", one)
	}
}

func TestManagedMultiUserConfigUsesAuthorizedIndependentCredentials(t *testing.T) {
	db := testDB(t)
	server := model.Server{Name: "multi", Address: "node.example.com", AgentToken: "multi-token", ConfigMode: model.ConfigModeManaged}
	if err := db.Create(&server).Error; err != nil {
		t.Fatal(err)
	}
	settings, _ := json.Marshal(singbox.InboundSettings{MultiUser: true})
	inbound := model.Inbound{
		ServerID: server.ID, Tag: "vless-multi", Type: model.InboundVLESS,
		ListenPort: 443, Enabled: true, Settings: settings,
	}
	if err := db.Create(&inbound).Error; err != nil {
		t.Fatal(err)
	}
	users := []model.User{
		{Email: "one", Password: "x", Role: model.RoleUser, Enabled: true, SubToken: "sub-one", ProxyToken: "proxy-one", ServerIDs: []uint{server.ID}},
		{Email: "two", Password: "x", Role: model.RoleUser, Enabled: true, SubToken: "sub-two", ProxyToken: "proxy-two", ServerIDs: []uint{server.ID}},
		{Email: "none", Password: "x", Role: model.RoleUser, Enabled: true, SubToken: "sub-none", ProxyToken: "proxy-none"},
	}
	for i := range users {
		if err := db.Create(&users[i]).Error; err != nil {
			t.Fatal(err)
		}
	}
	raw, err := NewOrchestrator(db, NewHub(db)).BuildServerConfig(&server)
	if err != nil {
		t.Fatal(err)
	}
	var config struct {
		Inbounds []struct {
			Users []struct {
				Name string `json:"name"`
				UUID string `json:"uuid"`
			} `json:"users"`
		} `json:"inbounds"`
	}
	if err := json.Unmarshal(raw, &config); err != nil {
		t.Fatal(err)
	}
	if len(config.Inbounds) != 1 || len(config.Inbounds[0].Users) != 2 {
		t.Fatalf("unexpected generated users: %s", raw)
	}
	if config.Inbounds[0].Users[0].UUID == config.Inbounds[0].Users[1].UUID {
		t.Fatalf("users share UUID: %s", raw)
	}

	app := &App{db: db}
	nodesOne := app.gatherNodes(&users[0])
	nodesTwo := app.gatherNodes(&users[1])
	if len(nodesOne) != 1 || len(nodesTwo) != 1 || nodesOne[0].user.UUID == nodesTwo[0].user.UUID {
		t.Fatalf("subscriptions did not receive independent identities: %+v %+v", nodesOne, nodesTwo)
	}
}

func TestExpiredUserIsRemovedButSnellStaysSingleCredential(t *testing.T) {
	db := testDB(t)
	server := model.Server{Name: "mixed", AgentToken: "mixed-token", ConfigMode: model.ConfigModeManaged}
	if err := db.Create(&server).Error; err != nil {
		t.Fatal(err)
	}
	vlessSettings, _ := json.Marshal(singbox.InboundSettings{MultiUser: true})
	snellSettings, _ := json.Marshal(singbox.InboundSettings{
		MultiUser: true, SnellVersion: 5, SnellPSK: "server-psk",
	})
	inbounds := []model.Inbound{
		{ServerID: server.ID, Tag: "vless", Type: model.InboundVLESS, ListenPort: 10001, Enabled: true, Settings: vlessSettings},
		{ServerID: server.ID, Tag: "snell", Type: model.InboundSnell, ListenPort: 10002, Enabled: true, Settings: snellSettings},
	}
	for i := range inbounds {
		if err := db.Create(&inbounds[i]).Error; err != nil {
			t.Fatal(err)
		}
	}
	past := time.Now().Add(-time.Hour)
	user := model.User{
		Email: "expired", Password: "x", Role: model.RoleUser, Enabled: true,
		SubToken: "expired-sub", ProxyToken: "expired-proxy", ServerIDs: []uint{server.ID}, ExpireAt: &past,
	}
	if err := db.Create(&user).Error; err != nil {
		t.Fatal(err)
	}
	raw, err := NewOrchestrator(db, NewHub(db)).BuildServerConfig(&server)
	if err != nil {
		t.Fatal(err)
	}
	var config struct {
		Inbounds []map[string]any `json:"inbounds"`
	}
	if err := json.Unmarshal(raw, &config); err != nil {
		t.Fatal(err)
	}
	if len(config.Inbounds) != 2 {
		t.Fatalf("unexpected inbounds: %s", raw)
	}
	users, ok := config.Inbounds[0]["users"].([]any)
	if !ok || len(users) != 1 {
		t.Fatalf("expired VLESS credential was not replaced by the lockout identity: %s", raw)
	}
	lockout, ok := users[0].(map[string]any)
	if !ok || lockout["name"] != "__singbox_panel_disabled__" {
		t.Fatalf("unexpected VLESS lockout identity: %s", raw)
	}
	if _, exists := config.Inbounds[1]["users"]; exists {
		t.Fatalf("Snell must remain top-level PSK only: %s", raw)
	}
}

func TestLegacyMultiUserSOCKSBecomesAuthenticatedSingleUser(t *testing.T) {
	db := testDB(t)
	server := model.Server{Name: "socks", AgentToken: "socks-token", ConfigMode: model.ConfigModeManaged}
	if err := db.Create(&server).Error; err != nil {
		t.Fatal(err)
	}
	settings := singbox.InboundSettings{MultiUser: true}
	if err := fillInboundSecrets("socks", &settings); err != nil {
		t.Fatal(err)
	}
	if settings.MultiUser || !settings.SingleUser {
		t.Fatal("legacy SOCKS did not switch to shared credentials")
	}
	encoded, _ := json.Marshal(settings)
	inbound := model.Inbound{
		ServerID: server.ID, Tag: "socks", Type: model.InboundSocks,
		ListenPort: 1080, Enabled: true, Settings: encoded,
	}
	if err := db.Create(&inbound).Error; err != nil {
		t.Fatal(err)
	}
	raw, err := NewOrchestrator(db, NewHub(db)).BuildServerConfig(&server)
	if err != nil {
		t.Fatal(err)
	}
	var config struct {
		Inbounds []struct {
			Users []struct {
				Username string `json:"username"`
				Password string `json:"password"`
			} `json:"users"`
		} `json:"inbounds"`
	}
	if err := json.Unmarshal(raw, &config); err != nil {
		t.Fatal(err)
	}
	if len(config.Inbounds) != 1 || len(config.Inbounds[0].Users) != 1 ||
		config.Inbounds[0].Users[0].Username == "" || config.Inbounds[0].Users[0].Password == "" {
		t.Fatalf("legacy SOCKS became unauthenticated: %s", raw)
	}
}

func TestHasInboundProvisionsCredentialsForIPv6OnlyAssignment(t *testing.T) {
	db := testDB(t)
	server := model.Server{
		Name: "v6-cred", AgentToken: "v6-cred-token", ConfigMode: model.ConfigModeManaged,
		IPv6Mode: model.ServerIPv6ModeAuto, PublicIPv6: "2001:db8::55",
	}
	if err := db.Create(&server).Error; err != nil {
		t.Fatal(err)
	}
	vlessSettings, _ := json.Marshal(singbox.InboundSettings{MultiUser: true})
	vless := model.Inbound{
		ServerID: server.ID, Tag: "vless", Type: model.InboundVLESS,
		ListenPort: 26001, Enabled: true, Settings: vlessSettings,
	}
	if err := db.Create(&vless).Error; err != nil {
		t.Fatal(err)
	}
	user := model.User{
		Email: "v6-user", Password: "x", Role: model.RoleUser, Enabled: true,
		SubToken: "v6-cred-sub", ProxyToken: "v6-cred-proxy",
		ServerIDs: []uint{server.ID}, InboundIPv6IDs: []uint{vless.ID},
	}
	if err := db.Create(&user).Error; err != nil {
		t.Fatal(err)
	}
	if user.IsServerWide() || !user.HasInbound(server.ID, vless.ID) || user.HasPrimaryInbound(server.ID, vless.ID) || !user.HasIPv6Inbound(server.ID, vless.ID) {
		t.Fatalf("unexpected inbound predicate state: serverWide=%v has=%v primary=%v ipv6=%v",
			user.IsServerWide(), user.HasInbound(server.ID, vless.ID), user.HasPrimaryInbound(server.ID, vless.ID), user.HasIPv6Inbound(server.ID, vless.ID))
	}
	raw, err := NewOrchestrator(db, NewHub(db)).BuildServerConfig(&server)
	if err != nil {
		t.Fatal(err)
	}
	var config struct {
		Inbounds []struct {
			Users []struct {
				UUID string `json:"uuid"`
			} `json:"users"`
		} `json:"inbounds"`
	}
	if err := json.Unmarshal(raw, &config); err != nil {
		t.Fatal(err)
	}
	if len(config.Inbounds) != 1 || len(config.Inbounds[0].Users) != 1 {
		t.Fatalf("v6-only user credential missing from server config: %s", raw)
	}
}
