package build

import (
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestServeTrailingSlashModes drives krate serve with each trailingSlash mode:
// static pages and request-time pages follow the same URL form, and the
// not-found page's own URLs answer 404 in every mode.
func TestServeTrailingSlashModes(t *testing.T) {
	cases := []struct {
		mode string
		want map[string][2]interface{}
	}{
		{"always", map[string][2]interface{}{
			"/about": {301, "about/"}, "/about/": {200, ""}, "/history": {301, "./history/"}, "/history/": {200, ""},
		}},
		{"never", map[string][2]interface{}{
			"/about": {200, ""}, "/about/": {301, "../about"}, "/history": {200, ""}, "/history/": {301, "../history"},
		}},
		{"ignore", map[string][2]interface{}{
			"/about": {200, ""}, "/about/": {200, ""}, "/history": {200, ""}, "/history/": {200, ""},
		}},
	}
	for _, c := range cases {
		t.Run(c.mode, func(t *testing.T) {
			e2eEnv(t)
			b, root := scProject(t, map[string]string{
				"src/pages/index.tsx":   `export default function Home() { return <h1>home</h1>; }`,
				"src/pages/about.tsx":   `export default function About() { return <h1>about-page</h1>; }`,
				"src/pages/404.tsx":     `export default function NF() { return <h1 id="p-root404">ROOT-404</h1>; }`,
				"src/pages/history.tsx": "export const config = { ssr: true };\nexport default function H() { return <h1>history-page</h1>; }",
			})
			b.Cfg.TrailingSlash = c.mode
			if err := linkRuntimePackage(t, filepath.Join(root, "node_modules", "@krate")); err != nil {
				t.Fatal(err)
			}
			if err := b.BuildAll(); err != nil {
				t.Fatalf("BuildAll: %v", err)
			}
			base, err := startKrateServer(t, root, b.Cfg)
			if err != nil {
				t.Fatalf("serve: %v", err)
			}
			client := &http.Client{Timeout: 20 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
			get := func(path string) (*http.Response, string) {
				t.Helper()
				req, _ := http.NewRequest("GET", base+path, nil)
				req.Header.Set("Accept", "text/html")
				resp, err := client.Do(req)
				if err != nil {
					t.Fatalf("GET %s: %v", path, err)
				}
				defer resp.Body.Close()
				data, _ := io.ReadAll(resp.Body)
				return resp, string(data)
			}
			for p, w := range c.want {
				resp, body := get(p)
				if resp.StatusCode != w[0].(int) || resp.Header.Get("Location") != w[1].(string) {
					t.Errorf("GET %s: %d %q, want %d %q", p, resp.StatusCode, resp.Header.Get("Location"), w[0], w[1])
				}
				if w[0].(int) == 200 && !strings.Contains(body, strings.TrimPrefix(strings.TrimSuffix(p, "/"), "/")+"-page") {
					t.Errorf("GET %s: wrong body %.200s", p, body)
				}
			}
			for _, p := range []string{"/404", "/404/", "/404.html"} {
				resp, body := get(p)
				if resp.StatusCode != 404 || !strings.Contains(body, "p-root404") {
					t.Errorf("GET %s: %d, want the 404 page with status 404 (%.120s)", p, resp.StatusCode, body)
				}
			}
		})
	}
}
