package build

import (
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// A route handler runs in the action store: cookies it sets reach the response
// and a thrown redirect signal (what @krate/auth signIn() throws) becomes a 307.
const routeScopeHandler = `
const store = () => globalThis[Symbol.for('krate.action.store')].getStore();
export async function GET(request) {
  const c = store();
  c.cookies.set('flow', c.cookies.get('sid') + '!', { maxAge: 900 });
  const e = new Error('r');
  e.__krateRedirect = 'https://provider.example/authorize?state=1';
  e.__krateRedirectExternal = true;
  throw e;
}
export async function POST(request) {
  const c = store();
  c.cookies.set('plain', 'yes');
  return Response.json({ method: c.method, host: c.headers.get('host') !== null });
}
`

func TestAPISidecarRouteHandlerScope(t *testing.T) {
	t.Parallel()
	for _, runtime := range []string{"node", "bun"} {
		t.Run(runtime, func(t *testing.T) {
			if _, err := exec.LookPath(runtime); err != nil {
				if runtime == "node" {
					requireE2E(t, "node is not installed")
				}
				t.Skipf("%s is not installed", runtime)
			}
			dir := t.TempDir()
			apiDir := filepath.Join(dir, "api")
			if err := os.MkdirAll(apiDir, 0755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dir, "package.json"), []byte(`{"type":"module"}`), 0644); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(apiDir, "scope.js"), []byte(routeScopeHandler), 0644); err != nil {
				t.Fatal(err)
			}
			script, args := apiServerScriptContent, []string{}
			if runtime == "bun" {
				script, args = apiServerBunScriptContent, append(args, "run")
			}
			scriptPath := filepath.Join(dir, "sidecar.mjs")
			if err := os.WriteFile(scriptPath, []byte(script), 0644); err != nil {
				t.Fatal(err)
			}
			port := freePort(t)
			args = append(args, scriptPath, apiDir, fmt.Sprint(port), "", filepath.Join(dir, "actions"))
			cmd := exec.Command(runtime, args...)
			var logs syncBuffer
			cmd.Stdout, cmd.Stderr = &logs, &logs
			if err := cmd.Start(); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				_ = cmd.Process.Kill()
				_, _ = cmd.Process.Wait()
				if t.Failed() {
					t.Logf("%s sidecar output:\n%s", runtime, logs.String())
				}
			})

			client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
			url := fmt.Sprintf("http://127.0.0.1:%d/api/scope", port)
			var res *http.Response
			deadline := time.Now().Add(15 * time.Second)
			for {
				req, _ := http.NewRequest("GET", url, nil)
				req.Header.Set("Cookie", "sid=abc")
				var err error
				if res, err = client.Do(req); err == nil {
					break
				}
				if time.Now().After(deadline) {
					t.Fatalf("%s sidecar did not answer: %v", runtime, err)
				}
				time.Sleep(50 * time.Millisecond)
			}
			res.Body.Close()
			if res.StatusCode != 307 || res.Header.Get("Location") != "https://provider.example/authorize?state=1" {
				t.Fatalf("thrown redirect: %d %v", res.StatusCode, res.Header)
			}
			if sc := strings.Join(res.Header.Values("Set-Cookie"), "\n"); !strings.Contains(sc, "flow=abc!") || !strings.Contains(sc, "Max-Age=900") {
				t.Fatalf("cookies set before the redirect must reach the response: %q", sc)
			}

			post, _ := http.NewRequest("POST", url, strings.NewReader("{}"))
			res2, err := client.Do(post)
			if err != nil {
				t.Fatal(err)
			}
			res2.Body.Close()
			if res2.StatusCode != 200 || !strings.Contains(strings.Join(res2.Header.Values("Set-Cookie"), "\n"), "plain=yes") {
				t.Fatalf("returned response must carry the cookie: %d %v", res2.StatusCode, res2.Header)
			}
		})
	}
}
