package shell

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestGuardRejectsCrossOriginCookieRequests(t *testing.T) {
	const token, port = "tok123", "4000"
	hit := 0
	h := guardHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { hit++ }), token, port)
	do := func(method string, hdr map[string]string, cookie bool) int {
		r := httptest.NewRequest(method, "http://127.0.0.1:"+port+"/__krate/call", nil)
		if cookie {
			r.AddCookie(&http.Cookie{Name: tokenCookieName(port), Value: token})
		}
		for k, v := range hdr {
			r.Header.Set(k, v)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w.Code
	}
	if c := do("POST", nil, true); c != 200 {
		t.Fatalf("plain client with the cookie: %d", c)
	}
	if c := do("POST", map[string]string{"Origin": "http://127.0.0.1:" + port, "Sec-Fetch-Site": "same-origin"}, true); c != 200 {
		t.Fatalf("the app page itself: %d", c)
	}
	before := hit
	// A page served by another localhost port is same-site, so SameSite=Strict
	// cookies still travel; the request must be refused anyway.
	for _, hdr := range []map[string]string{
		{"Origin": "http://localhost:3000"},
		{"Origin": "null"},
		{"Sec-Fetch-Site": "same-site"},
		{"Sec-Fetch-Site": "cross-site"},
	} {
		if c := do("POST", hdr, true); c != http.StatusForbidden {
			t.Errorf("%v: %d, want 403", hdr, c)
		}
	}
	if hit != before {
		t.Fatal("a cross-origin request reached the handler")
	}
	// The header token is not an ambient credential and stays usable.
	if c := do("POST", map[string]string{"Origin": "http://localhost:3000", tokenHeader: token}, false); c != 200 {
		t.Errorf("header token: %d", c)
	}
}

func TestGuardCookieNamePerPort(t *testing.T) {
	h := guardHandler(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}), "tok", "4000")
	r := httptest.NewRequest("GET", "http://127.0.0.1:4000/?"+tokenParam+"=tok", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	cs := w.Result().Cookies()
	if len(cs) != 1 || cs[0].Name != "krate_token_4000" {
		t.Fatalf("cookies: %+v", cs)
	}
}
