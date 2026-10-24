package panel

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/hann0w0/singbox-panel/internal/domain/model"
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
