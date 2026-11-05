package build

import (
	"net/http"
	"os"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/deploy"
)

type parityCase struct {
	name    string
	method  string
	path    string
	headers map[string]string
	body    string
	// contains and absent are checked on the node target's body.
	contains []string
	absent   []string
	// inHead and notInHead are checked on the document head of both hosts,
	// notInBody on everything after </head>.
	inHead, notInHead, notInBody []string
}

// parityHeaders are compared between `krate start` and the node target: the
// security and cache policy, redirects and the render markers.
var parityHeaders = []string{
	"Content-Type", "Cache-Control", "Location", "X-Krate-Cache", "X-Krate-Error", "Allow",
	"X-Content-Type-Options", "X-Frame-Options", "Referrer-Policy", "Permissions-Policy",
	"X-Custom", "X-Mw", "Content-Security-Policy", "Strict-Transport-Security",
}

var timestampRe = regexp.MustCompile(`\d{10,}`)

// digestRe matches the error digest in a rendered error page (the digest of a
// failed render depends on the stack, which differs between hosts).
var digestRe = regexp.MustCompile(`(id=d>|id="d">|"digest":")[0-9a-f]{10}`)

// normalizeBody blanks what legitimately differs between hosts. Digests go
// first: a digest is ten hex characters, and one that happens to be all
// decimal digits (about 1 in 100) looks like a timestamp, so normalizing
// timestamps first turned it into "N" on one host and "DIGEST" on the other,
// and the 500 page comparison failed on that run only.
func normalizeBody(s string) string {
	// Digests first: a digest of only decimal digits also looks like a timestamp.
	return timestampRe.ReplaceAllString(digestRe.ReplaceAllString(s, "${1}DIGEST"), "N")
}

func setCookieNames(h http.Header) string {
	var names []string
	for _, c := range h.Values("Set-Cookie") {
		names = append(names, strings.SplitN(c, "=", 2)[0])
	}
	sort.Strings(names)
	return strings.Join(names, ",")
}

// TestServerlessNodeParity builds the fixture app once, serves it with the Go
// host (`krate start`) and with the node target, and runs the same request
// matrix against both: status, security and cache headers, redirects and body
// must agree.
func TestServerlessNodeParity(t *testing.T) {
	if !canStopInProcessServer {
		t.Skip("in-process Go server needs a unix host")
	}
	root, cfg := serverlessProject(t, "serverless")
	goBase, err := startKrateServer(t, root, cfg)
	if err != nil {
		t.Fatalf("krate start: %v", err)
	}
	plan, err := BuildServerBundle(root, cfg, "node")
	if err != nil {
		t.Fatalf("BuildServerBundle: %v", err)
	}
	if len(plan.Warnings) > 0 {
		t.Logf("warnings: %v", plan.Warnings)
	}
	if fi, err := os.Stat(plan.Bundle); err == nil {
		t.Logf("server.mjs for the fixture: %d bytes", fi.Size())
		if fi.Size() > 2<<20 {
			t.Errorf("server.mjs is %d bytes; the handler should stay well under 2 MiB for this fixture", fi.Size())
		}
	}
	nodeBase := startNodeBundle(t, root, cfg)

	ids := actionIDs(t, root)
	same := map[string]string{"Sec-Fetch-Site": "same-origin", "X-Krate-Action": "fetch", "Content-Type": "application/json"}
	cases := []parityCase{
		{name: "home", method: "GET", path: "/"},
		{name: "static page", method: "GET", path: "/about/"},
		{name: "static no slash", method: "GET", path: "/about"},
		{name: "dynamic ssg baked", method: "GET", path: "/blog/first/"},
		{name: "dynamic ssg template", method: "GET", path: "/blog/zzz/"},
		{name: "catch-all", method: "GET", path: "/docs/a/b/"},
		{name: "ssr", method: "GET", path: "/ssr/?name=ada"},
		{name: "ssr middleware", method: "GET", path: "/ssr/"},
		{name: "isr miss", method: "GET", path: "/isr/"},
		{name: "isr hit", method: "GET", path: "/isr/"},
		{name: "streaming", method: "GET", path: "/stream/"},
		{name: "server component", method: "GET", path: "/post/abc/", headers: map[string]string{"Cookie": "sid=s1"}},
		{name: "notFound", method: "GET", path: "/post/missing/"},
		{name: "redirect()", method: "GET", path: "/post/old/"},
		{name: "500 render", method: "GET", path: "/post/boom/"},
		{name: "unknown page", method: "GET", path: "/nope/"},
		{name: "unknown asset", method: "GET", path: "/nope.png"},
		{name: "config redirect", method: "GET", path: "/old-about"},
		{name: "config redirect param", method: "GET", path: "/legacy/hello"},
		{name: "config rewrite", method: "GET", path: "/team"},
		{name: "custom header", method: "GET", path: "/about/"},
		{name: "middleware redirect", method: "GET", path: "/admin"},
		{name: "middleware block", method: "GET", path: "/teapot"},
		{name: "page POST", method: "POST", path: "/about/"},
		{name: "page OPTIONS", method: "OPTIONS", path: "/about/"},
		{name: "api get", method: "GET", path: "/api/hello?name=ada"},
		{name: "api post", method: "POST", path: "/api/hello", body: "abcde"},
		{name: "api dynamic", method: "GET", path: "/api/items/42"},
		{name: "api 404", method: "GET", path: "/api/nope"},
		{name: "api 405", method: "DELETE", path: "/api/hello"},
		{name: "api options", method: "OPTIONS", path: "/api/hello"},
		{name: "api head", method: "HEAD", path: "/api/hello"},
		{name: "api error", method: "GET", path: "/api/boom"},
		{name: "action add", method: "POST", path: "/__krate/action/" + ids["add"], headers: same, body: `{"args":[2,3]}`},
		{name: "action form redirect", method: "POST", path: "/__krate/action/" + ids["greet"], headers: map[string]string{"Sec-Fetch-Site": "same-origin", "Content-Type": "application/x-www-form-urlencoded"}, body: "who=ada"},
		{name: "action form state", method: "POST", path: "/__krate/action/" + ids["greet"], headers: map[string]string{"Sec-Fetch-Site": "same-origin", "Content-Type": "application/x-www-form-urlencoded", "Referer": "http://x/form/"}, body: "who="},
		{name: "action cross-site", method: "POST", path: "/__krate/action/" + ids["add"], headers: map[string]string{"Sec-Fetch-Site": "cross-site", "X-Krate-Action": "fetch", "Content-Type": "application/json"}, body: `{"args":[1,1]}`},
		{name: "action unknown", method: "POST", path: "/__krate/action/nope", headers: same, body: `{"args":[]}`},
		{name: "action GET", method: "GET", path: "/__krate/action/" + ids["add"]},
		{name: "internal path", method: "GET", path: "/.krate/manifest.json"},
		{name: "healthz", method: "GET", path: "/healthz"},
		{name: "path clean", method: "GET", path: "/about//"},
		{name: "api no slash", method: "GET", path: "/api"},
		{name: "api trailing slash", method: "GET", path: "/api/hello/"},
		{name: "encoded path", method: "GET", path: "/blog/a%20b/"},
		{name: "dot file", method: "GET", path: "/.env"},
		{name: "traversal", method: "GET", path: "/..%2f..%2fetc/passwd"},
		{name: "HEAD page", method: "HEAD", path: "/ssr/"},
		{name: "isr query variant", method: "GET", path: "/isr/?x=1"},
		// <Head> children of async pages land in the initial <head>.
		{name: "head ssr", method: "GET", path: "/headtags/hello/",
			inHead: []string{
				"<title>Post hello | Krate</title>",
				`<meta name="description" content="About &lt;&quot;hello&quot;&gt; &amp; more">`,
				`<meta property="og:title" content="OG hello">`,
				`<meta property="og:image" content="https://example.com/a.png">`,
				`<meta property="og:image" content="https://example.com/b.png">`,
				`<link rel="canonical" href="https://example.com/headtags/hello">`,
				`<script type="application/ld+json">{"@type":"Article","name":"hello","note":"<\/script>"}</script>`,
				"og:site_name",
			},
			notInHead: []string{"layout description", "https://example.com/layout"},
			notInBody: []string{"<meta ", "<title", "<link rel=\"canonical", "ld+json"}},
		{name: "head ssr other slug", method: "GET", path: "/headtags/other/",
			inHead: []string{"<title>Post other | Krate</title>", `href="https://example.com/headtags/other"`}},
		{name: "head isr miss", method: "GET", path: "/headisr/",
			inHead:    []string{"<title>ISR head title</title>", `<meta name="robots" content="noindex">`, `<meta property="og:type" content="website">`},
			notInBody: []string{"<meta ", "<title"}},
		{name: "head isr hit", method: "GET", path: "/headisr/",
			inHead:    []string{"<title>ISR head title</title>", `<meta name="robots" content="noindex">`},
			notInBody: []string{"<meta ", "<title"}},
		{name: "head server component region", method: "GET", path: "/headcomponent/",
			inHead:    []string{"<title>Written by Ada</title>", `<meta name="author" content="Ada">`},
			notInBody: []string{"<meta ", "<title"}},
		// A streamed Suspense region renders after the head was sent: its tags are
		// applied by the region script, the baked ones stay in the head.
		{name: "head streamed region", method: "GET", path: "/headstream/",
			inHead:    []string{`<meta name=keywords content="baked, static"/>`},
			notInHead: []string{"from a streamed region"},
			contains:  []string{"from a streamed region", "data-krate-stream"}},
		// Per-segment pages, inside the blog layout.
		{name: "segment 404 unmatched", method: "GET", path: "/blog/a/b/c/", contains: []string{"blog-404", "id=blog-layout"}},
		{name: "segment 404 unmatched accept", method: "GET", path: "/blog/a/b/c", headers: map[string]string{"Accept": "text/html"}, contains: []string{"blog-404"}},
		{name: "segment 404 closed dynamic", method: "GET", path: "/blog/post/zzz/", contains: []string{"blog-404", "id=blog-layout"}},
		{name: "closed dynamic known", method: "GET", path: "/blog/post/a/", contains: []string{"closed a"}},
		{name: "segment 404 notFound()", method: "GET", path: "/blog/ssr-gone/", contains: []string{"blog-404", "id=blog-layout"}},
		{name: "segment 404 dynamic dir", method: "GET", path: "/shop/shoes/nothing/", contains: []string{"cat-404 shoes"}},
		{name: "segment 500 digest", method: "GET", path: "/blog/ssr-crash/", contains: []string{"blog-error", "id=blog-layout", "Internal Server Error", `id="__krate_error"`}, absent: []string{"secret crash detail"}},
		{name: "global 404 outside", method: "GET", path: "/elsewhere/deep/", contains: []string{"custom-404"}, absent: []string{"blog-404"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			g := doMatrix(t, goBase, c.method, c.path, c.headers, c.body)
			n := doMatrix(t, nodeBase, c.method, c.path, c.headers, c.body)
			if g.status != n.status {
				t.Errorf("status: krate start %d, node target %d\nstart body: %.300s\nnode body: %.300s", g.status, n.status, g.body, n.body)
			}
			for _, h := range parityHeaders {
				gv := strings.ReplaceAll(g.headers.Get(h), goBase, "BASE")
				nv := strings.ReplaceAll(n.headers.Get(h), nodeBase, "BASE")
				if gv != nv {
					t.Errorf("header %s: krate start %q, node target %q", h, gv, nv)
				}
			}
			if gc, nc := setCookieNames(g.headers), setCookieNames(n.headers); gc != nc {
				t.Errorf("Set-Cookie names: krate start %q, node target %q", gc, nc)
			}
			if gb, nb := normalizeBody(g.body), normalizeBody(n.body); gb != nb {
				t.Errorf("body differs\nkrate start: %s\nnode target: %s", gb, nb)
			}
			for host, body := range map[string]string{"krate start": g.body, "node target": n.body} {
				head, rest := body, ""
				if i := strings.Index(strings.ToLower(body), "</head>"); i >= 0 {
					head, rest = body[:i], body[i:]
				}
				for _, want := range c.inHead {
					if !strings.Contains(head, want) {
						t.Errorf("%s: <head> lacks %q:\n%.1500s", host, want, head)
					}
				}
				for _, bad := range c.notInHead {
					if strings.Contains(head, bad) {
						t.Errorf("%s: <head> must not contain %q:\n%.1500s", host, bad, head)
					}
				}
				for _, bad := range c.notInBody {
					if strings.Contains(rest, bad) {
						t.Errorf("%s: body must not contain %q:\n%.1500s", host, bad, rest)
					}
				}
			}
			for _, want := range c.contains {
				if !strings.Contains(n.body, want) {
					t.Errorf("node target body lacks %q:\n%.600s", want, n.body)
				}
			}
			for _, bad := range c.absent {
				if strings.Contains(n.body, bad) {
					t.Errorf("node target body must not contain %q:\n%.600s", bad, n.body)
				}
			}
		})
	}
	_ = deploy.ServerFeatures
}

func TestNormalizeBodyDigestOfOnlyDigits(t *testing.T) {
	const page = `<script type="application/json" id="__krate_error">{"name":"Error","message":"Internal Server Error","digest":"%s"}</script> t=1700000000000`
	letters := normalizeBody(strings.Replace(page, "%s", "08926a4e5a", 1))
	digits := normalizeBody(strings.Replace(page, "%s", "0123456789", 1))
	if letters != digits {
		t.Errorf("two digests must normalize alike:\n%s\n%s", letters, digits)
	}
	if !strings.Contains(digits, "t=N") {
		t.Errorf("timestamps must still be blanked: %s", digits)
	}
}
