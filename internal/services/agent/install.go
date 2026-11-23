package agent

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/tokinx/singbox-panel/internal/domain/protocol"
)

// Official sing-box paths and service name.
const (
	SingboxBinary = "/usr/bin/sing-box"
	ConfigDir     = "/etc/sing-box"
	ConfigFile    = "/etc/sing-box/config.json"
	ServiceName   = "sing-box"
)

// singboxBinary returns the path to the sing-box executable. The official
// install lives at SingboxBinary, but a server may already carry a
// manually-installed binary elsewhere (commonly /usr/local/bin) — resolve those
// too so a pre-existing sing-box is still detected and usable. Falls back to the
// official path (the install target) when nothing is found.
func singboxBinary() string {
	candidates := []string{
		SingboxBinary,
		"/usr/local/bin/sing-box",
		"/usr/sbin/sing-box",
		"/usr/local/sbin/sing-box",
		"/opt/sing-box/sing-box",
	}
	for _, p := range candidates {
		if fi, err := os.Stat(p); err == nil && !fi.IsDir() {
			return p
		}
	}
	if p, err := exec.LookPath("sing-box"); err == nil {
		return p
	}
	return SingboxBinary
}

var versionRe = regexp.MustCompile(`^[0-9][0-9A-Za-z.\-]*$`)

var apkVersionRe = regexp.MustCompile(`apk-tools\s+(\d+)\.`)

const (
	maxGitHubReleaseJSONBytes  = 4 << 20
	maxSingboxTarballBytes     = 256 << 20
	maxSingboxBinaryBytes      = 256 << 20
	defaultGitHubAPIBase       = "https://api.github.com"
	defaultGitHubDownloadBase  = "https://github.com"
	defaultSingboxInstallShURL = "https://sing-box.app/install.sh"
)

// InstallSingbox installs the official sing-box using the requested method
// (script | apt | dnf) and channel (stable | beta). It only ever invokes the
// official installer/repositories documented by upstream.
func InstallSingbox(ctx context.Context, channel, version, method string) (string, error) {
	installer := singboxInstaller{
		run:                run,
		runInDir:           runInDir,
		lookPath:           commandExists,
		detectVersion:      DetectVersion,
		serviceManager:     currentServiceManager,
		binaryHash:         currentSingboxBinaryHash,
		goarch:             runtime.GOARCH,
		targetBinary:       SingboxBinary,
		httpClient:         &http.Client{Timeout: 5 * time.Minute},
		githubAPIBase:      defaultGitHubAPIBase,
		githubDownloadBase: defaultGitHubDownloadBase,
		isOpenWrt:          isOpenWrtHost,
	}
	return installer.install(ctx, channel, version, method)
}

type singboxInstaller struct {
	run                commandRunner
	runInDir           func(context.Context, string, string, ...string) (string, error)
	lookPath           func(string) bool
	detectVersion      func(context.Context) (bool, string)
	prepareService     func(context.Context) error
	serviceManager     func() (serviceManager, error)
	binaryHash         func() string
	goarch             string
	targetBinary       string
	apkCleanupPaths    []string
	httpClient         *http.Client
	githubAPIBase      string
	githubDownloadBase string
	isOpenWrt          func() bool
}

func (i singboxInstaller) install(ctx context.Context, channel, version, method string) (string, error) {
	if channel == "" {
		channel = protocol.ChannelStable
	}
	if channel != protocol.ChannelStable && channel != protocol.ChannelBeta {
		return "", fmt.Errorf("unknown sing-box channel %q", channel)
	}
	if version != "" && !versionRe.MatchString(version) {
		return "", fmt.Errorf("invalid version %q", version)
	}
	if version != "" && (method == protocol.MethodAPT || method == protocol.MethodDNF) {
		return "", fmt.Errorf("specific sing-box versions require the script installation method")
	}
	var pre preInstallServiceState
	if i.prepareService == nil && i.serviceManager != nil {
		if mgr, mgrErr := i.serviceManager(); mgrErr == nil {
			pre = capturePreInstallState(ctx, mgr, i.binaryHash)
		}
	}
	var output string
	var err error
	switch method {
	case "apt":
		output, err = i.installAPT(ctx, channel)
	case "dnf":
		output, err = i.installDNF(ctx, channel)
	case "script", "":
		output, err = i.installScript(ctx, channel, version)
	default:
		return "", fmt.Errorf("unknown install method %q", method)
	}
	if err != nil {
		return output, err
	}
	if err := ctx.Err(); err != nil {
		return output, err
	}
	installed, actualVersion := i.detectVersion(ctx)
	if err := ctx.Err(); err != nil {
		return output, err
	}
	if !installed || !versionRe.MatchString(actualVersion) {
		return output, fmt.Errorf("sing-box installation did not produce a working binary with a valid version")
	}
	if version != "" && actualVersion != version {
		return output, fmt.Errorf("sing-box version mismatch: requested %s, installed %s", version, actualVersion)
	}
	if i.prepareService != nil {
		if err := i.prepareService(ctx); err != nil {
			return output, fmt.Errorf("prepare sing-box service: %w", err)
		}
	} else if i.serviceManager != nil {
		mgr, err := i.serviceManager()
		if err != nil {
			return output, fmt.Errorf("prepare sing-box service: %w", err)
		}
		var postHash string
		if i.binaryHash != nil {
			postHash = i.binaryHash()
		}
		if err := finalizeSingboxServiceWithManager(ctx, mgr, pre, postHash); err != nil {
			return output, fmt.Errorf("prepare sing-box service: %w", err)
		}
	}
	return output, nil
}

type preInstallServiceState struct {
	wasActive  bool
	pid        int
	startedAt  time.Time
	binaryHash string
}

func currentSingboxBinaryHash() string {
	sum, err := fileSHA256(singboxBinary())
	if err != nil {
		return ""
	}
	return sum
}

func capturePreInstallState(ctx context.Context, manager serviceManager, hashBinary func() string) preInstallServiceState {
	if manager == nil {
		return preInstallServiceState{}
	}
	state := preInstallServiceState{
		wasActive: manager.Active(ctx),
	}
	if state.wasActive {
		if pid, err := manager.MainPID(ctx); err == nil && pid > 0 {
			state.pid = pid
			if startedAt, err := manager.StartedAt(ctx, pid); err == nil {
				state.startedAt = startedAt
			}
		}
	}
	if hashBinary != nil {
		state.binaryHash = hashBinary()
	}
	return state
}

func finalizeSingboxServiceWithManager(ctx context.Context, manager serviceManager, pre preInstallServiceState, postHash string) error {
	if err := prepareSingboxServiceWithManager(ctx, manager); err != nil {
		return err
	}
	if !pre.wasActive {
		return nil
	}
	binaryChanged := pre.binaryHash == "" || postHash == "" || pre.binaryHash != postHash
	if !binaryChanged {
		return nil
	}
	alreadyRestarted := false
	if manager.Active(ctx) {
		if curPID, err := manager.MainPID(ctx); err == nil && curPID > 0 {
			if curStartedAt, err := manager.StartedAt(ctx, curPID); err == nil &&
				pre.pid > 0 && !pre.startedAt.IsZero() &&
				(curPID != pre.pid || !curStartedAt.Equal(pre.startedAt)) {
				alreadyRestarted = true
			}
		}
	}
	if !alreadyRestarted {
		if out, err := manager.Action(ctx, "restart"); err != nil {
			return fmt.Errorf("restart service after upgrade: %w: %s", err, out)
		}
	}
	return nil
}

func prepareSingboxServiceWithManager(ctx context.Context, manager serviceManager) error {
	if out, err := manager.Prepare(ctx); err != nil {
		return fmt.Errorf("prepare service: %w: %s", err, out)
	}
	if !manager.Enabled(ctx) {
		if out, err := manager.SetEnabled(ctx, true); err != nil {
			return fmt.Errorf("enable service at boot: %w: %s", err, out)
		}
	}
	return nil
}

func (i singboxInstaller) execInDir(ctx context.Context, dir, name string, args ...string) (string, error) {
	if i.runInDir != nil {
		return i.runInDir(ctx, dir, name, args...)
	}
	return i.run(ctx, name, args...)
}

func (i singboxInstaller) client() *http.Client {
	if i.httpClient != nil {
		return i.httpClient
	}
	return &http.Client{Timeout: 5 * time.Minute}
}

func (i singboxInstaller) apiBase() string {
	if i.githubAPIBase != "" {
		return strings.TrimRight(i.githubAPIBase, "/")
	}
	return defaultGitHubAPIBase
}

func (i singboxInstaller) downloadBase() string {
	if i.githubDownloadBase != "" {
		return strings.TrimRight(i.githubDownloadBase, "/")
	}
	return defaultGitHubDownloadBase
}

func (i singboxInstaller) targetBin() string {
	if i.targetBinary != "" {
		return i.targetBinary
	}
	return SingboxBinary
}

func isOpenWrtHost() bool {
	raw, err := os.ReadFile("/etc/os-release")
	if err != nil {
		return false
	}
	return strings.Contains(string(raw), "OPENWRT_ARCH")
}

func parseAPKMajorVersion(out string) (int, bool) {
	m := apkVersionRe.FindStringSubmatch(out)
	if len(m) < 2 {
		return 0, false
	}
	major, err := strconv.Atoi(m[1])
	if err != nil {
		return 0, false
	}
	return major, true
}

func (i singboxInstaller) shouldUseAlpineMuslTarball(ctx context.Context) bool {
	if i.lookPath == nil {
		return false
	}
	// Follow upstream install.sh precedence so non-Alpine systems with a stray
	// apk binary never switch away from their native package manager.
	for _, pm := range []string{"pacman", "dpkg", "dnf", "rpm"} {
		if i.lookPath(pm) {
			return false
		}
	}
	if !i.lookPath("apk") {
		return false
	}
	if i.isOpenWrt != nil && i.isOpenWrt() {
		return false
	}
	out, err := i.run(ctx, "apk", "--version")
	if err != nil {
		return false
	}
	major, ok := parseAPKMajorVersion(out)
	return ok && major < 3
}

func singboxMuslArch(goarch string) (string, error) {
	switch goarch {
	case "amd64":
		return "amd64", nil
	case "arm64":
		return "arm64", nil
	case "386":
		return "386", nil
	case "arm":
		return "armv7", nil
	case "loong64":
		return "loong64", nil
	case "riscv64":
		return "riscv64", nil
	default:
		return "", fmt.Errorf("unsupported Alpine architecture for sing-box musl binary: %s", goarch)
	}
}

func (i singboxInstaller) installScript(ctx context.Context, channel, version string) (string, error) {
	if i.shouldUseAlpineMuslTarball(ctx) {
		return i.installAlpineMuslTarball(ctx, channel, version)
	}
	workDir, err := os.MkdirTemp("", "singbox-install-*")
	if err != nil {
		return "", fmt.Errorf("create installer work directory: %w", err)
	}
	defer os.RemoveAll(workDir)

	path := filepath.Join(workDir, "install.sh")
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		return "", fmt.Errorf("create installer file: %w", err)
	}
	output, err := i.run(ctx, "curl", "-fsSL", "--output", path, defaultSingboxInstallShURL)
	if err != nil {
		return output, fmt.Errorf("download sing-box installer: %w", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		return output, fmt.Errorf("read sing-box installer: %w", err)
	}
	if info.Size() == 0 {
		return output, fmt.Errorf("downloaded sing-box installer is empty")
	}
	// Upstream install.sh omits `set -e` and ends with `rm -f "$package_name"`,
	// so `sh -e` ensures package manager failures are never masked as exit 0.
	args := []string{"-e", path}
	if channel == protocol.ChannelBeta {
		args = append(args, "--beta")
	}
	if version != "" {
		args = append(args, "--version", version)
	}
	installedOutput, err := i.execInDir(ctx, workDir, "sh", args...)
	if installedOutput != "" {
		if output != "" {
			output += "\n"
		}
		output += installedOutput
	}
	if err != nil {
		return output, fmt.Errorf("run sing-box installer: %w", err)
	}
	return output, nil
}

func (i singboxInstaller) installAlpineMuslTarball(ctx context.Context, channel, version string) (string, error) {
	goarch := i.goarch
	if goarch == "" {
		goarch = runtime.GOARCH
	}
	arch, err := singboxMuslArch(goarch)
	if err != nil {
		return "", err
	}
	targetVersion := version
	if targetVersion == "" {
		targetVersion, err = i.resolveSingboxReleaseVersion(ctx, channel)
		if err != nil {
			return "", err
		}
	}
	target := i.targetBin()
	targetDir := filepath.Dir(target)
	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		return "", fmt.Errorf("create sing-box target directory: %w", err)
	}
	tmp, err := os.CreateTemp(targetDir, ".sing-box.XXXXXX")
	if err != nil {
		return "", fmt.Errorf("create temporary sing-box binary: %w", err)
	}
	tmpPath := tmp.Name()
	cleanupTmp := true
	defer func() {
		if cleanupTmp {
			_ = os.Remove(tmpPath)
		}
	}()

	tarballURL := fmt.Sprintf("%s/SagerNet/sing-box/releases/download/v%s/sing-box-%s-linux-%s-musl.tar.gz",
		i.downloadBase(), targetVersion, targetVersion, arch)
	if err := i.downloadSingboxTarballBinary(ctx, tarballURL, tmp); err != nil {
		_ = tmp.Close()
		return "", err
	}
	if err := tmp.Chmod(0o755); err != nil {
		_ = tmp.Close()
		return "", fmt.Errorf("chmod sing-box binary: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return "", fmt.Errorf("sync sing-box binary: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return "", fmt.Errorf("close sing-box binary: %w", err)
	}

	installed, actualVersion := detectVersionAt(ctx, i.run, tmpPath)
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if !installed || !versionRe.MatchString(actualVersion) {
		return "", fmt.Errorf("downloaded sing-box binary failed version check")
	}
	if actualVersion != targetVersion {
		return "", fmt.Errorf("downloaded sing-box version mismatch: requested %s, got %s", targetVersion, actualVersion)
	}

	if err := i.removeConflictingAPKPackages(ctx, target); err != nil {
		return "", err
	}
	if err := os.Rename(tmpPath, target); err != nil {
		return "", fmt.Errorf("install sing-box binary: %w", err)
	}
	cleanupTmp = false
	return fmt.Sprintf("apk-tools < 3 detected; installed official sing-box %s (%s-musl) to %s", targetVersion, arch, target), nil
}

func (i singboxInstaller) resolveSingboxReleaseVersion(ctx context.Context, channel string) (string, error) {
	endpoint := i.apiBase() + "/repos/SagerNet/sing-box/releases/latest"
	if channel == protocol.ChannelBeta {
		endpoint = i.apiBase() + "/repos/SagerNet/sing-box/releases?per_page=15"
	}
	req, err := i.newGitHubRequest(ctx, endpoint)
	if err != nil {
		return "", fmt.Errorf("build sing-box release request: %w", err)
	}
	resp, err := i.client().Do(req)
	if err != nil {
		return "", fmt.Errorf("query sing-box release: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return "", fmt.Errorf("query sing-box release: HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxGitHubReleaseJSONBytes+1))
	if err != nil {
		return "", fmt.Errorf("read sing-box release response: %w", err)
	}
	if len(body) > maxGitHubReleaseJSONBytes {
		return "", fmt.Errorf("sing-box release response exceeds size limit")
	}

	var tagName string
	if channel == protocol.ChannelBeta {
		var releases []struct {
			TagName string `json:"tag_name"`
		}
		if err := json.Unmarshal(body, &releases); err != nil {
			return "", fmt.Errorf("decode sing-box releases: %w", err)
		}
		for _, rel := range releases {
			if strings.TrimSpace(rel.TagName) != "" {
				tagName = rel.TagName
				break
			}
		}
	} else {
		var release struct {
			TagName string `json:"tag_name"`
		}
		if err := json.Unmarshal(body, &release); err != nil {
			return "", fmt.Errorf("decode sing-box latest release: %w", err)
		}
		tagName = release.TagName
	}

	ver := strings.TrimPrefix(strings.TrimSpace(tagName), "v")
	if !versionRe.MatchString(ver) {
		return "", fmt.Errorf("invalid sing-box release tag %q", tagName)
	}
	return ver, nil
}

func (i singboxInstaller) downloadSingboxTarballBinary(ctx context.Context, tarballURL string, dst io.Writer) error {
	req, err := i.newGitHubRequest(ctx, tarballURL)
	if err != nil {
		return fmt.Errorf("build sing-box tarball request: %w", err)
	}
	resp, err := i.client().Do(req)
	if err != nil {
		return fmt.Errorf("download sing-box tarball: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download sing-box tarball: HTTP %d", resp.StatusCode)
	}

	gz, err := gzip.NewReader(io.LimitReader(resp.Body, maxSingboxTarballBytes))
	if err != nil {
		return fmt.Errorf("open sing-box tarball gzip stream: %w", err)
	}
	defer gz.Close()

	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return errors.New("sing-box binary not found in official musl archive")
		}
		if err != nil {
			return fmt.Errorf("read sing-box tarball: %w", err)
		}
		if hdr.Typeflag != tar.TypeReg {
			continue
		}
		cleaned := path.Clean(hdr.Name)
		if strings.HasPrefix(cleaned, "..") || path.IsAbs(cleaned) {
			return fmt.Errorf("unsafe archive path %q", hdr.Name)
		}
		if path.Base(cleaned) != "sing-box" {
			continue
		}
		if hdr.Size <= 0 || hdr.Size > maxSingboxBinaryBytes {
			return fmt.Errorf("unexpected sing-box binary size %d in archive", hdr.Size)
		}
		n, err := io.Copy(dst, io.LimitReader(tr, maxSingboxBinaryBytes+1))
		if err != nil {
			return fmt.Errorf("extract sing-box binary: %w", err)
		}
		if n <= 0 || n > maxSingboxBinaryBytes {
			return fmt.Errorf("extracted sing-box binary size %d out of bounds", n)
		}
		return nil
	}
}

func (i singboxInstaller) newGitHubRequest(ctx context.Context, rawURL string) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "singbox-panel-agent")
	if token := strings.TrimSpace(os.Getenv("GITHUB_TOKEN")); token != "" {
		req.Header.Set("Authorization", "token "+token)
	}
	return req, nil
}

type preservedPackageFile struct {
	path string
	data []byte
	mode os.FileMode
}

func (i singboxInstaller) removeConflictingAPKPackages(ctx context.Context, target string) error {
	if i.lookPath == nil || !i.lookPath("apk") {
		return nil
	}
	cleanupPaths := i.apkCleanupPaths
	if len(cleanupPaths) == 0 {
		if target != SingboxBinary {
			return nil
		}
		cleanupPaths = []string{ConfigFile, openRCSingboxConf}
	}
	var preserved []preservedPackageFile
	for _, cfgPath := range cleanupPaths {
		info, err := os.Stat(cfgPath)
		if err != nil || info.IsDir() {
			continue
		}
		data, err := os.ReadFile(cfgPath)
		if err != nil {
			return fmt.Errorf("snapshot %s before apk cleanup: %w", cfgPath, err)
		}
		preserved = append(preserved, preservedPackageFile{path: cfgPath, data: data, mode: info.Mode().Perm()})
	}
	for _, pkg := range []string{"sing-box-beta", "sing-box"} {
		if _, err := i.run(ctx, "apk", "info", "-e", pkg); err != nil {
			continue
		}
		if out, err := i.run(ctx, "apk", "del", pkg); err != nil {
			return fmt.Errorf("remove conflicting apk package %s: %w: %s", pkg, err, out)
		}
	}
	for _, f := range preserved {
		if _, err := os.Stat(f.path); errors.Is(err, os.ErrNotExist) {
			if err := os.MkdirAll(filepath.Dir(f.path), 0o755); err != nil {
				return fmt.Errorf("restore %s directory after apk cleanup: %w", f.path, err)
			}
			if err := writeAtomicFile(f.path, f.data, f.mode); err != nil {
				return fmt.Errorf("restore %s after apk cleanup: %w", f.path, err)
			}
		}
	}
	return nil
}

func pkgName(channel string) string {
	if channel == protocol.ChannelBeta {
		return "sing-box-beta"
	}
	return "sing-box"
}

func (i singboxInstaller) installAPT(ctx context.Context, channel string) (string, error) {
	if i.lookPath != nil && !i.lookPath("apt-get") {
		return "", fmt.Errorf("当前节点未检测到 apt-get，不支持 APT 安装方式，请改用官方脚本安装")
	}
	script := `set -e
mkdir -p /etc/apt/keyrings
curl -fsSL https://sing-box.app/gpg.key -o /etc/apt/keyrings/sagernet.asc
chmod a+r /etc/apt/keyrings/sagernet.asc
printf '%s\n' 'Types: deb' 'URIs: https://deb.sagernet.org/' 'Suites: *' 'Components: *' 'Enabled: yes' 'Signed-By: /etc/apt/keyrings/sagernet.asc' > /etc/apt/sources.list.d/sagernet.sources
apt-get update
DEBIAN_FRONTEND=noninteractive apt-get install -y ` + pkgName(channel)
	return i.run(ctx, "sh", "-c", script)
}

func (i singboxInstaller) installDNF(ctx context.Context, channel string) (string, error) {
	if i.lookPath != nil && !i.lookPath("dnf") {
		return "", fmt.Errorf("当前节点未检测到 dnf，不支持 DNF 安装方式，请改用官方脚本安装")
	}
	script := `set -e
dnf config-manager addrepo --from-repofile=https://sing-box.app/sing-box.repo 2>/dev/null || dnf config-manager --add-repo https://sing-box.app/sing-box.repo
dnf install -y ` + pkgName(channel)
	return i.run(ctx, "sh", "-c", script)
}

// DetectVersion reports whether the sing-box binary is present and its version.
func DetectVersion(ctx context.Context) (installed bool, version string) {
	return detectVersionAt(ctx, run, singboxBinary())
}

func detectVersionAt(ctx context.Context, runFn commandRunner, bin string) (installed bool, version string) {
	if _, err := os.Stat(bin); err != nil {
		return false, ""
	}
	if runFn == nil {
		runFn = run
	}
	if out, err := runFn(ctx, bin, "version", "-n"); err == nil && out != "" {
		return true, firstLine(out)
	}
	// Fallback for builds without `-n`: parse `sing-box version <X> ...`.
	if out, err := runFn(ctx, bin, "version"); err == nil {
		fields := strings.Fields(firstLine(out))
		if len(fields) >= 3 && fields[0] == "sing-box" && fields[1] == "version" {
			return true, fields[2]
		}
		return true, firstLine(out)
	}
	return true, ""
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return strings.TrimSpace(s[:i])
	}
	return strings.TrimSpace(s)
}
