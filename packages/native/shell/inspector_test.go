package shell

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestInspectorScriptOnlyWhenAsked(t *testing.T) {
	page := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte("<html><head></head><body></body></html>"))
	})
	for _, tc := range []struct {
		extra string
		want  bool
	}{{"", false}, {inspectorKeyScript, true}} {
		rec := httptest.NewRecorder()
		windowIDHandler(page, tc.extra, false).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
		if got := strings.Contains(rec.Body.String(), inspectorPath); got != tc.want {
			t.Errorf("extra %q: script present = %v, want %v", tc.extra, got, tc.want)
		}
	}
}

func TestInspectorKeyScriptKeys(t *testing.T) {
	for _, want := range []string{`e.key==="F12"`, `e.ctrlKey&&e.shiftKey&&e.code==="KeyI"`, `e.metaKey&&e.altKey&&e.code==="KeyI"`} {
		if !strings.Contains(inspectorKeyScript, want) {
			t.Errorf("the shortcut script lacks %s", want)
		}
	}
}

func TestServeInspectorWithoutAWindow(t *testing.T) {
	a := &app{logf: func(string, ...any) {}}
	rec := httptest.NewRecorder()
	a.serveInspector(rec, httptest.NewRequest(http.MethodPost, inspectorPath, nil))
	if rec.Code != http.StatusNotImplemented {
		t.Errorf("status %d, want 501", rec.Code)
	}
	rec = httptest.NewRecorder()
	a.serveInspector(rec, httptest.NewRequest(http.MethodGet, inspectorPath, nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("status %d, want 405", rec.Code)
	}
}
