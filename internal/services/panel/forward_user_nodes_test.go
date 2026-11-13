package panel

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/hann0w0/singbox-panel/internal/domain/model"
	"github.com/hann0w0/singbox-panel/internal/domain/singbox"
)

func TestForwardedUserNodesAccessSubscriptionAndCredentialSync(t *testing.T) {
	db := testDB(t)
	tyo := model.Server{
		Name: "TYO", Address: "tyo.example.com", Region: "JP",
		AgentToken: "tyo-agent-token", ConfigMode: model.ConfigModeManaged,
	}
	lax := model.Server{
		Name: "LAX", Address: "lax.example.com", Region: "US",
		AgentToken: "lax-agent-token", ConfigMode: model.ConfigModeManaged,
	}
	mustCreate(t, db, &tyo)
	mustCreate(t, db, &lax)

	tyoDirect := model.Inbound{
		ServerID: tyo.ID, Tag: "tyo-direct", Type: model.InboundVLESS, ListenPort: 443, Enabled: true,
		Settings: model.JSONText(`{"multi_user":true}`),
	}
	laxReality := model.Inbound{
		ServerID: lax.ID, Tag: "lax-reality", Type: model.InboundVLESS, ListenPort: 23456, Enabled: true,
		Settings: model.JSONText(`{"multi_user":true,"tls":{"enabled":true,"reality":{"enabled":true,"handshake_server":"www.apple.com","handshake_server_port":443,"public_key":"pub-key-123","short_id":["a1b2"]}}}`),
	}
	laxTLS := model.Inbound{
		ServerID: lax.ID, Tag: "lax-tls", Type: model.InboundTrojan, ListenPort: 2443, Enabled: true,
		Settings: model.JSONText(`{"multi_user":true,"tls":{"enabled":true}}`),
	}
	mustCreate(t, db, &tyoDirect)
	mustCreate(t, db, &laxReality)
	mustCreate(t, db, &laxTLS)

	fwdReality := model.PortForward{
		ServerID: tyo.ID, Tag: "fwd-tyo-lax-reality", ListenPort: 33456, Network: "tcp",
		TargetType: model.ForwardTargetInbound, TargetServerID: lax.ID, TargetInboundID: laxReality.ID,
		Outbound: "direct", Enabled: true,
	}
	fwdTLS := model.PortForward{
		ServerID: tyo.ID, Tag: "fwd-tyo-lax-tls", ListenPort: 3443, Network: "",
		TargetType: model.ForwardTargetInbound, TargetServerID: lax.ID, TargetInboundID: laxTLS.ID,
		Outbound: "direct", Enabled: true,
	}
	fwdCustom := model.PortForward{
		ServerID: tyo.ID, Tag: "fwd-custom", ListenPort: 10080, Network: "tcp",
		TargetType: model.ForwardTargetCustom, TargetAddress: "203.0.113.10", TargetPort: 80,
		Outbound: "direct", Enabled: true,
	}
	mustCreate(t, db, &fwdReality)
	mustCreate(t, db, &fwdTLS)
	mustCreate(t, db, &fwdCustom)

	alice := model.User{
		Email: "alice", Password: "x", Role: model.RoleUser, Enabled: true,
		SubToken: "alice-fwd-sub", ProxyToken: "alice-fwd-proxy",
	}
	mustCreate(t, db, &alice)

	app, router := portForwardTestRouter(db)
	router.GET("/servers", app.listServers)
	router.GET("/servers/:id/node-formats", app.serverNodeFormats)
	router.GET("/users", app.listUsers)
	router.GET("/users/:id/access", app.getUserAccess)
	router.PUT("/users/:id/access", app.updateUserAccess)
	router.GET("/sub/:token", app.handleSubscription)

	putAccess := func(body any) *httptest.ResponseRecorder {
		t.Helper()
		raw, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		res := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPut, fmt.Sprintf("/users/%d/access", alice.ID), bytes.NewReader(raw))
		req.Header.Set("Content-Type", "application/json")
		router.ServeHTTP(res, req)
		return res
	}

	// Custom-address port forwards cannot be assigned as user proxy nodes.
	if res := putAccess(map[string]any{
		"server_ids":      []uint{},
		"inbound_ids":     []uint{},
		"forward_ids":     []uint{fwdCustom.ID},
		"custom_node_ids": []uint{},
	}); res.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for custom forward assignment, got %d (%s)", res.Code, res.Body.String())
	}

	// Assign a single forwarded node under TYO -> LAX without exposing TYO/LAX directly.
	if res := putAccess(map[string]any{
		"server_ids":      []uint{},
		"inbound_ids":     []uint{},
		"forward_ids":     []uint{fwdReality.ID},
		"custom_node_ids": []uint{},
	}); res.Code != http.StatusOK {
		t.Fatalf("assign single forward: %d %s", res.Code, res.Body.String())
	}

	if err := db.First(&alice, alice.ID).Error; err != nil {
		t.Fatal(err)
	}
	singleNodes, err := app.gatherNodesWithError(&alice)
	if err != nil {
		t.Fatalf("gatherNodesWithError(single): %v", err)
	}
	if len(singleNodes) != 1 {
		t.Fatalf("expected 1 forwarded node, got %d: %+v", len(singleNodes), singleNodes)
	}
	if singleNodes[0].name != "TYO -> LAX" || singleNodes[0].server != "tyo.example.com" || singleNodes[0].port != 33456 {
		t.Fatalf("unexpected single forwarded node: name=%q server=%q port=%d", singleNodes[0].name, singleNodes[0].server, singleNodes[0].port)
	}
	if wantUUID := proxyIdentity(&alice, laxReality.ID).UUID; singleNodes[0].user.UUID != wantUUID {
		t.Fatalf("expected target inbound derived uuid %q, got %q", wantUUID, singleNodes[0].user.UUID)
	}

	// Verify target LAX inbound grants Alice's credential via forward without direct ServerIDs/InboundIDs.
	var realitySettings singbox.InboundSettings
	if err := json.Unmarshal(laxReality.Settings, &realitySettings); err != nil {
		t.Fatal(err)
	}
	proxyUsers, err := proxyUsersForInbound(db, &laxReality, realitySettings)
	if err != nil {
		t.Fatalf("proxyUsersForInbound: %v", err)
	}
	wantTrafficName := proxyTrafficName(&alice)
	foundAlice := false
	for _, pu := range proxyUsers {
		if pu.Name == wantTrafficName && pu.UUID == proxyIdentity(&alice, laxReality.ID).UUID {
			foundAlice = true
		}
	}
	if !foundAlice {
		t.Fatalf("expected Alice credential provisioned on target LAX inbound, got %+v", proxyUsers)
	}

	syncIDs, err := subscriptionSyncServerIDs(db, &alice)
	if err != nil || !reflect.DeepEqual(syncIDs, []uint{lax.ID}) {
		t.Fatalf("subscriptionSyncServerIDs = %v, err=%v; want [%d]", syncIDs, err, lax.ID)
	}
	provIDs, err := userProvisionedServerIDs(db, &alice)
	if err != nil || !reflect.DeepEqual(provIDs, []uint{lax.ID}) {
		t.Fatalf("userProvisionedServerIDs = %v, err=%v; want [%d]", provIDs, err, lax.ID)
	}

	// Assign both forwards to test disambiguation, TLS SNI inference, custom rename & mixed order.
	res := putAccess(map[string]any{
		"server_ids":      []uint{},
		"inbound_ids":     []uint{},
		"forward_ids":     []uint{fwdReality.ID, fwdTLS.ID},
		"custom_node_ids": []uint{},
		"node_order": []userNodeOrderItem{
			{NodeType: userNodeTypeForward, NodeID: fwdTLS.ID, CustomName: "Tokyo -> LA TLS"},
			{NodeType: userNodeTypeForward, NodeID: fwdReality.ID},
		},
	})
	if res.Code != http.StatusOK {
		t.Fatalf("assign two forwards with order: %d %s", res.Code, res.Body.String())
	}

	if err := db.First(&alice, alice.ID).Error; err != nil {
		t.Fatal(err)
	}
	nodes, err := app.gatherNodesWithError(&alice)
	if err != nil {
		t.Fatalf("gatherNodesWithError(two): %v", err)
	}
	if len(nodes) != 2 {
		t.Fatalf("expected 2 forwarded nodes, got %d", len(nodes))
	}
	if nodes[0].name != "Tokyo -> LA TLS" || nodes[0].server != "tyo.example.com" || nodes[0].port != 3443 {
		t.Fatalf("unexpected first ordered forward node: %+v", nodes[0])
	}
	if nodes[0].settings.TLS.ServerName != "lax.example.com" {
		t.Fatalf("expected TLS SNI to fall back to target host lax.example.com, got %q", nodes[0].settings.TLS.ServerName)
	}
	if nodes[1].name != "TYO -> LAX (lax-reality :33456)" || nodes[1].server != "tyo.example.com" || nodes[1].port != 33456 {
		t.Fatalf("unexpected second disambiguated forward node: name=%q server=%q port=%d", nodes[1].name, nodes[1].server, nodes[1].port)
	}

	// Verify /servers preloads hydrated forwards, /users counts node_count, and /servers/:id/node-formats exports forwarded nodes.
	srvRec := httptest.NewRecorder()
	router.ServeHTTP(srvRec, httptest.NewRequest(http.MethodGet, "/servers", nil))
	if srvRec.Code != http.StatusOK || !strings.Contains(srvRec.Body.String(), `"target_server_name":"LAX"`) {
		t.Fatalf("listServers missing hydrated forwards: code=%d body=%s", srvRec.Code, srvRec.Body.String())
	}

	usersRec := httptest.NewRecorder()
	router.ServeHTTP(usersRec, httptest.NewRequest(http.MethodGet, "/users", nil))
	var usersPayload struct {
		Users []userListItem `json:"users"`
	}
	if err := json.Unmarshal(usersRec.Body.Bytes(), &usersPayload); err != nil || len(usersPayload.Users) != 1 {
		t.Fatalf("listUsers decode failed: %v %s", err, usersRec.Body.String())
	}
	if usersPayload.Users[0].NodeCount != 2 {
		t.Fatalf("expected NodeCount == 2 with forwarded nodes, got %d", usersPayload.Users[0].NodeCount)
	}

	fmtRec := httptest.NewRecorder()
	router.ServeHTTP(fmtRec, httptest.NewRequest(http.MethodGet, fmt.Sprintf("/servers/%d/node-formats?user_id=%d", tyo.ID, alice.ID), nil))
	if fmtRec.Code != http.StatusOK || !strings.Contains(fmtRec.Body.String(), "tyo.example.com") || !strings.Contains(fmtRec.Body.String(), `"33456"`) && !strings.Contains(fmtRec.Body.String(), "33456") {
		t.Fatalf("serverNodeFormats missing forwarded nodes on TYO: code=%d body=%s", fmtRec.Code, fmtRec.Body.String())
	}

	// Disabling fwdReality removes Alice from LAX proxy users and subscription output.
	if err := db.Model(&fwdReality).Update("enabled", false).Error; err != nil {
		t.Fatal(err)
	}
	proxyUsersAfterDisable, err := proxyUsersForInbound(db, &laxReality, realitySettings)
	if err != nil {
		t.Fatal(err)
	}
	for _, pu := range proxyUsersAfterDisable {
		if pu.Name == wantTrafficName {
			t.Fatalf("disabled forward should stop provisioning Alice on target inbound")
		}
	}
}

func TestForwardedUserNodeCleanupOnDeleteUpdateAndImport(t *testing.T) {
	db := testDB(t)
	relay := model.Server{Name: "Relay", Address: "relay.example.com", AgentToken: "relay-tok", ConfigMode: model.ConfigModeManaged}
	target := model.Server{Name: "Target", Address: "target.example.com", AgentToken: "target-tok", ConfigMode: model.ConfigModeManaged}
	mustCreate(t, db, &relay)
	mustCreate(t, db, &target)

	tib := model.Inbound{
		ServerID: target.ID, Tag: "t-vless", Type: model.InboundVLESS, ListenPort: 23456, Enabled: true,
		Settings: model.JSONText(`{"multi_user":true}`),
	}
	mustCreate(t, db, &tib)

	fwd1 := model.PortForward{
		ServerID: relay.ID, Tag: "fwd-1", ListenPort: 30001, Network: "tcp",
		TargetType: model.ForwardTargetInbound, TargetServerID: target.ID, TargetInboundID: tib.ID,
		Outbound: "direct", Enabled: true,
	}
	fwd2 := model.PortForward{
		ServerID: relay.ID, Tag: "fwd-2", ListenPort: 30002, Network: "tcp",
		TargetType: model.ForwardTargetInbound, TargetServerID: target.ID, TargetInboundID: tib.ID,
		Outbound: "direct", Enabled: true,
	}
	fwd3 := model.PortForward{
		ServerID: relay.ID, Tag: "fwd-3", ListenPort: 30003, Network: "tcp",
		TargetType: model.ForwardTargetInbound, TargetServerID: target.ID, TargetInboundID: tib.ID,
		Outbound: "direct", Enabled: true,
	}
	mustCreate(t, db, &fwd1)
	mustCreate(t, db, &fwd2)
	mustCreate(t, db, &fwd3)

	alice := model.User{
		Email: "alice-cleanup", Password: "x", Role: model.RoleUser, Enabled: true,
		ForwardIDs: []uint{fwd1.ID, fwd2.ID, fwd3.ID}, SubToken: "cleanup-sub", ProxyToken: "cleanup-proxy",
	}
	mustCreate(t, db, &alice)
	mustCreate(t, db, &model.UserNodeOrder{UserID: alice.ID, NodeType: userNodeTypeForward, NodeID: fwd1.ID, SortOrder: 0})
	mustCreate(t, db, &model.UserNodeOrder{UserID: alice.ID, NodeType: userNodeTypeForward, NodeID: fwd2.ID, SortOrder: 1})
	mustCreate(t, db, &model.UserNodeOrder{UserID: alice.ID, NodeType: userNodeTypeForward, NodeID: fwd3.ID, SortOrder: 2})

	_, router := portForwardTestRouter(db)

	// 1. Changing fwd1 from inbound -> custom cleans user assignment & order.
	updateBody := `{"tag":"fwd-1","listen_port":30001,"network":"tcp","target_type":"custom","target_address":"203.0.113.10","target_port":80,"outbound":"direct","enabled":true}`
	req := httptest.NewRequest(http.MethodPut, fmt.Sprintf("/servers/%d/forwards/%d", relay.ID, fwd1.ID), strings.NewReader(updateBody))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("updateForward custom: %d %s", rec.Code, rec.Body.String())
	}
	if err := db.First(&alice, alice.ID).Error; err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(alice.ForwardIDs, []uint{fwd2.ID, fwd3.ID}) {
		t.Fatalf("expected fwd1 stripped after custom conversion, got %v", alice.ForwardIDs)
	}

	// 2. Deleting fwd2 cleans user assignment & order.
	delFwdRec := httptest.NewRecorder()
	router.ServeHTTP(delFwdRec, httptest.NewRequest(http.MethodDelete, fmt.Sprintf("/servers/%d/forwards/%d", relay.ID, fwd2.ID), nil))
	if delFwdRec.Code != http.StatusOK {
		t.Fatalf("deleteForward: %d %s", delFwdRec.Code, delFwdRec.Body.String())
	}
	if err := db.First(&alice, alice.ID).Error; err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(alice.ForwardIDs, []uint{fwd3.ID}) {
		t.Fatalf("expected fwd2 stripped after deleteForward, got %v", alice.ForwardIDs)
	}

	// 3. Deleting relay server cleans fwd3 assignment & UserNodeOrder rows.
	delSrvRec := httptest.NewRecorder()
	router.ServeHTTP(delSrvRec, httptest.NewRequest(http.MethodDelete, fmt.Sprintf("/servers/%d", relay.ID), nil))
	if delSrvRec.Code != http.StatusOK {
		t.Fatalf("deleteServer: %d %s", delSrvRec.Code, delSrvRec.Body.String())
	}
	if err := db.First(&alice, alice.ID).Error; err != nil {
		t.Fatal(err)
	}
	if len(alice.ForwardIDs) != 0 {
		t.Fatalf("expected all relay forwards stripped after deleteServer, got %v", alice.ForwardIDs)
	}
	var orderCount int64
	if err := db.Model(&model.UserNodeOrder{}).Where("user_id = ? AND node_type = ?", alice.ID, userNodeTypeForward).Count(&orderCount).Error; err != nil {
		t.Fatal(err)
	}
	if orderCount != 0 {
		t.Fatalf("expected UserNodeOrder forward rows deleted, remaining=%d", orderCount)
	}

	// 4. syncImportedForwards dropping or downgrading a forward cleans user assignments & order.
	impRelay := model.Server{Name: "ImpRelay", Address: "imprelay.example.com", AgentToken: "imprelay-tok", ConfigMode: model.ConfigModeManaged}
	mustCreate(t, db, &impRelay)
	impFwdKeep := model.PortForward{
		ServerID: impRelay.ID, Tag: "imp-keep", ListenPort: 31001, Network: "tcp",
		TargetType: model.ForwardTargetInbound, TargetServerID: target.ID, TargetInboundID: tib.ID,
		Outbound: "direct", Enabled: true,
	}
	impFwdDowngrade := model.PortForward{
		ServerID: impRelay.ID, Tag: "imp-downgrade", ListenPort: 31002, Network: "tcp",
		TargetType: model.ForwardTargetInbound, TargetServerID: target.ID, TargetInboundID: tib.ID,
		Outbound: "direct", Enabled: true,
	}
	impFwdDrop := model.PortForward{
		ServerID: impRelay.ID, Tag: "imp-drop", ListenPort: 31003, Network: "tcp",
		TargetType: model.ForwardTargetInbound, TargetServerID: target.ID, TargetInboundID: tib.ID,
		Outbound: "direct", Enabled: true,
	}
	mustCreate(t, db, &impFwdKeep)
	mustCreate(t, db, &impFwdDowngrade)
	mustCreate(t, db, &impFwdDrop)

	bob := model.User{
		Email: "bob-import-cleanup", Password: "x", Role: model.RoleUser, Enabled: true,
		ForwardIDs: []uint{impFwdKeep.ID, impFwdDowngrade.ID, impFwdDrop.ID},
		SubToken:   "bob-imp-sub", ProxyToken: "bob-imp-proxy",
	}
	mustCreate(t, db, &bob)
	mustCreate(t, db, &model.UserNodeOrder{UserID: bob.ID, NodeType: userNodeTypeForward, NodeID: impFwdKeep.ID, SortOrder: 0})
	mustCreate(t, db, &model.UserNodeOrder{UserID: bob.ID, NodeType: userNodeTypeForward, NodeID: impFwdDowngrade.ID, SortOrder: 1})
	mustCreate(t, db, &model.UserNodeOrder{UserID: bob.ID, NodeType: userNodeTypeForward, NodeID: impFwdDrop.ID, SortOrder: 2})

	parsed := []singbox.ForwardInput{
		{Tag: "imp-keep", ListenPort: 31001, Network: "tcp", OverrideAddress: "target.example.com", OverridePort: 23456, Outbound: "direct"},
		{Tag: "imp-downgrade", ListenPort: 31002, Network: "tcp", OverrideAddress: "203.0.113.99", OverridePort: 8443, Outbound: "direct"},
	}
	affectedTargets, err := syncImportedForwards(db, impRelay.ID, model.ConfigModeManaged, parsed)
	if err != nil {
		t.Fatalf("syncImportedForwards: %v", err)
	}
	if !reflect.DeepEqual(affectedTargets, []uint{target.ID}) {
		t.Fatalf("expected affected target server [%d], got %v", target.ID, affectedTargets)
	}
	if err := db.First(&bob, bob.ID).Error; err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(bob.ForwardIDs, []uint{impFwdKeep.ID}) {
		t.Fatalf("expected only impFwdKeep retained after import sync, got %v", bob.ForwardIDs)
	}
	var bobOrder []model.UserNodeOrder
	if err := db.Where("user_id = ? AND node_type = ?", bob.ID, userNodeTypeForward).Find(&bobOrder).Error; err != nil {
		t.Fatal(err)
	}
	if len(bobOrder) != 1 || bobOrder[0].NodeID != impFwdKeep.ID {
		t.Fatalf("expected only impFwdKeep order retained, got %+v", bobOrder)
	}
}
