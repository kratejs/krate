package plugin

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// signalStatus subscribes to plugin/sig.level as the given window and returns
// the first frame's text.
func signalStatus(t *testing.T, h http.Handler, window string) string {
	t.Helper()
	req := httptest.NewRequest("GET", "/__krate/signals?names=plugin/sig.level", nil)
	req.Header.Set("x-krate-window", window)
	ctx, cancel := contextWithTimeout(time.Second)
	defer cancel()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req.WithContext(ctx))
	b, _ := io.ReadAll(rec.Body)
	return string(b)
}
