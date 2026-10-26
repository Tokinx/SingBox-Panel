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
	"github.com/hann0w0/singbox-panel/internal/config"
	"github.com/hann0w0/singbox-panel/internal/domain/model"
)

func TestAdminUserDetailUsesTargetSubscriptionAndProtectsCredentials(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := testDB(t)
	admin := model.User{Email: "admin", Password: "admin-secret-hash", Role: model.RoleAdmin, Enabled: true, SubToken: "admin-sub-token", ProxyToken: "admin-proxy-token"}
	alice := model.User{Email: "alice", Password: "alice-secret-hash", Role: model.RoleUser, Enabled: true, SubToken: "alice-sub-token", ProxyToken: "alice-proxy-token"}
	for _, user := range []*model.User{&admin, &alice} {
		if err := db.Create(user).Error; err != nil {
			t.Fatal(err)
		}
	}
	for _, fetch := range []model.SubscriptionFetch{
		{UserID: alice.ID, CreatedAt: time.Now()},
		{UserID: alice.ID, CreatedAt: time.Now().Add(-25 * time.Hour)},
		{UserID: admin.ID, CreatedAt: time.Now()},
	} {
		if err := db.Create(&fetch).Error; err != nil {
			t.Fatal(err)
		}
	}
	cfg := config.Default()
	cfg.BaseURL = "https://panel.example.test/"
	cfg.Subscription.PathPrefix = "/subscriptions"
	cfg.WebDir = t.TempDir()
	a := &App{db: db, cfg: cfg, auth: NewAuth("0123456789abcdef0123456789abcdef", db)}
	router := a.routes()
	adminToken, err := a.auth.Issue(&admin, sessionTTL)
	if err != nil {
		t.Fatal(err)
	}
	userToken, err := a.auth.Issue(&alice, sessionTTL)
	if err != nil {
		t.Fatal(err)
	}
	get := func(path, token string) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(http.MethodGet, path, nil)
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		return w
	}
	path := fmt.Sprintf("/api/admin/users/%d", alice.ID)
	w := get(path, adminToken)
	if w.Code != http.StatusOK {
		t.Fatalf("detail: status=%d body=%s", w.Code, w.Body.String())
	}
	var response struct {
		User struct {
			model.User
			SubFetches24h int64 `json:"sub_fetches_24h"`
		} `json:"user"`
		SubscriptionURL string `json:"subscription_url"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response.User.ID != alice.ID || response.User.SubFetches24h != 1 || response.SubscriptionURL != "https://panel.example.test/subscriptions/alice-sub-token" {
		t.Fatalf("wrong target: %+v", response)
	}
	for _, secret := range []string{admin.SubToken, admin.Password, admin.ProxyToken, alice.Password, alice.ProxyToken, `"sub_token"`} {
		if strings.Contains(w.Body.String(), secret) {
			t.Fatalf("detail leaked a credential: %s", secret)
		}
	}
	if !strings.Contains(w.Header().Get("Cache-Control"), "no-store") {
		t.Fatal("subscription detail must not be cached")
	}
	for _, test := range []struct {
		name, path, token string
		status            int
	}{
		{"anonymous", path, "", http.StatusUnauthorized},
		{"ordinary user own detail", path, userToken, http.StatusForbidden},
		{"ordinary user admin detail", fmt.Sprintf("/api/admin/users/%d", admin.ID), userToken, http.StatusForbidden},
		{"missing", "/api/admin/users/99999", adminToken, http.StatusNotFound},
		{"invalid", "/api/admin/users/not-a-number", adminToken, http.StatusBadRequest},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := get(test.path, test.token); got.Code != test.status {
				t.Fatalf("status=%d want=%d body=%s", got.Code, test.status, got.Body.String())
			}
		})
	}
	// Looking at another account neither changes the admin session nor rotates
	// either subscription token. The original self-service endpoint stays scoped.
	w = get("/api/user/me", adminToken)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "/subscriptions/admin-sub-token") || strings.Contains(w.Body.String(), alice.SubToken) {
		t.Fatalf("self profile changed: %s", w.Body.String())
	}
	w = get("/api/admin/users", adminToken)
	if w.Code != http.StatusOK || strings.Contains(w.Body.String(), alice.SubToken) || strings.Contains(w.Body.String(), admin.SubToken) {
		t.Fatalf("user list must remain token-free: status=%d body=%s", w.Code, w.Body.String())
	}
}
