package runtime

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRecoverPanicsAnswers500JSON(t *testing.T) {
	h := recoverPanics(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { panic("boom") }))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/api/x", nil))
	if rec.Code != 500 || strings.TrimSpace(rec.Body.String()) != `{"error":"Internal Server Error"}` {
		t.Fatalf("panic response: %d %q", rec.Code, rec.Body)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("content-type = %q", ct)
	}
}
