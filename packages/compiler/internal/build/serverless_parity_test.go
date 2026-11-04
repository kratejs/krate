package build

import (
	"net/http"
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
}

// parityHeaders are compared between `krate start` and the node target: the
// security and cache policy, redirects and the render markers.
var parityHeaders = []string{
	"Content-Type", "Cache-Control", "Location", "X-Krate-Cache", "X-Krate-Error", "Allow",
	"X-Content-Type-Options", "X-Frame-Options", "Referrer-Policy", "Permissions-Policy",
	"X-Custom", "X-Mw", "Content-Security-Policy", "Strict-Transport-Security",
}

var timestampRe = regexp.MustCompile(`\d{10,}`)

func normalizeBody(s string) string { return timestampRe.ReplaceAllString(s, "N") }

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
				t.Errorf("body differs\nkrate start: %.600s\nnode target: %.600s", gb, nb)
			}
		})
	}
	_ = deploy.ServerFeatures
}
