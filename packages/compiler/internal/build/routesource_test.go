package build

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/config"
)

func TestMergeRequestQuery(t *testing.T) {
	tests := []struct {
		name, dest, raw, want string
	}{
		{"no request query", "/templates", "", "/templates"},
		{"carried over", "/templates", "vehicle=x", "/templates?vehicle=x"},
		{"order kept", "/t", "b=2&a=1", "/t?b=2&a=1"},
		{"repeated keys kept", "/t", "a=1&a=2", "/t?a=1&a=2"},
		{"destination query wins", "/t?a=9", "a=1&b=2", "/t?a=9&b=2"},
		{"destination adds a key", "/t?c=3", "a=1", "/t?a=1&c=3"},
		{"fragment kept", "/t#top", "a=1", "/t?a=1#top"},
		{"space encoded", "/t", "q=a+b", "/t?q=a%20b"},
		{"absolute destination", "https://example.com/x", "a=1", "https://example.com/x?a=1"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := mergeRequestQuery(tt.dest, tt.raw); got != tt.want {
				t.Fatalf("mergeRequestQuery(%q, %q) = %q, want %q", tt.dest, tt.raw, got, tt.want)
			}
		})
	}
}

func TestRewriteDestinationNextSyntax(t *testing.T) {
	tests := []struct {
		name, path, source, dest, want string
	}{
		{"lookahead slug", "/store/foo", "/store/:slug((?!compare$)[^/]+)", "/templates/:slug", "/templates/foo"},
		{"lookahead rejects", "/store/compare", "/store/:slug((?!compare$)[^/]+)", "/templates/:slug", "/templates/:slug"},
		{"star keeps slashes", "/docs/a/b", "/docs/:path*", "/documentation/:path*", "/documentation/a/b"},
		{"param in the query is query escaped", "/u/a&b=c", "/u/:name", "/x?n=:name", "/x?n=a%26b%3Dc"},
		{"explicit regex param", "/f/42", "/f/:id(\\d+)", "/files/:id", "/files/42"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := rewriteDestination(tt.path, tt.source, tt.dest); got != tt.want {
				t.Fatalf("rewriteDestination = %q, want %q", got, tt.want)
			}
		})
	}
}

// ddRedirects are the redirects of the migrated DigitalDesigns app.
func ddRedirects() []config.Redirect {
	has := func(key string) []config.RouteCondition {
		return []config.RouteCondition{{Type: "query", Key: key}}
	}
	return []config.Redirect{
		{Source: "/store/plans/compare", Destination: "/store/compare", StatusCode: 308},
		{Source: "/store/plans", Destination: "/store", StatusCode: 308},
		{Source: "/store", Destination: "/templates", StatusCode: 308, Has: has("vehicle")},
		{Source: "/store", Destination: "/templates", StatusCode: 308, Has: has("category")},
		{Source: "/store", Destination: "/templates", StatusCode: 308, Has: has("q")},
		{Source: "/store/:slug((?!compare$)[^/]+)", Destination: "/templates/:slug", StatusCode: 308},
	}
}

func okHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("inner " + r.URL.Path))
	})
}

func do(h http.Handler, method, target string, hdr map[string]string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, target, nil)
	for k, v := range hdr {
		if strings.EqualFold(k, "host") {
			r.Host = v
			continue
		}
		r.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func TestConfigRedirectsLikeNext(t *testing.T) {
	h := configRedirectMiddleware(ddRedirects(), okHandler())
	tests := []struct {
		target string
		status int
		loc    string
	}{
		{"/store?vehicle=x", 308, "/templates?vehicle=x"},
		{"/store?category=a&q=b", 308, "/templates?category=a&q=b"},
		{"/store?q=hi+there", 308, "/templates?q=hi%20there"},
		{"/store/some-slug", 308, "/templates/some-slug"},
		{"/store/some-slug?ref=1", 308, "/templates/some-slug?ref=1"},
		{"/store/compare", 200, ""},
		{"/store/plans?x=1", 308, "/store?x=1"},
		{"/store/plans/compare", 308, "/store/compare"},
		{"/store", 200, ""},
		{"/store?other=1", 200, ""},
		{"/store?vehicle=", 200, ""},
		{"/templates", 200, ""},
	}
	for _, tt := range tests {
		t.Run(tt.target, func(t *testing.T) {
			w := do(h, "GET", tt.target, nil)
			if w.Code != tt.status {
				t.Fatalf("status = %d, want %d", w.Code, tt.status)
			}
			if got := w.Header().Get("Location"); got != tt.loc {
				t.Fatalf("Location = %q, want %q", got, tt.loc)
			}
		})
	}
}

func TestConfigRedirectStatusCodes(t *testing.T) {
	rules := []config.Redirect{
		{Source: "/a", Destination: "/x"},
		{Source: "/b", Destination: "/x", Permanent: true},
		{Source: "/c", Destination: "/x", StatusCode: 307},
		{Source: "/d", Destination: "/x", StatusCode: 303},
		{Source: "/e", Destination: "/x", Permanent: true, StatusCode: 302},
	}
	h := configRedirectMiddleware(rules, okHandler())
	for path, want := range map[string]int{"/a": 302, "/b": 301, "/c": 307, "/d": 303, "/e": 302} {
		if got := do(h, "GET", path, nil).Code; got != want {
			t.Errorf("%s: status %d, want %d", path, got, want)
		}
	}
}

func TestConfigRedirectHasMissing(t *testing.T) {
	rules := []config.Redirect{
		{Source: "/app", Destination: "/new/:org", StatusCode: 307, Has: []config.RouteCondition{{Type: "host", Value: "(?P<org>[a-z]+)\\.example\\.com"}}},
		{Source: "/gate", Destination: "/login", StatusCode: 307, Missing: []config.RouteCondition{{Type: "cookie", Key: "session"}}},
		{Source: "/m", Destination: "/mobile/:ua", StatusCode: 307, Has: []config.RouteCondition{{Type: "header", Key: "x-device", Value: "(?P<ua>ios|android)"}}},
		{Source: "/t", Destination: "/to/:token", StatusCode: 307, Has: []config.RouteCondition{{Type: "query", Key: "token"}}},
	}
	h := configRedirectMiddleware(rules, okHandler())
	tests := []struct {
		name, target string
		hdr          map[string]string
		status       int
		loc          string
	}{
		{"host capture", "/app", map[string]string{"Host": "acme.example.com"}, 307, "/new/acme"},
		{"host no match", "/app", map[string]string{"Host": "localhost:3000"}, 200, ""},
		{"missing cookie redirects", "/gate", nil, 307, "/login"},
		{"cookie present passes", "/gate", map[string]string{"Cookie": "session=1"}, 200, ""},
		{"header capture", "/m", map[string]string{"X-Device": "ios"}, 307, "/mobile/ios"},
		{"header mismatch", "/m", map[string]string{"X-Device": "palm"}, 200, ""},
		{"key as capture", "/t?token=ab", nil, 307, "/to/ab?token=ab"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := do(h, "GET", tt.target, tt.hdr)
			if w.Code != tt.status || w.Header().Get("Location") != tt.loc {
				t.Fatalf("got %d %q, want %d %q", w.Code, w.Header().Get("Location"), tt.status, tt.loc)
			}
		})
	}
}

// The redirect layer sits in front of the handler it wraps, so it answers
// before a middleware gate that refuses everything and also for /api.
func TestConfigRedirectRunsBeforeInnerAndOnAPI(t *testing.T) {
	deny := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	})
	rules := append(ddRedirects(), config.Redirect{Source: "/api/old/:rest*", Destination: "/api/new/:rest*", StatusCode: 308})
	h := configRedirectMiddleware(rules, deny)
	if w := do(h, "GET", "/store?vehicle=x", nil); w.Code != 308 {
		t.Errorf("/store?vehicle=x: %d, want 308 despite the gate", w.Code)
	}
	if w := do(h, "GET", "/api/old/a/b?k=v", nil); w.Code != 308 || w.Header().Get("Location") != "/api/new/a/b?k=v" {
		t.Errorf("/api/old: %d %q", w.Code, w.Header().Get("Location"))
	}
	if w := do(h, "GET", "/store", nil); w.Code != 503 {
		t.Errorf("/store: %d, want the inner 503", w.Code)
	}
	if w := do(h, "GET", "/__krate/x", nil); w.Code != 503 {
		t.Errorf("internal path: %d, want it left alone", w.Code)
	}
}

func TestConfigRedirectOpenRedirectProtection(t *testing.T) {
	rules := []config.Redirect{
		{Source: "/u/:name", Destination: "/:name"},
		{Source: "/docs/:path*", Destination: "/documentation/:path*"},
		{Source: "/legacy/*", Destination: "/:splat"},
		{Source: "/s/:slug((?!x$)[^/]+)", Destination: "/:slug"},
	}
	h := configRedirectMiddleware(rules, okHandler())
	for _, target := range []string{
		"/u/%5Cevil.com",
		"/u/%2F%2Fevil.com",
		"/legacy//evil.com",
		"/legacy/%5Cevil.com",
		"/s/%5Cevil.com",
		"/s/..%2f..%2fevil.com",
	} {
		w := do(h, "GET", target, nil)
		loc := w.Header().Get("Location")
		if w.Code == 200 {
			continue // a multi-segment value does not match a one-segment parameter
		}
		if w.Code < 300 || w.Code > 399 {
			t.Errorf("%s: status %d, want a redirect", target, w.Code)
			continue
		}
		if strings.HasPrefix(loc, "//") || strings.HasPrefix(loc, `/\`) || !strings.HasPrefix(loc, "/") {
			t.Errorf("%s: Location %q leaves the site", target, loc)
		}
	}
	// A query value never adds a parameter or a host.
	h2 := configRedirectMiddleware([]config.Redirect{{Source: "/u/:name", Destination: "/go?n=:name"}}, okHandler())
	w := do(h2, "GET", "/u/a%26admin%3D1", nil)
	if loc := w.Header().Get("Location"); loc != "/go?n=a%26admin%3D1" {
		t.Errorf("query substitution: Location %q", loc)
	}
}

func TestCustomHeadersWinOverHandlerHeaders(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/_hashed/") {
			w.Header().Set("Cache-Control", immutableCacheControl)
		} else {
			w.Header().Set("Cache-Control", "no-cache")
		}
		w.Header().Add("Vary", "Accept-Encoding")
		_, _ = w.Write([]byte("body"))
	})
	rules := []config.HeaderRule{
		{Source: "/:path*", Headers: map[string]string{"X-Frame": "deny", "Cache-Control": "public, max-age=60"}},
		{Source: "/assets/*", Headers: map[string]string{"X-Assets": "1"}},
		{Source: "/:file(.+\\.wasm)", Headers: map[string]string{"Cache-Control": "public, max-age=31536000, immutable", "Vary": "Origin"}},
		{Source: "/m", Headers: map[string]string{"X-Mobile": "1"}, Has: []config.RouteCondition{{Type: "header", Key: "x-device", Value: "ios"}}},
		{Source: "/quiet", Headers: map[string]string{"X-Quiet": "1"}, Missing: []config.RouteCondition{{Type: "query", Key: "loud"}}},
	}
	h := customHeadersMiddleware(rules, inner)
	t.Run("rule beats handler Cache-Control", func(t *testing.T) {
		w := do(h, "GET", "/page", nil)
		if got := w.Header().Get("Cache-Control"); got != "public, max-age=60" {
			t.Errorf("Cache-Control = %q", got)
		}
		if w.Header().Get("X-Frame") != "deny" {
			t.Errorf("X-Frame missing")
		}
	})
	t.Run("next style source matches nested paths", func(t *testing.T) {
		if w := do(h, "GET", "/a/b/c", nil); w.Header().Get("X-Frame") != "deny" {
			t.Errorf("/:path* did not match /a/b/c")
		}
		if w := do(h, "GET", "/", nil); w.Header().Get("X-Frame") != "deny" {
			t.Errorf("/:path* did not match /")
		}
	})
	t.Run("legacy prefix still works", func(t *testing.T) {
		if w := do(h, "GET", "/assets/x.js", nil); w.Header().Get("X-Assets") != "1" {
			t.Errorf("/assets/* did not match")
		}
		if w := do(h, "GET", "/other", nil); w.Header().Get("X-Assets") != "" {
			t.Errorf("/assets/* matched /other")
		}
	})
	t.Run("Krate's immutable asset cache is kept", func(t *testing.T) {
		w := do(h, "GET", "/_hashed/a.1234567.js", nil)
		if got := w.Header().Get("Cache-Control"); got != immutableCacheControl {
			t.Errorf("Cache-Control = %q, want the immutable value", got)
		}
		if w.Header().Get("X-Frame") != "deny" {
			t.Errorf("other headers of the rule must still apply")
		}
	})
	t.Run("Vary is appended", func(t *testing.T) {
		w := do(h, "GET", "/x.wasm", nil)
		if got := strings.Join(w.Header().Values("Vary"), ","); got != "Accept-Encoding,Origin" {
			t.Errorf("Vary = %q", got)
		}
	})
	t.Run("has", func(t *testing.T) {
		if w := do(h, "GET", "/m", map[string]string{"X-Device": "ios"}); w.Header().Get("X-Mobile") != "1" {
			t.Errorf("has did not apply")
		}
		if w := do(h, "GET", "/m", nil); w.Header().Get("X-Mobile") != "" {
			t.Errorf("rule applied without its has item")
		}
	})
	t.Run("missing", func(t *testing.T) {
		if w := do(h, "GET", "/quiet", nil); w.Header().Get("X-Quiet") != "1" {
			t.Errorf("missing did not apply")
		}
		if w := do(h, "GET", "/quiet?loud=1", nil); w.Header().Get("X-Quiet") != "" {
			t.Errorf("rule applied although its missing item matched")
		}
	})
}

func TestCustomHeadersApplyToRedirectsAndErrors(t *testing.T) {
	rules := []config.HeaderRule{{Source: "/:path*", Headers: map[string]string{"X-Rule": "1"}}}
	h := customHeadersMiddleware(rules, configRedirectMiddleware(ddRedirects(), http.NotFoundHandler()))
	if w := do(h, "GET", "/store/foo", nil); w.Code != 308 || w.Header().Get("X-Rule") != "1" {
		t.Errorf("redirect: %d X-Rule=%q", w.Code, w.Header().Get("X-Rule"))
	}
	if w := do(h, "GET", "/missing", nil); w.Code != 404 || w.Header().Get("X-Rule") != "1" {
		t.Errorf("404: %d X-Rule=%q", w.Code, w.Header().Get("X-Rule"))
	}
}
