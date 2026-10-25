package singbox

import (
	"encoding/json"
	"strings"
	"testing"
)

func testGuardInbound() InboundInput {
	return InboundInput{Tag: "reality-in", Type: "vless", ListenPort: 31735, Settings: InboundSettings{
		SingleUser: true, UUID: "11111111-2222-4333-8444-555555555555", Flow: "xtls-rprx-vision",
		TLS: TLSSettings{Enabled: true, ServerName: "music.apple.com", ALPN: []string{"h2", "http/1.1"}, Reality: RealitySettings{
			Enabled: true, HandshakeServer: "music.apple.com", HandshakeServerPort: 443,
			PrivateKey: "UuMBgl7MXTPx9inmQp2UC7Jcnwc6XYbwDNebonM-FCc", PublicKey: "public-key", ShortID: []string{"abcd"}, MaxTimeDifference: "1m",
			Guard: &RealityGuardSettings{Enabled: true, InternalPort: 31736, Tag: "reality-guard"},
		}},
	}}
}

func TestRealityGuardBuildKeepsPublicSubscriptionAndLocalIdentity(t *testing.T) {
	in := testGuardInbound()
	raw, err := BuildServerConfig(ServerConfigInput{Inbounds: []InboundInput{in}, StatsController: "127.0.0.1:29091", StatsSecret: "secret"})
	if err != nil {
		t.Fatal(err)
	}
	var root map[string]any
	if err := json.Unmarshal(raw, &root); err != nil {
		t.Fatal(err)
	}
	ins := mSlice(root, "inbounds")
	if len(ins) != 2 {
		t.Fatalf("inbounds = %s", raw)
	}
	gate, inner := ins[0].(map[string]any), ins[1].(map[string]any)
	if mStr(gate, "type") != "direct" || mInt(gate, "listen_port") != 31735 || mStr(gate, "network") != "tcp" || mStr(gate, "override_address") != "127.0.0.1" || mInt(gate, "override_port") != 31736 {
		t.Fatalf("invalid public listener: %v", gate)
	}
	if mStr(inner, "tag") != in.Tag || mStr(inner, "listen") != "127.0.0.1" || mInt(inner, "listen_port") != 31736 {
		t.Fatalf("invalid private listener: %v", inner)
	}
	reality := mMap(mMap(inner, "tls"), "reality")
	if mStr(reality, "max_time_difference") != "1m" || reality["guard"] != nil {
		t.Fatalf("invalid wire reality: %v", reality)
	}
	user := mSlice(inner, "users")[0].(map[string]any)
	if mStr(user, "uuid") != in.Settings.UUID || mStr(user, "flow") != in.Settings.Flow {
		t.Fatalf("credentials changed: %v", user)
	}
	rules := mSlice(mMap(root, "route"), "rules")
	if len(rules) != 4 || !IsLoopbackGuardRule(rules[3].(map[string]any)) {
		t.Fatalf("loopback protection not after isolated guard: %v", rules)
	}
	sniff := rules[0].(map[string]any)
	if mStr(sniff, "timeout") != "300ms" || canonicalJSON(sniff["sniffer"]) != `["tls"]` {
		t.Fatalf("sniff = %v", sniff)
	}
	allow := rules[1].(map[string]any)
	if mStr(allow, "type") != "logical" || mStr(allow, "mode") != "and" || mStr(allow, "outbound") != "direct" {
		t.Fatalf("SNI and destination must be ANDed: %v", allow)
	}
	parts := mSlice(allow, "rules")
	if len(parts) != 2 || canonicalJSON(parts[0]) != `{"domain":["music.apple.com"],"inbound":["reality-guard"]}` || canonicalJSON(parts[1]) != `{"ip_cidr":["127.0.0.1/32"],"port":[31736]}` {
		t.Fatalf("allow is not narrowly scoped: %v", allow)
	}
	reject := rules[2].(map[string]any)
	if mStr(reject, "action") != "reject" || mStr(reject, "method") != "drop" || canonicalJSON(reject["inbound"]) != `["reality-guard"]` {
		t.Fatalf("missing terminal reject: %v", reject)
	}
	link, err := BuildShareLink(ClientNode{Name: in.Tag, Type: in.Type, Server: "node.example.com", ServerPort: in.ListenPort, Settings: in.Settings, User: in.Settings.SingleUserIdentity()})
	if err != nil || !strings.Contains(link, "node.example.com:31735") || strings.Contains(link, "31736") {
		t.Fatalf("public link: %s, %v", link, err)
	}
}

func TestRealityGuardDisabledRestoresSingleListener(t *testing.T) {
	in := testGuardInbound()
	in.Settings.TLS.Reality.Guard.Enabled = false
	raw, err := BuildServerConfig(ServerConfigInput{Inbounds: []InboundInput{in}})
	if err != nil {
		t.Fatal(err)
	}
	var root map[string]any
	_ = json.Unmarshal(raw, &root)
	ins := mSlice(root, "inbounds")
	if len(ins) != 1 || mInt(ins[0].(map[string]any), "listen_port") != 31735 || mStr(ins[0].(map[string]any), "listen") != "::" {
		t.Fatalf("disabled guard: %s", raw)
	}
	if strings.Contains(string(raw), "reality-guard") || strings.Contains(string(raw), "31736") {
		t.Fatalf("stale guard emitted: %s", raw)
	}
}

func TestRealityGuardNormalizesSNICase(t *testing.T) {
	in := testGuardInbound()
	in.Settings.TLS.Reality.Guard.ServerNames = []string{"Music.Apple.Com", "Example.COM"}
	if err := in.Settings.Validate("vless"); err != nil {
		t.Fatal(err)
	}
	if got := canonicalJSON(in.Settings.RealityGuardServerNames()); got != `["music.apple.com","example.com"]` {
		t.Fatalf("case-sensitive domain matcher would reject SNI: %s", got)
	}
	if in.Settings.TLS.Reality.Guard.ServerNames[0] != "Music.Apple.Com" {
		t.Fatal("generation mutated stored settings")
	}
	in.Settings.TLS.Reality.Guard.ServerNames = nil
	in.Settings.TLS.ServerName = "Music.Apple.Com"
	if got := in.Settings.RealityGuardServerNames()[0]; got != "music.apple.com" {
		t.Fatalf("fallback domain = %s", got)
	}
}

func TestRealityGuardRejectsInvalidSettings(t *testing.T) {
	cases := map[string]func(*InboundInput){
		"vmess":                    func(in *InboundInput) { in.Type = "vmess" },
		"without reality":          func(in *InboundInput) { in.Settings.TLS.Reality.Enabled = false },
		"websocket":                func(in *InboundInput) { in.Settings.Transport.Type = "ws" },
		"missing internal port":    func(in *InboundInput) { in.Settings.TLS.Reality.Guard.InternalPort = 0 },
		"overflow port":            func(in *InboundInput) { in.Settings.TLS.Reality.Guard.InternalPort = 65536 },
		"same ports":               func(in *InboundInput) { in.ListenPort = 31736 },
		"same tags":                func(in *InboundInput) { in.Settings.TLS.Reality.Guard.Tag = in.Tag },
		"empty SNI":                func(in *InboundInput) { in.Settings.TLS.Reality.Guard.ServerNames = []string{""} },
		"wrong client SNI":         func(in *InboundInput) { in.Settings.TLS.Reality.Guard.ServerNames = []string{"example.com"} },
		"wildcard":                 func(in *InboundInput) { in.Settings.TLS.Reality.Guard.ServerNames = []string{"*.apple.com"} },
		"IP SNI":                   func(in *InboundInput) { in.Settings.TLS.Reality.Guard.ServerNames = []string{"127.0.0.1"} },
		"bad timeout":              func(in *InboundInput) { in.Settings.TLS.Reality.Guard.SniffTimeout = "slow" },
		"zero timeout":             func(in *InboundInput) { in.Settings.TLS.Reality.Guard.SniffTimeout = "0s" },
		"negative time difference": func(in *InboundInput) { in.Settings.TLS.Reality.MaxTimeDifference = "-1m" },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			in := testGuardInbound()
			mutate(&in)
			if _, err := BuildServerConfig(ServerConfigInput{Inbounds: []InboundInput{in}}); err == nil {
				t.Fatal("invalid guard accepted")
			}
		})
	}
}

func TestRealityGuardLayoutRejectsAllPortAndTagCollisions(t *testing.T) {
	for _, other := range []InboundInput{
		{Tag: "other", Type: "socks", ListenPort: 31735},
		{Tag: "other", Type: "socks", ListenPort: 31736},
		{Tag: "reality-guard", Type: "socks", ListenPort: 31737},
		{Tag: "other", Type: "vless", ListenPort: 31737, Settings: InboundSettings{TLS: TLSSettings{Reality: RealitySettings{Enabled: true, HandshakeServer: "music.apple.com", Guard: &RealityGuardSettings{Enabled: true, InternalPort: 31736}}}}},
	} {
		if err := ValidateRealityGuardLayout([]InboundInput{testGuardInbound(), other}); err == nil {
			t.Fatalf("collision accepted: %+v", other)
		}
		if err := ValidateRealityGuardLayout([]InboundInput{other, testGuardInbound()}); err == nil {
			t.Fatal("order-dependent collision check")
		}
	}
	for _, stats := range []string{"127.0.0.1:31735", "127.0.0.1:31736"} {
		if _, err := BuildServerConfig(ServerConfigInput{Inbounds: []InboundInput{testGuardInbound()}, StatsController: stats}); err == nil {
			t.Fatal("stats port conflict accepted")
		}
	}
}

func TestRealityGuardRulesPrecedeCatchAllUserRoutes(t *testing.T) {
	in := testGuardInbound()
	in.Settings.TLS.Reality.Guard.Tag = ""
	in.Settings.TLS.Reality.Guard.SniffTimeout = "500ms"
	raw, err := BuildServerConfig(ServerConfigInput{Inbounds: []InboundInput{in}, Rules: []RuleInput{{Outbound: "direct"}}})
	if err != nil {
		t.Fatal(err)
	}
	var root map[string]any
	_ = json.Unmarshal(raw, &root)
	rules := mSlice(mMap(root, "route"), "rules")
	if canonicalJSON(rules[0].(map[string]any)["inbound"]) != `["reality-in-guard"]` || mStr(rules[0].(map[string]any), "timeout") != "500ms" || mStr(rules[2].(map[string]any), "action") != "reject" || mStr(rules[3].(map[string]any), "action") != "route" {
		t.Fatalf("guard order: %s", raw)
	}
}
