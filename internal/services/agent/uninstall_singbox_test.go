package agent

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestUninstallSingboxUsesPersistentTransactionalBackup(t *testing.T) {
	for _, required := range []string{
		"/var/lib/singbox-panel-agent/.singbox-uninstall.",
		`if [ "$committed" != "1" ]`,
		`systemctl is-active --quiet sing-box.service`,
		`rc-service sing-box status`,
		`rc-update del sing-box default`,
		`cp -a "$backup_root/config/." /etc/sing-box/`,
		`恢复副本保留在 $backup_root`,
		`# BEGIN Managed by SingBox Panel log forwarding`,
		`# END Managed by SingBox Panel log forwarding`,
		`chmod 644 "$conf_tmp"`,
	} {
		if !strings.Contains(uninstallSingboxScript, required) {
			t.Fatalf("uninstall script missing %q", required)
		}
	}
	if strings.Contains(uninstallSingboxScript, "mktemp -d /tmp/") {
		t.Fatal("uninstall backup still uses volatile /tmp")
	}
	cmd := exec.Command("sh", "-n")
	cmd.Stdin = strings.NewReader(uninstallSingboxScript)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("uninstall script syntax: %v: %s", err, output)
	}
}

func TestUninstallSingboxPreservesCustomConfDSettings(t *testing.T) {
	start := strings.Index(uninstallSingboxScript, `if [ -f /etc/conf.d/sing-box ] && grep -F "BEGIN Managed by SingBox Panel"`)
	end := -1
	if start >= 0 {
		if rel := strings.Index(uninstallSingboxScript[start:], `if [ -d "$backup_root/config" ]`); rel >= 0 {
			end = start + rel
		}
	}
	if start < 0 || end <= start {
		t.Fatalf("failed to locate conf.d cleanup block (start=%d end=%d)", start, end)
	}
	block := uninstallSingboxScript[start:end]

	dir := t.TempDir()
	confDir := filepath.Join(dir, "etc", "conf.d")
	if err := os.MkdirAll(confDir, 0o755); err != nil {
		t.Fatal(err)
	}
	confPath := filepath.Join(confDir, "sing-box")
	localized := strings.ReplaceAll(block, "/etc/conf.d/sing-box", confPath)
	localized = strings.ReplaceAll(localized, "/etc/conf.d/.sing-box.XXXXXX", filepath.Join(confDir, ".sing-box.XXXXXX"))

	// Case 1: custom settings + managed log block -> keep custom settings only.
	customContent := "# custom config\nSINGBOX_WORKDIR=/var/lib/custom-sing-box\n" +
		string(appendOpenRCLogging(nil))
	if err := os.WriteFile(confPath, []byte(customContent), 0o644); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("sh", "-eu", "-c", "umask 077\n"+localized).CombinedOutput(); err != nil {
		t.Fatalf("conf.d surgical cleanup failed: %v\n%s", err, out)
	}
	got, err := os.ReadFile(confPath)
	if err != nil {
		t.Fatalf("custom conf.d file was unexpectedly removed: %v", err)
	}
	if string(got) != "# custom config\nSINGBOX_WORKDIR=/var/lib/custom-sing-box\n" {
		t.Fatalf("unexpected conf.d contents after cleanup:\n%q", got)
	}
	info, err := os.Stat(confPath)
	if err != nil || info.Mode().Perm() != 0o644 {
		t.Fatalf("conf.d permissions = %v, err=%v; want 0644", info.Mode().Perm(), err)
	}

	// Case 2: managed log block only -> file is removed cleanly.
	if err := os.WriteFile(confPath, appendOpenRCLogging(nil), 0o644); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("sh", "-eu", "-c", "umask 077\n"+localized).CombinedOutput(); err != nil {
		t.Fatalf("managed-only conf.d cleanup failed: %v\n%s", err, out)
	}
	if _, err := os.Stat(confPath); !os.IsNotExist(err) {
		t.Fatalf("managed-only conf.d file was not deleted: %v", err)
	}
}

// The backup directory lives under /var/lib/singbox-panel-agent, which may not
// exist yet on a fresh or manually provisioned node. Creating it after mktemp
// made the script abort under set -e before any uninstall work happened.
func TestUninstallSingboxCreatesBackupParentBeforeMktemp(t *testing.T) {
	mkdir := strings.Index(uninstallSingboxScript, "mkdir -p /var/lib/singbox-panel-agent")
	mktemp := strings.Index(uninstallSingboxScript, "mktemp -d /var/lib/singbox-panel-agent/")
	if mkdir < 0 || mktemp < 0 {
		t.Fatalf("uninstall script missing backup parent creation (mkdir=%d mktemp=%d)", mkdir, mktemp)
	}
	if mkdir > mktemp {
		t.Fatal("uninstall script runs mktemp before creating /var/lib/singbox-panel-agent")
	}
}
