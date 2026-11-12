package caps

import (
	"bytes"
	"errors"
	"image/png"
	"math"
	"strings"
	"testing"
)

func TestPDFOptionsResolve(t *testing.T) {
	o, err := PDFOptions{}.Resolve()
	if err != nil {
		t.Fatal(err)
	}
	if o.PageWidth != 8.27 || o.PageHeight != 11.69 || *o.Margins != DefaultMargins {
		t.Fatalf("defaults: %+v", o)
	}
	o, err = PDFOptions{PageWidth: 8.5, PageHeight: 11, Landscape: true, Margins: &Margins{1, 1, 1, 1}}.Resolve()
	if err != nil {
		t.Fatal(err)
	}
	if w, h := o.Sheet(); w != 11 || h != 8.5 {
		t.Fatalf("landscape sheet %vx%v", w, h)
	}
	for name, bad := range map[string]PDFOptions{
		"negative size":   {PageWidth: -1, PageHeight: 5},
		"half size":       {PageWidth: 5},
		"nan":             {PageWidth: math.NaN(), PageHeight: 4},
		"negative margin": {Margins: &Margins{Top: -0.1}},
		"no room":         {PageWidth: 4, PageHeight: 4, Margins: &Margins{Left: 2, Right: 2}},
	} {
		if _, err := bad.Resolve(); err == nil {
			t.Errorf("%s: want an error", name)
		}
	}
	zero := Margins{}
	if _, err := (PDFOptions{Margins: &zero}).Resolve(); err != nil {
		t.Fatalf("zero margins: %v", err)
	}
}

func TestPaperSize(t *testing.T) {
	if w, h, ok := PaperSize("Letter"); !ok || w != 8.5 || h != 11 {
		t.Fatal("Letter")
	}
	if _, _, ok := PaperSize("nope"); ok {
		t.Fatal("unknown size accepted")
	}
}

func TestZoomSteps(t *testing.T) {
	if got := NextZoom(1, true); got != 1.1 {
		t.Fatalf("up from 1: %v", got)
	}
	if got := NextZoom(1, false); got != 0.9 {
		t.Fatalf("down from 1: %v", got)
	}
	if NextZoom(5, true) != MaxZoom || NextZoom(0.25, false) != MinZoom {
		t.Fatal("limits")
	}
	if got := NextZoom(1.05, true); got != 1.1 {
		t.Fatalf("between steps: %v", got)
	}
	if ClampZoom(9) != MaxZoom || ClampZoom(0) != MinZoom || ClampZoom(math.NaN()) != 1 || ClampZoom(1.5) != 1.5 {
		t.Fatal("clamp")
	}
}

func TestEncodePNGFromBGRA(t *testing.T) {
	// 2x1: opaque red, and half-transparent premultiplied green (0,128,0,128).
	data := []byte{0, 0, 255, 255, 0, 128, 0, 128}
	b, err := EncodePNGFromBGRA(2, 1, 8, data, true, false)
	if err != nil {
		t.Fatal(err)
	}
	img, err := png.Decode(bytes.NewReader(b))
	if err != nil {
		t.Fatal(err)
	}
	if img.Bounds().Dx() != 2 || img.Bounds().Dy() != 1 {
		t.Fatalf("size %v", img.Bounds())
	}
	r, g, bl, a := img.At(0, 0).RGBA()
	if r>>8 != 255 || g != 0 || bl != 0 || a>>8 != 255 {
		t.Fatalf("pixel 0: %v %v %v %v", r, g, bl, a)
	}
	if _, g, _, a := img.At(1, 0).RGBA(); a>>8 != 128 || g == 0 {
		t.Fatalf("pixel 1: g=%v a=%v", g, a)
	}
	if _, err := EncodePNGFromBGRA(2, 2, 8, data, true, false); err == nil {
		t.Fatal("short data accepted")
	}
	if _, err := EncodePNGFromBGRA(0, 1, 8, data, true, false); err == nil {
		t.Fatal("empty image accepted")
	}
	// Opaque ignores alpha.
	b, _ = EncodePNGFromBGRA(1, 1, 4, []byte{1, 2, 3, 0}, false, true)
	im, _ := png.Decode(bytes.NewReader(b))
	if _, _, _, a := im.At(0, 0).RGBA(); a != 0xffff {
		t.Fatal("alpha not forced opaque")
	}
}

func TestUniqueName(t *testing.T) {
	taken := map[string]bool{"a.txt": true, "a (1).txt": true, "noext": true}
	ex := func(s string) bool { return taken[s] }
	if got := UniqueName("a.txt", ex); got != "a (2).txt" {
		t.Fatal(got)
	}
	if got := UniqueName("noext", ex); got != "noext (1)" {
		t.Fatal(got)
	}
	if got := UniqueName("b.txt", ex); got != "b.txt" {
		t.Fatal(got)
	}
}

func TestSafeFileName(t *testing.T) {
	for in, want := range map[string]string{
		"../../etc/passwd": "_.._etc_passwd",
		"..":               "download",
		"":                 "download",
		"a/b.txt":          "a_b.txt",
		"C:\\x.txt":        "C__x.txt",
		"  .hidden":        "hidden",
		"ok name.pdf":      "ok name.pdf",
		"ctl\x01\x7fz":     "ctlz",
	} {
		if got := SafeFileName(in); got != want {
			t.Errorf("SafeFileName(%q) = %q, want %q", in, got, want)
		}
	}
	long := make([]byte, 0, 400)
	for len(long) < 400 {
		long = append(long, "é"...)
	}
	if got := SafeFileName(string(long)); len(got) > 200 || len(got)%2 != 0 {
		t.Fatalf("long name: %d bytes", len(got))
	}
}

func TestCookieDomainMatches(t *testing.T) {
	for _, c := range []struct {
		domain, host string
		want         bool
	}{
		{"app", "app", true},
		{".example.com", "www.example.com", true},
		{".example.com", "example.com", true},
		{"example.com", "www.example.com", false},
		{"other", "app", false},
		{"", "app", false},
		{"APP", "app", true},
	} {
		if got := CookieDomainMatches(c.domain, c.host); got != c.want {
			t.Errorf("CookieDomainMatches(%q, %q) = %v", c.domain, c.host, got)
		}
	}
}

func TestFindTracker(t *testing.T) {
	var f FindTracker
	if f.Start("foo", FindOptions{}) {
		t.Fatal("first search counted as a continuation")
	}
	if a := f.Report(3); a != 1 {
		t.Fatalf("first match %d", a)
	}
	if !f.Start("foo", FindOptions{Continue: true}) || !f.Continuing() {
		t.Fatal("next was not a continuation")
	}
	if a := f.Report(1); a != 2 { // an engine that reports the stepped count only
		t.Fatalf("second match %d", a)
	}
	f.Start("foo", FindOptions{Continue: true})
	if a := f.Report(3); a != 3 {
		t.Fatalf("third %d", a)
	}
	f.Start("foo", FindOptions{Continue: true})
	if a := f.Report(3); a != 1 {
		t.Fatalf("wrap forward %d", a)
	}
	f.Start("foo", FindOptions{Continue: true, Backward: true})
	if a := f.Report(3); a != 3 {
		t.Fatalf("wrap backward %d", a)
	}
	f.Start("foo", FindOptions{Continue: true, Backward: true})
	if a := f.Report(3); a != 2 {
		t.Fatalf("previous %d", a)
	}
	// A different text or case rule starts over; so does a search without matches.
	if f.Start("bar", FindOptions{Continue: true}) || f.Start("bar", FindOptions{Continue: true, MatchCase: true}) {
		t.Fatal("a different search continued")
	}
	if a := f.Report(0); a != 0 {
		t.Fatalf("no match %d", a)
	}
	if f.Start("bar", FindOptions{Continue: true}) {
		t.Fatal("a search without matches continued")
	}
	f.Start("zed", FindOptions{Backward: true})
	if a := f.Report(4); a != 4 {
		t.Fatalf("fresh backward search starts at the last match, got %d", a)
	}
	f.Reset()
	if f.Text() != "" || f.Total() != 0 {
		t.Fatal("Reset")
	}
}

func TestFindScripts(t *testing.T) {
	s := FindCountScript(`a"b</script>`, true)
	if strings.Contains(s, "</script>") || !strings.Contains(s, `a\"b`) {
		t.Fatalf("text is not safely quoted: %s", s)
	}
	if !strings.Contains(FindScript("x", FindOptions{Backward: true, NoWrap: true}, true), "window.find(\"x\",false,true,false") {
		t.Fatal(FindScript("x", FindOptions{Backward: true, NoWrap: true}, true))
	}
	if strings.Contains(FindScript("x", FindOptions{}, false), "removeAllRanges") {
		t.Fatal("a continued search cleared the selection")
	}
}

func TestCookieOrigin(t *testing.T) {
	bad := errors.New("unsupported")
	uri, host, err := CookieOrigin("https://api.example.com:8443/x?y=1", bad)
	if err != nil || uri != "https://api.example.com:8443/" || host != "api.example.com" {
		t.Fatalf("%q %q %v", uri, host, err)
	}
	if _, _, err := CookieOrigin("krate://app", bad); !errors.Is(err, bad) {
		t.Fatalf("custom scheme: %v", err)
	}
	for _, in := range []string{"", "nonsense", "https://", "https://u:p@host/"} {
		if _, _, err := CookieOrigin(in, bad); err == nil || errors.Is(err, bad) {
			t.Errorf("CookieOrigin(%q) = %v", in, err)
		}
	}
}
