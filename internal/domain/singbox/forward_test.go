package singbox

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestBuildForwardInboundAndRuleOrdering(t *testing.T) {
	raw, err := BuildServerConfig(ServerConfigInput{
		Inbounds: []InboundInput{testGuardInbound()},
		Forwards: []ForwardInput{
			{
				Tag:             "fwd-hk-tcp",
				ListenPort:      10080,
				Network:         "tcp",
				OverrideAddress: "198.51.100.10",
				OverridePort:    443,
				Outbound:        "direct",
			},
			{
				Tag:             "fwd-us-dual",
				ListenPort:      10081,
				Network:         "",
				OverrideAddress: "us.example.com",
				OverridePort:    8443,
				Outbound:        "transit-out",
			},
		},
		Outbounds: []OutboundInput{
			{Tag: "transit-out", Type: "socks", Server: "203.0.113.5", ServerPort: 1080},
		},
		Rules: []RuleInput{
			{DomainSuffix: []string{"example.org"}, Outbound: "block"},
			{Outbound: "transit-out"},
		},
		Final:           "transit-out",
		StatsController: "127.0.0.1:29091",
		StatsSecret:     "secret",
		StatsAPI:        "127.0.0.1:29092",
	})
	if err != nil {
		t.Fatal(err)
	}

	var root map[string]any
	if err := json.Unmarshal(raw, &root); err != nil {
		t.Fatal(err)
	}

	var foundTCP, foundDual map[string]any
	for _, entry := range mSlice(root, "inbounds") {
		m := entry.(map[string]any)
		switch mStr(m, "tag") {
		case "fwd-hk-tcp":
			foundTCP = m
		case "fwd-us-dual":
			foundDual = m
		}
	}
	if foundTCP == nil || foundDual == nil {
		t.Fatalf("missing generated forward inbounds: %s", raw)
	}
	if mStr(foundTCP, "type") != "direct" || mStr(foundTCP, "listen") != "::" ||
		mInt(foundTCP, "listen_port") != 10080 || mStr(foundTCP, "network") != "tcp" ||
		mStr(foundTCP, "override_address") != "198.51.100.10" || mInt(foundTCP, "override_port") != 443 {
		t.Fatalf("unexpected TCP forward inbound: %+v", foundTCP)
	}
	if _, hasNet := foundDual["network"]; hasNet {
		t.Fatalf("empty network should be omitted so both TCP and UDP listen: %+v", foundDual)
	}
	if mStr(foundDual, "override_address") != "us.example.com" || mInt(foundDual, "override_port") != 8443 {
		t.Fatalf("unexpected dual forward inbound: %+v", foundDual)
	}

	rules := mSlice(mMap(root, "route"), "rules")
	// Expected order: 3 guard rules, 1 loopback guard rule, 2 forward rules, 1 sniff rule, 2 user rules = 9 rules
	if len(rules) != 9 {
		t.Fatalf("rules count = %d, want 9; rules = %s", len(rules), raw)
	}
	if !IsLoopbackGuardRule(rules[3].(map[string]any)) {
		t.Fatalf("rule[3] should be loopback guard: %v", rules[3])
	}
	if canonicalJSON(rules[4]) != `{"action":"route","inbound":["fwd-hk-tcp"],"outbound":"direct"}` {
		t.Fatalf("rule[4] = %s", canonicalJSON(rules[4]))
	}
	if canonicalJSON(rules[5]) != `{"action":"route","inbound":["fwd-us-dual"],"outbound":"transit-out"}` {
		t.Fatalf("rule[5] = %s", canonicalJSON(rules[5]))
	}
	if mStr(rules[6].(map[string]any), "action") != "sniff" {
		t.Fatalf("sniff should appear after forward rules: %v", rules[6])
	}
	if mStr(rules[7].(map[string]any), "action") != "reject" {
		t.Fatalf("user rule should follow forward/sniff rules: %v", rules[7])
	}
}

func TestBuildServerConfigForwardsOnly(t *testing.T) {
	raw, err := BuildServerConfig(ServerConfigInput{
		Forwards: []ForwardInput{
			{
				Tag:             "only-fwd",
				ListenPort:      12000,
				Network:         "udp",
				OverrideAddress: "2001:db8::1",
				OverridePort:    51820,
			},
		},
		StatsController: "127.0.0.1:29091",
		StatsSecret:     "s",
		StatsAPI:        "127.0.0.1:29092",
	})
	if err != nil {
		t.Fatal(err)
	}
	var root map[string]any
	if err := json.Unmarshal(raw, &root); err != nil {
		t.Fatal(err)
	}
	ins := mSlice(root, "inbounds")
	if len(ins) != 1 || mStr(ins[0].(map[string]any), "tag") != "only-fwd" {
		t.Fatalf("unexpected inbounds: %s", raw)
	}
	if got := mStr(mSlice(mMap(root, "route"), "rules")[1].(map[string]any), "outbound"); got != "direct" {
		t.Fatalf("default forward outbound = %q, want direct", got)
	}
}

func TestValidateForwardLayoutAndSecurityChecks(t *testing.T) {
	validInbounds := []InboundInput{testGuardInbound()}
	baseFwd := ForwardInput{
		Tag:             "fwd-ok",
		ListenPort:      20001,
		Network:         "",
		OverrideAddress: "198.51.100.2",
		OverridePort:    443,
		Outbound:        "direct",
	}
	if err := ValidateForwardLayout(validInbounds, []ForwardInput{baseFwd}, "127.0.0.1:29091", "127.0.0.1:29092"); err != nil {
		t.Fatalf("valid layout rejected: %v", err)
	}

	invalidCases := map[string]ForwardInput{
		"empty tag":      {Tag: "", ListenPort: 20001, OverrideAddress: "198.51.100.2", OverridePort: 443},
		"port 0":         {Tag: "f1", ListenPort: 0, OverrideAddress: "198.51.100.2", OverridePort: 443},
		"port 65536":     {Tag: "f1", ListenPort: 65536, OverrideAddress: "198.51.100.2", OverridePort: 443},
		"bad network":    {Tag: "f1", ListenPort: 20001, Network: "icmp", OverrideAddress: "198.51.100.2", OverridePort: 443},
		"empty target":   {Tag: "f1", ListenPort: 20001, OverrideAddress: "", OverridePort: 443},
		"target w/ port": {Tag: "f1", ListenPort: 20001, OverrideAddress: "198.51.100.2:443", OverridePort: 443},
		"target scheme":  {Tag: "f1", ListenPort: 20001, OverrideAddress: "https://example.com", OverridePort: 443},
		"target port 0":  {Tag: "f1", ListenPort: 20001, OverrideAddress: "198.51.100.2", OverridePort: 0},
		"outbound block": {Tag: "f1", ListenPort: 20001, OverrideAddress: "198.51.100.2", OverridePort: 443, Outbound: "block"},
		"outbound sniff": {Tag: "f1", ListenPort: 20001, OverrideAddress: "198.51.100.2", OverridePort: 443, Outbound: "sniff"},
		"inbound tag":    {Tag: "reality-in", ListenPort: 20001, OverrideAddress: "198.51.100.2", OverridePort: 443},
		"guard tag":      {Tag: "reality-guard", ListenPort: 20001, OverrideAddress: "198.51.100.2", OverridePort: 443},
		"inbound port":   {Tag: "f1", ListenPort: 31735, OverrideAddress: "198.51.100.2", OverridePort: 443},
		"guard int port": {Tag: "f1", ListenPort: 31736, OverrideAddress: "198.51.100.2", OverridePort: 443},
		"stats ctrl":     {Tag: "f1", ListenPort: 29091, OverrideAddress: "198.51.100.2", OverridePort: 443},
		"stats api":      {Tag: "f1", ListenPort: 29092, OverrideAddress: "198.51.100.2", OverridePort: 443},
	}
	for name, tc := range invalidCases {
		t.Run(name, func(t *testing.T) {
			if err := ValidateForwardLayout(validInbounds, []ForwardInput{tc}, "127.0.0.1:29091", "127.0.0.1:29092"); err == nil {
				t.Fatalf("invalid case %q accepted: %+v", name, tc)
			}
		})
	}

	// Duplicate forward tags / ports among forwards.
	if err := ValidateForwardLayout(nil, []ForwardInput{baseFwd, {Tag: "fwd-ok", ListenPort: 20002, OverrideAddress: "198.51.100.3", OverridePort: 80}}); err == nil {
		t.Fatal("duplicate forward tags accepted")
	}
	if err := ValidateForwardLayout(nil, []ForwardInput{baseFwd, {Tag: "fwd-other", ListenPort: 20001, OverrideAddress: "198.51.100.3", OverridePort: 80}}); err == nil {
		t.Fatal("duplicate forward listen ports accepted")
	}

	disallowed := []string{
		"", "127.0.0.1", "127.0.1.1", "::1", "[::1]", "::ffff:127.0.0.1",
		"0.0.0.0", "::", "169.254.169.254", "::ffff:169.254.169.254", "fe80::1",
		"224.0.0.1", "localhost", "api.localhost", "metadata.google.internal", "LOCALHOST.",
	}
	for _, target := range disallowed {
		if !IsDisallowedForwardTarget(target) {
			t.Fatalf("target %q should be rejected as unsafe", target)
		}
	}
	allowed := []string{"198.51.100.1", "2001:db8::1", "10.0.0.5", "192.168.10.20", "relay.example.com"}
	for _, target := range allowed {
		if IsDisallowedForwardTarget(target) {
			t.Fatalf("target %q should be allowed", target)
		}
	}
}

func TestParseServerConfigForwardsRoundTripAndEdgeCases(t *testing.T) {
	input := ServerConfigInput{
		Inbounds: []InboundInput{testGuardInbound()},
		Forwards: []ForwardInput{
			{Tag: "fwd-direct", ListenPort: 10080, Network: "", OverrideAddress: "198.51.100.10", OverridePort: 443, Outbound: "direct"},
			{Tag: "fwd-proxy", ListenPort: 10081, Network: "tcp", OverrideAddress: "relay.example.com", OverridePort: 8443, Outbound: "landing-socks"},
		},
		Outbounds: []OutboundInput{
			{Tag: "landing-socks", Type: "socks", Server: "203.0.113.10", ServerPort: 1080},
		},
		Rules: []RuleInput{
			{DomainSuffix: []string{"example.com"}, Outbound: "landing-socks"},
		},
		Final:           "direct",
		StatsController: "127.0.0.1:29091",
		StatsSecret:     "secret",
		StatsAPI:        "127.0.0.1:29092",
	}

	raw, err := BuildServerConfig(input)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := ParseServerConfig(raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(parsed.Skipped) != 0 {
		t.Fatalf("unexpected skipped items: %v", parsed.Skipped)
	}
	if len(parsed.Inbounds) != 1 || len(parsed.Forwards) != 2 || len(parsed.Rules) != 2 {
		t.Fatalf("unexpected parsed counts: inbounds=%d forwards=%d rules=%d", len(parsed.Inbounds), len(parsed.Forwards), len(parsed.Rules))
	}
	if parsed.Rules[0].Match.Action != "sniff" || parsed.Rules[1].Outbound != "landing-socks" {
		t.Fatalf("unexpected parsed rules: %+v", parsed.Rules)
	}
	if parsed.Forwards[0] != input.Forwards[0] || parsed.Forwards[1] != input.Forwards[1] {
		t.Fatalf("parsed forwards mismatch: got %+v, want %+v", parsed.Forwards, input.Forwards)
	}

	r1 := parsed.Rules[0].Match
	r1.Outbound = parsed.Rules[0].Outbound
	r2 := parsed.Rules[1].Match
	r2.Outbound = parsed.Rules[1].Outbound
	rebuilt, err := BuildServerConfig(ServerConfigInput{
		Inbounds:        []InboundInput{{Tag: parsed.Inbounds[0].Tag, Type: parsed.Inbounds[0].Type, ListenPort: parsed.Inbounds[0].ListenPort, Settings: parsed.Inbounds[0].Settings}},
		Forwards:        parsed.Forwards,
		Outbounds:       input.Outbounds,
		Rules:           []RuleInput{r1, r2},
		Final:           parsed.Final,
		StatsController: "127.0.0.1:29091",
		StatsSecret:     "secret",
		StatsAPI:        "127.0.0.1:29092",
	})
	if err != nil {
		t.Fatal(err)
	}
	if canonicalJSON(raw) != canonicalJSON(rebuilt) {
		t.Fatalf("round-trip mismatch:\norig:    %s\nrebuilt: %s", canonicalJSON(raw), canonicalJSON(rebuilt))
	}

	// Ensure earlier global non-sniff rules or unsupported direct fields are skipped safely instead of changing semantics.
	handWritten := `{
		"inbounds": [
			{"type": "direct", "tag": "fwd-early-global", "listen": "::", "listen_port": 11000, "override_address": "1.1.1.1", "override_port": 80},
			{"type": "direct", "tag": "fwd-no-target", "listen": "::", "listen_port": 11001},
			{"type": "direct", "tag": "fwd-selector", "listen": "::", "listen_port": 11002, "override_address": "1.1.1.1", "override_port": 443}
		],
		"outbounds": [
			{"type": "direct", "tag": "direct"},
			{"type": "selector", "tag": "auto-group", "outbounds": ["direct"]}
		],
		"route": {
			"rules": [
				{"inbound": ["fwd-selector"], "action": "route", "outbound": "auto-group"},
				{"protocol": "bittorrent", "action": "reject"},
				{"inbound": ["fwd-early-global"], "action": "route", "outbound": "direct"}
			]
		}
	}`
	parsedHand, err := ParseServerConfig([]byte(handWritten))
	if err != nil {
		t.Fatal(err)
	}
	if len(parsedHand.Forwards) != 0 {
		t.Fatalf("unsafe/unsupported direct inbounds should be skipped, got %+v", parsedHand.Forwards)
	}
	if len(parsedHand.Skipped) != 3 {
		t.Fatalf("expected 3 skip notes (unimported outbound, missing target, earlier global rule), got %v", parsedHand.Skipped)
	}
}

func resolveTestSingboxBinary() string {
	if p := strings.TrimSpace(os.Getenv("SING_BOX_BINARY")); p != "" {
		return p
	}
	candidates := []string{
		filepath.Join("..", "..", "..", ".codex-build", "reality-guard", "sing-box-musl"),
		"sing-box",
	}
	for _, c := range candidates {
		if path, err := exec.LookPath(c); err == nil {
			return path
		}
		if info, err := os.Stat(c); err == nil && !info.IsDir() {
			return c
		}
	}
	return ""
}

func reserveFreeTCPPort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	return ln.Addr().(*net.TCPAddr).Port
}

func reserveFreeUDPPort(t *testing.T) int {
	t.Helper()
	pc, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer pc.Close()
	return pc.LocalAddr().(*net.UDPAddr).Port
}

func TestForwardSingBoxCheckAndLiveTraffic(t *testing.T) {
	bin := resolveTestSingboxBinary()
	if bin == "" {
		t.Skip("sing-box binary not found; set SING_BOX_BINARY to run live validation")
	}

	// 1. Validate full managed config (including StatsController + StatsAPI + forwards-only & hybrid).
	checkRaw, err := BuildServerConfig(ServerConfigInput{
		Inbounds: []InboundInput{testGuardInbound()},
		Forwards: []ForwardInput{
			{Tag: "fwd-dual", ListenPort: 10080, Network: "", OverrideAddress: "198.51.100.10", OverridePort: 443, Outbound: "direct"},
			{Tag: "fwd-tcp", ListenPort: 10081, Network: "tcp", OverrideAddress: "relay.example.com", OverridePort: 8443, Outbound: "landing-socks"},
			{Tag: "fwd-udp", ListenPort: 10082, Network: "udp", OverrideAddress: "198.51.100.20", OverridePort: 51820, Outbound: "direct"},
		},
		Outbounds: []OutboundInput{
			{Tag: "landing-socks", Type: "socks", Server: "203.0.113.10", ServerPort: 1080},
		},
		StatsController: "127.0.0.1:29091",
		StatsSecret:     "test-secret",
		StatsAPI:        "127.0.0.1:29092",
	})
	if err != nil {
		t.Fatal(err)
	}
	checkCfgPath := filepath.Join(t.TempDir(), "check.json")
	if err := os.WriteFile(checkCfgPath, checkRaw, 0o600); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command(bin, "check", "-c", checkCfgPath).CombinedOutput(); err != nil {
		t.Fatalf("sing-box check failed: %v\n%s\nconfig:\n%s", err, out, checkRaw)
	}

	// 2. Live TCP, UDP, and outbound-chain forwarding end-to-end.
	tcpEchoLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer tcpEchoLn.Close()
	go func() {
		for {
			conn, err := tcpEchoLn.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				_, _ = io.Copy(c, c)
			}(conn)
		}
	}()

	udpEchoConn, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer udpEchoConn.Close()
	go func() {
		buf := make([]byte, 2048)
		for {
			n, addr, err := udpEchoConn.ReadFrom(buf)
			if err != nil {
				return
			}
			_, _ = udpEchoConn.WriteTo(buf[:n], addr)
		}
	}()

	socksInPort := reserveFreeTCPPort(t)
	fwdDirectTCPPort := reserveFreeTCPPort(t)
	fwdDirectUDPPort := reserveFreeUDPPort(t)
	fwdRelayPort := reserveFreeTCPPort(t)

	liveRaw, err := BuildServerConfig(ServerConfigInput{
		LogLevel: "warn",
		Inbounds: []InboundInput{
			{Tag: "local-socks-in", Type: "socks", ListenPort: socksInPort},
		},
		Forwards: []ForwardInput{
			{
				Tag:             "live-tcp",
				ListenPort:      fwdDirectTCPPort,
				Network:         "tcp",
				OverrideAddress: "127.0.0.1",
				OverridePort:    tcpEchoLn.Addr().(*net.TCPAddr).Port,
				Outbound:        "direct",
			},
			{
				Tag:             "live-udp",
				ListenPort:      fwdDirectUDPPort,
				Network:         "udp",
				OverrideAddress: "127.0.0.1",
				OverridePort:    udpEchoConn.LocalAddr().(*net.UDPAddr).Port,
				Outbound:        "direct",
			},
			{
				Tag:             "live-via-socks",
				ListenPort:      fwdRelayPort,
				Network:         "tcp",
				OverrideAddress: "127.0.0.1",
				OverridePort:    tcpEchoLn.Addr().(*net.TCPAddr).Port,
				Outbound:        "local-socks-out",
			},
		},
		Outbounds: []OutboundInput{
			{Tag: "local-socks-out", Type: "socks", Server: "127.0.0.1", ServerPort: socksInPort},
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	liveCfgPath := filepath.Join(t.TempDir(), "live.json")
	if err := os.WriteFile(liveCfgPath, liveRaw, 0o600); err != nil {
		t.Fatal(err)
	}
	var sbOut bytes.Buffer
	cmd := exec.Command(bin, "run", "-c", liveCfgPath)
	cmd.Stdout = &sbOut
	cmd.Stderr = &sbOut
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	}()

	// Verify TCP direct forward, TCP via outbound chain, and UDP direct forward.
	assertTCPEcho(t, fwdDirectTCPPort, []byte("hello-direct-tcp"), &sbOut)
	assertTCPEcho(t, fwdRelayPort, []byte("hello-relay-socks"), &sbOut)
	assertUDPEcho(t, fwdDirectUDPPort, []byte("hello-direct-udp"), &sbOut)
}

func assertTCPEcho(t *testing.T, port int, payload []byte, sbLog *bytes.Buffer) {
	t.Helper()
	var conn net.Conn
	var err error
	deadline := time.Now().Add(4 * time.Second)
	for time.Now().Before(deadline) {
		conn, err = net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), 300*time.Millisecond)
		if err == nil {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if err != nil {
		t.Fatalf("dial tcp 127.0.0.1:%d failed: %v\nsing-box log:\n%s", port, err, sbLog.String())
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(2 * time.Second))
	if _, err := conn.Write(payload); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, len(payload))
	if _, err := io.ReadFull(conn, buf); err != nil {
		t.Fatalf("read tcp echo on port %d failed: %v\nsing-box log:\n%s", port, err, sbLog.String())
	}
	if !bytes.Equal(buf, payload) {
		t.Fatalf("tcp echo mismatch = %q, want %q", buf, payload)
	}
}

func assertUDPEcho(t *testing.T, port int, payload []byte, sbLog *bytes.Buffer) {
	t.Helper()
	deadline := time.Now().Add(4 * time.Second)
	for {
		conn, err := net.DialTimeout("udp", fmt.Sprintf("127.0.0.1:%d", port), 300*time.Millisecond)
		if err == nil {
			_ = conn.SetDeadline(time.Now().Add(500 * time.Millisecond))
			_, werr := conn.Write(payload)
			buf := make([]byte, len(payload)*2)
			n, rerr := conn.Read(buf)
			_ = conn.Close()
			if werr == nil && rerr == nil && bytes.Equal(buf[:n], payload) {
				return
			}
			if rerr != nil && !errors.Is(rerr, os.ErrDeadlineExceeded) && time.Now().After(deadline) {
				t.Fatalf("udp echo on port %d failed: %v\nsing-box log:\n%s", port, rerr, sbLog.String())
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("udp echo on port %d timed out\nsing-box log:\n%s", port, sbLog.String())
		}
		time.Sleep(75 * time.Millisecond)
	}
}
