package panel

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/tokinx/singbox-panel/internal/domain/model"
)

func TestDeleteServerPreservesExplicitAndWholeServerAccess(t *testing.T) {
	db := testDB(t)
	servers := []model.Server{{Name: "removed", AgentToken: "remove"}, {Name: "kept", AgentToken: "keep"}}
	mustCreate(t, db, &servers)
	inbounds := []model.Inbound{
		{ServerID: servers[0].ID, Tag: "removed", Type: model.InboundVLESS, ListenPort: 443},
		{ServerID: servers[1].ID, Tag: "assigned", Type: model.InboundVLESS, ListenPort: 443},
		{ServerID: servers[1].ID, Tag: "unassigned", Type: model.InboundVLESS, ListenPort: 8443},
	}
	mustCreate(t, db, &inbounds)
	users := []model.User{
		{Email: "only-removed", SubToken: "only-removed", ServerIDs: []uint{servers[0].ID, servers[1].ID}, InboundIDs: []uint{inbounds[0].ID}},
		{Email: "explicit", SubToken: "explicit", ServerIDs: []uint{servers[0].ID, servers[1].ID}, InboundIDs: []uint{inbounds[0].ID, inbounds[1].ID}},
		{Email: "whole-server", SubToken: "whole-server", ServerIDs: []uint{servers[0].ID, servers[1].ID}},
		{Email: "unrelated", SubToken: "unrelated", ServerIDs: []uint{servers[1].ID}, InboundIDs: []uint{inbounds[2].ID}},
	}
	mustCreate(t, db, &users)
	app := &App{db: db}
	router := gin.New()
	router.DELETE("/servers/:id", app.deleteServer)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodDelete, fmt.Sprintf("/servers/%d", servers[0].ID), nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("delete status %d: %s", recorder.Code, recorder.Body.String())
	}
	for i := range users {
		if err := db.First(&users[i], users[i].ID).Error; err != nil {
			t.Fatal(err)
		}
		if users[i].HasInbound(servers[0].ID, inbounds[0].ID) {
			t.Fatalf("%s still has the deleted server", users[i].Email)
		}
	}
	if len(users[0].ServerIDs) != 0 || len(users[0].InboundIDs) != 0 || users[0].HasInbound(servers[1].ID, inbounds[1].ID) {
		t.Fatalf("removing final explicit grant expanded access: %+v", users[0])
	}
	if !users[1].HasInbound(servers[1].ID, inbounds[1].ID) || users[1].HasInbound(servers[1].ID, inbounds[2].ID) {
		t.Fatal("remaining explicit grant changed scope")
	}
	if !users[2].HasInbound(servers[1].ID, inbounds[1].ID) || !users[2].HasInbound(servers[1].ID, inbounds[2].ID) {
		t.Fatal("whole-server grant was narrowed")
	}
	if users[3].HasInbound(servers[1].ID, inbounds[1].ID) || !users[3].HasInbound(servers[1].ID, inbounds[2].ID) {
		t.Fatal("unrelated explicit grant changed")
	}
}

func TestDeleteInboundAndServerPruneIPv6Assignments(t *testing.T) {
	db := testDB(t)
	servers := []model.Server{
		{Name: "removed-v6", AgentToken: "rem-v6", IPv6Mode: model.ServerIPv6ModeAuto, PublicIPv6: "2001:db8::1"},
		{Name: "kept-v6", AgentToken: "keep-v6", ConfigMode: model.ConfigModeManaged, IPv6Mode: model.ServerIPv6ModeAuto, PublicIPv6: "2001:db8::2"},
	}
	mustCreate(t, db, &servers)
	inbounds := []model.Inbound{
		{ServerID: servers[0].ID, Tag: "rem-in", Type: model.InboundVLESS, ListenPort: 25001},
		{ServerID: servers[1].ID, Tag: "kept-rem-in", Type: model.InboundVLESS, ListenPort: 25002},
		{ServerID: servers[1].ID, Tag: "kept-in", Type: model.InboundTrojan, ListenPort: 25003},
	}
	mustCreate(t, db, &inbounds)

	v6OnlyOnDeletedServer := model.User{
		Email: "v6-only-deleted", SubToken: "v6-only-del",
		ServerIDs: []uint{servers[0].ID, servers[1].ID}, InboundIPv6IDs: []uint{inbounds[0].ID},
	}
	v6AcrossServers := model.User{
		Email: "v6-across", SubToken: "v6-across",
		ServerIDs: []uint{servers[0].ID, servers[1].ID}, InboundIDs: []uint{inbounds[2].ID},
		InboundIPv6IDs: []uint{inbounds[0].ID, inbounds[1].ID, inbounds[2].ID},
	}
	mustCreate(t, db, &v6OnlyOnDeletedServer)
	mustCreate(t, db, &v6AcrossServers)

	if err := saveUserNodeOrder(db, v6AcrossServers.ID, []userNodeOrderItem{
		{NodeType: userNodeTypeManagedIPv6, NodeID: inbounds[0].ID},
		{NodeType: userNodeTypeManagedIPv6, NodeID: inbounds[1].ID},
		{NodeType: userNodeTypeManaged, NodeID: inbounds[2].ID},
		{NodeType: userNodeTypeManagedIPv6, NodeID: inbounds[2].ID},
	}); err != nil {
		t.Fatal(err)
	}

	hub := NewHub(db)
	app := &App{db: db, hub: hub, orch: testOrchestrator(t, db, hub)}
	router := gin.New()
	router.DELETE("/servers/:id", app.deleteServer)
	router.DELETE("/servers/:id/inbounds/:inboundID", app.deleteInbound)

	// Delete servers[0]: v6OnlyOnDeletedServer must end up empty (no wildcard leak!).
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodDelete, fmt.Sprintf("/servers/%d", servers[0].ID), nil))
	if w.Code != http.StatusOK {
		t.Fatalf("delete server status %d: %s", w.Code, w.Body.String())
	}
	if err := db.First(&v6OnlyOnDeletedServer, v6OnlyOnDeletedServer.ID).Error; err != nil {
		t.Fatal(err)
	}
	if len(v6OnlyOnDeletedServer.ServerIDs) != 0 || len(v6OnlyOnDeletedServer.InboundIPv6IDs) != 0 ||
		v6OnlyOnDeletedServer.HasInbound(servers[1].ID, inbounds[1].ID) {
		t.Fatalf("v6-only server deletion leaked wildcard grant: %+v", v6OnlyOnDeletedServer)
	}

	// Delete inbounds[1] on kept server: prune InboundIPv6IDs + UserNodeOrder for managed_ipv6.
	w = httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodDelete, fmt.Sprintf("/servers/%d/inbounds/%d", servers[1].ID, inbounds[1].ID), nil))
	if w.Code != http.StatusOK {
		t.Fatalf("delete inbound status %d: %s", w.Code, w.Body.String())
	}
	if err := db.First(&v6AcrossServers, v6AcrossServers.ID).Error; err != nil {
		t.Fatal(err)
	}
	if fmt.Sprint(v6AcrossServers.InboundIPv6IDs) != fmt.Sprint([]uint{inbounds[2].ID}) ||
		fmt.Sprint(v6AcrossServers.ServerIDs) != fmt.Sprint([]uint{servers[1].ID}) {
		t.Fatalf("after inbound deletion access = %+v", v6AcrossServers)
	}
	var orderCount int64
	if err := db.Model(&model.UserNodeOrder{}).Where("user_id = ? AND node_id IN ?", v6AcrossServers.ID, []uint{inbounds[0].ID, inbounds[1].ID}).Count(&orderCount).Error; err != nil {
		t.Fatal(err)
	}
	if orderCount != 0 {
		t.Fatalf("stale managed_ipv6 order rows remain: %d", orderCount)
	}
}
