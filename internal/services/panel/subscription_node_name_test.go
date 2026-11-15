package panel

import (
	"encoding/json"
	"testing"

	"github.com/tokinx/singbox-panel/internal/domain/model"
	"github.com/tokinx/singbox-panel/internal/domain/singbox"
)

func TestManagedNodeNamesFollowServerName(t *testing.T) {
	db := testDB(t)
	hk := model.Server{Name: "🇭🇰 香港", AgentToken: "name-hk", Address: "hk.example.com"}
	jp := model.Server{Name: "日本", AgentToken: "name-jp", Address: "jp.example.com"}
	us := model.Server{Name: "美国", AgentToken: "name-us", Address: "us.example.com"}
	for _, srv := range []*model.Server{&hk, &jp, &us} {
		if err := db.Create(srv).Error; err != nil {
			t.Fatal(err)
		}
	}
	hkSS := model.Inbound{ServerID: hk.ID, Tag: "ss-in", Type: model.InboundShadowsocks, ListenPort: 20001, Enabled: true}
	jpVLESS := model.Inbound{ServerID: jp.ID, Tag: "vless-a1b2c3", Type: model.InboundVLESS, ListenPort: 20002, Enabled: true}
	jpTrojan := model.Inbound{ServerID: jp.ID, Tag: "trojan-cdn", Type: model.InboundTrojan, ListenPort: 20003, Enabled: true}
	usVLESS := model.Inbound{ServerID: us.ID, Tag: "vless-in", Type: model.InboundVLESS, ListenPort: 20004, Enabled: true}
	usTrojan := model.Inbound{ServerID: us.ID, Tag: "trojan-in", Type: model.InboundTrojan, ListenPort: 20005, Enabled: true}
	for _, ib := range []*model.Inbound{&hkSS, &jpVLESS, &jpTrojan, &usVLESS, &usTrojan} {
		if err := db.Create(ib).Error; err != nil {
			t.Fatal(err)
		}
	}
	// The user holds every inbound on 香港 and 日本 but only one of the two
	// inbounds on 美国, so 美国 contributes a single node to this subscription.
	user := model.User{
		Email: "name-user", Password: "unused", Role: model.RoleUser, Enabled: true,
		ServerIDs:  []uint{hk.ID, jp.ID, us.ID},
		InboundIDs: []uint{hkSS.ID, jpVLESS.ID, jpTrojan.ID, usTrojan.ID},
		SubToken:   "name-token",
	}
	if err := db.Create(&user).Error; err != nil {
		t.Fatal(err)
	}

	nodes := (&App{db: db}).gatherNodes(&user)
	var got []string
	for _, n := range nodes {
		got = append(got, n.name)
	}
	want := []string{"🇭🇰 香港", "日本 - VLESS", "日本 - trojan-cdn", "美国"}
	if len(got) != len(want) {
		t.Fatalf("node names = %q; want %q", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("node names = %q; want %q", got, want)
		}
	}
}

func TestManagedIPv6NodeSplittingAndSubscriptionEmission(t *testing.T) {
	db := testDB(t)
	srv := model.Server{
		Name: "🇭🇰 Edge", AgentToken: "sub-ipv6-edge",
		Address: "edge.example.com", IPv6Mode: model.ServerIPv6ModeAuto, PublicIPv6: "2001:db8::99",
	}
	mustCreate(t, db, &srv)
	tlsSettings, err := json.Marshal(singbox.InboundSettings{
		MultiUser: true,
		TLS:       singbox.TLSSettings{Enabled: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	vless := model.Inbound{ServerID: srv.ID, Tag: "vless-in", Type: model.InboundVLESS, ListenPort: 20443, Enabled: true, Settings: tlsSettings}
	trojan := model.Inbound{ServerID: srv.ID, Tag: "trojan-cdn", Type: model.InboundTrojan, ListenPort: 20444, Enabled: true}
	mustCreate(t, db, &vless)
	mustCreate(t, db, &trojan)

	app := &App{db: db}

	// Single inbound split into v4 + v6 appends - v4 / - v6 and preserves TLS SNI on raw IPv6.
	dualSingle := model.User{
		Email: "dual-single", Password: "unused", Role: model.RoleUser, Enabled: true,
		ServerIDs: []uint{srv.ID}, InboundIDs: []uint{vless.ID}, InboundIPv6IDs: []uint{vless.ID},
		SubToken: "dual-single-sub",
	}
	mustCreate(t, db, &dualSingle)
	nodes := app.gatherNodes(&dualSingle)
	if len(nodes) != 2 {
		t.Fatalf("single split nodes count = %d; want 2", len(nodes))
	}
	if nodes[0].name != "🇭🇰 Edge - v4" || nodes[0].server != "edge.example.com" || nodes[0].orderType != userNodeTypeManaged {
		t.Fatalf("v4 node = %+v", nodes[0])
	}
	if nodes[1].name != "🇭🇰 Edge - v6" || nodes[1].server != "2001:db8::99" || nodes[1].orderType != userNodeTypeManagedIPv6 {
		t.Fatalf("v6 node = %+v", nodes[1])
	}
	if nodes[1].settings.TLS.ServerName != "edge.example.com" {
		t.Fatalf("v6 TLS ServerName = %q; want edge.example.com", nodes[1].settings.TLS.ServerName)
	}

	// Multi-inbound partial split adds (v4)/(v6) only to the split inbound.
	partialSplit := model.User{
		Email: "partial-split", Password: "unused", Role: model.RoleUser, Enabled: true,
		ServerIDs: []uint{srv.ID}, InboundIDs: []uint{vless.ID, trojan.ID}, InboundIPv6IDs: []uint{vless.ID},
		SubToken: "partial-split-sub",
	}
	mustCreate(t, db, &partialSplit)
	multiNodes := app.gatherNodes(&partialSplit)
	var multiNames []string
	for _, n := range multiNodes {
		multiNames = append(multiNames, n.name)
	}
	wantMulti := []string{"🇭🇰 Edge - vless-in (v4)", "🇭🇰 Edge - vless-in (v6)", "🇭🇰 Edge - trojan-cdn"}
	if len(multiNames) != len(wantMulti) {
		t.Fatalf("multi node names = %q; want %q", multiNames, wantMulti)
	}
	for i := range wantMulti {
		if multiNames[i] != wantMulti[i] {
			t.Fatalf("multi node names = %q; want %q", multiNames, wantMulti)
		}
	}

	// Custom user order and custom names apply independently to managed vs managed_ipv6.
	if err := saveUserNodeOrder(db, dualSingle.ID, []userNodeOrderItem{
		{NodeType: userNodeTypeManagedIPv6, NodeID: vless.ID, CustomName: "自定义 IPv6"},
		{NodeType: userNodeTypeManaged, NodeID: vless.ID, CustomName: "自定义 IPv4"},
	}); err != nil {
		t.Fatal(err)
	}
	reordered := app.gatherNodes(&dualSingle)
	if len(reordered) != 2 || reordered[0].name != "自定义 IPv6" || reordered[0].server != "2001:db8::99" ||
		reordered[1].name != "自定义 IPv4" || reordered[1].server != "edge.example.com" {
		t.Fatalf("reordered nodes = %+v", reordered)
	}

	// v6-only assignment emits only the IPv6 endpoint.
	v6Only := model.User{
		Email: "v6-only", Password: "unused", Role: model.RoleUser, Enabled: true,
		ServerIDs: []uint{srv.ID}, InboundIPv6IDs: []uint{vless.ID}, SubToken: "v6-only-sub",
	}
	mustCreate(t, db, &v6Only)
	v6Nodes := app.gatherNodes(&v6Only)
	if len(v6Nodes) != 1 || v6Nodes[0].name != "🇭🇰 Edge - v6" || v6Nodes[0].server != "2001:db8::99" {
		t.Fatalf("v6-only nodes = %+v", v6Nodes)
	}
}
