package singbox

import (
	"encoding/json"
	"testing"
)

func TestXrayRealityFingerprintSurvivesImportAndExport(t *testing.T) {
	for _, fingerprint := range []string{"firefox", "safari"} {
		t.Run(fingerprint, func(t *testing.T) {
			outbound := map[string]any{
				"protocol": "vless", "tag": "node",
				"settings": map[string]any{"vnext": []any{map[string]any{
					"address": "edge.example.com", "port": 443,
					"users": []any{map[string]any{"id": "11111111-1111-4111-8111-111111111111"}},
				}}},
				"streamSettings": map[string]any{
					"network": "tcp", "security": "reality",
					"realitySettings": map[string]any{
						"serverName": "cdn.example.com", "publicKey": "public-key", "shortId": "abcd", "fingerprint": fingerprint,
					},
				},
			}
			raw, err := json.Marshal(map[string]any{"outbounds": []any{outbound}})
			if err != nil {
				t.Fatal(err)
			}
			imported, err := ParseSubscription(raw)
			if err != nil || len(imported.Nodes) != 1 {
				t.Fatalf("parse subscription: %+v, %v", imported, err)
			}
			if got := imported.Nodes[0].Params["fingerprint"]; got != fingerprint {
				t.Fatalf("imported fingerprint = %v, want %s", got, fingerprint)
			}
			nodes, issues := parseV2RayOutbound(outbound)
			if len(nodes) != 1 || len(issues) != 0 {
				t.Fatalf("parse outbound: %+v, %+v", nodes, issues)
			}
			link, err := BuildShareLink(nodes[0])
			if err != nil {
				t.Fatal(err)
			}
			fromLink, err := ParseShareLink(link)
			if err != nil || fromLink.Settings.TLS.Fingerprint != fingerprint {
				t.Fatalf("share fingerprint lost: %s, %v", link, err)
			}
			generated, err := BuildClientOutbound(nodes[0])
			if err != nil {
				t.Fatal(err)
			}
			var config map[string]any
			if err := json.Unmarshal(generated, &config); err != nil {
				t.Fatal(err)
			}
			if got := config["tls"].(map[string]any)["utls"].(map[string]any)["fingerprint"]; got != fingerprint {
				t.Fatalf("sing-box fingerprint = %v, want %s", got, fingerprint)
			}
		})
	}
}
