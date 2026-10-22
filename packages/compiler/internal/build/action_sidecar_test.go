package build

import (
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
)

const sidecarTestActions = `
export async function echo(...args) {
  return { args: args.map((a) => (a instanceof FormData ? Object.fromEntries(a) : a)) };
}
export async function step(prev, fd) { return prev + Number(fd.get('n')); }
export async function boom() { throw new Error('secret detail'); }
export async function go() { const e = new Error('r'); e.__krateRedirect = '/after'; throw e; }
export async function back() { return { redirect: '/r' }; }
export async function ctx() {
  const c = globalThis[Symbol.for('krate.action.store')].getStore();
  c.cookies.set('seen', 'yes');
  return { header: c.headers.get('x-test'), sid: c.cookies.get('sid'), id: c.actionId };
}
`

// startActionSidecar runs the real sidecar script on the given runtime with a
// manifest of test actions, fronted by the Go action handler.
func startActionSidecar(t *testing.T, runtime string, hot bool) *httptest.Server {
	t.Helper()
	if _, err := exec.LookPath(runtime); err != nil {
		if runtime == "node" {
			requireE2E(t, "node is not installed")
		}
		t.Skipf("%s is not installed", runtime)
	}
	dir := t.TempDir()
	actionDir := filepath.Join(dir, "actions")
	if err := os.MkdirAll(actionDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(actionDir, "test.mjs"), []byte(sidecarTestActions), 0644); err != nil {
		t.Fatal(err)
	}
	var entries []actionEntry
	for _, name := range []string{"echo", "step", "boom", "go", "back", "ctx"} {
		entries = append(entries, actionEntry{ID: "a" + name, Export: name, File: "test.mjs", Source: "src/actions/test.ts"})
	}
	manifest, _ := json.Marshal(actionManifest{Actions: entries})
	if err := os.WriteFile(filepath.Join(actionDir, "manifest.json"), manifest, 0644); err != nil {
		t.Fatal(err)
	}
	script := apiServerScriptContent
	if runtime == "bun" {
		script = apiServerBunScriptContent
	}
	scriptPath := filepath.Join(dir, "sidecar.mjs")
	if err := os.WriteFile(scriptPath, []byte(script), 0644); err != nil {
		t.Fatal(err)
	}
	port := freePort(t)
	args := []string{scriptPath, filepath.Join(dir, "api"), fmt.Sprint(port), "", actionDir}
	if runtime == "bun" {
		args = append([]string{"run"}, args...)
	}
	cmd := exec.Command(runtime, args...)
	cmd.Env = append(os.Environ(), fmt.Sprintf("KRATE_API_HOT=%d", boolToInt(hot)))
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_, _ = cmd.Process.Wait()
	})
	target, _ := url.Parse(fmt.Sprintf("http://127.0.0.1:%d", port))
	deadline := time.Now().Add(15 * time.Second)
	for {
		c, err := net.DialTimeout("tcp", target.Host, 200*time.Millisecond)
		if err == nil {
			c.Close()
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("%s sidecar did not start", runtime)
		}
		time.Sleep(50 * time.Millisecond)
	}
	srv := httptest.NewServer(actionHandler(httputil.NewSingleHostReverseProxy(target), 1024))
	t.Cleanup(srv.Close)
	return srv
}

type sidecarResult struct {
	status int
	header http.Header
	body   map[string]any
	raw    string
}

func doAction(t *testing.T, req *http.Request) sidecarResult {
	t.Helper()
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	res, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	data, _ := io.ReadAll(res.Body)
	out := sidecarResult{status: res.StatusCode, header: res.Header, raw: string(data)}
	_ = json.Unmarshal(data, &out.body)
	return out
}

func actionRequest(t *testing.T, srv *httptest.Server, path, contentType, body string, fetch bool) *http.Request {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, srv.URL+path, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	if fetch {
		req.Header.Set("X-Krate-Action", "fetch")
		req.Header.Set("Accept", "application/json")
	}
	return req
}

func TestActionSidecarTransport(t *testing.T) {
	for _, runtime := range []string{"node", "bun"} {
		t.Run(runtime, func(t *testing.T) {
			srv := startActionSidecar(t, runtime, false)

			// JSON envelope.
			r := doAction(t, actionRequest(t, srv, "/__krate/action/aecho", "application/json", `{"args":[1,"a",{"k":true}]}`, true))
			if r.status != 200 || r.body["ok"] != true {
				t.Fatalf("echo: %d %s", r.status, r.raw)
			}
			if got, _ := json.Marshal(r.body["result"]); string(got) != `{"args":[1,"a",{"k":true}]}` {
				t.Fatalf("echo result: %s", got)
			}
			if r.header.Get("Cache-Control") != "no-store" {
				t.Fatalf("missing no-store: %v", r.header)
			}

			// prevState round trip: multipart with leading JSON args.
			body := "--b\r\nContent-Disposition: form-data; name=\"n\"\r\n\r\n2\r\n" +
				"--b\r\nContent-Disposition: form-data; name=\"__krate_args\"\r\n\r\n[5]\r\n--b--\r\n"
			r = doAction(t, actionRequest(t, srv, "/__krate/action/astep", "multipart/form-data; boundary=b", body, true))
			if r.status != 200 || r.body["result"] != float64(7) {
				t.Fatalf("step: %d %s", r.status, r.raw)
			}

			// Native (no-JS) post: leading args and permalink from the query.
			r = doAction(t, actionRequest(t, srv, "/__krate/action/astep?__krate_args=%5B1%5D&__krate_return=%2Ftodos", "application/x-www-form-urlencoded", "n=3", false))
			if r.status != http.StatusSeeOther || r.header.Get("Location") != "/todos" {
				t.Fatalf("no-JS step: %d %v %s", r.status, r.header, r.raw)
			}
			req := actionRequest(t, srv, "/__krate/action/aecho?__krate_return=https%3A%2F%2Fevil.example%2F", "application/x-www-form-urlencoded", "x=1", false)
			req.Header.Set("Referer", srv.URL+"/form/?q=1")
			r = doAction(t, req)
			if r.status != http.StatusSeeOther || r.header.Get("Location") != "/form/?q=1" {
				t.Fatalf("no-JS return must ignore off-site permalinks: %d %v", r.status, r.header)
			}

			// Structured error with a digest and no server detail in production.
			r = doAction(t, actionRequest(t, srv, "/__krate/action/aboom", "application/json", `{"args":[]}`, true))
			errObj, _ := r.body["error"].(map[string]any)
			if r.status != 500 || r.body["ok"] != false || errObj["message"] != "Server action failed" {
				t.Fatalf("boom: %d %s", r.status, r.raw)
			}
			if d, _ := errObj["digest"].(string); !regexp.MustCompile(`^[0-9a-f]{12}$`).MatchString(d) {
				t.Fatalf("digest: %v", errObj["digest"])
			}
			if strings.Contains(r.raw, "secret detail") {
				t.Fatalf("server error detail leaked: %s", r.raw)
			}
			r = doAction(t, actionRequest(t, srv, "/__krate/action/aboom", "application/x-www-form-urlencoded", "", false))
			if r.status != 500 || strings.Contains(r.raw, "secret") {
				t.Fatalf("no-JS boom: %d %s", r.status, r.raw)
			}

			// Redirects: thrown helper and returned object.
			r = doAction(t, actionRequest(t, srv, "/__krate/action/ago", "application/json", `{"args":[]}`, true))
			if r.body["redirect"] != "/after" || r.body["ok"] != true {
				t.Fatalf("thrown redirect: %s", r.raw)
			}
			r = doAction(t, actionRequest(t, srv, "/__krate/action/aback", "application/x-www-form-urlencoded", "", false))
			if r.status != http.StatusSeeOther || r.header.Get("Location") != "/r" {
				t.Fatalf("returned redirect: %d %v", r.status, r.header)
			}

			// Request context: headers, cookies, Set-Cookie.
			req = actionRequest(t, srv, "/__krate/action/actx", "application/json", `{"args":[]}`, true)
			req.Header.Set("X-Test", "hi")
			req.Header.Set("Cookie", "sid=abc; other=1")
			r = doAction(t, req)
			res, _ := r.body["result"].(map[string]any)
			if res["header"] != "hi" || res["sid"] != "abc" || res["id"] != "actx" {
				t.Fatalf("context: %s", r.raw)
			}
			if sc := r.header.Get("Set-Cookie"); !strings.Contains(sc, "seen=yes") || !strings.Contains(sc, "HttpOnly") || !strings.Contains(sc, "SameSite=Lax") {
				t.Fatalf("set-cookie: %q", sc)
			}

			// Unknown action and malformed leading args.
			r = doAction(t, actionRequest(t, srv, "/__krate/action/nope", "application/json", `{"args":[]}`, true))
			if r.status != 404 || r.body["ok"] != false {
				t.Fatalf("unknown: %d %s", r.status, r.raw)
			}
			r = doAction(t, actionRequest(t, srv, "/__krate/action/astep?__krate_args=%7Bbad", "application/x-www-form-urlencoded", "n=1", true))
			if r.status != 400 {
				t.Fatalf("bad args: %d %s", r.status, r.raw)
			}
		})
	}
}

func TestActionSidecarDevErrorMessage(t *testing.T) {
	srv := startActionSidecar(t, "node", true)
	r := doAction(t, actionRequest(t, srv, "/__krate/action/aboom", "application/json", `{"args":[]}`, true))
	errObj, _ := r.body["error"].(map[string]any)
	if errObj["message"] != "secret detail" {
		t.Fatalf("dev mode should expose the error message: %s", r.raw)
	}
}

func TestActionHandlerSecurity(t *testing.T) {
	reached := 0
	var lastReturn string
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reached++
		lastReturn = r.Header.Get("X-Krate-Return")
		_, _ = io.Copy(io.Discard, r.Body)
		w.WriteHeader(204)
	})
	srv := httptest.NewServer(actionHandler(next, 16))
	defer srv.Close()

	check := func(name string, req *http.Request, status int, jsonBody bool) {
		t.Helper()
		r := doAction(t, req)
		if r.status != status {
			t.Fatalf("%s: status %d, want %d (%s)", name, r.status, status, r.raw)
		}
		if r.header.Get("Cache-Control") != "no-store" {
			t.Fatalf("%s: missing no-store", name)
		}
		if jsonBody && r.body["ok"] != false {
			t.Fatalf("%s: want JSON error envelope, got %s", name, r.raw)
		}
	}

	get, _ := http.NewRequest(http.MethodGet, srv.URL+"/__krate/action/a1", nil)
	get.Header.Set("X-Krate-Action", "fetch")
	check("GET", get, http.StatusMethodNotAllowed, true)

	cross := actionRequest(t, srv, "/__krate/action/a1", "application/json", `{}`, true)
	cross.Header.Set("Origin", "https://evil.example")
	check("cross-origin", cross, http.StatusForbidden, true)

	site := actionRequest(t, srv, "/__krate/action/a1", "application/x-www-form-urlencoded", `a=1`, false)
	site.Header.Set("Sec-Fetch-Site", "cross-site")
	check("cross-site form", site, http.StatusForbidden, false)

	big := actionRequest(t, srv, "/__krate/action/a1", "application/json", strings.Repeat("x", 64), true)
	check("oversized", big, http.StatusRequestEntityTooLarge, true)

	if reached != 0 {
		t.Fatalf("rejected requests reached the sidecar %d times", reached)
	}

	ok := actionRequest(t, srv, "/__krate/action/a1?__krate_return=%2Fdone", "application/x-www-form-urlencoded", `a=1`, false)
	ok.Header.Set("Origin", srv.URL)
	check("same-origin", ok, http.StatusNoContent, false)
	if lastReturn != "/done" {
		t.Fatalf("permalink return = %q", lastReturn)
	}
	spoof := actionRequest(t, srv, "/__krate/action/a1", "application/json", `{}`, true)
	spoof.Header.Set("X-Krate-Return", "//evil.example/x")
	check("spoofed return", spoof, http.StatusNoContent, false)
	if lastReturn != "" {
		t.Fatalf("unsafe return header must be dropped, got %q", lastReturn)
	}
}
