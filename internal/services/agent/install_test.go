package agent

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tokinx/singbox-panel/internal/domain/protocol"
)

func TestInstallSingboxPropagatesDownloadFailure(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "apk"), []byte("#!/bin/sh\necho 'apk-tools 3.0.0'\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "curl"), []byte("#!/bin/sh\necho mock-download-failed >&2\nexit 22\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("TMPDIR", dir)
	output, err := InstallSingbox(context.Background(), protocol.ChannelStable, "", "script")
	if err == nil || !strings.Contains(err.Error(), "download sing-box installer") || !strings.Contains(output, "mock-download-failed") {
		t.Fatalf("download failure was swallowed: output=%q error=%v", output, err)
	}
	files, err := filepath.Glob(filepath.Join(dir, "singbox-install-*"))
	if err != nil || len(files) != 0 {
		t.Fatalf("temporary installer files remain: %v, %v", files, err)
	}
}

func TestInstallSingboxPropagatesInstallerFailure(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "apk"), []byte("#!/bin/sh\necho 'apk-tools 3.0.0'\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	// This curl fixture writes only a harmless local script; no remote installer
	// or package manager is invoked by this test.
	fixture := "#!/bin/sh\nprintf '#!/bin/sh\\necho mock-install-failed >&2\\nexit 9\\n' > \"$3\"\n"
	if err := os.WriteFile(filepath.Join(dir, "curl"), []byte(fixture), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	output, err := InstallSingbox(context.Background(), protocol.ChannelBeta, "1.14.0-alpha.1", "script")
	if err == nil || !strings.Contains(err.Error(), "run sing-box installer") || !strings.Contains(output, "mock-install-failed") {
		t.Fatalf("installer failure was swallowed: output=%q error=%v", output, err)
	}
}

func TestInstallSingboxRejectsEmptyDownload(t *testing.T) {
	executed := false
	installer := singboxInstaller{
		run: func(_ context.Context, command string, _ ...string) (string, error) {
			if command != "curl" {
				executed = true
			}
			return "", nil
		},
		detectVersion: func(context.Context) (bool, string) {
			t.Fatal("empty installer must not reach version verification")
			return true, "1.14.0"
		},
	}
	_, err := installer.install(context.Background(), protocol.ChannelStable, "", "script")
	if err == nil || !strings.Contains(err.Error(), "empty") || executed {
		t.Fatalf("empty download was executed: %v", err)
	}
}

func TestAllSingboxInstallMethodsVerifyBinaryAndRequestedVersion(t *testing.T) {
	for _, method := range []string{"script", "apt", "dnf"} {
		for _, tc := range []struct {
			name      string
			installed bool
			actual    string
			requested string
			wantError bool
		}{
			{"missing", false, "", "", true},
			{"broken", true, "", "", true},
			{"invalid version", true, "not a version", "", true},
			{"wrong version", true, "1.13.0", "1.14.0", true},
			{"expected version", true, "1.14.0", "1.14.0", false},
			{"latest beta", true, "1.14.0-alpha.1", "", false},
		} {
			if method != "script" && tc.requested != "" {
				continue // Package methods reject version pins before installation.
			}
			t.Run(method+"/"+tc.name, func(t *testing.T) {
				installer := singboxInstaller{
					run: func(_ context.Context, command string, args ...string) (string, error) {
						if command == "curl" {
							return "", os.WriteFile(args[2], []byte("#!/bin/sh\nexit 0\n"), 0o600)
						}
						return "mock installation complete", nil
					},
					detectVersion: func(context.Context) (bool, string) { return tc.installed, tc.actual },
				}
				_, err := installer.install(context.Background(), protocol.ChannelBeta, tc.requested, method)
				if (err != nil) != tc.wantError {
					t.Fatalf("verification error = %v, want error = %t", err, tc.wantError)
				}
			})
		}
	}
}

func TestPackageInstallRejectsVersionPinBeforeCommands(t *testing.T) {
	for _, method := range []string{protocol.MethodAPT, protocol.MethodDNF} {
		t.Run(method, func(t *testing.T) {
			installer := singboxInstaller{
				run: func(context.Context, string, ...string) (string, error) {
					t.Fatal("a rejected version pin must not execute any system command")
					return "", nil
				},
				detectVersion: func(context.Context) (bool, string) {
					t.Fatal("a rejected version pin must not reach post-install verification")
					return true, "1.14.0"
				},
			}
			_, err := installer.install(context.Background(), protocol.ChannelStable, "1.14.0", method)
			if err == nil || !strings.Contains(err.Error(), "script") {
				t.Fatalf("version pin must be rejected with script-method guidance: %v", err)
			}
		})
	}
}

func TestPackageInstallFailureSkipsVersionCheck(t *testing.T) {
	for _, method := range []string{"apt", "dnf"} {
		t.Run(method, func(t *testing.T) {
			failure := errors.New("mock package manager failure")
			installer := singboxInstaller{
				run: func(context.Context, string, ...string) (string, error) { return "failed", failure },
				detectVersion: func(context.Context) (bool, string) {
					t.Fatal("failed package manager must not be rescued by an existing binary")
					return true, "1.14.0"
				},
			}
			_, err := installer.install(context.Background(), protocol.ChannelStable, "", method)
			if !errors.Is(err, failure) {
				t.Fatalf("failure = %v", err)
			}
		})
	}
}

func TestSingboxInstallerUsesCommandDeadline(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	steps := 0
	installer := singboxInstaller{
		run: func(commandCtx context.Context, command string, args ...string) (string, error) {
			if commandCtx != ctx {
				t.Fatal("installation command lost its timeout context")
			}
			steps++
			if command == "curl" {
				return "", os.WriteFile(args[2], []byte("#!/bin/sh\nexit 0\n"), 0o600)
			}
			cancel()
			return "", nil
		},
		detectVersion: func(context.Context) (bool, string) {
			t.Fatal("canceled installer must not report success")
			return true, "1.14.0"
		},
	}
	_, err := installer.install(ctx, protocol.ChannelStable, "", "script")
	if !errors.Is(err, context.Canceled) || steps != 2 {
		t.Fatalf("cancellation not propagated: steps=%d error=%v", steps, err)
	}
}

func TestSingboxInstallerPreparesHostServiceAfterVerification(t *testing.T) {
	prepared := 0
	installer := singboxInstaller{
		run: func(_ context.Context, command string, args ...string) (string, error) {
			if command == "curl" {
				return "", os.WriteFile(args[2], []byte("#!/bin/sh\nexit 0\n"), 0o600)
			}
			return "", nil
		},
		detectVersion: func(context.Context) (bool, string) { return true, "1.14.0" },
		prepareService: func(context.Context) error {
			prepared++
			return nil
		},
	}
	if _, err := installer.install(context.Background(), protocol.ChannelStable, "", "script"); err != nil {
		t.Fatal(err)
	}
	if prepared != 1 {
		t.Fatalf("service preparation count = %d, want 1", prepared)
	}
}

func TestSingboxInstallerSurfacesServicePreparationFailure(t *testing.T) {
	failure := errors.New("openrc unavailable")
	installer := singboxInstaller{
		run: func(_ context.Context, command string, args ...string) (string, error) {
			if command == "curl" {
				return "", os.WriteFile(args[2], []byte("#!/bin/sh\nexit 0\n"), 0o600)
			}
			return "", nil
		},
		detectVersion: func(context.Context) (bool, string) { return true, "1.14.0" },
		prepareService: func(context.Context) error {
			return failure
		},
	}
	_, err := installer.install(context.Background(), protocol.ChannelStable, "", "script")
	if !errors.Is(err, failure) {
		t.Fatalf("service preparation failure = %v", err)
	}
}

func TestPrepareSingboxServiceEnablesBootStart(t *testing.T) {
	enabled := false
	manager := systemdServiceManager{run: func(_ context.Context, command string, args ...string) (string, error) {
		if command == "systemctl" && len(args) > 0 {
			switch args[0] {
			case "daemon-reload":
				return "", nil
			case "is-enabled":
				if enabled {
					return "enabled", nil
				}
				return "disabled", errors.New("disabled")
			case "enable":
				enabled = true
				return "", nil
			}
		}
		return "", nil
	}}
	if err := prepareSingboxServiceWithManager(context.Background(), manager); err != nil {
		t.Fatal(err)
	}
	if !enabled {
		t.Fatal("sing-box service was not enabled at boot")
	}
}

func TestParseAPKMajorVersion(t *testing.T) {
	for _, tc := range []struct {
		input string
		major int
		ok    bool
	}{
		{"apk-tools 2.14.13, compiled for aarch64.", 2, true},
		{"apk-tools 3.0.9-r0, compiled for x86_64.", 3, true},
		{"apk-tools 2.12.14", 2, true},
		{"unknown", 0, false},
		{"", 0, false},
	} {
		major, ok := parseAPKMajorVersion(tc.input)
		if ok != tc.ok || major != tc.major {
			t.Fatalf("parseAPKMajorVersion(%q) = (%d, %t), want (%d, %t)", tc.input, major, ok, tc.major, tc.ok)
		}
	}
}

func TestShouldUseAlpineMuslTarball(t *testing.T) {
	for _, tc := range []struct {
		name       string
		cmds       map[string]bool
		openwrt    bool
		apkVersion string
		apkErr     error
		want       bool
	}{
		{
			name:       "alpine 3.22 apk v2",
			cmds:       map[string]bool{"apk": true},
			apkVersion: "apk-tools 2.14.13, compiled for aarch64.",
			want:       true,
		},
		{
			name:       "alpine 3.23 apk v3",
			cmds:       map[string]bool{"apk": true},
			apkVersion: "apk-tools 3.0.9-r0, compiled for x86_64.",
			want:       false,
		},
		{
			name:       "debian with stray apk",
			cmds:       map[string]bool{"apk": true, "dpkg": true},
			apkVersion: "apk-tools 2.14.13",
			want:       false,
		},
		{
			name:       "openwrt apk",
			cmds:       map[string]bool{"apk": true},
			openwrt:    true,
			apkVersion: "apk-tools 2.14.13",
			want:       false,
		},
		{
			name:   "apk version failed",
			cmds:   map[string]bool{"apk": true},
			apkErr: errors.New("boom"),
			want:   false,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			inst := singboxInstaller{
				lookPath:  func(name string) bool { return tc.cmds[name] },
				isOpenWrt: func() bool { return tc.openwrt },
				run: func(_ context.Context, cmd string, _ ...string) (string, error) {
					if cmd == "apk" {
						return tc.apkVersion, tc.apkErr
					}
					return "", nil
				},
			}
			if got := inst.shouldUseAlpineMuslTarball(context.Background()); got != tc.want {
				t.Fatalf("shouldUseAlpineMuslTarball() = %t, want %t", got, tc.want)
			}
		})
	}
}

func TestInstallScriptFailsWhenPackageInstallCommandFailsBeforeRm(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "apk"), []byte("#!/bin/sh\necho 'apk-tools 3.0.0'\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	// Upstream sing-box install.sh has no `set -e` and ends with `rm -f`; running
	// it via `sh -e` must fail immediately instead of returning rm's exit 0.
	curlFixture := `#!/bin/sh
cat <<'EOF' > "$3"
#!/bin/sh
touch downloaded.apk
sh -c 'echo "ERROR: IO ERROR" >&2; exit 2'
rm -f downloaded.apk
EOF
`
	if err := os.WriteFile(filepath.Join(dir, "curl"), []byte(curlFixture), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("TMPDIR", dir)

	output, err := InstallSingbox(context.Background(), protocol.ChannelStable, "", "script")
	if err == nil || !strings.Contains(err.Error(), "run sing-box installer") {
		t.Fatalf("expected non-zero failure from failing command before rm, got output=%q err=%v", output, err)
	}
	if !strings.Contains(output, "ERROR: IO ERROR") {
		t.Fatalf("expected package error in output, got %q", output)
	}
	leftovers, err := filepath.Glob(filepath.Join(dir, "singbox-install-*"))
	if err != nil || len(leftovers) != 0 {
		t.Fatalf("work directory was not cleaned up: %v, %v", leftovers, err)
	}
}

func buildMockSingboxTarGz(t *testing.T, archiveDir, scriptBody string) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	content := []byte(scriptBody)
	if err := tw.WriteHeader(&tar.Header{
		Name: archiveDir + "/sing-box",
		Mode: 0o755,
		Size: int64(len(content)),
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write(content); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestInstallAlpineMuslTarballInstallsAndVerifiesBinary(t *testing.T) {
	dir := t.TempDir()
	targetBin := filepath.Join(dir, "usr", "bin", "sing-box")
	cfgFile := filepath.Join(dir, "etc", "sing-box", "config.json")
	confFile := filepath.Join(dir, "etc", "conf.d", "sing-box")
	writeTestFile(t, cfgFile, `{"log":{"level":"warn"}}`)
	writeTestFile(t, confFile, "output_log=/var/log/sing-box.log\n")

	stableTarball := buildMockSingboxTarGz(t, "sing-box-1.14.4-linux-amd64-musl", "#!/bin/sh\necho 1.14.4\n")
	betaTarball := buildMockSingboxTarGz(t, "sing-box-1.15.0-alpha.13-linux-amd64-musl", "#!/bin/sh\necho 1.15.0-alpha.13\n")

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/SagerNet/sing-box/releases/latest":
			_, _ = w.Write([]byte(`{"tag_name":"v1.14.4"}`))
		case "/repos/SagerNet/sing-box/releases":
			if r.URL.RawQuery != "per_page=1" {
				http.Error(w, "unexpected query: "+r.URL.RawQuery, http.StatusBadRequest)
				return
			}
			_, _ = w.Write([]byte(`[{"tag_name":"v1.15.0-alpha.13"}]`))
		case "/SagerNet/sing-box/releases/download/v1.14.4/sing-box-1.14.4-linux-amd64-musl.tar.gz":
			_, _ = w.Write(stableTarball)
		case "/SagerNet/sing-box/releases/download/v1.15.0-alpha.13/sing-box-1.15.0-alpha.13-linux-amd64-musl.tar.gz":
			_, _ = w.Write(betaTarball)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	var apkCalls [][]string
	apkInstalled := map[string]bool{"sing-box": true}

	newInstaller := func() singboxInstaller {
		return singboxInstaller{
			lookPath:           func(name string) bool { return name == "apk" },
			isOpenWrt:          func() bool { return false },
			goarch:             "amd64",
			targetBinary:       targetBin,
			apkCleanupPaths:    []string{cfgFile, confFile},
			githubAPIBase:      srv.URL,
			githubDownloadBase: srv.URL,
			httpClient:         srv.Client(),
			run: func(ctx context.Context, cmd string, args ...string) (string, error) {
				if cmd == "apk" {
					apkCalls = append(apkCalls, append([]string{cmd}, args...))
					if len(args) == 1 && args[0] == "--version" {
						return "apk-tools 2.14.13, compiled for x86_64.", nil
					}
					if len(args) == 3 && args[0] == "info" && args[1] == "-e" {
						if apkInstalled[args[2]] {
							return "", nil
						}
						return "", errors.New("not installed")
					}
					if len(args) == 2 && args[0] == "del" {
						apkInstalled[args[1]] = false
						_ = os.Remove(confFile) // simulate apk del removing unmodified conffile
						return "", nil
					}
				}
				return run(ctx, cmd, args...)
			},
			detectVersion: func(ctx context.Context) (bool, string) {
				return detectVersionAt(ctx, run, targetBin)
			},
		}
	}

	out, err := newInstaller().install(context.Background(), protocol.ChannelStable, "", protocol.MethodScript)
	if err != nil {
		t.Fatalf("stable musl install failed: %v (out=%q)", err, out)
	}
	if !strings.Contains(out, "1.14.4") || !strings.Contains(out, "amd64-musl") {
		t.Fatalf("unexpected output: %q", out)
	}
	if apkInstalled["sing-box"] {
		t.Fatal("conflicting apk sing-box package was not removed")
	}
	if gotConf, err := os.ReadFile(confFile); err != nil || string(gotConf) != "output_log=/var/log/sing-box.log\n" {
		t.Fatalf("conf.d file was lost during apk cleanup: %q, err=%v", gotConf, err)
	}
	if ok, ver := detectVersionAt(context.Background(), run, targetBin); !ok || ver != "1.14.4" {
		t.Fatalf("installed binary version = (%t, %q), want (true, 1.14.4)", ok, ver)
	}

	// Beta channel resolves prerelease tag and replaces the binary.
	out, err = newInstaller().install(context.Background(), protocol.ChannelBeta, "", protocol.MethodScript)
	if err != nil {
		t.Fatalf("beta musl install failed: %v (out=%q)", err, out)
	}
	if ok, ver := detectVersionAt(context.Background(), run, targetBin); !ok || ver != "1.15.0-alpha.13" {
		t.Fatalf("beta binary version = (%t, %q), want (true, 1.15.0-alpha.13)", ok, ver)
	}

	// Pinned version mismatch inside archive must fail before replacing existing binary.
	mismatchTar := buildMockSingboxTarGz(t, "sing-box-1.14.0-linux-amd64-musl", "#!/bin/sh\necho 1.14.9\n")
	mismatchSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(mismatchTar)
	}))
	defer mismatchSrv.Close()
	badInst := newInstaller()
	badInst.githubDownloadBase = mismatchSrv.URL
	badInst.httpClient = mismatchSrv.Client()
	if _, err := badInst.install(context.Background(), protocol.ChannelStable, "1.14.0", protocol.MethodScript); err == nil || !strings.Contains(err.Error(), "mismatch") {
		t.Fatalf("expected version mismatch error, got %v", err)
	}
	if ok, ver := detectVersionAt(context.Background(), run, targetBin); !ok || ver != "1.15.0-alpha.13" {
		t.Fatalf("failed pinned install overwrote existing binary: (%t, %q)", ok, ver)
	}
	_ = apkCalls
}

type stubServiceManager struct {
	kind      serviceKind
	active    bool
	enabled   bool
	pid       int
	startedAt time.Time
	actionErr error
	actions   []string
	prepared  int
}

func (s *stubServiceManager) Kind() serviceKind { return s.kind }
func (s *stubServiceManager) Prepare(context.Context) (string, error) {
	s.prepared++
	return "", nil
}
func (s *stubServiceManager) Action(_ context.Context, action string) (string, error) {
	s.actions = append(s.actions, action)
	if s.actionErr != nil {
		return "boom", s.actionErr
	}
	return "", nil
}
func (s *stubServiceManager) SetEnabled(_ context.Context, enabled bool) (string, error) {
	s.enabled = enabled
	return "", nil
}
func (s *stubServiceManager) Active(context.Context) bool          { return s.active }
func (s *stubServiceManager) Enabled(context.Context) bool         { return s.enabled }
func (s *stubServiceManager) Status(context.Context) string        { return "" }
func (s *stubServiceManager) MainPID(context.Context) (int, error) { return s.pid, nil }
func (s *stubServiceManager) StartedAt(context.Context, int) (time.Time, error) {
	return s.startedAt, nil
}
func (s *stubServiceManager) LogCommand(int, bool) (string, []string) { return "", nil }

func TestFinalizeSingboxServiceAfterUpgrade(t *testing.T) {
	t0 := time.Unix(1700000000, 0)
	t1 := t0.Add(5 * time.Second)

	t.Run("active service restarted when binary hash changes and pid unchanged", func(t *testing.T) {
		mgr := &stubServiceManager{kind: serviceKindOpenRC, active: true, enabled: true, pid: 100, startedAt: t0}
		pre := capturePreInstallState(context.Background(), mgr, func() string { return "old-sha" })
		if err := finalizeSingboxServiceWithManager(context.Background(), mgr, pre, "new-sha"); err != nil {
			t.Fatal(err)
		}
		if len(mgr.actions) != 1 || mgr.actions[0] != "restart" {
			t.Fatalf("actions = %v, want [restart]", mgr.actions)
		}
	})

	t.Run("skips restart when package installer already restarted process", func(t *testing.T) {
		mgr := &stubServiceManager{kind: serviceKindSystemd, active: true, enabled: true, pid: 100, startedAt: t0}
		pre := capturePreInstallState(context.Background(), mgr, func() string { return "old-sha" })
		mgr.pid = 205
		mgr.startedAt = t1
		if err := finalizeSingboxServiceWithManager(context.Background(), mgr, pre, "new-sha"); err != nil {
			t.Fatal(err)
		}
		if len(mgr.actions) != 0 {
			t.Fatalf("expected no double restart, got actions = %v", mgr.actions)
		}
	})

	t.Run("skips restart when binary hash is unchanged", func(t *testing.T) {
		mgr := &stubServiceManager{kind: serviceKindOpenRC, active: true, enabled: true, pid: 100, startedAt: t0}
		pre := capturePreInstallState(context.Background(), mgr, func() string { return "same-sha" })
		if err := finalizeSingboxServiceWithManager(context.Background(), mgr, pre, "same-sha"); err != nil {
			t.Fatal(err)
		}
		if len(mgr.actions) != 0 {
			t.Fatalf("expected no restart on identical binary, got actions = %v", mgr.actions)
		}
	})

	t.Run("inactive service is enabled without starting", func(t *testing.T) {
		mgr := &stubServiceManager{kind: serviceKindOpenRC, active: false, enabled: false}
		pre := capturePreInstallState(context.Background(), mgr, func() string { return "" })
		if err := finalizeSingboxServiceWithManager(context.Background(), mgr, pre, "new-sha"); err != nil {
			t.Fatal(err)
		}
		if !mgr.enabled || mgr.prepared != 1 || len(mgr.actions) != 0 {
			t.Fatalf("unexpected state: enabled=%t prepared=%d actions=%v", mgr.enabled, mgr.prepared, mgr.actions)
		}
	})

	t.Run("surfaces restart failure", func(t *testing.T) {
		mgr := &stubServiceManager{
			kind:      serviceKindOpenRC,
			active:    true,
			enabled:   true,
			pid:       100,
			startedAt: t0,
			actionErr: errors.New("exit status 1"),
		}
		pre := capturePreInstallState(context.Background(), mgr, func() string { return "old-sha" })
		err := finalizeSingboxServiceWithManager(context.Background(), mgr, pre, "new-sha")
		if err == nil || !strings.Contains(err.Error(), "restart service after upgrade") {
			t.Fatalf("expected restart error, got %v", err)
		}
	})

	t.Run("installer end-to-end restarts active service on binary change", func(t *testing.T) {
		mgr := &stubServiceManager{kind: serviceKindOpenRC, active: true, enabled: false, pid: 42, startedAt: t0}
		hash := "before"
		inst := singboxInstaller{
			run: func(_ context.Context, cmd string, args ...string) (string, error) {
				if cmd == "curl" {
					return "", os.WriteFile(args[2], []byte("#!/bin/sh\nexit 0\n"), 0o600)
				}
				hash = "after"
				return "", nil
			},
			detectVersion:  func(context.Context) (bool, string) { return true, "1.14.4" },
			serviceManager: func() (serviceManager, error) { return mgr, nil },
			binaryHash:     func() string { return hash },
		}
		if _, err := inst.install(context.Background(), protocol.ChannelStable, "", protocol.MethodScript); err != nil {
			t.Fatal(err)
		}
		if !mgr.enabled || len(mgr.actions) != 1 || mgr.actions[0] != "restart" {
			t.Fatalf("expected enable+restart, got enabled=%t actions=%v", mgr.enabled, mgr.actions)
		}
	})
}

func TestPackageInstallRejectsMissingPackageManager(t *testing.T) {
	for _, method := range []string{protocol.MethodAPT, protocol.MethodDNF} {
		t.Run(method, func(t *testing.T) {
			inst := singboxInstaller{
				lookPath: func(name string) bool { return name == "apk" },
				run: func(context.Context, string, ...string) (string, error) {
					t.Fatal("missing package manager should fail before running commands")
					return "", nil
				},
			}
			_, err := inst.install(context.Background(), protocol.ChannelStable, "", method)
			if err == nil || !strings.Contains(err.Error(), "官方脚本") {
				t.Fatalf("expected official script guidance error, got %v", err)
			}
		})
	}
}
