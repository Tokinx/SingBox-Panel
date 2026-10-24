package singbox

import (
	"encoding/json"
	"testing"
)

func TestTransportHostSurvivesSubscriptionImportAndShareLinks(t *testing.T) {
	for _, header := range []string{"Host", "host", "HOST"} {
		for _, transport := range []string{"ws", "httpupgrade"} {
			for _, protocol := range []string{"vless", "vmess", "trojan"} {
				t.Run(header+"/"+transport+"/"+protocol, func(t *testing.T) {
					raw, err := json.Marshal(map[string]any{"outbounds": []any{map[string]any{
						"type": protocol, "tag": "node", "server": "edge.example.com", "server_port": 443,
						"uuid": "11111111-1111-4111-8111-111111111111", "password": "secret",
						"tls":       map[string]any{"enabled": true},
						"transport": map[string]any{"type": transport, "path": "/proxy", "headers": map[string]any{header: []string{"cdn.example.com", "backup.example.com"}}},
					}}})
					if err != nil {
						t.Fatal(err)
					}
					imported, err := ParseSubscription(raw)
					if err != nil || len(imported.Nodes) != 1 {
						t.Fatalf("parse subscription: %+v, %v", imported, err)
					}
					if got := imported.Nodes[0].Params["host"]; got != "cdn.example.com" {
						t.Fatalf("imported Host = %v", got)
					}
					parsed, err := ParseServerConfig(raw)
					if err != nil || len(parsed.Outbounds) != 1 {
						t.Fatalf("parse config: %+v, %v", parsed, err)
					}
					outbound := parsed.Outbounds[0]
					link, err := BuildShareLink(ClientNode{
						Name: outbound.Tag, Type: outbound.Type, Server: outbound.Server, ServerPort: outbound.ServerPort,
						Settings: outbound.Settings, User: ProxyUser{UUID: outbound.UUID, Password: outbound.Password},
					})
					if err != nil {
						t.Fatal(err)
					}
					fromLink, err := ParseShareLink(link)
					if err != nil || fromLink.Settings.Transport.Host() != "cdn.example.com" {
						t.Fatalf("share Host lost: %s, %v", link, err)
					}
				})
			}
		}
	}
}
