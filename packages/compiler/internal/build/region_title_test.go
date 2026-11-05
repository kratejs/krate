package build

import (
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The page-region title replaces the baked head title. The marker offsets come
// from the baked shell, so a title of another length must not shift the splice
// (it used to cut the markers apart and leave the page inside a comment).
func TestStreamRegionPageTitleOfOtherLengthKeepsMarkers(t *testing.T) {
	for name, title := range map[string]string{"longer": "A much longer fresh page title", "shorter": "T"} {
		t.Run(name, func(t *testing.T) {
			absOut := t.TempDir()
			shell := `<!DOCTYPE html><html><head><title>Baked title</title></head><body><div id=root>` +
				`<!--suspense:page--><p>stale</p><!--/suspense:page--></div></body></html>`
			path := filepath.Join(absOut, "live", "index.html")
			if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, []byte(shell), 0644); err != nil {
				t.Fatal(err)
			}
			sidecar := newFakeRegionsSidecar(t, []string{
				`{"type":"region","id":"page","kind":"page","status":200,"html":"<p>fresh</p>","title":"` + title + `"}`,
				`{"type":"end","count":1}`,
			})
			rec := &flushRecorder{ResponseRecorder: httptest.NewRecorder()}
			rr := streamRegionPage(rec, rec, newRegionShellCache(), absOut, "/live", sidecar.port, nil, nil, nil, nil, nil, false, 0, "")
			if !rr.served {
				t.Fatal("not served")
			}
			want := `<title>` + title + `</title></head><body><div id=root><!--suspense:page--><p>fresh</p><!--/suspense:page--></div></body></html>`
			if got := rec.Body.String(); !strings.Contains(got, want) {
				t.Errorf("spliced page is wrong:\n got: %s\nwant it to contain: %s", got, want)
			}
		})
	}
}
