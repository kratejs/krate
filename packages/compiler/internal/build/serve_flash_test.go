package build

import (
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func flashCookieValue(t *testing.T, id, resultJSON string) string {
	t.Helper()
	// Serve tests in the same binary may have set the process-wide signing secret.
	flashSecret.Store("")
	return base64.RawURLEncoding.EncodeToString([]byte(`{"a":"` + id + `","r":` + resultJSON + `}`))
}

func TestFlashNoticeRenderedForMatchingForm(t *testing.T) {
	page := `<!doctype html><body><form method=post action="/__krate/action/abc123?__krate_args=%5B%5D"><input name=u></form>` +
		`<form action=/__krate/action/other><button>x</button></form></body>`
	h := flashNoticeMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(page))
	}))
	val := flashCookieValue(t, "abc123", `{"error":"user <b>is</b> required","fields":{"u":"too short"}}`)
	req := httptest.NewRequest("GET", "/", nil)
	req.AddCookie(&http.Cookie{Name: "krate_flash", Value: val})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	body := rec.Body.String()
	want := `><input name=u>`
	if !strings.Contains(body, `class="krate-flash"`) || !strings.Contains(body, "user &lt;b&gt;is&lt;/b&gt; required") || !strings.Contains(body, "<p>too short</p>") {
		t.Fatalf("notice missing or unescaped:\n%s", body)
	}
	if i, j := strings.Index(body, `class="krate-flash"`), strings.Index(body, want); i < 0 || j < i || strings.Index(body, "action=/__krate/action/other") < j {
		t.Errorf("notice must sit inside the matching form:\n%s", body)
	}
	if strings.Count(body, `class="krate-flash"`) != 1 {
		t.Errorf("exactly one notice expected:\n%s", body)
	}
	if rec.Header().Get("Cache-Control") != "no-store" {
		t.Errorf("Cache-Control = %q, want no-store", rec.Header().Get("Cache-Control"))
	}
	var cleared bool
	for _, c := range rec.Result().Cookies() {
		if c.Name == "krate_flash" && c.MaxAge < 0 {
			cleared = true
		}
	}
	if !cleared {
		t.Errorf("flash cookie must be cleared: %v", rec.Header()["Set-Cookie"])
	}
}

func TestFlashNoticeLeavesOtherRequestsAlone(t *testing.T) {
	page := `<body><form action="/__krate/action/abc123"></form></body>`
	h := flashNoticeMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(page))
	}))
	cases := map[string]*http.Cookie{
		"no cookie":       nil,
		"other action":    {Name: "krate_flash", Value: flashCookieValue(t, "zzz", `{"error":"x"}`)},
		"nothing to show": {Name: "krate_flash", Value: flashCookieValue(t, "abc123", `{"count":3}`)},
		"bad cookie":      {Name: "krate_flash", Value: "!!!"},
	}
	for name, c := range cases {
		req := httptest.NewRequest("GET", "/", nil)
		if c != nil {
			req.AddCookie(c)
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Body.String() != page {
			t.Errorf("%s: page changed: %s", name, rec.Body.String())
		}
		for _, sc := range rec.Header()["Set-Cookie"] {
			t.Errorf("%s: unexpected Set-Cookie %q", name, sc)
		}
	}
}

// An action id that merely starts with the flashed id must not receive the
// notice, and the pattern must work for ids that need regexp escaping.
func TestInjectFlashNoticeMatchesWholeActionID(t *testing.T) {
	page := []byte(`<form action="/__krate/action/a10"></form><form action="/__krate/action/a1?x=1"></form>`)
	out, ok := injectFlashNotice(page, "raw", flashPayload{A: "a1", R: []byte(`{"error":"bad"}`)})
	if !ok {
		t.Fatal("notice not injected")
	}
	s := string(out)
	i := strings.Index(s, `class="krate-flash"`)
	if i < strings.Index(s, "a1?x=1") {
		t.Errorf("notice went into the a10 form:\n%s", s)
	}
	if _, ok := injectFlashNotice(page, "raw", flashPayload{A: ".*", R: []byte(`{"error":"bad"}`)}); ok {
		t.Error("an id with regexp metacharacters must not match other forms")
	}
}

// The same vector is pinned in packages/runtime/src/handler-html.test.ts: the
// portable handler signs and verifies exactly what the Go host does, so a
// cookie minted by one is read by the other when they share KRATE_FLASH_SECRET.
const (
	flashVectorSecret  = "krate-test-flash-secret-0123456789"
	flashVectorPayload = "eyJhIjoiYWN0MSIsInIiOnsiZXJyb3IiOiJOYW1lIGlzIHJlcXVpcmVkIn19"
	flashVectorCookie  = flashVectorPayload + ".zLQd73rhp1HN8J0vNvSeLqvlCqquygUX-mXuuN24dlY"
)

func TestFlashSignatureMatchesPortableHandler(t *testing.T) {
	if got := signFlashValue(flashVectorSecret, flashVectorPayload); got != flashVectorCookie {
		t.Fatalf("signFlashValue = %q, want %q", got, flashVectorCookie)
	}
	old, _ := flashSecret.Load().(string)
	flashSecret.Store(flashVectorSecret)
	defer flashSecret.Store(old)
	if payload, ok := verifyFlash(flashVectorCookie); !ok || payload != flashVectorPayload {
		t.Errorf("a cookie signed by the portable handler must verify: %q %v", payload, ok)
	}
	for _, bad := range []string{flashVectorPayload, flashVectorPayload + ".", flashVectorPayload + ".AAAA", "x" + flashVectorCookie} {
		if _, ok := verifyFlash(bad); ok {
			t.Errorf("%q must not verify", bad)
		}
	}
}

func TestFlashSecretEnvOverridesInternalSecret(t *testing.T) {
	t.Setenv(flashSecretEnv, "")
	if got := flashSecretFor("internal"); got != "internal" {
		t.Errorf("without the variable the internal secret signs, got %q", got)
	}
	t.Setenv(flashSecretEnv, "shared-secret")
	if got := flashSecretFor("internal"); got != "shared-secret" {
		t.Errorf("KRATE_FLASH_SECRET must win, got %q", got)
	}
}
