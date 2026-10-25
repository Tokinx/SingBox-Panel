package singbox

import (
	"context"
	"crypto/ecdh"
	"crypto/rand"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

// This opt-in test runs a real sing-box server and client against local TLS
// camouflage and HTTP fixtures. It never depends on a public camouflage site.
// Run with SING_BOX_BINARY=/absolute/path/to/sing-box go test ./internal/domain/singbox -run TestRealityGuardLive -v.
func TestRealityGuardLive(t *testing.T) {
	bin := os.Getenv("SING_BOX_BINARY")
	if bin == "" {
		t.Skip("set SING_BOX_BINARY to run real REALITY connections")
	}
	bin, err := filepath.Abs(bin)
	if err != nil {
		t.Fatal(err)
	}
	localIP := guardLiveNonLoopbackIPv4(t)
	handshake := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = io.WriteString(w, "camouflage") }))
	handshake.TLS = &tls.Config{MinVersion: tls.VersionTLS13}
	handshake.StartTLS()
	defer handshake.Close()
	_, handshakePort, _ := net.SplitHostPort(handshake.Listener.Addr().String())
	hp, _ := strconv.Atoi(handshakePort)
	origin := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = io.WriteString(w, "guard-live-ok") }))
	_ = origin.Listener.Close()
	origin.Listener, err = net.Listen("tcp4", "0.0.0.0:0")
	if err != nil {
		t.Fatal(err)
	}
	origin.Start()
	defer origin.Close()
	_, originPort, _ := net.SplitHostPort(origin.Listener.Addr().String())
	ports := guardLivePorts(t, 4)
	publicPort, internalPort, socksPort, statsPort := ports[0], ports[1], ports[2], ports[3]
	private, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	in := testGuardInbound()
	in.ListenPort = publicPort
	in.Settings.TLS.ServerName = "guard.test"
	in.Settings.TLS.Reality.HandshakeServer = "127.0.0.1"
	in.Settings.TLS.Reality.HandshakeServerPort = hp
	in.Settings.TLS.Reality.PrivateKey = base64.RawURLEncoding.EncodeToString(private.Bytes())
	in.Settings.TLS.Reality.PublicKey = base64.RawURLEncoding.EncodeToString(private.PublicKey().Bytes())
	in.Settings.TLS.Reality.Guard.InternalPort = internalPort
	statsAddress := net.JoinHostPort("127.0.0.1", strconv.Itoa(statsPort))
	server, err := BuildServerConfig(ServerConfigInput{Inbounds: []InboundInput{in}, StatsController: statsAddress, StatsSecret: "live-test-secret"})
	if err != nil {
		t.Fatal(err)
	}
	guardLiveRun(t, bin, "server", server, publicPort)
	outbound, err := BuildClientOutbound(ClientNode{Name: "proxy", Type: "vless", Server: "127.0.0.1", ServerPort: publicPort, Settings: in.Settings, User: in.Settings.SingleUserIdentity()})
	if err != nil {
		t.Fatal(err)
	}
	client, _ := json.Marshal(map[string]any{
		"log":       map[string]any{"level": "debug"},
		"inbounds":  []any{map[string]any{"type": "mixed", "tag": "local", "listen": "127.0.0.1", "listen_port": socksPort}},
		"outbounds": []json.RawMessage{outbound}, "route": map[string]any{"final": "proxy"},
	})
	guardLiveRun(t, bin, "client", client, socksPort)
	proxyURL, _ := url.Parse("socks5://127.0.0.1:" + strconv.Itoa(socksPort))
	transport := &http.Transport{Proxy: http.ProxyURL(proxyURL), DisableKeepAlives: true}
	defer transport.CloseIdleConnections()
	httpClient := &http.Client{Transport: transport, Timeout: 5 * time.Second}
	t.Run("valid SNI and REALITY credentials", func(t *testing.T) {
		resp, err := httpClient.Get("http://" + net.JoinHostPort(localIP, originPort))
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		if string(body) != "guard-live-ok" {
			t.Fatalf("unexpected origin reply: %s", body)
		}
	})
	for _, name := range []string{"wrong.test", ""} {
		t.Run("reject SNI "+name, func(t *testing.T) {
			dialer := &net.Dialer{Timeout: 2 * time.Second}
			conn, err := tls.DialWithDialer(dialer, "tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(publicPort)), &tls.Config{ServerName: name, InsecureSkipVerify: true, MinVersion: tls.VersionTLS13})
			if err == nil {
				conn.Close()
				t.Fatal("SNI outside whitelist reached the TLS listener")
			}
		})
	}
	t.Run("reject plaintext", func(t *testing.T) {
		conn, err := net.DialTimeout("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(publicPort)), 2*time.Second)
		if err != nil {
			t.Fatal(err)
		}
		defer conn.Close()
		_ = conn.SetDeadline(time.Now().Add(2 * time.Second))
		_, _ = io.WriteString(conn, "GET / HTTP/1.1\r\nHost: guard.test\r\n\r\n")
		buf := make([]byte, 256)
		n, err := conn.Read(buf)
		if n != 0 || err == nil {
			t.Fatalf("plaintext was forwarded: %q, %v", buf[:n], err)
		}
	})
	t.Run("internal listener stays loopback only", func(t *testing.T) {
		conn, err := net.DialTimeout("tcp", net.JoinHostPort(localIP, strconv.Itoa(internalPort)), time.Second)
		if err == nil {
			conn.Close()
			t.Fatal("internal listener is publicly bound")
		}
	})
	t.Run("authenticated users cannot bypass loopback protection", func(t *testing.T) {
		originLoopbackPort, _ := strconv.Atoi(originPort)
		// The same HTTP origin succeeds through its non-loopback address above;
		// failure here proves routing protection, not a TLS/plaintext mismatch.
		for _, port := range []int{statsPort, originLoopbackPort} {
			resp, err := httpClient.Get("http://127.0.0.1:" + strconv.Itoa(port))
			if err == nil {
				resp.Body.Close()
				t.Fatalf("proxied loopback port %d was reachable", port)
			}
		}
	})
}

func guardLivePorts(t *testing.T, n int) []int {
	t.Helper()
	var listeners []net.Listener
	defer func() {
		for _, ln := range listeners {
			ln.Close()
		}
	}()
	var ports []int
	for i := 0; i < n; i++ {
		ln, err := net.Listen("tcp", ":0")
		if err != nil {
			t.Fatal(err)
		}
		listeners = append(listeners, ln)
		ports = append(ports, ln.Addr().(*net.TCPAddr).Port)
	}
	return ports
}

func guardLiveNonLoopbackIPv4(t *testing.T) string {
	t.Helper()
	addresses, err := net.InterfaceAddrs()
	if err != nil {
		t.Fatal(err)
	}
	for _, address := range addresses {
		if network, ok := address.(*net.IPNet); ok && !network.IP.IsLoopback() && network.IP.To4() != nil {
			return network.IP.String()
		}
	}
	t.Skip("real connection test needs a non-loopback IPv4 interface")
	return ""
}

func guardLiveRun(t *testing.T, bin, name string, config []byte, port int) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, name+".json")
	if err := os.WriteFile(path, config, 0600); err != nil {
		t.Fatal(err)
	}
	if output, err := exec.Command(bin, "check", "-c", path).CombinedOutput(); err != nil {
		t.Fatalf("%s config check: %v\n%s", name, err, output)
	}
	logPath := filepath.Join(dir, name+".log")
	log, err := os.Create(logPath)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cmd := exec.CommandContext(ctx, bin, "run", "-c", path)
	cmd.Stdout, cmd.Stderr = log, log
	if err := cmd.Start(); err != nil {
		cancel()
		log.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cancel()
		_ = cmd.Wait()
		_ = log.Close()
		if t.Failed() {
			data, _ := os.ReadFile(logPath)
			t.Logf("%s log:\n%s", name, data)
		}
	})
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		conn, err := net.DialTimeout("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)), 100*time.Millisecond)
		if err == nil {
			conn.Close()
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("%s listener did not become ready", name)
}
