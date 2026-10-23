package panel

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"runtime"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestMaintenanceReleasesListsOnlyInstallableReleases(t *testing.T) {
	gin.SetMode(gin.TestMode)
	bin := "singbox-panel-linux-" + runtime.GOARCH
	full := func(tag string) string {
		return fmt.Sprintf(`[{"name":%q},{"name":"singbox-panel-web-%s.tar.gz"},{"name":"singbox-panel-agents-%s.tar.gz"}]`, bin, tag, tag)
	}
	body := `[
		{"tag_name":"v1.0.6","name":"v1.0.6","published_at":"2026-10-20T00:00:00Z","assets":` + full("v1.0.6") + `},
		{"tag_name":"v1.0.5","name":"v1.0.5","assets":` + full("v1.0.5") + `},
		{"tag_name":"v1.0.4","draft":true,"assets":` + full("v1.0.4") + `},
		{"tag_name":"v1.0.3","assets":[{"name":"singbox-panel-web-v1.0.3.tar.gz"}]},
		{"tag_name":"bad tag; rm","assets":` + full("bad") + `}
	]`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(body)) }))
	defer srv.Close()

	oldURL := panelReleaseListURL
	panelReleaseListURL = srv.URL
	panelReleaseListMu.Lock()
	panelReleaseListCache = nil
	panelReleaseListMu.Unlock()
	t.Cleanup(func() {
		panelReleaseListURL = oldURL
		panelReleaseListMu.Lock()
		panelReleaseListCache = nil
		panelReleaseListMu.Unlock()
	})

	a := &App{version: "v1.0.5"}
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/admin/maintenance/releases?refresh=1", nil)
	a.maintenanceReleases(c)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body %s", w.Code, w.Body.String())
	}
	var got struct {
		Releases []panelReleaseInfo `json:"releases"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Releases) != 2 || got.Releases[0].Version != "v1.0.6" || got.Releases[1].Version != "v1.0.5" {
		t.Fatalf("releases = %+v", got.Releases)
	}
	if got.Releases[0].Current || !got.Releases[1].Current {
		t.Fatalf("current flags wrong: %+v", got.Releases)
	}
}
