package panel

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"

	"github.com/hann0w0/singbox-panel/internal/domain/model"
	"github.com/hann0w0/singbox-panel/internal/domain/protocol"
)

func TestLiveSessionRejectsExpiryAndRevocation(t *testing.T) {
	db := testDB(t)
	u := model.User{Email: "live-admin", Role: model.RoleAdmin, Enabled: true, SubToken: "live-session"}
	if err := db.Create(&u).Error; err != nil {
		t.Fatal(err)
	}
	auth := NewAuth("0123456789abcdef0123456789abcdef", db)
	token, err := auth.Issue(&u, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	claims, err := auth.parse(token)
	if err != nil {
		t.Fatal(err)
	}
	if !auth.sessionActive(context.Background(), claims) {
		t.Fatal("new session rejected")
	}
	expired := *claims
	expired.ExpiresAt = jwt.NewNumericDate(time.Now().Add(-time.Second))
	if auth.sessionActive(context.Background(), &expired) {
		t.Fatal("expired session accepted")
	}
	if err := db.Model(&u).Update("token_version", u.TokenVersion+1).Error; err != nil {
		t.Fatal(err)
	}
	if auth.sessionActive(context.Background(), claims) {
		t.Fatal("revoked session accepted")
	}
}

func TestTrafficSSEStopsBeforeSendingDataAfterRevocation(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := testDB(t)
	u := model.User{Email: "stream-admin", Role: model.RoleAdmin, Enabled: true, SubToken: "stream-session"}
	server := model.Server{Name: "stream-node", AgentToken: "stream-node-token"}
	if err := db.Create(&u).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&server).Error; err != nil {
		t.Fatal(err)
	}
	a := &App{db: db, auth: NewAuth("0123456789abcdef0123456789abcdef", db), hub: NewHub(db)}
	a.hub.live.publishTraffic(server.ID, &protocol.TrafficSnapshot{UploadRate: 10})
	router := gin.New()
	router.GET("/servers/:id/live", a.auth.Middleware(), AdminOnly(), a.handleTrafficSSE)
	httpServer := httptest.NewServer(router)
	defer httpServer.Close()
	token, err := a.auth.Issue(&u, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf("%s/servers/%d/live", httpServer.URL, server.ID), nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	response, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	reader := bufio.NewReader(response.Body)
	line, err := reader.ReadString('\n')
	if err != nil || !strings.HasPrefix(line, "data: ") {
		t.Fatalf("initial event: %q, %v", line, err)
	}
	if err := db.Model(&u).Update("token_version", u.TokenVersion+1).Error; err != nil {
		t.Fatal(err)
	}
	a.hub.live.publishLog(server.ID, protocol.LogEvt{Msg: "after-revocation"})
	rest, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("stream did not close: %v", err)
	}
	if strings.Contains(string(rest), "after-revocation") {
		t.Fatal("revoked stream leaked a new event")
	}
}
