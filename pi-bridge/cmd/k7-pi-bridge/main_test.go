package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/cp296944/k7-led-Raspberry-controller/pi-bridge/internal/config"
	"github.com/cp296944/k7-led-Raspberry-controller/pi-bridge/internal/updater"
)

// A bare POST /api/update/apply (stray click, replay, script) must be rejected —
// applying restarts the service, so it needs {"confirm":true,"tag":...}.
func TestUpdateApplyNeedsConfirmation(t *testing.T) {
	up := updater.New(updater.Options{Repo: "cp296944/x", CurrentTag: "dev"})
	var auto atomic.Bool
	h := routes(config.Config{}, "", up, &auto, http.NotFoundHandler())

	for _, body := range []string{"", "{}", `{"confirm":true}`, `{"tag":"pi-v9.9.9"}`} {
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, httptest.NewRequest(http.MethodPost, "/api/update/apply", strings.NewReader(body)))
		if rr.Code != http.StatusBadRequest {
			t.Errorf("body %q: got %d, want 400", body, rr.Code)
		}
		if !strings.Contains(rr.Body.String(), "confirm") {
			t.Errorf("body %q: response %q missing the confirmation hint", body, rr.Body.String())
		}
	}
}
