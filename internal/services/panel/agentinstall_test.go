package panel

import (
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/tokinx/singbox-panel/internal/config"
	"github.com/tokinx/singbox-panel/internal/domain/model"
)

func TestAgentInstallScriptSyntaxAndTransactionalRollback(t *testing.T) {
	path := filepath.Join(t.TempDir(), "install-agent.sh")
	if err := os.WriteFile(path, []byte(agentInstallScript), 0o700); err != nil {
		t.Fatal(err)
	}
	if output, err := exec.Command("sh", "-n", path).CombinedOutput(); err != nil {
		t.Fatalf("install script syntax: %v\n%s", err, output)
	}
	for _, required := range []string{
		"restore_target \"$BIN\"",
		"restore_target \"$CONFIG\"",
		"restore_target \"$SERVICE_FILE\"",
		"TRANSACTION_STARTED=1",
		"COMMITTED=1",
		"READY_SHA=",
		"/etc/init.d/singbox-panel-agent",
		"rc-update add singbox-panel-agent default",
		`supervisor="supervise-daemon"`,
		`umask="0077"`,
		"NoNewPrivileges=true",
		"ProtectHome=true",
		"PrivateTmp=true",
		"UMask=0077",
		"--connect-timeout 10",
		"--retry 3",
		`for elevate in sudo doas; do`,
		`exec "$elevate" sh -c`,
	} {
		if !strings.Contains(agentInstallScript, required) {
			t.Fatalf("install script is missing transactional step %q", required)
		}
	}
}

func postAgentRegistrationCode(router http.Handler, code string) *httptest.ResponseRecorder {
	form := url.Values{"code": {code}}
	req := httptest.NewRequest(http.MethodPost, "/api/agent/register", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	return w
}

func TestAgentInstallCodeIsOneTimeAndExpires(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := testDB(t)
	server := model.Server{Name: "node", AgentToken: "0123456789abcdef0123456789abcdef"}
	if err := db.Create(&server).Error; err != nil {
		t.Fatal(err)
	}
	a := &App{db: db, agentInstallCodes: make(map[string]agentInstallCode)}
	router := gin.New()
	router.POST("/api/agent/register", a.exchangeAgentInstallCode)

	const validCode = "abcdef0123456789abcdef0123456789abcdef0123456789"
	a.agentInstallCodes[validCode] = agentInstallCode{ServerID: server.ID, Expires: time.Now().Add(time.Minute)}
	first := postAgentRegistrationCode(router, validCode)
	if first.Code != http.StatusOK || first.Body.String() != server.AgentToken {
		t.Fatalf("first exchange = %d %q", first.Code, first.Body.String())
	}
	second := postAgentRegistrationCode(router, validCode)
	if second.Code != http.StatusUnauthorized {
		t.Fatalf("reused code status = %d", second.Code)
	}

	const expiredCode = "111111111111111111111111111111111111111111111111"
	a.agentInstallCodes[expiredCode] = agentInstallCode{ServerID: server.ID, Expires: time.Now().Add(-time.Second)}
	expired := postAgentRegistrationCode(router, expiredCode)
	if expired.Code != http.StatusUnauthorized {
		t.Fatalf("expired code status = %d", expired.Code)
	}
	if _, exists := a.agentInstallCodes[expiredCode]; exists {
		t.Fatal("expired code was not consumed")
	}
}

func TestAgentInstallCommandNeverContainsLongLivedToken(t *testing.T) {
	db := testDB(t)
	server := model.Server{Name: "node", AgentToken: "0123456789abcdef0123456789abcdef"}
	if err := db.Create(&server).Error; err != nil {
		t.Fatal(err)
	}
	a := &App{db: db, cfg: config.PanelConfig{BaseURL: "https://panel.example.com"}, agentInstallCodes: make(map[string]agentInstallCode)}
	command := a.newAgentInstallCommand(server.ID)
	if strings.Contains(command, server.AgentToken) {
		t.Fatal("install command exposed the long-lived Agent token")
	}
	if len(a.agentInstallCodes) != 1 {
		t.Fatalf("install command did not mint a one-time code: %s", command)
	}
	var mintedCode string
	for code := range a.agentInstallCodes {
		mintedCode = code
	}
	if !regexp.MustCompile(`^[0-9A-Za-z]{22}$`).MatchString(mintedCode) {
		t.Fatalf("minted code = %q; want 22-char Base62", mintedCode)
	}
	want := "curl -fsSL https://panel.example.com/api/agent/install.sh | sh -s -- " + mintedCode
	if command != want {
		t.Fatalf("install command = %q; want %q", command, want)
	}
}

func TestRandBase62FormatAndUniqueness(t *testing.T) {
	if got := randBase62(0); got != "" {
		t.Fatalf("randBase62(0) = %q", got)
	}
	re := regexp.MustCompile(`^[0-9A-Za-z]{22}$`)
	seen := make(map[string]struct{}, 64)
	for i := 0; i < 64; i++ {
		code := randBase62(22)
		if !re.MatchString(code) {
			t.Fatalf("randBase62(22) = %q; want 22-char Base62", code)
		}
		if _, dup := seen[code]; dup {
			t.Fatalf("duplicate code generated: %q", code)
		}
		seen[code] = struct{}{}
	}
}

func TestHandleAgentInstallScriptEmbedsBaseURLAndValidSyntax(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, rawBaseURL := range []string{
		"https://panel.example.com/",
		"https://panel.example.com/it's",
	} {
		a := &App{cfg: config.PanelConfig{BaseURL: rawBaseURL}}
		router := gin.New()
		router.GET("/api/agent/install.sh", a.handleAgentInstallScript)

		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/agent/install.sh", nil))
		if recorder.Code != http.StatusOK {
			t.Fatalf("GET /api/agent/install.sh status = %d", recorder.Code)
		}
		if cacheControl := recorder.Header().Get("Cache-Control"); cacheControl != "no-store" {
			t.Fatalf("Cache-Control = %q; want no-store", cacheControl)
		}
		script := recorder.Body.String()
		expectedLine := "DEFAULT_URL=" + shellQuote(strings.TrimRight(rawBaseURL, "/"))
		if !strings.Contains(script, expectedLine) {
			t.Fatalf("rendered script missing %q", expectedLine)
		}
		cmd := exec.Command("sh", "-n")
		cmd.Stdin = strings.NewReader(script)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("rendered script syntax (%s): %v: %s", rawBaseURL, err, out)
		}
	}
}

func TestRenderedInstallScriptParsesPositionalCodeAndDefaultURL(t *testing.T) {
	script := renderAgentInstallScript("https://panel.example.com/")
	marker := "\nif ! command -v curl"
	idx := strings.Index(script, marker)
	if idx < 0 {
		t.Fatal("marker not found in rendered script")
	}
	harness := script[:idx] + "\nprintf 'URL=%s\\nCODE=%s\\nTOKEN=%s\\n' \"$URL\" \"$CODE\" \"$TOKEN\"\n"

	rootBin := t.TempDir()
	rootID := filepath.Join(rootBin, "id")
	if err := os.WriteFile(rootID, []byte("#!/bin/sh\necho 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	runRoot := func(args ...string) (string, error) {
		cmdArgs := append([]string{"-s", "--"}, args...)
		cmd := exec.Command("sh", cmdArgs...)
		cmd.Stdin = strings.NewReader(harness)
		cmd.Env = []string{"PATH=" + rootBin + ":" + os.Getenv("PATH")}
		out, err := cmd.CombinedOutput()
		return string(out), err
	}

	out, err := runRoot("AbCdEf0123456789GhIjKl")
	if err != nil {
		t.Fatalf("positional code failed: %v\n%s", err, out)
	}
	if out != "URL=https://panel.example.com\nCODE=AbCdEf0123456789GhIjKl\nTOKEN=\n" {
		t.Fatalf("positional code output = %q", out)
	}

	out, err = runRoot("--url", "https://override.example.com/", "--code", "legacyCode123")
	if err != nil {
		t.Fatalf("legacy flags failed: %v\n%s", err, out)
	}
	if out != "URL=https://override.example.com\nCODE=legacyCode123\nTOKEN=\n" {
		t.Fatalf("legacy flags output = %q", out)
	}

	for _, badArgs := range [][]string{
		{"--unknown", "AbCdEf0123456789GhIjKl"},
		{"firstCode", "secondCode"},
		{"--code", "firstCode", "secondCode"},
	} {
		if out, err := runRoot(badArgs...); err == nil {
			t.Fatalf("expected failure for args %v, got %q", badArgs, out)
		}
	}

	// Non-root execution falls back to DEFAULT_URL when --url is omitted.
	userBin := t.TempDir()
	if err := os.WriteFile(filepath.Join(userBin, "id"), []byte("#!/bin/sh\necho 1000\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(userBin, "sudo"), []byte("#!/bin/sh\nprintf 'PANEL=%s\\nARGS=%s\\n' \"$4\" \"$5\"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	nonRootCmd := exec.Command("sh", "-s", "--", "AbCdEf0123456789GhIjKl")
	nonRootCmd.Stdin = strings.NewReader(script)
	nonRootCmd.Env = []string{"PATH=" + userBin + ":/usr/bin:/bin"}
	nonRootOut, err := nonRootCmd.CombinedOutput()
	if err != nil {
		t.Fatalf("non-root escalation failed: %v\n%s", err, nonRootOut)
	}
	if string(nonRootOut) != "PANEL=https://panel.example.com\nARGS=AbCdEf0123456789GhIjKl\n" {
		t.Fatalf("non-root escalation output = %q", nonRootOut)
	}
}

func agentArtifactAuthStatus(a *App, remoteIP, token string) int {
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	req := httptest.NewRequest(http.MethodGet, "/api/agent/download?arch=amd64", nil)
	req.RemoteAddr = net.JoinHostPort(remoteIP, "12345")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	ctx.Request = req
	if a.authenticateAgentArtifact(ctx) {
		return http.StatusNoContent
	}
	return recorder.Code
}

func TestAgentArtifactAuthenticationRequiresFixedToken(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := testDB(t)
	const (
		activeToken = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
		agentIP     = "198.51.100.10"
	)
	otherToken := strings.Repeat("b", 64)
	server := model.Server{Name: "node", AgentToken: activeToken, PublicIP: agentIP}
	if err := db.Create(&server).Error; err != nil {
		t.Fatal(err)
	}
	a := &App{db: db}

	if status := agentArtifactAuthStatus(a, agentIP, activeToken); status != http.StatusNoContent {
		t.Fatalf("active token status = %d", status)
	}
	for name, token := range map[string]string{
		"different token": otherToken,
		"missing token":   "",
		"unknown token":   "not-a-real-agent-token",
	} {
		t.Run(name, func(t *testing.T) {
			if status := agentArtifactAuthStatus(a, agentIP, token); status != http.StatusUnauthorized {
				t.Fatalf("status = %d", status)
			}
		})
	}
}
