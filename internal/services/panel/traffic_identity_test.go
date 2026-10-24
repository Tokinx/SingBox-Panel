package panel

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/hann0w0/singbox-panel/internal/domain/model"
	"github.com/hann0w0/singbox-panel/internal/domain/protocol"
	"github.com/hann0w0/singbox-panel/internal/domain/singbox"
)

func TestRenamedTrafficCannotMoveToReusedUsername(t *testing.T) {
	f := newUserTrafficFixture(t, model.ConfigModeManaged)
	oldCredentials := proxyIdentity(&f.alice, f.multi.ID)
	oldTrafficName := proxyTrafficName(&f.alice)
	app := &App{db: f.db}
	router := gin.New()
	router.PUT("/users/:id", app.updateUser)
	req := httptest.NewRequest(http.MethodPut, fmt.Sprintf("/users/%d", f.alice.ID), bytes.NewBufferString(`{"email":"bob"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("rename: %d %s", rec.Code, rec.Body.String())
	}
	if err := f.db.First(&f.alice, f.alice.ID).Error; err != nil {
		t.Fatal(err)
	}
	newCredentials := proxyIdentity(&f.alice, f.multi.ID)
	if newCredentials.UUID != oldCredentials.UUID || newCredentials.Password != oldCredentials.Password || proxyTrafficName(&f.alice) != oldTrafficName {
		t.Fatal("rename changed immutable traffic identity or client credentials")
	}
	name := "alice@example.com"
	reused := model.User{Email: "Alice@Example.com", EmailNormalized: &name, Role: model.RoleUser, Enabled: true, SubToken: "reused-sub", ProxyToken: "reused-proxy", ServerIDs: []uint{f.server.ID}}
	mustCreate(t, f.db, &reused)
	identities, err := proxyUsersForInbound(f.db, &f.multi, singbox.InboundSettings{MultiUser: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(identities) != 3 || identities[0].Name != oldTrafficName || identities[2].Name != proxyTrafficName(&reused) {
		t.Fatalf("generated config did not use stable identities: %+v", identities)
	}
	// A fresh writer models a panel restart while the Agent retained old deltas.
	w := newTrafficWriter(f.db)
	f.report(t, w, []protocol.UserTrafficSnapshot{
		{Inbound: f.multi.Tag, User: "Alice@Example.com", Upload: 11, Download: 110},
		{Inbound: f.multi.Tag, User: oldTrafficName, Upload: 7, Download: 70},
		{Inbound: f.multi.Tag, User: proxyTrafficName(&reused), Upload: 22, Download: 220},
	})
	got := userRows(t, f.db)
	if got[f.alice.ID] != [2]uint64{18, 180} || got[reused.ID] != [2]uint64{22, 220} || len(got) != 2 {
		t.Fatalf("renamed/reused account traffic mixed: %v", got)
	}
}

func TestDeletedLegacyIdentityIsNeverRebound(t *testing.T) {
	f := newUserTrafficFixture(t, model.ConfigModeManaged)
	oldID := f.alice.ID
	if err := f.db.Delete(&f.alice).Error; err != nil {
		t.Fatal(err)
	}
	name := "alice@example.com"
	// Even restoring an explicit numeric ID must not revive a previous owner's
	// binding when the independently issued credential seed is different.
	reused := model.User{ID: oldID, Email: "Alice@Example.com", EmailNormalized: &name, Enabled: true, SubToken: "reused-sub", ProxyToken: "reused-proxy", ServerIDs: []uint{f.server.ID}}
	mustCreate(t, f.db, &reused)
	if err := migrateUserTrafficIdentities(f.db); err != nil {
		t.Fatal(err)
	}
	if _, err := proxyUsersForInbound(f.db, &f.multi, singbox.InboundSettings{MultiUser: true}); err != nil {
		t.Fatal(err)
	}
	w := newTrafficWriter(f.db)
	deltas := []userTrafficDelta{
		{ServerID: f.server.ID, InboundID: f.multi.ID, Name: "alice@example.com", Upload: 10},
		{ServerID: f.server.ID, InboundID: f.multi.ID, Name: proxyTrafficName(&reused), Upload: 20},
		{ServerID: f.server.ID + 1, InboundID: f.multi.ID, Name: proxyTrafficName(&reused), Upload: 30},
		{ServerID: f.server.ID, InboundID: f.single.ID, Name: proxyTrafficName(&reused), Upload: 40},
	}
	resolved, err := w.resolveUserDeltas(deltas, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if len(resolved) != 1 {
		t.Fatalf("unbound or deleted identity accepted: %v", resolved)
	}
	for key, value := range resolved {
		if key.UserID != reused.ID || value.Upload != 20 {
			t.Fatalf("legacy or foreign inbound traffic rebound: %v %v", key, value)
		}
	}
}

func TestTrafficIdentityMigrationAndConfigKeepCredentialFingerprint(t *testing.T) {
	f := newUserTrafficFixture(t, model.ConfigModeManaged)
	before, err := calculateCredentialFingerprint(f.db)
	if err != nil {
		t.Fatal(err)
	}
	if err := migrateUserTrafficIdentities(f.db); err != nil {
		t.Fatal(err)
	}
	if _, err := proxyUsersForInbound(f.db, &f.multi, singbox.InboundSettings{MultiUser: true}); err != nil {
		t.Fatal(err)
	}
	after, err := calculateCredentialFingerprint(f.db)
	if err != nil || before != after {
		t.Fatalf("internal stats name invalidated existing backup credential checks: %v %v %v", before, after, err)
	}
}

func TestTrafficIdentityMigrationOnlyBindsGrantedMultiUserInbounds(t *testing.T) {
	f := newUserTrafficFixture(t, model.ConfigModeManaged)
	unassigned := model.User{Email: "unassigned", Enabled: true, SubToken: "unassigned-sub", ProxyToken: "unassigned-proxy"}
	mustCreate(t, f.db, &unassigned)
	if err := migrateUserTrafficIdentities(f.db); err != nil {
		t.Fatal(err)
	}
	var bindings []model.UserTrafficIdentity
	if err := f.db.Find(&bindings).Error; err != nil {
		t.Fatal(err)
	}
	if len(bindings) != 2 {
		t.Fatalf("unexpected migration identity count: %d", len(bindings))
	}
	for _, binding := range bindings {
		if binding.UserID == unassigned.ID || binding.InboundID != f.multi.ID || binding.ServerID != f.server.ID {
			t.Fatalf("ungranted or shared inbound name was bound: %+v", binding)
		}
	}
}

func TestUserAPIsReserveTrafficIdentityNamespace(t *testing.T) {
	f := newUserTrafficFixture(t, model.ConfigModeManaged)
	app := &App{db: f.db}
	router := gin.New()
	router.POST("/users", app.createUser)
	router.PUT("/users/:id", app.updateUser)
	body, err := json.Marshal(map[string]any{"email": proxyTrafficName(&f.alice), "password": "test"})
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct{ method, path string }{{http.MethodPost, "/users"}, {http.MethodPut, fmt.Sprintf("/users/%d", f.namedUser.ID)}} {
		req := httptest.NewRequest(test.method, test.path, bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("reserved name %s: %d %s", test.method, rec.Code, rec.Body.String())
		}
	}
}
