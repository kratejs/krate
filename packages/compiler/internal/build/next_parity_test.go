package build

import (
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/config"
	"github.com/kratejs/krate/packages/compiler/internal/migrate"
)

// parityGolden is testdata/next-routing/golden.json, recorded from a real
// Next 16 server by testdata/next-routing/gen.mjs.
type parityGolden struct {
	Next      string `json:"next"`
	Registers int    `json:"registers"`
	Cases     []struct {
		Name     string            `json:"name"`
		Method   string            `json:"method"`
		Path     string            `json:"path"`
		Query    map[string]string `json:"query"`
		Headers  map[string]string `json:"headers"`
		Cookies  map[string]string `json:"cookies"`
		Response struct {
			Status   int               `json:"status"`
			Location string            `json:"location"`
			Headers  map[string]string `json:"headers"`
			Body     string            `json:"body"`
		} `json:"response"`
	} `json:"cases"`
}

// parityIgnoredHeaders lists response headers that may differ from Next, with
// the reason for each. A header in the golden that is not listed here must be
// present with the same value, and Krate must not add one that is not listed.
var parityIgnoredHeaders = map[string]string{
	// Set by Next itself, meaningless for another server.
	"x-powered-by": "Next advertises itself; Krate sends no such header",
	// Next's prerender and RSC bookkeeping.
	"x-nextjs-cache":      "Next prerender cache state",
	"x-nextjs-prerender":  "Next prerender marker",
	"x-nextjs-stale-time": "Next client router cache hint",
	// Validators derive from body bytes, which differ between the two renderers.
	"etag": "validator of a differently rendered body",
	// Next lists its RSC negotiation headers in Vary.
	"vary": "Next varies pages on its RSC headers",
	// Added by Krate's own hardening, not by next.config.
	"strict-transport-security": "Krate sends HSTS on HTTPS requests; the fixture is plain HTTP so it is checked only when present",
	// Transport details.
	"content-length":    "differs with the rendered body",
	"content-encoding":  "compression is negotiated per server",
	"transfer-encoding": "framing is chosen per server",
	"accept-ranges":     "static file serving detail",
	"last-modified":     "file mtime of the copy under test",
	// Volatile or connection-level; gen.mjs drops them from the golden.
	"date": "changes on every response", "connection": "keep-alive negotiation", "keep-alive": "keep-alive negotiation",
	// Per-server bookkeeping.
	"x-request-id": "Krate tags every response with a request id; Next sends none",
	"refresh":      "Next adds Refresh: 0;url=... to permanent redirects for ancient clients; Location carries the same target",
}

// parityIgnoredPrefixes are header families that may differ.
var parityIgnoredPrefixes = []string{"x-krate-"}

func parityIgnored(name string) bool {
	if _, ok := parityIgnoredHeaders[name]; ok {
		return true
	}
	for _, p := range parityIgnoredPrefixes {
		if strings.HasPrefix(name, p) {
			return true
		}
	}
	return false
}

// parityRedirectExtras may appear on a 3xx response from Krate although Next
// sends only Location: Krate applies header rules to redirects too (pinned by
// TestCustomHeadersApplyToRedirectsAndErrors), and its redirect carries a small
// HTML stub body. Both are inert for a redirect, so only extras are allowed;
// a header Next sent must still match. A Cache-Control header rule (such as
// /brand/:path*) applies to a redirect inside its source as well.
var parityRedirectExtras = map[string]bool{
	"content-type": true, "cache-control": true, "x-content-type-options": true, "referrer-policy": true, "x-frame-options": true,
	"permissions-policy": true, "cross-origin-opener-policy": true,
	"content-security-policy": true, "content-security-policy-report-only": true,
}

// parityHeaderEqual compares one header, tolerating the differences listed
// in the comments. It reports whether got is acceptable for want.
func parityHeaderEqual(name, got, want string, status int) bool {
	if got == want {
		return true
	}
	switch name {
	case "cache-control":
		// Next marks a prerendered page "s-maxage=31536000" (a CDN directive) and a
		// not-found page no-store; Krate's static pages carry neither.
		if want == "s-maxage=31536000" {
			return true
		}
		// A server-rendered page: Next sends its full no-store spelling, Krate "no-store".
		if want == "private, no-cache, no-store, max-age=0, must-revalidate" && strings.Contains(got, "no-store") {
			return true
		}
		// A public file without a rule: Next says "public, max-age=0", Krate says
		// "no-cache". Both mean revalidate on every use.
		return want == "public, max-age=0" && got == "no-cache"
	case "content-type":
		// Same media type, spelled differently by the two MIME tables
		// (application/javascript vs text/javascript, charset case).
		norm := func(v string) string {
			v = strings.ToLower(strings.ReplaceAll(v, " ", ""))
			return strings.Replace(v, "application/javascript", "text/javascript", 1)
		}
		if norm(got) == norm(want) {
			return true
		}
		// A missing page: the fixture has no not-found page, so Krate answers with its
		// plain-text 404 where Next renders its built-in HTML one. The status matches.
		return status == 404
	}
	return false
}

// TestNextRoutingParity migrates the Next 16 fixture in testdata/next-routing,
// builds and serves it with Krate, and replays every request in cases.mjs
// against the status, Location and headers Next 16 produced.
func TestNextRoutingParity(t *testing.T) {
	fixture := filepath.Join("testdata", "next-routing")
	raw, err := os.ReadFile(filepath.Join(fixture, "golden.json"))
	if err != nil {
		t.Fatal(err)
	}
	var golden parityGolden
	if err := json.Unmarshal(raw, &golden); err != nil {
		t.Fatal(err)
	}

	root := t.TempDir()
	copyFixtureTree(t, filepath.Join(fixture, "app"), root)
	marker := filepath.Join(t.TempDir(), "instrumentation.marker")
	t.Setenv("INSTR_MARKER", marker)

	rep, err := migrate.ScanNext(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := migrate.Apply(rep, migrate.ApplyOptions{Force: true}); err != nil {
		t.Fatal(err)
	}
	prepareMigratedRuntime(t, root)
	cfg, err := config.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	b := New(root, cfg)
	b.SkipQualityChecks = true
	if err := b.BuildAll(); err != nil {
		t.Fatalf("build: %v", err)
	}
	provideRuntime(t, root)
	base, err := startKrateServer(t, root, cfg)
	if err != nil {
		t.Fatal(err)
	}

	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	for _, c := range golden.Cases {
		t.Run(c.Name, func(t *testing.T) {
			method := c.Method
			if method == "" {
				method = "GET"
			}
			target := base + c.Path
			if len(c.Query) > 0 {
				q := url.Values{}
				for k, v := range c.Query {
					q.Set(k, v)
				}
				target += "?" + q.Encode()
			}
			req, err := http.NewRequest(method, target, nil)
			if err != nil {
				t.Fatal(err)
			}
			req.Header.Set("Accept-Encoding", "identity")
			for k, v := range c.Headers {
				if strings.EqualFold(k, "host") {
					req.Host = v
					continue
				}
				req.Header.Set(k, v)
			}
			var cookies []string
			for k, v := range c.Cookies {
				cookies = append(cookies, k+"="+v)
			}
			sort.Strings(cookies)
			if len(cookies) > 0 {
				req.Header.Set("Cookie", strings.Join(cookies, "; "))
			}
			resp, err := client.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			body, _ := io.ReadAll(resp.Body)
			_ = resp.Body.Close()

			want := c.Response
			if resp.StatusCode != want.Status {
				t.Errorf("status %d, Next gave %d", resp.StatusCode, want.Status)
			}
			if got := resp.Header.Get("Location"); got != want.Location {
				t.Errorf("Location %q, Next gave %q", got, want.Location)
			}
			got := map[string]string{}
			for k, v := range resp.Header {
				got[strings.ToLower(k)] = strings.Join(v, ", ")
			}
			for name, wv := range want.Headers {
				if name == "location" || parityIgnored(name) {
					continue
				}
				if gv, ok := got[name]; !ok {
					// Next marks an error page uncacheable; Krate's plain 404 sends no Cache-Control
					// at all, which carries no freshness either.
					if name == "cache-control" && want.Status == 404 {
						continue
					}
					t.Errorf("header %s missing, Next sent %q", name, wv)
				} else if !parityHeaderEqual(name, gv, wv, want.Status) {
					t.Errorf("header %s = %q, Next sent %q", name, gv, wv)
				}
			}
			redirect := want.Status >= 300 && want.Status < 400
			for name, gv := range got {
				if _, ok := want.Headers[name]; ok || parityIgnored(name) || (redirect && parityRedirectExtras[name]) {
					continue
				}
				t.Errorf("extra header %s: %q (Next sent none)", name, gv)
			}
			if strings.HasPrefix(c.Path, "/api/") && want.Status == 200 {
				if string(body) != want.Body {
					t.Errorf("body %q, Next gave %q", body, want.Body)
				}
			}
		})
	}

	data, _ := os.ReadFile(marker)
	if n := len(strings.Fields(string(data))); n != golden.Registers {
		t.Errorf("register() ran %d time(s), Next ran it %d", n, golden.Registers)
	}
}
