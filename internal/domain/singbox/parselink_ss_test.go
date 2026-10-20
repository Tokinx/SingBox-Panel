package singbox

import (
	"encoding/base64"
	"testing"
)

func TestParseShadowsocksDecodesNameAndCredentials(t *testing.T) {
	ui := base64.RawURLEncoding.EncodeToString([]byte("aes-128-gcm:pa:ss"))
	legacy := base64.StdEncoding.EncodeToString([]byte("aes-256-gcm:legacy-pass@5.6.7.8:9000"))
	cases := []struct {
		name, link                               string
		wantName, wantHost, wantMethod, wantPass string
		wantPort                                 int
		wantPlugin                               string
	}{
		{
			name:     "percent-encoded unicode name",
			link:     "ss://" + ui + "@1.2.3.4:8388#%E9%A6%99%E6%B8%AF%20SS",
			wantName: "香港 SS", wantHost: "1.2.3.4", wantPort: 8388, wantMethod: "aes-128-gcm", wantPass: "pa:ss",
		},
		{
			name:     "SIP002 empty path before plugin query",
			link:     "ss://" + ui + "@1.2.3.4:8388/?plugin=obfs-local%3Bobfs%3Dhttp#n",
			wantName: "n", wantHost: "1.2.3.4", wantPort: 8388, wantMethod: "aes-128-gcm", wantPass: "pa:ss",
			wantPlugin: "obfs-local;obfs=http",
		},
		{
			name:     "SIP002 plain percent-encoded userinfo keeps literal plus",
			link:     "ss://2022-blake3-aes-128-gcm:ab+c%2Fdef%3D%3D@1.2.3.4:8388#plain",
			wantName: "plain", wantHost: "1.2.3.4", wantPort: 8388, wantMethod: "2022-blake3-aes-128-gcm", wantPass: "ab+c/def==",
		},
		{
			name:     "base64 userinfo with percent-encoded padding",
			link:     "ss://" + base64.URLEncoding.EncodeToString([]byte("aes-128-gcm:x")) + "@1.2.3.4:8388",
			wantHost: "1.2.3.4", wantPort: 8388, wantMethod: "aes-128-gcm", wantPass: "x",
		},
		{
			name:     "legacy fully base64 encoded link",
			link:     "ss://" + legacy + "#legacy",
			wantName: "legacy", wantHost: "5.6.7.8", wantPort: 9000, wantMethod: "aes-256-gcm", wantPass: "legacy-pass",
		},
		{
			name:     "unescaped hash inside name",
			link:     "ss://" + ui + "@[2001:db8::1]:8388#HK#01",
			wantName: "HK#01", wantHost: "2001:db8::1", wantPort: 8388, wantMethod: "aes-128-gcm", wantPass: "pa:ss",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			n, err := ParseShareLink(tc.link)
			if err != nil {
				t.Fatalf("ParseShareLink(%q): %v", tc.link, err)
			}
			if n.Name != tc.wantName || n.Server != tc.wantHost || n.ServerPort != tc.wantPort ||
				n.Settings.Method != tc.wantMethod || n.Settings.SSServerPSK != tc.wantPass ||
				n.Settings.SSPlugin != tc.wantPlugin {
				t.Fatalf("got name=%q host=%q port=%d method=%q pass=%q plugin=%q",
					n.Name, n.Server, n.ServerPort, n.Settings.Method, n.Settings.SSServerPSK, n.Settings.SSPlugin)
			}
		})
	}
}

// Links produced by the panel itself must import back unchanged.
func TestShadowsocksShareLinkRoundTrip(t *testing.T) {
	for _, plugin := range []string{"", "obfs-local;obfs=tls;obfs-host=example.com"} {
		in := ClientNode{Name: "香港 01 🚀", Server: "1.2.3.4", ServerPort: 8388, Type: "shadowsocks"}
		in.Settings.Method = "2022-blake3-aes-128-gcm"
		in.Settings.SingleUser = true
		in.Settings.SSServerPSK = "c2VjcmV0LWtleS0xMjM0NQ=="
		in.Settings.SSPlugin = plugin
		link, err := BuildShareLink(in)
		if err != nil {
			t.Fatal(err)
		}
		out, err := ParseShareLink(link)
		if err != nil {
			t.Fatalf("ParseShareLink(%q): %v", link, err)
		}
		if out.Name != in.Name || out.Server != in.Server || out.ServerPort != in.ServerPort ||
			out.Settings.Method != in.Settings.Method || out.Settings.SSServerPSK != in.Settings.SSServerPSK ||
			out.Settings.SSPlugin != plugin {
			t.Fatalf("round trip of %q lost data: %+v", link, out)
		}
	}
}
