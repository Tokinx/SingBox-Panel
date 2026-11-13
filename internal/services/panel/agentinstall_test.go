package panel

import (
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/hann0w0/singbox-panel/internal/config"
	"github.com/hann0w0/singbox-panel/internal/domain/model"
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
		"supervisor=\"supervise-daemon\"",
		"NoNewPrivileges=true",
		"ProtectHome=true",
		"PrivateTmp=true",
		"UMask=0077",
		"--connect-timeout 10",
		"--retry 3",
	} {
		if !strings.Contains(agentInstallScript, required) {
			t.Fatalf("install script is missing transactional step %q", required)
		}
	}
}

func TestAgentInstallCommandPrivilegeBootstrap(t *testing.T) {
	const baseURL = "https://panel.example.com/it's"
	const code = "code'$(exit 99)"
	wantArgs := "--url\n" + baseURL + "\n--code\n" + code + "\n"
	for _, tc := range []struct {
		name           string
		uid            string
		sudo           bool
		doas           bool
		downloadStatus string
		installStatus  string
		wantElevation  string
		wantStatus     int
		wantMessage    string
	}{
		{name: "root runs directly", uid: "0", sudo: true, wantStatus: 1, wantMessage: "systemd or OpenRC is required"},
		{name: "sudo takes precedence", uid: "1000", sudo: true, doas: true, wantElevation: "sudo"},
		{name: "doas fallback", uid: "1000", doas: true, wantElevation: "doas"},
		{name: "no elevation tool", uid: "1000", wantStatus: 1, wantMessage: "run this command as root or install sudo/doas"},
		{name: "download fails before elevation", uid: "1000", sudo: true, downloadStatus: "22", wantStatus: 22},
		{name: "installer failure is preserved", uid: "1000", sudo: true, installStatus: "7", wantElevation: "sudo", wantStatus: 7},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			bin := filepath.Join(dir, "bin")
			if err := os.Mkdir(bin, 0o700); err != nil {
				t.Fatal(err)
			}
			for _, name := range []string{"sh", "cat", "cp"} {
				path, err := exec.LookPath(name)
				if err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(path, filepath.Join(bin, name)); err != nil {
					t.Fatal(err)
				}
			}
			realRM, err := exec.LookPath("rm")
			if err != nil {
				t.Fatal(err)
			}
			// Limit execution to the bootstrap: the downloaded fixture records its
			// arguments, and no service manager or host cleanup is available.
			source := filepath.Join(dir, "source.sh")
			payload := filepath.Join(dir, "payload.sh")
			if err := os.WriteFile(source, []byte(agentInstallScript), 0o600); err != nil {
				t.Fatal(err)
			}
			writeExecutable(t, payload, "#!/bin/sh\nprintf '%s\\n' \"$@\" > \"$ARGS_LOG\"\nexit \"${INSTALL_STATUS:-0}\"\n")
			writeExecutable(t, filepath.Join(bin, "id"), "#!/bin/sh\nprintf '%s\\n' \"$TEST_UID\"\n")
			writeExecutable(t, filepath.Join(bin, "mktemp"), "#!/bin/sh\numask 077\n: > \"$INSTALLER_PATH\"\nprintf '%s\\n' \"$INSTALLER_PATH\"\n")
			writeExecutable(t, filepath.Join(bin, "rm"), "#!/bin/sh\n[ \"$2\" != \"$INSTALLER_PATH\" ] || exec "+shellQuote(realRM)+" \"$@\"\n")
			writeExecutable(t, filepath.Join(bin, "curl"), `#!/bin/sh
output=""
while [ $# -gt 0 ]; do
  case "$1" in
    -o) output="$2"; shift 2 ;;
    *) shift ;;
  esac
done
if [ -z "$output" ]; then
  cat "$SCRIPT_SOURCE"
else
  cp "$SCRIPT_PAYLOAD" "$output"
  exit "${DOWNLOAD_STATUS:-0}"
fi
`)
			for name, enabled := range map[string]bool{"sudo": tc.sudo, "doas": tc.doas} {
				if enabled {
					writeExecutable(t, filepath.Join(bin, name), "#!/bin/sh\nprintf '%s\\n' "+shellQuote(name)+" > \"$ELEVATION_LOG\"\nexec \"$@\"\n")
				}
			}
			installerPath := filepath.Join(dir, "installer.sh")
			elevationLog := filepath.Join(dir, "elevation.log")
			argsLog := filepath.Join(dir, "args.log")
			cmd := exec.Command(filepath.Join(bin, "sh"), "-c", installCommand(baseURL, code))
			cmd.Env = append(os.Environ(), "PATH="+bin, "TEST_UID="+tc.uid,
				"SCRIPT_SOURCE="+source, "SCRIPT_PAYLOAD="+payload, "INSTALLER_PATH="+installerPath,
				"ELEVATION_LOG="+elevationLog, "ARGS_LOG="+argsLog,
				"DOWNLOAD_STATUS="+tc.downloadStatus, "INSTALL_STATUS="+tc.installStatus)
			output, err := cmd.CombinedOutput()
			if cmd.ProcessState == nil || cmd.ProcessState.ExitCode() != tc.wantStatus {
				t.Fatalf("bootstrap exit: %v; want %d\n%s", err, tc.wantStatus, output)
			}
			if !strings.Contains(string(output), tc.wantMessage) {
				t.Fatalf("bootstrap output = %q; want %q", output, tc.wantMessage)
			}
			elevation, _ := os.ReadFile(elevationLog)
			if strings.TrimSpace(string(elevation)) != tc.wantElevation {
				t.Fatalf("elevation = %q; want %q", elevation, tc.wantElevation)
			}
			args, _ := os.ReadFile(argsLog)
			if tc.wantElevation != "" && string(args) != wantArgs {
				t.Fatalf("installer arguments = %q; want %q", args, wantArgs)
			}
			if _, err := os.Stat(installerPath); !os.IsNotExist(err) {
				t.Fatalf("temporary installer was not removed: %v", err)
			}
		})
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
	if !strings.Contains(command, "--code") || len(a.agentInstallCodes) != 1 {
		t.Fatalf("install command did not mint a one-time code: %s", command)
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
