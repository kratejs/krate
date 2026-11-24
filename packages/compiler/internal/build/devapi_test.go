package build

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/kratejs/krate/packages/compiler/internal/config"
)

// devAPIProject is a small app built in dev mode: a layout, a static page,
// a dynamic route expanded by generateStaticParams and a client component.
var devAPIProject = map[string]string{
	"src/pages/_layout.tsx": `export default function Layout({ children }: any) {
  return <main class="shell">{children}</main>;
}
`,
	"src/pages/index.tsx": `import Counter from "../components/Counter";
export default function Home() {
  return <section><h1>Home</h1><Counter /></section>;
}
`,
	"src/components/Counter.tsx": `import { createSignal } from "@krate/runtime";
export default function Counter() {
  const [count, setCount] = createSignal(0);
  return <button onClick={() => setCount(count() + 1)}>Count {count()}</button>;
}
`,
	"src/pages/about.tsx": `export default function About() { return <p>About</p>; }
`,
	"src/pages/blog/[slug].tsx": `export default function Post({ params }: { params: { slug: string } }) {
  return <article>Post {params.slug}</article>;
}
export function generateStaticParams() {
  return [{ slug: "first" }, { slug: "second" }];
}
`,
	"src/lib/handlers.tsx": `export function Button() {
  return <button onclick={() => 1}>x</button>;
}
`,
}

func writeProject(t *testing.T, root string, files map[string]string) {
	t.Helper()
	for rel, content := range files {
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// buildDevAPIProject builds devAPIProject in dev mode and returns a devAPI
// for it and its http handler.
func buildDevAPIProject(t *testing.T, mutate func(*config.Config)) (*devAPI, http.Handler) {
	t.Helper()
	root := t.TempDir()
	writeProject(t, root, devAPIProject)
	linkRuntimeDist(t, root)
	cfg := config.Default()
	cfg.PagesDir = filepath.Join(root, "src", "pages")
	UseDevOutDir(root, cfg)
	cfg.PublicDir = filepath.Join(root, "public")
	if mutate != nil {
		mutate(cfg)
	}
	b := New(root, cfg)
	b.DevMode = true
	if err := b.BuildAll(); err != nil {
		t.Fatalf("BuildAll: %v", err)
	}
	d := &devAPI{root: root, cfg: cfg, outDir: cfg.OutDir, hub: NewDevHub(), startedAt: time.UnixMilli(1700000000000)}
	mux := http.NewServeMux()
	d.register(func(pattern string, h http.HandlerFunc) { mux.Handle(pattern, devEndpointGuard("127.0.0.1", h)) })
	return d, mux
}

func devDo(t *testing.T, h http.Handler, method, target, body string) *httptest.ResponseRecorder {
	t.Helper()
	var rd io.Reader
	if body != "" {
		rd = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, "http://127.0.0.1"+target, rd)
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func decodeJSON(t *testing.T, rec *httptest.ResponseRecorder, v any) {
	t.Helper()
	if got := rec.Header().Get("Cache-Control"); got != "no-store" {
		t.Errorf("Cache-Control = %q, want no-store", got)
	}
	if err := json.Unmarshal(rec.Body.Bytes(), v); err != nil {
		t.Fatalf("decoding %s: %v", rec.Body.String(), err)
	}
}

func TestDevAPIInfo(t *testing.T) {
	t.Parallel()
	_, h := buildDevAPIProject(t, func(c *config.Config) { c.DevServer.Editor = "code -r" })
	rec := devDo(t, h, "GET", "/__krate/dev/info", "")
	if rec.Code != 200 {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	var info struct {
		Version   string `json:"version"`
		StartedAt int64  `json:"startedAt"`
		DocsURL   string `json:"docsURL"`
		Editor    struct {
			Configured string      `json:"configured"`
			Available  []devEditor `json:"available"`
		} `json:"editor"`
	}
	decodeJSON(t, rec, &info)
	if info.Version == "" || info.StartedAt != 1700000000000 || info.DocsURL != devDocsURL {
		t.Errorf("info = %+v", info)
	}
	if info.Editor.Configured != "code -r" || info.Editor.Available == nil {
		t.Errorf("editor = %+v", info.Editor)
	}
	if rec := devDo(t, h, "POST", "/__krate/dev/info", "{}"); rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("POST /info = %d, want 405", rec.Code)
	}
}

func TestDevAPIRoute(t *testing.T) {
	t.Parallel()
	d, h := buildDevAPIProject(t, func(c *config.Config) {
		c.Checks = map[string]any{"budget": map[string]any{"js": 30, "route": 100}}
	})
	rec := devDo(t, h, "GET", "/__krate/dev/route?path=/blog/first/", "")
	if rec.Code != 200 {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	var info DevRouteInfo
	decodeJSON(t, rec, &info)
	if info.Pattern != "/blog/[slug]" || info.Source != "src/pages/blog/[slug].tsx" || info.Mode != "ssg" {
		t.Errorf("route = %+v", info)
	}
	if !info.Dynamic || info.Params["slug"] != "first" || info.StaticPaths != 2 {
		t.Errorf("params/dynamic/staticPaths = %v %v %d", info.Params, info.Dynamic, info.StaticPaths)
	}
	if len(info.Layouts) != 1 || info.Layouts[0] != "src/pages/_layout.tsx" {
		t.Errorf("layouts = %v", info.Layouts)
	}
	if info.Sizes == nil || info.Sizes.HTML == 0 || info.Sizes.GzipHTML == 0 || info.BuiltAt == 0 {
		t.Errorf("sizes = %+v builtAt %d", info.Sizes, info.BuiltAt)
	}
	if info.Budget == nil || info.Budget.JS != 30*1024 || info.Budget.Route != 100*1024 || info.Budget.Hydration != 0 {
		t.Errorf("budget = %+v", info.Budget)
	}

	// The home page loads the runtime for its client component.
	rec = devDo(t, h, "GET", "/__krate/dev/route?path=/", "")
	info = DevRouteInfo{}
	decodeJSON(t, rec, &info)
	if info.Pattern != "/" || info.Sizes == nil || info.Sizes.GzipJS == 0 || info.Sizes.GzipRuntime == 0 || info.Sizes.GzipRuntime > info.Sizes.GzipJS {
		t.Errorf("home sizes = %+v", info.Sizes)
	}
	if info.Dynamic || len(info.Params) != 0 || info.StaticPaths != 0 {
		t.Errorf("static route = %+v", info)
	}

	// A path no page serves.
	if rec := devDo(t, h, "GET", "/__krate/dev/route?path=/nope/x", ""); rec.Code != 404 {
		t.Errorf("unknown route = %d, want 404", rec.Code)
	}
	// No build yet.
	empty := &devAPI{root: d.root, cfg: d.cfg, outDir: t.TempDir(), hub: NewDevHub()}
	mux := http.NewServeMux()
	empty.register(func(p string, h http.HandlerFunc) { mux.Handle(p, h) })
	if rec := devDo(t, mux, "GET", "/__krate/dev/route?path=/", ""); rec.Code != 404 {
		t.Errorf("route without a build = %d, want 404", rec.Code)
	}
}

func TestDevAPIRoutes(t *testing.T) {
	t.Parallel()
	d, h := buildDevAPIProject(t, nil)
	writeProject(t, d.root, map[string]string{
		"src/api/hello.ts":        "export function GET() { return new Response('hi'); }\n",
		"src/api/users/[id].ts":   "export function GET() { return new Response('u'); }\n",
		"src/api/_private.ts":     "export const x = 1;\n",
		".krate/go-routes.ignore": "",
	})
	rec := devDo(t, h, "GET", "/__krate/dev/routes", "")
	if rec.Code != 200 {
		t.Fatalf("status %d", rec.Code)
	}
	var routes []DevRouteEntry
	decodeJSON(t, rec, &routes)
	got := map[string]DevRouteEntry{}
	var order []string
	for _, r := range routes {
		got[r.Pattern] = r
		order = append(order, r.Kind+" "+r.Pattern)
	}
	blog := got["/blog/[slug]"]
	if blog.Kind != "page" || strings.Join(blog.Paths, ",") != "/blog/first,/blog/second" || blog.Mode != "ssg" {
		t.Errorf("blog = %+v", blog)
	}
	if about := got["/about"]; strings.Join(about.Paths, ",") != "/about" {
		t.Errorf("about = %+v", about)
	}
	if hello := got["/api/hello"]; hello.Kind != "api" || hello.Source != "src/api/hello.ts" {
		t.Errorf("api hello = %+v", hello)
	}
	if user := got["/api/users/[id]"]; user.Kind != "api" || len(user.Paths) != 0 {
		t.Errorf("api users = %+v", user)
	}
	if _, ok := got["/api/_private"]; ok {
		t.Error("private API files are not routes")
	}
	want := "page /,page /about,page /blog/[slug],api /api/hello,api /api/users/[id]"
	if strings.Join(order, ",") != want {
		t.Errorf("order = %v, want %s", order, want)
	}
}

func TestDevAPIHTMLAndSource(t *testing.T) {
	t.Parallel()
	_, h := buildDevAPIProject(t, nil)
	rec := devDo(t, h, "GET", "/__krate/dev/html?path=/about", "")
	if rec.Code != 200 || !strings.HasPrefix(rec.Header().Get("Content-Type"), "text/plain") || !strings.Contains(rec.Body.String(), ">About</p>") {
		t.Errorf("html = %d %q", rec.Code, rec.Header().Get("Content-Type"))
	}
	if rec := devDo(t, h, "GET", "/__krate/dev/html?path=/../../etc/passwd", ""); rec.Code != 404 {
		t.Errorf("html outside = %d", rec.Code)
	}
	if rec := devDo(t, h, "GET", "/__krate/dev/html?path=/missing", ""); rec.Code != 404 {
		t.Errorf("html missing = %d", rec.Code)
	}

	rec = devDo(t, h, "GET", "/__krate/dev/source?file=src/pages/blog/[slug].tsx&line=2&context=1", "")
	if rec.Code != 200 {
		t.Fatalf("source = %d %s", rec.Code, rec.Body)
	}
	var frame DevSourceFrame
	decodeJSON(t, rec, &frame)
	if frame.File != "src/pages/blog/[slug].tsx" || frame.Lang != "tsx" || frame.Start != 1 || len(frame.Lines) != 3 || !strings.Contains(frame.Lines[1], "<article>") {
		t.Errorf("frame = %+v", frame)
	}
	for _, bad := range []string{"../outside.txt", "/etc/passwd", "", "src/pages"} {
		if rec := devDo(t, h, "GET", "/__krate/dev/source?file="+bad+"&line=1", ""); rec.Code != 404 {
			t.Errorf("source %q = %d, want 404", bad, rec.Code)
		}
	}
}

func TestReadSourceFrameLimits(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	long := strings.Repeat("x", devSourceLineMax+50)
	writeProject(t, root, map[string]string{
		"a.ts":    "one\r\ntwo\n" + long + "\nfour\n",
		"bin.dat": "ab\x00cd",
	})
	f, ok := readSourceFrame(root, filepath.Join(root, "a.ts"), 3, 1000)
	if !ok || f.Start != 1 || len(f.Lines) != 4 || f.Lines[0] != "one" || len(f.Lines[2]) != devSourceLineMax+3 {
		t.Errorf("frame = %+v %v", f.Start, len(f.Lines))
	}
	if _, ok := readSourceFrame(root, filepath.Join(root, "bin.dat"), 1, 2); ok {
		t.Error("binary files have no source frame")
	}
}

func TestDevAPIFix(t *testing.T) {
	t.Parallel()
	d, h := buildDevAPIProject(t, nil)
	rec := devDo(t, h, "GET", "/__krate/dev/fix?file=src/lib/handlers.tsx", "")
	if rec.Code != 200 {
		t.Fatalf("fix preview = %d %s", rec.Code, rec.Body)
	}
	var preview struct {
		Changes []DevFixChange `json:"changes"`
	}
	decodeJSON(t, rec, &preview)
	if len(preview.Changes) != 1 || preview.Changes[0].From != "onclick" || preview.Changes[0].To != "onClick" || preview.Changes[0].File != "src/lib/handlers.tsx" || preview.Changes[0].Line != 2 {
		t.Fatalf("preview = %+v", preview.Changes)
	}
	code := preview.Changes[0].Code
	// A preview limited to another code finds nothing; the file is untouched.
	rec = devDo(t, h, "GET", "/__krate/dev/fix?file=src/lib/handlers.tsx&code=KR9999", "")
	decodeJSON(t, rec, &preview)
	if len(preview.Changes) != 0 {
		t.Errorf("filtered preview = %+v", preview.Changes)
	}
	if src, _ := os.ReadFile(filepath.Join(d.root, "src/lib/handlers.tsx")); !strings.Contains(string(src), "onclick") {
		t.Fatal("a preview must not write")
	}

	// POST needs JSON.
	req := httptest.NewRequest("POST", "http://127.0.0.1/__krate/dev/fix", strings.NewReader(`{"file":"src/lib/handlers.tsx"}`))
	req.Header.Set("Content-Type", "text/plain")
	r2 := httptest.NewRecorder()
	h.ServeHTTP(r2, req)
	if r2.Code != http.StatusUnsupportedMediaType {
		t.Errorf("text/plain POST = %d, want 415", r2.Code)
	}
	if rec := devDo(t, h, "POST", "/__krate/dev/fix", `{"file":"../x.tsx"}`); rec.Code != 404 {
		t.Errorf("fix outside the root = %d", rec.Code)
	}
	if rec := devDo(t, h, "POST", "/__krate/dev/fix", `{"file":"`+strings.Repeat("a", devAPIBodyLimit)+`"}`); rec.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("huge body = %d, want 413", rec.Code)
	}

	rec = devDo(t, h, "POST", "/__krate/dev/fix", `{"file":"src/lib/handlers.tsx","code":"`+code+`"}`)
	var applied struct {
		Applied bool           `json:"applied"`
		Changes []DevFixChange `json:"changes"`
	}
	decodeJSON(t, rec, &applied)
	if rec.Code != 200 || !applied.Applied || len(applied.Changes) != 1 {
		t.Fatalf("apply = %d %+v", rec.Code, applied)
	}
	src, _ := os.ReadFile(filepath.Join(d.root, "src/lib/handlers.tsx"))
	if !strings.Contains(string(src), "onClick=") || strings.Contains(string(src), "onclick") {
		t.Errorf("fixed source:\n%s", src)
	}

	// Fix all.
	writeProject(t, d.root, map[string]string{"src/lib/more.tsx": "export const A = () => <a onclick={() => 1}>a</a>;\n"})
	rec = devDo(t, h, "POST", "/__krate/dev/fix", `{"all":true}`)
	decodeJSON(t, rec, &applied)
	if !applied.Applied || len(applied.Changes) != 1 || applied.Changes[0].File != "src/lib/more.tsx" {
		t.Errorf("fix all = %+v", applied)
	}
}

func TestDevAPIActions(t *testing.T) {
	t.Parallel()
	d, h := buildDevAPIProject(t, nil)
	// No watch loop takes actions.
	if rec := devDo(t, h, "POST", "/__krate/dev/action", `{"action":"rebuild-all"}`); rec.Code != http.StatusConflict {
		t.Errorf("rebuild-all without a watcher = %d, want 409", rec.Code)
	}
	actions, stop := d.hub.watchActions()
	defer stop()
	if rec := devDo(t, h, "POST", "/__krate/dev/action", `{"action":"rebuild-page","route":"/blog/second"}`); rec.Code != http.StatusAccepted {
		t.Fatalf("rebuild-page = %d %s", rec.Code, rec.Body)
	}
	a := <-actions
	if a.Kind != "rebuild-page" || a.File != filepath.Join(d.root, "src", "pages", "blog", "[slug].tsx") {
		t.Errorf("action = %+v", a)
	}
	if rec := devDo(t, h, "POST", "/__krate/dev/action", `{"action":"rebuild-page","route":"/missing/page"}`); rec.Code != 404 {
		t.Errorf("rebuild of an unknown page = %d", rec.Code)
	}
	for _, kind := range []string{"rebuild-all", "clear-cache"} {
		if rec := devDo(t, h, "POST", "/__krate/dev/action", `{"action":"`+kind+`"}`); rec.Code != http.StatusAccepted {
			t.Errorf("%s = %d", kind, rec.Code)
		}
		if a := <-actions; a.Kind != kind {
			t.Errorf("queued %+v, want %s", a, kind)
		}
	}
	if rec := devDo(t, h, "POST", "/__krate/dev/action", `{"action":"explode"}`); rec.Code != 400 {
		t.Errorf("unknown action = %d", rec.Code)
	}
	if rec := devDo(t, h, "GET", "/__krate/dev/action", ""); rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("GET action = %d", rec.Code)
	}

	// Sidecars.
	if rec := devDo(t, h, "POST", "/__krate/dev/action", `{"action":"restart-sidecar","target":"ssr"}`); rec.Code != http.StatusConflict {
		t.Errorf("restart without sidecars = %d, want 409", rec.Code)
	}
	var restarted []string
	d.restartSidecar = func(target string) error {
		switch target {
		case "ssr":
			restarted = append(restarted, target)
			return nil
		case "api", "go":
			return errSidecarNotRunning
		}
		return errSidecarUnknown
	}
	for target, want := range map[string]int{"ssr": 202, "api": 409, "go": 409, "db": 400} {
		if rec := devDo(t, h, "POST", "/__krate/dev/action", `{"action":"restart-sidecar","target":"`+target+`"}`); rec.Code != want {
			t.Errorf("restart %s = %d, want %d", target, rec.Code, want)
		}
	}
	if len(restarted) != 1 {
		t.Errorf("restarted = %v", restarted)
	}

	// restart-server asks the hub.
	if rec := devDo(t, h, "POST", "/__krate/dev/action", `{"action":"restart-server"}`); rec.Code != http.StatusAccepted {
		t.Errorf("restart-server = %d", rec.Code)
	}
	select {
	case <-d.hub.RestartRequested():
	default:
		t.Error("restart-server did not request a restart")
	}
}

func TestDevAPIGuard(t *testing.T) {
	t.Parallel()
	_, h := buildDevAPIProject(t, nil)
	req := httptest.NewRequest("GET", "http://127.0.0.1/__krate/dev/info", nil)
	req.Header.Set("Origin", "http://evil.example")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Errorf("cross-origin = %d, want 403", rec.Code)
	}
	req = httptest.NewRequest("GET", "http://evil.example/__krate/dev/info", nil)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Errorf("foreign Host = %d, want 403", rec.Code)
	}
}

func TestDevAPILogsAndActivity(t *testing.T) {
	// Not parallel: the log ring is process-wide.
	d, h := buildDevAPIProject(t, nil)
	seq, _ := DevLogsSince(0)
	recordDevLog("", false, "  Affected pages: [/]")
	recordDevLog("", true, "[browser] TypeError: x is undefined /:1:2")
	recordDevLog(LogSrcSSR, true, "  ! render slow")
	recordDevLog(LogSrcAPI, false, "listening")
	rec := devDo(t, h, "GET", "/__krate/dev/logs?since="+itoa64(seq), "")
	var logs struct {
		Seq   int64        `json:"seq"`
		Lines []DevLogLine `json:"lines"`
	}
	decodeJSON(t, rec, &logs)
	if logs.Seq != seq+4 || len(logs.Lines) != 4 {
		t.Fatalf("logs = %+v", logs)
	}
	want := [][3]string{{"krate", "info", "  Affected pages: [/]"}, {"browser", "error", "[browser] TypeError: x is undefined /:1:2"}, {"ssr", "warn", "  ! render slow"}, {"api", "info", "listening"}}
	for i, w := range want {
		l := logs.Lines[i]
		if l.Src != w[0] || l.Lvl != w[1] || l.Text != w[2] || l.T == 0 || l.Seq != seq+int64(i)+1 {
			t.Errorf("line %d = %+v, want %v", i, l, w)
		}
	}
	rec = devDo(t, h, "GET", "/__krate/dev/logs?since="+itoa64(logs.Seq), "")
	decodeJSON(t, rec, &logs)
	if len(logs.Lines) != 0 {
		t.Errorf("nothing new, got %+v", logs.Lines)
	}

	act := d.hub.BeginActivity(d.root, []string{filepath.Join(d.root, "src/pages/about.tsx")})
	act.SetKind(BuildKindPartial)
	act.SetPages(1)
	d.hub.Publish(DevEvent{Type: "reload", Routes: []string{"/about"}})
	rec = devDo(t, h, "GET", "/__krate/dev/activity", "")
	var snap BuildActivitySnapshot
	decodeJSON(t, rec, &snap)
	if len(snap.Builds) != 1 || snap.LastGoodAt == 0 || snap.Building {
		t.Fatalf("activity = %+v", snap)
	}
	b := snap.Builds[0]
	if b.ID != 1 || b.Kind != BuildKindPartial || b.Pages != 1 || !b.OK || strings.Join(b.Files, ",") != "src/pages/about.tsx" || strings.Join(b.Routes, ",") != "/about" {
		t.Errorf("record = %+v", b)
	}
}

func itoa64(n int64) string { return strconv.FormatInt(n, 10) }

func TestLogLevel(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		stderr bool
		text   string
		want   string
	}{
		{false, "  Watching src for changes...", "info"},
		{true, "  ! Node API sidecar exited (exit status 1); restarting", "warn"},
		{false, "  x Chunk error: boom", "error"},
		{false, "Error: something failed", "error"},
		{false, "(node:1) DeprecationWarning: Buffer()", "warn"},
		{true, "plain stderr text", "error"},
		{false, "a terror alert", "info"},
	} {
		if got := logLevel(c.stderr, c.text); got != c.want {
			t.Errorf("logLevel(%v, %q) = %s, want %s", c.stderr, c.text, got, c.want)
		}
	}
}

func TestDevLogRingCap(t *testing.T) {
	// Not parallel: the log ring is process-wide.
	for i := 0; i < devLogCap+20; i++ {
		recordDevLog(LogSrcGo, false, "line")
	}
	seq, lines := DevLogsSince(0)
	if len(lines) != devLogCap || lines[len(lines)-1].Seq != seq {
		t.Errorf("ring holds %d lines ending at %d (seq %d)", len(lines), lines[len(lines)-1].Seq, seq)
	}
	recordDevLog(LogSrcGo, false, strings.Repeat("y", devLogLineMax+10))
	_, lines = DevLogsSince(seq)
	if len(lines) != 1 || len(lines[0].Text) != devLogLineMax+3 {
		t.Errorf("long line stored as %d bytes", len(lines[0].Text))
	}
}

func TestAllowedEditor(t *testing.T) {
	prev := lookEditor
	t.Cleanup(func() { lookEditor = prev })
	lookEditor = func(id string) (string, error) {
		if id == "zed" || id == "sh" {
			return "/usr/bin/" + id, nil
		}
		return "", errors.New("not found")
	}
	for id, want := range map[string]bool{"zed": true, "code": false, "sh": false, "zed --x": false, "": false} {
		if got := allowedEditor(id); got != want {
			t.Errorf("allowedEditor(%q) = %v, want %v", id, got, want)
		}
	}
	if got := availableEditors(); len(got) != 1 || got[0].ID != "zed" || got[0].Label != "Zed" {
		t.Errorf("available = %+v", got)
	}
}

// Sidecar output keeps reaching the terminal and every tee (dev.log, the run
// log, the crash tail) unchanged, and the Logs panel learns its source.
func TestSidecarOutputIsAttributed(t *testing.T) {
	// Not parallel: it swaps os.Stdout and os.Stderr.
	sh, err := exec.LookPath("sh")
	if err != nil {
		t.Skip("no sh")
	}
	termR, termW, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	origOut, origErr := os.Stdout, os.Stderr
	os.Stdout, os.Stderr = termW, termW
	var seen []teeLine
	stop := teeStdioLines(func(l teeLine) {
		seen = append(seen, teeLine{text: append([]byte(nil), l.text...), stderr: l.stderr, src: l.src})
	})
	devLogsOn.Add(1)
	seq, _ := DevLogsSince(0)

	cmd := exec.Command(sh, "-c", "echo from-stdout; echo from-stderr >&2; printf partial")
	after := attachSidecarOutput(cmd, LogSrcGo)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	after()
	if err := cmd.Wait(); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "sidecar lines", func() bool {
		_, lines := DevLogsSince(seq)
		return len(lines) >= 3
	})
	fmt.Fprintln(os.Stdout, "krate line")
	devLogsOn.Add(-1)
	stop()
	os.Stdout, os.Stderr = origOut, origErr
	_ = termW.Close()
	term, _ := io.ReadAll(termR)

	for _, want := range []string{"from-stdout\n", "from-stderr\n", "partial", "krate line\n"} {
		if !strings.Contains(string(term), want) {
			t.Errorf("terminal output %q lacks %q", term, want)
		}
	}
	if strings.Contains(string(term), "[go]") {
		t.Errorf("the terminal must not get a source prefix: %q", term)
	}
	srcs := map[string]string{}
	for _, l := range seen {
		srcs[string(l.text)] = l.src
	}
	if srcs["from-stdout"] != LogSrcGo || srcs["from-stderr"] != LogSrcGo || srcs["krate line"] != "" {
		t.Errorf("tee saw %v", srcs)
	}
	_, lines := DevLogsSince(seq)
	got := map[string]string{}
	for _, l := range lines {
		got[l.Text] = l.Src + "/" + l.Lvl
	}
	if got["from-stdout"] != "go/info" || got["from-stderr"] != "go/error" || got["partial"] != "go/info" {
		t.Errorf("ring = %v", got)
	}
}

func TestBuildCrashErrorFields(t *testing.T) {
	t.Parallel()
	code := 3
	ce := buildCrashError("SSR renderer", "exited unexpectedly", "Error: x\n    at a (src/pages/a.tsx:3:4)", []string{"one", "two"})
	ce.ExitCode = &code
	if ce.Source != "SSR renderer" || ce.Detail != "exited unexpectedly" || ce.Restart != "ssr" || strings.Join(ce.Log, ",") != "one,two" {
		t.Errorf("crash = %+v", ce)
	}
	if !strings.HasPrefix(ce.Message, "SSR renderer failed: exited unexpectedly") || !strings.Contains(ce.Stack, "Last 2 console line(s)") {
		t.Errorf("message/stack changed: %q %q", ce.Message, ce.Stack)
	}
	data, _ := json.Marshal(ce)
	for _, want := range []string{`"source":"SSR renderer"`, `"exitCode":3`, `"log":["one","two"]`, `"restart":"ssr"`, `"kind":"server"`} {
		if !strings.Contains(string(data), want) {
			t.Errorf("JSON %s lacks %s", data, want)
		}
	}
	for src, want := range map[string]string{"Node API sidecar": "api", "Go API sidecar": "go", "Middleware": "", "Instrumentation": ""} {
		if got := crashRestartTarget(src); got != want {
			t.Errorf("restart target of %s = %q, want %q", src, got, want)
		}
	}
	if c := exitCodeOf(nil); c == nil || *c != 0 {
		t.Errorf("exit code of a clean exit = %v", c)
	}
	if c := exitCodeOf(errors.New("killed")); c != nil {
		t.Errorf("exit code of a non-exit error = %v", *c)
	}
}
