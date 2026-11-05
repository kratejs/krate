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

func writeHeadTestShell(t *testing.T, shell string) string {
	t.Helper()
	absOut := t.TempDir()
	path := filepath.Join(absOut, "live", "index.html")
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(shell), 0644); err != nil {
		t.Fatal(err)
	}
	return absOut
}

// The page's <Head> tags arrive on the page frame and end up in the head of
// the response, ahead of the body: the page wins over the layout's baked tags,
// the shell had no <title>, and the body keeps its markers.
func TestStreamRegionPageMergesHeadTags(t *testing.T) {
	shell := `<!DOCTYPE html><html><head><meta charset=UTF-8><meta name=description content="layout desc"/>` +
		`<link rel=canonical href=https://e.com/layout /></head><body><div id=root>` +
		`<!--suspense:page--><p>stale</p><!--/suspense:page--></div></body></html>`
	absOut := writeHeadTestShell(t, shell)
	head := `<meta name=\"description\" content=\"page desc\"><link rel=\"canonical\" href=\"https://e.com/page\">` +
		`<script type=\"application/ld+json\">{\"a\":1}</script>`
	sidecar := newFakeRegionsSidecar(t, []string{
		`{"type":"region","id":"page","kind":"page","status":200,"html":"<p>fresh</p>","title":"Page &amp; title","head":"` + head + `"}`,
		`{"type":"end","count":1}`,
	})
	rec := &flushRecorder{ResponseRecorder: httptest.NewRecorder()}
	rr := streamRegionPage(rec, rec, newRegionShellCache(), absOut, "/live", sidecar.port, nil, nil, nil, nil, nil, false, 0, "")
	if !rr.served {
		t.Fatal("not served")
	}
	want := `<!DOCTYPE html><html><head><meta charset=UTF-8><title>Page &amp; title</title>` +
		`<meta name="description" content="page desc"><link rel="canonical" href="https://e.com/page">` +
		`<script type="application/ld+json">{"a":1}</script></head><body><div id=root>` +
		`<!--suspense:page--><p>fresh</p><!--/suspense:page--></div></body></html>`
	if got := rec.Body.String(); got != want {
		t.Errorf("response:\n got: %s\nwant: %s", got, want)
	}
}

// A region nested in a replaced page region is dropped with it, and so are its
// head tags.
func TestStreamRegionPageIgnoresHeadOfReplacedRegions(t *testing.T) {
	shell := `<html><head></head><body><!--suspense:page--><!--suspense:inner-->x<!--/suspense:inner--><!--/suspense:page--></body></html>`
	absOut := writeHeadTestShell(t, shell)
	sidecar := newFakeRegionsSidecar(t, []string{
		`{"type":"region","id":"page","kind":"page","status":200,"html":"<p>fresh</p>","head":"<meta name=\"a\" content=\"page\">"}`,
		`{"type":"region","id":"inner","status":200,"html":"<p>inner</p>","head":"<meta name=\"b\" content=\"inner\">"}`,
		`{"type":"end","count":2}`,
	})
	rec := &flushRecorder{ResponseRecorder: httptest.NewRecorder()}
	rr := streamRegionPage(rec, rec, newRegionShellCache(), absOut, "/live", sidecar.port, nil, nil, nil, nil, nil, false, 0, "")
	if !rr.served {
		t.Fatal("not served")
	}
	got := rec.Body.String()
	if !strings.Contains(got, `<meta name="a" content="page">`) || strings.Contains(got, `name="b"`) {
		t.Errorf("head of a replaced region leaked:\n%s", got)
	}
}
