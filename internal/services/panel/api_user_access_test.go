package panel

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/tokinx/singbox-panel/internal/domain/model"
)

func TestUserAccessReplacesAssignmentsAtomically(t *testing.T) {
	db := testDB(t)
	serverA := model.Server{Name: "A", AgentToken: "access-a"}
	serverB := model.Server{Name: "B", AgentToken: "access-b"}
	if err := db.Create(&serverA).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&serverB).Error; err != nil {
		t.Fatal(err)
	}
	inbounds := []model.Inbound{
		{ServerID: serverA.ID, Tag: "a1", Type: model.InboundVLESS, ListenPort: 10001, Enabled: true},
		{ServerID: serverA.ID, Tag: "a2", Type: model.InboundTrojan, ListenPort: 10002, Enabled: true},
		{ServerID: serverB.ID, Tag: "b1", Type: model.InboundShadowsocks, ListenPort: 10003, Enabled: true},
	}
	if err := db.Create(&inbounds).Error; err != nil {
		t.Fatal(err)
	}
	user := model.User{
		Email: "access-user", Password: "unused", Role: model.RoleUser, Enabled: true,
		ServerIDs: []uint{serverA.ID}, InboundIDs: []uint{}, SubToken: "access-user-token",
	}
	other := model.User{
		Email: "future-user", Password: "unused", Role: model.RoleUser, Enabled: true,
		SubToken: "future-user-token",
	}
	if err := db.Create(&user).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&other).Error; err != nil {
		t.Fatal(err)
	}
	globalNode := model.CustomNode{Name: "global", Link: "socks5://127.0.0.1:1080#global", AllUsers: true, Enabled: true}
	scopedNode := model.CustomNode{Name: "scoped", Link: "socks5://127.0.0.1:1081#scoped", AllUsers: false, Enabled: true}
	if err := db.Create(&globalNode).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&scopedNode).Error; err != nil {
		t.Fatal(err)
	}

	legacy, err := effectiveUserAccess(db, &user)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := fmt.Sprint(legacy.InboundIDs), fmt.Sprint([]uint{inbounds[0].ID, inbounds[1].ID}); got != want {
		t.Fatalf("legacy wildcard inbounds = %s; want %s", got, want)
	}
	if got, want := fmt.Sprint(legacy.CustomNodeIDs), fmt.Sprint([]uint{globalNode.ID}); got != want {
		t.Fatalf("initial custom nodes = %s; want %s", got, want)
	}

	gin.SetMode(gin.TestMode)
	app := &App{db: db, orch: NewOrchestrator(db, NewHub(db))}
	router := gin.New()
	router.PUT("/users/:id/access", app.updateUserAccess)

	put := func(body string) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(http.MethodPut, fmt.Sprintf("/users/%d/access", user.ID), bytes.NewBufferString(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		return w
	}

	validBody, err := json.Marshal(map[string]any{
		"inbound_ids":     []uint{inbounds[2].ID, inbounds[0].ID, inbounds[0].ID},
		"custom_node_ids": []uint{scopedNode.ID},
		"node_order": []map[string]any{
			{"node_type": "custom", "node_id": scopedNode.ID},
			{"node_type": "managed", "node_id": inbounds[2].ID},
			{"node_type": "managed", "node_id": inbounds[0].ID},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if w := put(string(validBody)); w.Code != http.StatusOK {
		t.Fatalf("update status = %d, body=%s", w.Code, w.Body.String())
	}
	if err := db.First(&user, user.ID).Error; err != nil {
		t.Fatal(err)
	}
	if got, want := fmt.Sprint(user.ServerIDs), fmt.Sprint([]uint{serverA.ID, serverB.ID}); got != want {
		t.Fatalf("server ids = %s; want %s", got, want)
	}
	if got, want := fmt.Sprint(user.InboundIDs), fmt.Sprint([]uint{inbounds[0].ID, inbounds[2].ID}); got != want {
		t.Fatalf("inbound ids = %s; want %s", got, want)
	}
	if err := db.First(&globalNode, globalNode.ID).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.First(&scopedNode, scopedNode.ID).Error; err != nil {
		t.Fatal(err)
	}
	if globalNode.HasUser(user.ID) || !globalNode.HasUser(other.ID) {
		t.Fatalf("global audience exclusion is wrong: %+v", globalNode.ExcludedUserIDs)
	}
	if !scopedNode.HasUser(user.ID) || scopedNode.HasUser(other.ID) {
		t.Fatalf("scoped audience is wrong: %+v", scopedNode.UserIDs)
	}
	var savedOrder []model.UserNodeOrder
	if err := db.Where("user_id = ?", user.ID).Order("sort_order").Find(&savedOrder).Error; err != nil {
		t.Fatal(err)
	}
	if len(savedOrder) != 3 || savedOrder[0].NodeType != "custom" || savedOrder[0].NodeID != scopedNode.ID ||
		savedOrder[1].NodeType != "managed" || savedOrder[1].NodeID != inbounds[2].ID {
		t.Fatalf("saved node order = %+v", savedOrder)
	}
	access, err := effectiveUserAccess(db, &user)
	if err != nil {
		t.Fatal(err)
	}
	if len(access.NodeOrder) != 3 || access.NodeOrder[0].NodeType != "custom" {
		t.Fatalf("effective node order = %+v", access.NodeOrder)
	}
	invalidOrderBody, err := json.Marshal(map[string]any{
		"inbound_ids":     []uint{inbounds[2].ID, inbounds[0].ID},
		"custom_node_ids": []uint{scopedNode.ID},
		"node_order": []map[string]any{
			{"node_type": "custom", "node_id": scopedNode.ID},
			{"node_type": "managed", "node_id": inbounds[2].ID},
			{"node_type": "managed", "node_id": inbounds[2].ID},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if w := put(string(invalidOrderBody)); w.Code != http.StatusBadRequest {
		t.Fatalf("invalid order status = %d, body=%s", w.Code, w.Body.String())
	}
	var orderAfterInvalid []model.UserNodeOrder
	if err := db.Where("user_id = ?", user.ID).Order("sort_order").Find(&orderAfterInvalid).Error; err != nil {
		t.Fatal(err)
	}
	if len(orderAfterInvalid) != len(savedOrder) || orderAfterInvalid[0].NodeType != savedOrder[0].NodeType || orderAfterInvalid[0].NodeID != savedOrder[0].NodeID {
		t.Fatalf("invalid request changed node order: before=%+v after=%+v", savedOrder, orderAfterInvalid)
	}

	beforeServers := fmt.Sprint(user.ServerIDs)
	beforeInbounds := fmt.Sprint(user.InboundIDs)
	invalidBody, err := json.Marshal(map[string]any{
		"inbound_ids":     []uint{inbounds[1].ID},
		"custom_node_ids": []uint{scopedNode.ID + globalNode.ID + 9999},
	})
	if err != nil {
		t.Fatal(err)
	}
	if w := put(string(invalidBody)); w.Code != http.StatusBadRequest {
		t.Fatalf("invalid update status = %d, body=%s", w.Code, w.Body.String())
	}
	if err := db.First(&user, user.ID).Error; err != nil {
		t.Fatal(err)
	}
	if fmt.Sprint(user.ServerIDs) != beforeServers || fmt.Sprint(user.InboundIDs) != beforeInbounds {
		t.Fatalf("invalid request changed managed access: servers=%v inbounds=%v", user.ServerIDs, user.InboundIDs)
	}
}

func TestUserAccessPreservesServerWideGrant(t *testing.T) {
	db := testDB(t)
	server := model.Server{Name: "wildcard", AgentToken: "wildcard-agent"}
	if err := db.Create(&server).Error; err != nil {
		t.Fatal(err)
	}
	inbounds := []model.Inbound{
		{ServerID: server.ID, Tag: "first", Type: model.InboundVLESS, ListenPort: 11001, Enabled: true},
		{ServerID: server.ID, Tag: "second", Type: model.InboundTrojan, ListenPort: 11002, Enabled: true},
	}
	if err := db.Create(&inbounds).Error; err != nil {
		t.Fatal(err)
	}
	user := model.User{
		Email: "wildcard-user", Password: "unused", Role: model.RoleUser, Enabled: true,
		ServerIDs: []uint{server.ID}, InboundIDs: []uint{}, SubToken: "wildcard-user-token",
	}
	if err := db.Create(&user).Error; err != nil {
		t.Fatal(err)
	}

	access, err := effectiveUserAccess(db, &user)
	if err != nil {
		t.Fatal(err)
	}
	if !access.ServerWide || fmt.Sprint(access.ServerIDs) != fmt.Sprint([]uint{server.ID}) {
		t.Fatalf("server-wide response = %+v", access)
	}

	gin.SetMode(gin.TestMode)
	app := &App{db: db, orch: NewOrchestrator(db, NewHub(db))}
	router := gin.New()
	router.PUT("/users/:id/access", app.updateUserAccess)
	body, err := json.Marshal(map[string]any{
		"server_ids":      []uint{server.ID},
		"inbound_ids":     []uint{},
		"custom_node_ids": []uint{},
		"node_order": []map[string]any{
			{"node_type": "managed", "node_id": inbounds[1].ID},
			{"node_type": "managed", "node_id": inbounds[0].ID},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPut, fmt.Sprintf("/users/%d/access", user.ID), bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	if err := db.First(&user, user.ID).Error; err != nil {
		t.Fatal(err)
	}
	if fmt.Sprint(user.ServerIDs) != fmt.Sprint([]uint{server.ID}) || len(user.InboundIDs) != 0 {
		t.Fatalf("wildcard grant was materialized: servers=%v inbounds=%v", user.ServerIDs, user.InboundIDs)
	}

	third := model.Inbound{ServerID: server.ID, Tag: "future", Type: model.InboundAnyTLS, ListenPort: 11003, Enabled: true}
	if err := db.Create(&third).Error; err != nil {
		t.Fatal(err)
	}
	if !user.HasInbound(server.ID, third.ID) {
		t.Fatal("server-wide grant did not include a newly-created inbound")
	}
}

func TestCustomNodeHasUser(t *testing.T) {
	for _, tc := range []struct {
		name string
		node model.CustomNode
		want bool
	}{
		{name: "empty audience", node: model.CustomNode{}, want: false},
		{name: "allow list", node: model.CustomNode{UserIDs: []uint{7}}, want: true},
		{name: "all users", node: model.CustomNode{AllUsers: true}, want: true},
		{name: "excluded from all", node: model.CustomNode{AllUsers: true, ExcludedUserIDs: []uint{7}}, want: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.node.HasUser(7); got != tc.want {
				t.Fatalf("HasUser(7) = %v; want %v", got, tc.want)
			}
		})
	}
}

func TestListUsersIncludesEffectiveNodeCount(t *testing.T) {
	db := testDB(t)
	server := model.Server{Name: "count-server", AgentToken: "count-token"}
	if err := db.Create(&server).Error; err != nil {
		t.Fatal(err)
	}
	inbounds := []model.Inbound{
		{ServerID: server.ID, Tag: "one", Type: model.InboundVLESS, ListenPort: 12001, Enabled: true},
		{ServerID: server.ID, Tag: "two", Type: model.InboundTrojan, ListenPort: 12002, Enabled: true},
	}
	if err := db.Create(&inbounds).Error; err != nil {
		t.Fatal(err)
	}
	wildcardUser := model.User{
		Email: "wildcard", Password: "unused", Role: model.RoleUser, Enabled: true,
		ServerIDs: []uint{server.ID}, SubToken: "count-wildcard-token",
	}
	explicitUser := model.User{
		Email: "explicit", Password: "unused", Role: model.RoleUser, Enabled: true,
		ServerIDs: []uint{server.ID}, InboundIDs: []uint{inbounds[0].ID}, SubToken: "count-explicit-token",
	}
	if err := db.Create(&wildcardUser).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&explicitUser).Error; err != nil {
		t.Fatal(err)
	}
	nodes := []model.CustomNode{
		{Name: "global", Link: "socks5://127.0.0.1:13001#global", AllUsers: true, Enabled: true, ExcludedUserIDs: []uint{explicitUser.ID}},
		{Name: "scoped", Link: "socks5://127.0.0.1:13002#scoped", UserIDs: []uint{wildcardUser.ID}, Enabled: true},
	}
	if err := db.Create(&nodes).Error; err != nil {
		t.Fatal(err)
	}

	gin.SetMode(gin.TestMode)
	app := &App{db: db}
	router := gin.New()
	router.GET("/users", app.listUsers)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/users", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("list status = %d, body=%s", w.Code, w.Body.String())
	}
	var response struct {
		Users []struct {
			ID        uint `json:"id"`
			NodeCount int  `json:"node_count"`
		} `json:"users"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	counts := make(map[uint]int, len(response.Users))
	for _, user := range response.Users {
		counts[user.ID] = user.NodeCount
	}
	if got, want := counts[wildcardUser.ID], 4; got != want {
		t.Fatalf("wildcard node_count = %d; want %d", got, want)
	}
	if got, want := counts[explicitUser.ID], 1; got != want {
		t.Fatalf("explicit node_count = %d; want %d", got, want)
	}
}

func TestUserAccessIPv6SplittingAndValidation(t *testing.T) {
	db := testDB(t)
	dualServer := model.Server{
		Name: "Dual", AgentToken: "dual-token", Address: "dual.example.com",
		IPv6Mode: model.ServerIPv6ModeAuto, PublicIPv6: "2001:db8::10",
	}
	v4OnlyServer := model.Server{Name: "V4Only", AgentToken: "v4-token", Address: "v4.example.com"}
	mustCreate(t, db, &dualServer)
	mustCreate(t, db, &v4OnlyServer)

	inbounds := []model.Inbound{
		{ServerID: dualServer.ID, Tag: "vless-dual", Type: model.InboundVLESS, ListenPort: 24001, Enabled: true},
		{ServerID: dualServer.ID, Tag: "trojan-dual", Type: model.InboundTrojan, ListenPort: 24002, Enabled: true},
		{ServerID: v4OnlyServer.ID, Tag: "vless-v4", Type: model.InboundVLESS, ListenPort: 24003, Enabled: true},
	}
	mustCreate(t, db, &inbounds)

	user := model.User{
		Email: "split-user", Password: "unused", Role: model.RoleUser, Enabled: true,
		SubToken: "split-user-sub",
	}
	mustCreate(t, db, &user)

	gin.SetMode(gin.TestMode)
	app := &App{db: db}
	router := gin.New()
	router.GET("/users", app.listUsers)
	router.GET("/users/:id/access", app.getUserAccess)
	router.PUT("/users/:id/access", app.updateUserAccess)

	// Reject IPv6 assignment for an inbound whose server has no IPv6 entry enabled.
	badBody, _ := json.Marshal(map[string]any{
		"server_ids":       []uint{},
		"inbound_ids":      []uint{inbounds[2].ID},
		"inbound_ipv6_ids": []uint{inbounds[2].ID},
		"custom_node_ids":  []uint{},
	})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodPut, fmt.Sprintf("/users/%d/access", user.ID), bytes.NewReader(badBody)))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 when server has no IPv6 entry, got %d body=%s", w.Code, w.Body.String())
	}

	// Assign dual-stack endpoint on inbound 0 plus v4-only on inbound 1, with independent names and order.
	body, _ := json.Marshal(map[string]any{
		"server_ids":       []uint{},
		"inbound_ids":      []uint{inbounds[0].ID, inbounds[1].ID},
		"inbound_ipv6_ids": []uint{inbounds[0].ID},
		"custom_node_ids":  []uint{},
		"node_order": []map[string]any{
			{"node_type": userNodeTypeManagedIPv6, "node_id": inbounds[0].ID, "custom_name": "Dual IPv6 First"},
			{"node_type": userNodeTypeManaged, "node_id": inbounds[1].ID},
			{"node_type": userNodeTypeManaged, "node_id": inbounds[0].ID, "custom_name": "Dual IPv4 Last"},
		},
	})
	w = httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodPut, fmt.Sprintf("/users/%d/access", user.ID), bytes.NewReader(body)))
	if w.Code != http.StatusOK {
		t.Fatalf("PUT dual-stack access = %d body=%s", w.Code, w.Body.String())
	}

	w = httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, fmt.Sprintf("/users/%d/access", user.ID), nil))
	if w.Code != http.StatusOK {
		t.Fatalf("GET dual-stack access = %d body=%s", w.Code, w.Body.String())
	}
	var gotResp struct {
		Access userAccessResp `json:"access"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &gotResp); err != nil {
		t.Fatal(err)
	}
	if gotResp.Access.ServerWide {
		t.Fatal("expected server_wide=false after explicit split assignment")
	}
	if fmt.Sprint(gotResp.Access.InboundIDs) != fmt.Sprint([]uint{inbounds[0].ID, inbounds[1].ID}) {
		t.Fatalf("inbound_ids = %v; want [%d %d]", gotResp.Access.InboundIDs, inbounds[0].ID, inbounds[1].ID)
	}
	if fmt.Sprint(gotResp.Access.InboundIPv6IDs) != fmt.Sprint([]uint{inbounds[0].ID}) {
		t.Fatalf("inbound_ipv6_ids = %v; want [%d]", gotResp.Access.InboundIPv6IDs, inbounds[0].ID)
	}
	if len(gotResp.Access.NodeOrder) != 3 ||
		gotResp.Access.NodeOrder[0].NodeType != userNodeTypeManagedIPv6 ||
		gotResp.Access.NodeOrder[0].CustomName != "Dual IPv6 First" ||
		gotResp.Access.NodeOrder[2].CustomName != "Dual IPv4 Last" {
		t.Fatalf("unexpected node_order: %+v", gotResp.Access.NodeOrder)
	}

	// Switch to v6-only and confirm wildcard expansion does not kick in and node_count = 1.
	v6OnlyBody, _ := json.Marshal(map[string]any{
		"server_ids":       []uint{},
		"inbound_ids":      []uint{},
		"inbound_ipv6_ids": []uint{inbounds[0].ID},
		"custom_node_ids":  []uint{},
		"node_order": []map[string]any{
			{"node_type": userNodeTypeManagedIPv6, "node_id": inbounds[0].ID},
		},
	})
	w = httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodPut, fmt.Sprintf("/users/%d/access", user.ID), bytes.NewReader(v6OnlyBody)))
	if w.Code != http.StatusOK {
		t.Fatalf("PUT v6-only access = %d body=%s", w.Code, w.Body.String())
	}

	w = httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, fmt.Sprintf("/users/%d/access", user.ID), nil))
	var v6OnlyResp struct {
		Access userAccessResp `json:"access"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &v6OnlyResp); err != nil {
		t.Fatal(err)
	}
	if v6OnlyResp.Access.ServerWide || len(v6OnlyResp.Access.InboundIDs) != 0 || fmt.Sprint(v6OnlyResp.Access.InboundIPv6IDs) != fmt.Sprint([]uint{inbounds[0].ID}) {
		t.Fatalf("v6-only get access unexpected: %+v", v6OnlyResp.Access)
	}

	w = httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/users", nil))
	var usersResp struct {
		Users []struct {
			ID        uint `json:"id"`
			NodeCount int  `json:"node_count"`
		} `json:"users"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &usersResp); err != nil {
		t.Fatal(err)
	}
	if len(usersResp.Users) != 1 || usersResp.Users[0].NodeCount != 1 {
		t.Fatalf("v6-only node_count = %+v; want 1", usersResp.Users)
	}
}
