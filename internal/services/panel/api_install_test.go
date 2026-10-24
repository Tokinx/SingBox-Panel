package panel

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestInstallSingboxRejectsPackageVersionPinBeforeAgentCommand(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, method := range []string{"apt", "dnf"} {
		t.Run(method, func(t *testing.T) {
			// No hub is attached: invalid requests must not contact an Agent,
			// including an older Agent that does not yet reject package pins.
			a := &App{serverOperations: newKeyedMutex[uint]()}
			router := gin.New()
			router.POST("/servers/:id/install-singbox", a.installSingbox)
			recorder := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodPost, "/servers/1/install-singbox", strings.NewReader(`{"method":"`+method+`","version":"1.14.0"}`))
			req.Header.Set("Content-Type", "application/json")
			router.ServeHTTP(recorder, req)
			if recorder.Code != http.StatusBadRequest || !strings.Contains(recorder.Body.String(), "官方脚本") {
				t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
			}
		})
	}
}
