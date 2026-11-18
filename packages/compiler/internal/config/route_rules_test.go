package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeConfig(t *testing.T, src string) string {
	t.Helper()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "krate.config.ts"), []byte(src), 0644); err != nil {
		t.Fatal(err)
	}
	return root
}

func TestLoadRouteRules(t *testing.T) {
	root := writeConfig(t, `export default {
  redirects: [
    { source: "/store", destination: "/templates", statusCode: 308, has: [{ type: "query", key: "vehicle" }] },
    { source: "/store/:slug((?!compare$)[^/]+)", destination: "/templates/:slug", permanent: true },
  ],
  rewrites: [{ source: "/a", destination: "/b", missing: [{ type: "cookie", key: "x" }] }],
  headers: [{ source: "/:path*", headers: { "X-A": "1" }, has: [{ type: "host", value: "example\\.com" }] }],
};`)
	Warnings = nil
	cfg, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	rd := cfg.Redirects[0]
	if rd.StatusCode != 308 || len(rd.Has) != 1 || rd.Has[0].Key != "vehicle" || rd.Status() != 308 {
		t.Errorf("redirect 0 = %+v", rd)
	}
	if cfg.Redirects[1].Status() != 301 {
		t.Errorf("permanent redirect status = %d", cfg.Redirects[1].Status())
	}
	if len(cfg.Rewrites[0].Missing) != 1 || cfg.Rewrites[0].Missing[0].Type != "cookie" {
		t.Errorf("rewrite = %+v", cfg.Rewrites[0])
	}
	if len(cfg.Headers[0].Has) != 1 || cfg.Headers[0].Has[0].Type != "host" {
		t.Errorf("header rule = %+v", cfg.Headers[0])
	}
	for _, w := range Warnings {
		if strings.Contains(w, "unknown config key") {
			t.Errorf("route rule keys reported as unknown: %s", w)
		}
	}
}

func TestLoadRejectsUnrunnableRouteRegex(t *testing.T) {
	tests := []struct{ name, rule, want string }{
		{"redirect lookbehind", `redirects: [{ source: "/a/:x((?<=b)c)", destination: "/" }]`, "redirects[0]"},
		{"rewrite backreference", `rewrites: [{ source: "/a/:x(([ab])\\1)", destination: "/" }]`, "rewrites[0]"},
		{"header regex", `headers: [{ source: "/a/:x([^/]+(?!b))", headers: { A: "1" } }]`, "headers[0]"},
		{"has value", `redirects: [{ source: "/a", destination: "/", has: [{ type: "query", key: "k", value: "(?<=a)b" }] }]`, "has[0]"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := writeConfig(t, "export default { "+tt.rule+" };")
			_, err := Load(root)
			if err == nil {
				t.Fatal("want a load error, not a rule that never matches")
			}
			if !strings.Contains(err.Error(), "KR8355") || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error %q should carry KR8355 and %q", err, tt.want)
			}
		})
	}
}

func TestValidateRouteRuleErrors(t *testing.T) {
	c := Default()
	c.Redirects = []Redirect{{Source: "/a", Destination: "/b", StatusCode: 404}}
	if _, err := c.Validate(); err == nil || !strings.Contains(err.Error(), "statusCode") {
		t.Errorf("bad statusCode: %v", err)
	}
	c = Default()
	c.Redirects = []Redirect{{Source: "/a", Destination: "/b", Has: []RouteCondition{{Type: "body", Key: "k"}}}}
	if _, err := c.Validate(); err == nil || !strings.Contains(err.Error(), "type must be") {
		t.Errorf("bad condition type: %v", err)
	}
	c = Default()
	c.Redirects = []Redirect{{Source: "/a", Destination: "/b", Has: []RouteCondition{{Type: "query"}}}}
	if _, err := c.Validate(); err == nil || !strings.Contains(err.Error(), "needs a key") {
		t.Errorf("missing key: %v", err)
	}
	c = Default()
	c.Redirects = []Redirect{{Source: "/old/*", Destination: "/new/:splat"}, {Source: "/u/:id", Destination: "/x/:id", StatusCode: 307}}
	c.Headers = []HeaderRule{{Source: "/assets/*", Headers: map[string]string{"A": "1"}}}
	if _, err := c.Validate(); err != nil {
		t.Errorf("legacy forms must stay valid: %v", err)
	}
}

func TestLoadHeadersNextArrayShape(t *testing.T) {
	root := writeConfig(t, `const hs = [
  { key: "X-A", value: "1" },
  { key: "Cache-Control", value: "no-store" },
  { key: "x-a", value: "2" },
];
export default {
  headers: [
    { source: "/:path*", headers: hs },
    { source: "/obj", headers: { "X-B": "3" } },
  ],
};`)
	Warnings = nil
	cfg, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	got := cfg.Headers[0].Headers
	if len(got) != 2 || got["x-a"] != "2" || got["Cache-Control"] != "no-store" {
		t.Errorf("array headers = %v, want last x-a to win", got)
	}
	if cfg.Headers[1].Headers["X-B"] != "3" {
		t.Errorf("object headers = %v", cfg.Headers[1].Headers)
	}
}

func TestHeaderRuleRejectsBadHeadersShape(t *testing.T) {
	var r HeaderRule
	if err := r.UnmarshalJSON([]byte(`{"source":"/","headers":"x"}`)); err == nil {
		t.Fatal("string headers accepted")
	}
}
