package singbox

import (
	"encoding/json"
	"net/url"
	"testing"
)

func TestVLESSPacketEncodingImportAndExport(t *testing.T) {
	for _, tc := range []struct {
		name, field, stored, wire, share string
	}{
		{"default", "", "", "xudp", "xudp"},
		{"disabled", `,"packet_encoding":""`, "none", "", "none"},
		{"xudp", `,"packet_encoding":"xudp"`, "xudp", "xudp", "xudp"},
		{"packetaddr", `,"packet_encoding":"packetaddr"`, "packetaddr", "packetaddr", "packetaddr"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			raw := []byte(`{"outbounds":[{"type":"vless","tag":"node","server":"edge.example.com","server_port":443,"uuid":"11111111-1111-4111-8111-111111111111"` + tc.field + `}]}`)
			parsed, err := ParseServerConfig(raw)
			if err != nil || len(parsed.Outbounds) != 1 {
				t.Fatalf("parse server config: %+v, %v", parsed, err)
			}
			outbound := parsed.Outbounds[0]
			stored, err := json.Marshal(outbound.Settings)
			if err != nil {
				t.Fatal(err)
			}
			var settings InboundSettings
			if err := json.Unmarshal(stored, &settings); err != nil {
				t.Fatal(err)
			}
			if settings.PacketEncoding != tc.stored {
				t.Fatalf("stored encoding = %q, want %q", settings.PacketEncoding, tc.stored)
			}
			node := ClientNode{Name: outbound.Tag, Type: outbound.Type, Server: outbound.Server, ServerPort: outbound.ServerPort, Settings: settings, User: ProxyUser{UUID: outbound.UUID}}
			generated, err := BuildClientOutbound(node)
			if err != nil {
				t.Fatal(err)
			}
			var config map[string]any
			if err := json.Unmarshal(generated, &config); err != nil {
				t.Fatal(err)
			}
			if got, present := config["packet_encoding"]; !present || got != tc.wire {
				t.Fatalf("wire encoding = %#v (present %t), want %q", got, present, tc.wire)
			}
			link, err := BuildShareLink(node)
			if err != nil {
				t.Fatal(err)
			}
			u, err := url.Parse(link)
			if err != nil || u.Query().Get("packetEncoding") != tc.share {
				t.Fatalf("share encoding: %s, %v", link, err)
			}
			fromLink, err := ParseShareLink(link)
			if err != nil || fromLink.Settings.PacketEncoding != tc.share {
				t.Fatalf("reimported share encoding: %+v, %v", fromLink.Settings, err)
			}
			subscription, err := ParseSubscription(raw)
			if err != nil || len(subscription.Nodes) != 1 {
				t.Fatalf("parse subscription: %+v, %v", subscription, err)
			}
			if value, _ := subscription.Nodes[0].Params["packet_encoding"].(string); value != tc.stored {
				t.Fatalf("subscription encoding = %q, want %q", value, tc.stored)
			}
		})
	}
}
