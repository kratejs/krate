package build

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/evanw/esbuild/pkg/api"
	"github.com/kratejs/krate/packages/compiler/internal/jsruntime"
)

// corpusDir holds the differential API runtime corpus. See its README.md for
// the spec format.
const corpusDir = "testdata/api_runtime_corpus"

// defaultCompareHeaders are compared when a spec does not list its own.
var defaultCompareHeaders = []string{"content-type", "location", "allow", "x-custom"}

type corpusRequest struct {
	Method     string            `json:"method"`
	Path       string            `json:"path"`
	Query      string            `json:"query"`
	Headers    map[string]string `json:"headers"`
	Body       *string           `json:"body"`
	BodyBase64 *string           `json:"bodyBase64"`
	BodyRepeat int               `json:"bodyRepeat"`
	Status     int               `json:"status"`
}

type corpusCompare struct {
	Status  *bool    `json:"status"`
	Headers []string `json:"headers"`
	Cookies *bool    `json:"cookies"`
	Body    string   `json:"body"`
}

type corpusExpect struct {
	Status     int     `json:"status"`
	Body       *string `json:"body"`
	BodyBase64 *string `json:"bodyBase64"`
}

type corpusDivergence struct {
	Reason      string         `json:"reason"`
	QuickJS     []corpusExpect `json:"quickjs"`
	Node        []corpusExpect `json:"node"`
	NodeVersion string         `json:"nodeVersion"`
}

type corpusSpec struct {
	Description  string            `json:"description"`
	Requests     []corpusRequest   `json:"requests"`
	Compare      corpusCompare     `json:"compare"`
	Divergence   *corpusDivergence `json:"divergence"`
	SkipRuntimes map[string]string `json:"skipRuntimes"`
	Heavy        bool              `json:"heavy"`
}

type corpusCase struct {
	name   string // slash-separated, e.g. "url/relative-resolution"
	module string // absolute path of the .ts route module
	spec   corpusSpec
}

type confResp struct {
	status  int
	headers map[string]string
	cookies []string
	body    []byte
}

func loadCorpus(t *testing.T) []corpusCase {
	t.Helper()
	var cases []corpusCase
	err := filepath.WalkDir(corpusDir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || filepath.Ext(p) != ".json" {
			return err
		}
		raw, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		var c corpusCase
		dec := json.NewDecoder(bytes.NewReader(raw))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&c.spec); err != nil {
			return fmt.Errorf("%s: %w", p, err)
		}
		rel, _ := filepath.Rel(corpusDir, strings.TrimSuffix(p, ".json"))
		c.name = filepath.ToSlash(rel)
		c.module, _ = filepath.Abs(strings.TrimSuffix(p, ".json") + ".ts")
		if _, err := os.Stat(c.module); err != nil {
			return fmt.Errorf("%s: missing route module: %w", p, err)
		}
		if len(c.spec.Requests) == 0 {
			return fmt.Errorf("%s: no requests", p)
		}
		if dv := c.spec.Divergence; dv != nil && (dv.Reason == "" || len(dv.QuickJS) != len(c.spec.Requests)) {
			return fmt.Errorf("%s: a divergence needs a reason and one quickjs entry per request", p)
		}
		cases = append(cases, c)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	sort.Slice(cases, func(i, j int) bool { return cases[i].name < cases[j].name })
	if len(cases) == 0 {
		t.Fatal("empty API runtime corpus")
	}
	return cases
}

func (rq corpusRequest) payload(t *testing.T) []byte {
	var b []byte
	switch {
	case rq.BodyBase64 != nil:
		var err error
		if b, err = base64.StdEncoding.DecodeString(*rq.BodyBase64); err != nil {
			t.Fatalf("bodyBase64: %v", err)
		}
	case rq.Body != nil:
		b = []byte(*rq.Body)
	default:
		return nil
	}
	if rq.BodyRepeat > 1 {
		b = bytes.Repeat(b, rq.BodyRepeat)
	}
	return b
}

func doCorpusRequest(t *testing.T, base, route string, rq corpusRequest) confResp {
	t.Helper()
	method := rq.Method
	if method == "" {
		method = "GET"
	}
	p := "/api/" + route
	if rq.Path != "" {
		p = rq.Path
	}
	var rd io.Reader
	if b := rq.payload(t); b != nil {
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, base+p+rq.Query, rd)
	if err != nil {
		t.Fatal(err)
	}
	req.Host = "blog.example.com"
	for k, v := range rq.Headers {
		req.Header.Set(k, v)
	}
	client := &http.Client{
		Timeout:       60 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		Transport:     &http.Transport{DisableCompression: true},
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, p, err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	out := confResp{status: resp.StatusCode, headers: map[string]string{}, cookies: resp.Header.Values("Set-Cookie"), body: body}
	for k := range resp.Header {
		v := resp.Header.Get(k)
		if strings.EqualFold(k, "Content-Type") {
			// Runtimes differ in charset spelling/casing; compare the type.
			v = strings.TrimSuffix(strings.ReplaceAll(strings.ToLower(v), " ", ""), ";charset=utf-8")
		}
		out.headers[strings.ToLower(k)] = v
	}
	return out
}

func startNodeConformance(t *testing.T, apiDir, middlewarePath string) string {
	t.Helper()
	return startSidecarConformance(t, "node", apiDir, middlewarePath)
}

// startSidecarConformance starts the real API sidecar script on node or bun.
func startSidecarConformance(t *testing.T, runtime, apiDir, middlewarePath string) string {
	t.Helper()
	return startSidecarConformanceEnv(t, runtime, apiDir, middlewarePath)
}

// startSidecarConformanceEnv is startSidecarConformance with extra environment
// (for example KRATE_API_HOT=1).
func startSidecarConformanceEnv(t *testing.T, runtime, apiDir, middlewarePath string, env ...string) string {
	t.Helper()
	if _, err := exec.LookPath(runtime); err != nil {
		if runtime == "node" {
			requireE2E(t, "node is not installed")
		}
		t.Skipf("%s is not installed", runtime)
	}
	dir := t.TempDir()
	port := freePort(t)
	scriptPath := filepath.Join(dir, "sidecar.mjs")
	script := apiServerScriptContent
	args := []string{scriptPath, apiDir, fmt.Sprint(port), middlewarePath, filepath.Join(dir, "actions")}
	if runtime == "bun" {
		script = apiServerBunScriptContent
		args = append([]string{"run"}, args...)
	}
	if err := os.WriteFile(scriptPath, []byte(script), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(runtime, args...)
	cmd.Env = append(append(os.Environ(), "KRATE_API_WORKERS=1"), env...)
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
	addr := fmt.Sprintf("127.0.0.1:%d", port)
	deadline := time.Now().Add(15 * time.Second)
	for {
		c, err := net.DialTimeout("tcp", addr, 200*time.Millisecond)
		if err == nil {
			c.Close()
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("%s sidecar did not start", runtime)
		}
		time.Sleep(50 * time.Millisecond)
	}
	return "http://" + addr
}

// buildCorpus compiles every route module twice: ESM for the sidecars and the
// IIFE bundle shape the real build emits for the embedded runtime.
func buildCorpus(t *testing.T, cases []corpusCase) (esmDir, iifeDir string) {
	t.Helper()
	dir := t.TempDir()
	esmDir, iifeDir = filepath.Join(dir, "api"), filepath.Join(dir, "qjs-api")
	if err := os.MkdirAll(esmDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(esmDir, "package.json"), []byte(`{"type":"module"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	root, _ := filepath.Abs(corpusDir)
	entries := make([]string, len(cases))
	for i, c := range cases {
		entries[i] = c.module
	}
	for _, out := range []struct {
		dir    string
		format api.Format
	}{{esmDir, api.FormatESModule}, {iifeDir, api.FormatIIFE}} {
		opts := api.BuildOptions{
			EntryPoints: entries,
			Outbase:     root,
			Outdir:      out.dir,
			Bundle:      true,
			Platform:    api.PlatformNode,
			Format:      out.format,
			Write:       true,
			LogLevel:    api.LogLevelSilent,
		}
		if out.format == api.FormatIIFE {
			opts.GlobalName = "__krate_route"
		}
		if res := api.Build(opts); len(res.Errors) > 0 {
			e := res.Errors[0]
			loc := ""
			if e.Location != nil {
				loc = fmt.Sprintf("%s:%d: ", e.Location.File, e.Location.Line)
			}
			t.Fatalf("bundling corpus: %s%s", loc, e.Text)
		}
	}
	return esmDir, iifeDir
}

func checkCorpusExpect(t *testing.T, label, who string, want corpusExpect, got confResp) {
	t.Helper()
	if want.Status != 0 && want.Status != got.status {
		t.Errorf("%s: documented %s status %d, got %d", label, who, want.Status, got.status)
	}
	if want.Body != nil && *want.Body != string(got.body) {
		t.Errorf("%s: documented %s body\n want: %s\n got:  %s", label, who, *want.Body, got.body)
	}
	if want.BodyBase64 != nil && *want.BodyBase64 != base64.StdEncoding.EncodeToString(got.body) {
		t.Errorf("%s: documented %s body (base64) differs, got %q", label, who, got.body)
	}
}

// diffCorpus returns the differences between two responses on the parts the
// spec selects.
func diffCorpus(cmp corpusCompare, q, n confResp) []string {
	var out []string
	if cmp.Status == nil || *cmp.Status {
		if q.status != n.status {
			out = append(out, fmt.Sprintf("status quickjs=%d node=%d", q.status, n.status))
		}
	}
	hs := cmp.Headers
	if hs == nil {
		hs = defaultCompareHeaders
	}
	for _, h := range hs {
		h = strings.ToLower(h)
		if q.headers[h] != n.headers[h] {
			out = append(out, fmt.Sprintf("header %s quickjs=%q node=%q", h, q.headers[h], n.headers[h]))
		}
	}
	if cmp.Cookies == nil || *cmp.Cookies {
		if strings.Join(q.cookies, "\n") != strings.Join(n.cookies, "\n") {
			out = append(out, fmt.Sprintf("set-cookie quickjs=%q node=%q", q.cookies, n.cookies))
		}
	}
	switch cmp.Body {
	case "", "bytes":
		if !bytes.Equal(q.body, n.body) {
			out = append(out, fmt.Sprintf("body differs\n quickjs: %s\n node:    %s", clip(q.body), clip(n.body)))
		}
	case "none":
	default:
		out = append(out, "unknown compare.body mode "+cmp.Body)
	}
	return out
}

// dumpCorpusBodies writes both full bodies of a differing response under dir
// (set KRATE_CORPUS_DUMP=dir), since failure messages clip long bodies.
func dumpCorpusBodies(t *testing.T, dir, runtime, name string, i int, q, n confResp) {
	base := filepath.Join(dir, runtime, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(base), 0o755); err != nil {
		t.Logf("dump: %v", err)
		return
	}
	_ = os.WriteFile(fmt.Sprintf("%s.%d.quickjs", base, i), q.body, 0o644)
	_ = os.WriteFile(fmt.Sprintf("%s.%d.%s", base, i, runtime), n.body, 0o644)
	_ = os.WriteFile(fmt.Sprintf("%s.%d.status", base, i), fmt.Appendf(nil, "%d %d\n", q.status, n.status), 0o644)
}

// majorVersion turns "v24.1.0" into "24".
func majorVersion(v string) string {
	v = strings.TrimPrefix(strings.TrimSpace(v), "v")
	if i := strings.IndexByte(v, '.'); i >= 0 {
		v = v[:i]
	}
	return v
}

func clip(b []byte) string {
	if len(b) > 600 {
		return fmt.Sprintf("%q... (%d bytes)", b[:600], len(b))
	}
	return fmt.Sprintf("%q", b)
}

// TestAPIRuntimeConformance runs every route in the differential corpus under
// the embedded QuickJS runtime and under the real Node (and Bun) sidecar and
// requires identical responses on the parts each spec selects. Cases with a
// documented divergence assert the recorded QuickJS output instead.
func TestAPIRuntimeConformance(t *testing.T) {
	cases := loadCorpus(t)
	esmDir, iifeDir := buildCorpus(t, cases)

	apiRT := jsruntime.NewAPIRouteRuntime(iifeDir)
	apiRT.SetEnv(map[string]string{"KRATE_CORPUS_VAR": "corpus-value"})
	// Every request gets a fresh VM that installs only what the runner needs,
	// so each case also exercises loading web API features on first use.
	apiRT.SetPool(jsruntime.DefaultAPIWorkers(), true)
	t.Cleanup(apiRT.Close)
	qjs := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		serveQuickJSAPI(w, r, apiRT, false)
	}))
	t.Cleanup(qjs.Close)
	// Pooled workers of a build without feature lists preload everything;
	// they must answer exactly like the lazily set up VMs.
	eagerRT := jsruntime.NewAPIRouteRuntime(iifeDir)
	eagerRT.SetEnv(map[string]string{"KRATE_CORPUS_VAR": "corpus-value"})
	t.Cleanup(eagerRT.Close)
	eager := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		serveQuickJSAPI(w, r, eagerRT, false)
	}))
	t.Cleanup(eager.Close)
	t.Setenv("KRATE_CORPUS_VAR", "corpus-value")

	// The embedded runtime's answer does not depend on the reference runtime,
	// so each request runs in QuickJS once and is reused for Bun.
	var qmu sync.Mutex
	qcache := map[string]confResp{}
	quickjs := func(t *testing.T, name string, i int, rq corpusRequest) confResp {
		key := fmt.Sprintf("%s#%d", name, i)
		qmu.Lock()
		r, ok := qcache[key]
		qmu.Unlock()
		if !ok {
			r = doCorpusRequest(t, qjs.URL, name, rq)
			if e := doCorpusRequest(t, eager.URL, name, rq); e.status != r.status || !bytes.Equal(e.body, r.body) {
				t.Errorf("%s[%d]: preloaded VM answered %d %s, lazily set up VM %d %s", name, i, e.status, clip(e.body), r.status, clip(r.body))
			}
			qmu.Lock()
			qcache[key] = r
			qmu.Unlock()
		}
		return r
	}

	// Node-side divergence records are only asserted on the Node major they
	// were captured with; other majors may legitimately differ.
	nodeMajor := ""
	if out, err := exec.Command("node", "--version").Output(); err == nil {
		nodeMajor = majorVersion(string(out))
	}

	for _, runtime := range []string{"node", "bun"} {
		t.Run(runtime, func(t *testing.T) {
			ref := startSidecarConformance(t, runtime, esmDir, "")
			for _, c := range cases {
				t.Run(c.name, func(t *testing.T) {
					t.Parallel()
					if c.spec.Heavy && raceEnabled {
						t.Skip("megabyte-sized work in the embedded runtime is too slow under the race detector")
					}
					if why, ok := c.spec.SkipRuntimes[runtime]; ok {
						t.Skipf("%s differs from Node here: %s", runtime, why)
					}
					dv := c.spec.Divergence
					if dv != nil && runtime != "node" {
						t.Skipf("known QuickJS divergence: %s", dv.Reason)
					}
					same := true
					for i, rq := range c.spec.Requests {
						label := fmt.Sprintf("%s[%d] %s", c.name, i, rq.Method)
						q := quickjs(t, c.name, i, rq)
						n := doCorpusRequest(t, ref, c.name, rq)
						// A failing handler on both sides would compare equal, so a
						// 5xx is only accepted when the spec expects that status.
						if rq.Status != 0 && n.status != rq.Status {
							t.Errorf("%s: %s status %d, spec expects %d", label, runtime, n.status, rq.Status)
						}
						if rq.Status == 0 && n.status >= 500 {
							t.Errorf("%s: %s answered %d (%s); fix the case or declare the status", label, runtime, n.status, clip(n.body))
						}
						diffs := diffCorpus(c.spec.Compare, q, n)
						if dump := os.Getenv("KRATE_CORPUS_DUMP"); dump != "" && len(diffs) > 0 {
							dumpCorpusBodies(t, dump, runtime, c.name, i, q, n)
						}
						if dv == nil {
							for _, d := range diffs {
								t.Errorf("%s: %s", label, d)
							}
							continue
						}
						if len(diffs) > 0 {
							same = false
						}
						checkCorpusExpect(t, label, "quickjs", dv.QuickJS[i], q)
						if i < len(dv.Node) && majorVersion(dv.NodeVersion) == nodeMajor {
							checkCorpusExpect(t, label, "node", dv.Node[i], n)
						}
					}
					if dv != nil && same {
						if majorVersion(dv.NodeVersion) == nodeMajor {
							t.Errorf("%s: QuickJS now matches Node; remove the documented divergence (%s)", c.name, dv.Reason)
						} else {
							t.Logf("%s: QuickJS matches this Node version (divergence recorded on %s)", c.name, dv.NodeVersion)
						}
					}
				})
			}
		})
	}
}

// TestNodeSidecarMiddlewareEnvelope runs the same middleware module through the
// embedded runtime and the Node sidecar endpoint and requires the same
// decisions: matcher scoping, pass-through headers with multiple cookies,
// rewrites and short-circuit responses (including an explicit 200).
func TestNodeSidecarMiddlewareEnvelope(t *testing.T) {
	code := `export function middleware(request) {
  const p = new URL(request.url).pathname;
  if (p === "/teapot") return new Response("teapot", { status: 200, headers: { "x-from": "mw" } });
  if (p === "/rewrite") return new Response(null, { headers: { "x-middleware-rewrite": "/other" } });
  const h = new Headers({ "x-middleware-next": "1" });
  h.append("set-cookie", "a=1"); h.append("set-cookie", "b=2");
  return new Response(null, { headers: h });
}
export const config = { matcher: ["/teapot", "/rewrite", "/pass/:path*"] };
`
	dir := t.TempDir()
	mwPath := filepath.Join(dir, "middleware.js")
	if err := os.WriteFile(mwPath, []byte(code), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "package.json"), []byte(`{"type":"module"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	apiDir := filepath.Join(dir, "api")
	_ = os.MkdirAll(apiDir, 0o755)
	base := startNodeConformance(t, apiDir, mwPath)
	rt, err := jsruntime.NewMiddlewareRuntime(mwPath)
	if err != nil {
		t.Fatal(err)
	}
	defer rt.Close()

	type env struct {
		Action  string      `json:"action"`
		Status  int         `json:"status"`
		Headers [][2]string `json:"headers"`
		Body    string      `json:"body"`
		Rewrite string      `json:"rewrite"`
		Matcher bool        `json:"matcher"`
	}
	for _, path := range []string{"/teapot", "/rewrite", "/pass/a/b", "/unmatched", "/styles.css"} {
		payload := fmt.Sprintf(`{"url":"http://x%[1]s","method":"GET","path":%[1]q,"headers":{}}`, path)
		resp, err := http.Post(base+"/__krate/middleware", "application/json", strings.NewReader(payload))
		if err != nil {
			t.Fatal(err)
		}
		raw, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		var n env
		if err := json.Unmarshal(raw, &n); err != nil {
			t.Fatalf("%s: %v (%s)", path, err, raw)
		}
		q := rt.Execute(jsruntime.MiddlewareRequest{URL: "http://x" + path, Method: "GET", Path: path})
		if !n.Matcher {
			t.Errorf("%s: sidecar did not report the matcher", path)
		}
		qAction := q.Action
		if qAction == jsruntime.MiddlewareSkip {
			qAction = "continue"
		}
		if n.Action != qAction {
			t.Errorf("%s: action node=%q quickjs=%q", path, n.Action, qAction)
			continue
		}
		if n.Action == "response" && (n.Status != q.Status || n.Body != string(q.Body)) {
			t.Errorf("%s: response node=%d/%q quickjs=%d/%q", path, n.Status, n.Body, q.Status, q.Body)
		}
		cookies := 0
		for _, h := range n.Headers {
			if h[0] == "set-cookie" {
				cookies++
			}
		}
		qCookies := 0
		for _, h := range q.Headers {
			if h[0] == "set-cookie" {
				qCookies++
			}
		}
		if cookies != qCookies {
			t.Errorf("%s: set-cookie count node=%d quickjs=%d", path, cookies, qCookies)
		}
		if n.Rewrite != q.Rewrite {
			t.Errorf("%s: rewrite node=%q quickjs=%q", path, n.Rewrite, q.Rewrite)
		}
	}
}

// In dev (KRATE_API_HOT=1) the Node and Bun sidecars reload the middleware
// module when its file changes; in production it is loaded once.
func TestSidecarMiddlewareHotReload(t *testing.T) {
	for _, runtime := range []string{"node", "bun"} {
		for _, hot := range []bool{true, false} {
			name := runtime + "/prod"
			if hot {
				name = runtime + "/dev"
			}
			t.Run(name, func(t *testing.T) {
				mk := func(v string) string {
					return `export function middleware(request) {
  return new Response(null, { headers: { "x-middleware-next": "1", "x-version": "` + v + `" } });
}
`
				}
				dir := t.TempDir()
				mwPath := filepath.Join(dir, "middleware.js")
				if err := os.WriteFile(mwPath, []byte(mk("one")), 0o644); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(dir, "package.json"), []byte(`{"type":"module"}`), 0o644); err != nil {
					t.Fatal(err)
				}
				apiDir := filepath.Join(dir, "api")
				_ = os.MkdirAll(apiDir, 0o755)
				var env []string
				if hot {
					env = append(env, "KRATE_API_HOT=1")
				}
				base := startSidecarConformanceEnv(t, runtime, apiDir, mwPath, env...)
				version := func() string {
					resp, err := http.Post(base+"/__krate/middleware", "application/json", strings.NewReader(`{"url":"http://x/a","method":"GET","path":"/a","headers":{}}`))
					if err != nil {
						t.Fatal(err)
					}
					defer resp.Body.Close()
					var out struct {
						Headers [][2]string `json:"headers"`
					}
					raw, _ := io.ReadAll(resp.Body)
					if err := json.Unmarshal(raw, &out); err != nil {
						t.Fatalf("%v (%s)", err, raw)
					}
					for _, h := range out.Headers {
						if h[0] == "x-version" {
							return h[1]
						}
					}
					return ""
				}
				if got := version(); got != "one" {
					t.Fatalf("initial x-version = %q", got)
				}
				if err := os.WriteFile(mwPath, []byte(mk("two-changed")), 0o644); err != nil {
					t.Fatal(err)
				}
				want := "one"
				if hot {
					want = "two-changed"
				}
				if got := version(); got != want {
					t.Errorf("after edit x-version = %q, want %q", got, want)
				}
			})
		}
	}
}
