package mcp

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/kratejs/krate/packages/compiler/internal/build"
	"github.com/kratejs/krate/packages/compiler/internal/diag"
)

var insightTools = []string{
	"dev_server_status", "dev_server", "get_errors", "get_logs",
	"explain_page", "why_hydrated", "why_in_bundle", "list_islands", "get_page_metadata", "get_rendered_html", "render_preview",
	"get_project_metadata", "get_config", "list_api_routes", "list_middleware", "list_collections", "list_actions",
	"get_server_action_by_id", "env_check", "bundle_report", "compat_check",
	"audit_page", "explain_diagnostic", "typecheck", "upgrade", "migrate_nextjs",
	"page_check", "browser_eval",
}

func TestInsightToolsAreRegisteredWithSchemas(t *testing.T) {
	svc := newTestService(t, map[string]string{"src/pages/index.tsx": "export default function P() { return <h1>x</h1>; }"})
	srv := NewServer(os.Stderr, "krate", "test")
	svc.Register(srv)
	for _, name := range insightTools {
		tool, ok := srv.tools[name]
		if !ok {
			t.Errorf("tool %s is not registered", name)
			continue
		}
		if len(tool.Description) < 80 {
			t.Errorf("%s: description is too short for a model to choose it (%d chars)", name, len(tool.Description))
		}
		if tool.InputSchema["type"] != "object" {
			t.Errorf("%s: input schema must be an object", name)
		}
		if _, ok := tool.Annotations["readOnlyHint"]; !ok {
			t.Errorf("%s: missing annotations", name)
		}
		if tool.Handler == nil {
			t.Errorf("%s: no handler", name)
		}
		props, _ := tool.InputSchema["properties"].(map[string]any)
		for pname, p := range props {
			if d, _ := asMap(p)["description"].(string); d == "" {
				t.Errorf("%s.%s: parameter has no description", name, pname)
			}
		}
	}
	// Every list-returning tool must be bounded.
	for _, name := range []string{"get_errors", "list_api_routes", "list_actions", "list_collections", "list_islands", "bundle_report", "env_check", "audit_page", "get_config", "typecheck", "migrate_nextjs", "why_in_bundle", "explain_diagnostic"} {
		props, _ := srv.tools[name].InputSchema["properties"].(map[string]any)
		if _, ok := props["limit"]; !ok {
			t.Errorf("%s has no limit parameter", name)
		}
		if _, ok := props["cursor"]; !ok {
			t.Errorf("%s has no cursor parameter", name)
		}
	}
	// Read-only tools say so; process and browser tools do not.
	for _, name := range []string{"explain_page", "get_config", "list_middleware", "env_check", "get_logs"} {
		if srv.tools[name].Annotations["readOnlyHint"] != true {
			t.Errorf("%s should be annotated read-only", name)
		}
	}
	for _, name := range []string{"dev_server", "upgrade", "page_check", "browser_eval"} {
		if srv.tools[name].Annotations["readOnlyHint"] == true {
			t.Errorf("%s must not be annotated read-only", name)
		}
	}
	if srv.tools["upgrade"].Annotations["destructiveHint"] != true {
		t.Error("upgrade writes files and should be annotated destructive")
	}
	if srv.tools["dev_server"].Annotations["destructiveHint"] == true {
		t.Error("dev_server only manages its own child process")
	}
}

// fakeDevServer serves the endpoints krate dev exposes and records it where
// the real server would.
func fakeDevServer(t *testing.T, svc *Service, snap build.DevSnapshot) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/__krate/status", func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(`{}`)) })
	mux.HandleFunc("/__krate/state", func(w http.ResponseWriter, r *http.Request) { _ = json.NewEncoder(w).Encode(snap) })
	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)
	writeDevRecord(t, svc, ts.Listener.Addr().String(), os.Getpid())
	return ts
}

func writeDevRecord(t *testing.T, svc *Service, addr string, pid int) {
	t.Helper()
	host, portStr, _ := net.SplitHostPort(addr)
	port, _ := strconv.Atoi(portStr)
	rec := build.DevServerRecord{PID: pid, Host: host, Port: port, URL: "http://" + addr, StartedAt: time.Now()}
	data, _ := json.Marshal(rec)
	writeFile(t, svc.root, ".krate/dev-server.json", string(data))
}

func TestDevServerStatusAndGetErrorsReadTheLiveServer(t *testing.T) {
	svc := newTestService(t, map[string]string{"src/pages/index.tsx": "export default function P() { return <h1>x</h1>; }"})
	if m := mustOK(t, svc, "dev_server_status", nil); m["running"] != false {
		t.Fatalf("no dev server yet: %v", m)
	}

	snap := build.DevSnapshot{
		PID: 1, Built: true, BuildOK: false, Rebuilds: 3,
		Diagnostics: []diag.Diagnostic{
			{File: filepath.Join(svc.root, "src/pages/index.tsx"), Line: 4, Col: 9, Code: "KR7115", Message: "tier decorator cannot apply", Hint: "move it", Source: "@server x"},
			{File: "src/pages/b.tsx", Line: 1, Message: "second"},
		},
		ClientErrors: []build.ClientErrorEntry{
			{ClientError: build.ClientError{Message: "TypeError: boom", Stack: strings.Repeat("at f (a.js:1:1)\n", 30), URL: "/x", Line: 7, Col: 2}, Time: time.Now()},
		},
	}
	fakeDevServer(t, svc, snap)

	st := mustOK(t, svc, "dev_server_status", nil)
	if st["running"] != true || st["buildOk"] != false || st["diagnosticCount"] != float64(2) || st["browserErrorCount"] != float64(1) {
		t.Fatalf("status = %v", st)
	}

	m := mustOK(t, svc, "get_errors", nil)
	if m["source"] != "dev-server" || m["buildOk"] != false || m["total"] != float64(3) {
		t.Fatalf("get_errors = %v", m)
	}
	errs := list(t, m, "errors")
	first := asMap(errs[0])
	if first["kind"] != "build" || first["code"] != "KR7115" || first["file"] != "src/pages/index.tsx" || first["line"] != float64(4) {
		t.Errorf("build error = %v", first)
	}
	browser := asMap(errs[2])
	if browser["kind"] != "browser" || !strings.Contains(browser["stack"].(string), "more frames") {
		t.Errorf("browser error (stack must be trimmed) = %v", browser)
	}

	// Pagination.
	p1 := mustOK(t, svc, "get_errors", map[string]any{"limit": 2})
	if len(list(t, p1, "errors")) != 2 || p1["nextCursor"] == "" {
		t.Fatalf("page 1 = %v", p1)
	}
	p2 := mustOK(t, svc, "get_errors", map[string]any{"limit": 2, "cursor": p1["nextCursor"]})
	if len(list(t, p2, "errors")) != 1 || p2["nextCursor"] != "" {
		t.Fatalf("page 2 = %v", p2)
	}
	// Kind filter.
	b := mustOK(t, svc, "get_errors", map[string]any{"kind": "browser"})
	if b["total"] != float64(1) {
		t.Errorf("browser only = %v", b)
	}
	if msg := mustFail(t, svc, "get_errors", map[string]any{"kind": "nope"}); !strings.Contains(msg, "kind must be") {
		t.Errorf("bad kind: %s", msg)
	}
	if msg := mustRPCError(t, svc, "get_errors", map[string]any{"cursor": "!!"}); !strings.Contains(msg, "invalid cursor") {
		t.Errorf("invalid cursor: %s", msg)
	}
}

func TestDevRecordOnNonLoopbackHostIsIgnored(t *testing.T) {
	svc := newTestService(t, map[string]string{"src/pages/index.tsx": "export default function P() { return <h1>x</h1>; }"})
	hit := false
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { hit = true }))
	defer ts.Close()
	_, port, _ := net.SplitHostPort(ts.Listener.Addr().String())
	p, _ := strconv.Atoi(port)
	data, _ := json.Marshal(build.DevServerRecord{PID: 1, Host: "203.0.113.9", Port: p, URL: "http://203.0.113.9"})
	writeFile(t, svc.root, ".krate/dev-server.json", string(data))
	if m := mustOK(t, svc, "dev_server_status", nil); m["running"] != false {
		t.Fatalf("a record naming a non-loopback host must not be contacted: %v", m)
	}
	if hit {
		t.Fatal("the tool connected to a non-loopback address named in a project file")
	}
}

func TestGetErrorsWithoutDevServerBuildsAndReportsDiagnostics(t *testing.T) {
	svc := newTestService(t, map[string]string{
		"src/pages/index.tsx": "import Missing from './nope';\nexport default function P() { return <h1>x</h1>; }\n",
	})
	m := mustOK(t, svc, "get_errors", nil)
	if m["source"] != "build" || m["buildOk"] != false {
		t.Fatalf("expected a failing fresh build: %v", m)
	}
	errs := list(t, m, "errors")
	if len(errs) == 0 {
		t.Fatalf("no errors reported: %v", m)
	}
	e := asMap(errs[0])
	if e["file"] != "src/pages/index.tsx" || e["line"] != float64(1) || !strings.Contains(e["message"].(string), "cannot resolve") {
		t.Errorf("error = %v", e)
	}
	if s, _ := json.Marshal(m); strings.Contains(string(s), svc.root) {
		t.Errorf("the project root path leaked into the result: %s", s)
	}
	if !strings.Contains(m["note"].(string), "no dev server") {
		t.Errorf("note = %v", m["note"])
	}
}

func devLogLine(ts time.Time, text string) string {
	return ts.UTC().Format(build.DevLogTimeFormat) + " " + text + "\n"
}

func TestGetLogsFiltersAndGetErrorsServerLog(t *testing.T) {
	svc := newTestService(t, map[string]string{"src/pages/index.tsx": "export default function P() { return <h1>x</h1>; }"})
	if msg := mustFail(t, svc, "get_logs", nil); !strings.Contains(msg, "no dev log") {
		t.Fatalf("missing log: %s", msg)
	}
	now := time.Now()
	old := now.Add(-3 * time.Hour)
	log := devLogLine(old, "Render error (/old) [digest 1]: ancient failure") +
		devLogLine(now.Add(-time.Minute), "  GET /about 200") +
		devLogLine(now.Add(-time.Minute), "  Warning: something odd") +
		devLogLine(now.Add(-30*time.Second), "  Render error (/blog) [digest 9]: boom in "+svc.root+"/src/pages/blog.tsx") +
		devLogLine(now.Add(-20*time.Second), "  x Error: bundling page a.tsx: cannot resolve import") +
		devLogLine(now.Add(-10*time.Second), "[browser] TypeError: x")
	writeFile(t, svc.root, ".krate/dev.log", log)

	all := mustOK(t, svc, "get_logs", map[string]any{"lines": 3})
	if all["truncated"] != true || len(list(t, all, "lines")) != 3 {
		t.Errorf("lines cap = %v", all)
	}
	errsOnly := mustOK(t, svc, "get_logs", map[string]any{"level": "error"})
	for _, l := range strs(list(t, errsOnly, "lines")) {
		if strings.Contains(l, "GET /about") {
			t.Errorf("level=error kept a plain request line: %s", l)
		}
	}
	grep := mustOK(t, svc, "get_logs", map[string]any{"grep": "WARNING"})
	if len(list(t, grep, "lines")) != 1 {
		t.Errorf("grep = %v", grep)
	}
	since := mustOK(t, svc, "get_logs", map[string]any{"since": now.Add(-2 * time.Minute).UTC().Format(time.RFC3339)})
	for _, l := range strs(list(t, since, "lines")) {
		if strings.Contains(l, "ancient") {
			t.Errorf("since did not drop an old line: %s", l)
		}
	}
	if s, _ := json.Marshal(all); strings.Contains(string(s), svc.root) {
		t.Errorf("root leaked: %s", s)
	}
	if msg := mustFail(t, svc, "get_logs", map[string]any{"since": "yesterday"}); !strings.Contains(msg, "RFC 3339") {
		t.Errorf("bad since: %s", msg)
	}
	if msg := mustFail(t, svc, "get_logs", map[string]any{"level": "x"}); !strings.Contains(msg, "level must be") {
		t.Errorf("bad level: %s", msg)
	}

	// With a live server, runtime log errors appear next to build and browser
	// errors, but build-phase noise, warnings and old lines do not.
	fakeDevServer(t, svc, build.DevSnapshot{Built: true, BuildOK: true})
	m := mustOK(t, svc, "get_errors", nil)
	var msgs []string
	for _, e := range list(t, m, "errors") {
		em := asMap(e)
		if em["kind"] != "server-log" {
			t.Errorf("unexpected kind %v", em)
		}
		msgs = append(msgs, em["message"].(string))
	}
	if len(msgs) != 1 || !strings.Contains(msgs[0], "Render error (/blog)") {
		t.Errorf("server-log errors = %q (want only the recent render error)", msgs)
	}
	wide := mustOK(t, svc, "get_errors", map[string]any{"kind": "server", "sinceMinutes": 600})
	if wide["total"] != float64(2) {
		t.Errorf("sinceMinutes should widen the window: %v", wide)
	}
}

// TestFakeDevProcess is the child process the dev_server tests start in place
// of `krate dev`.
func TestFakeDevProcess(t *testing.T) {
	root := os.Getenv("KRATE_FAKE_DEV_ROOT")
	if root == "" {
		t.Skip("helper process")
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/__krate/status", func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(`{}`)) })
	mux.HandleFunc("/__krate/state", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(build.DevSnapshot{PID: os.Getpid(), Built: true, BuildOK: true})
	})
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		os.Exit(2)
	}
	if os.Getenv("KRATE_FAKE_DEV_EXIT") == "1" {
		fmt.Fprintln(os.Stderr, "config error: boom")
		os.Exit(3)
	}
	addr := ln.Addr().String()
	host, portStr, _ := net.SplitHostPort(addr)
	port, _ := strconv.Atoi(portStr)
	data, _ := json.Marshal(build.DevServerRecord{PID: os.Getpid(), Host: host, Port: port, URL: "http://" + addr, StartedAt: time.Now()})
	_ = os.MkdirAll(filepath.Join(root, ".krate"), 0o755)
	_ = os.WriteFile(filepath.Join(root, ".krate", "dev-server.json"), data, 0o644)
	go func() { _ = http.Serve(ln, mux) }()
	select {}
}

func useFakeDev(t *testing.T, svc *Service, exitEarly bool) {
	t.Helper()
	old := devCommand
	devCommand = func(root string) (string, []string, error) {
		return os.Args[0], []string{"-test.run=^TestFakeDevProcess$"}, nil
	}
	t.Setenv("KRATE_FAKE_DEV_ROOT", svc.root)
	if exitEarly {
		t.Setenv("KRATE_FAKE_DEV_EXIT", "1")
	}
	t.Cleanup(func() { svc.Close(); devCommand = old })
}

func TestDevServerToolPolicyAndDryRun(t *testing.T) {
	svc := newTestService(t, map[string]string{"src/pages/index.tsx": "export default function P() { return <h1>x</h1>; }"})
	useFakeDev(t, svc, false)
	if msg := mustFail(t, svc, "dev_server", map[string]any{"action": "start", "apply": true}); !strings.Contains(msg, `"dev" to allowTasks`) {
		t.Fatalf("default policy must not allow starting project code: %s", msg)
	}
	writePolicy(t, svc, `{"allowTasks":["dev"]}`)
	dry := mustOK(t, svc, "dev_server", map[string]any{"action": "start"})
	if dry["dryRun"] != true {
		t.Fatalf("dry run = %v", dry)
	}
	if _, err := os.Stat(filepath.Join(svc.root, ".krate", "dev-server.json")); err == nil {
		t.Fatal("a dry run started a server")
	}
	if msg := mustFail(t, svc, "dev_server", map[string]any{"action": "explode"}); !strings.Contains(msg, "action must be") {
		t.Errorf("bad action: %s", msg)
	}
	writePolicy(t, svc, `{"allowTasks":["dev"],"readOnlyMode":true}`)
	if msg := mustFail(t, svc, "dev_server", map[string]any{"action": "start", "apply": true}); !strings.Contains(msg, "readOnlyMode") {
		t.Errorf("readOnlyMode must disable dev_server: %s", msg)
	}
}

func TestDevServerLifecycle(t *testing.T) {
	if testing.Short() {
		t.Skip("starts a helper process")
	}
	svc := newTestService(t, map[string]string{"src/pages/index.tsx": "export default function P() { return <h1>x</h1>; }"})
	useFakeDev(t, svc, false)
	writePolicy(t, svc, `{"allowTasks":["dev"]}`)

	started := mustOK(t, svc, "dev_server", map[string]any{"action": "start", "apply": true})
	if started["started"] != true || !strings.HasPrefix(started["url"].(string), "http://127.0.0.1:") {
		t.Fatalf("start = %v", started)
	}
	pid := started["pid"].(float64)
	st := mustOK(t, svc, "dev_server_status", nil)
	if st["running"] != true || st["pid"] != pid || asMap(st["managed"])["running"] != true {
		t.Fatalf("status = %v", st)
	}
	// Starting again is a no-op, not a second server.
	again := mustOK(t, svc, "dev_server", map[string]any{"action": "start", "apply": true})
	if again["started"] != false {
		t.Errorf("second start = %v", again)
	}

	restarted := mustOK(t, svc, "dev_server", map[string]any{"action": "restart", "apply": true})
	if restarted["started"] != true || restarted["pid"] == pid {
		t.Fatalf("restart must start a new process: %v", restarted)
	}
	stopped := mustOK(t, svc, "dev_server", map[string]any{"action": "stop", "apply": true})
	if stopped["stopped"] != true {
		t.Fatalf("stop = %v", stopped)
	}
	waitFor(t, "record removal", func() bool {
		// The fake never removes its record, so the process must be gone.
		return !pidAlive(int(restarted["pid"].(float64)))
	})
	if m := mustOK(t, svc, "dev_server", map[string]any{"action": "stop", "apply": true}); m["stopped"] != false {
		t.Errorf("stopping nothing = %v", m)
	}
}

func TestDevServerRefusesToStopOrDuplicateAnExternalServer(t *testing.T) {
	svc := newTestService(t, map[string]string{"src/pages/index.tsx": "export default function P() { return <h1>x</h1>; }"})
	useFakeDev(t, svc, false)
	writePolicy(t, svc, `{"allowTasks":["dev"]}`)
	fakeDevServer(t, svc, build.DevSnapshot{Built: true, BuildOK: true})
	if msg := mustFail(t, svc, "dev_server", map[string]any{"action": "stop", "apply": true}); !strings.Contains(msg, "not started by this tool") {
		t.Errorf("stop external: %s", msg)
	}
	if msg := mustFail(t, svc, "dev_server", map[string]any{"action": "start", "apply": true}); !strings.Contains(msg, "already running") {
		t.Errorf("duplicate start: %s", msg)
	}
}

func TestDevServerStartupFailureIsExplained(t *testing.T) {
	if testing.Short() {
		t.Skip("starts a helper process")
	}
	svc := newTestService(t, map[string]string{"src/pages/index.tsx": "export default function P() { return <h1>x</h1>; }"})
	useFakeDev(t, svc, true)
	writePolicy(t, svc, `{"allowTasks":["dev"]}`)
	msg := mustFail(t, svc, "dev_server", map[string]any{"action": "start", "apply": true})
	if !strings.Contains(msg, "exited during startup") || !strings.Contains(msg, "config error: boom") {
		t.Errorf("startup failure should carry the process output: %s", msg)
	}
	st := mustOK(t, svc, "dev_server_status", nil)
	if m := asMap(st["managed"]); m["running"] != false || m["exit"] == nil {
		t.Errorf("managed status after a crash = %v", st)
	}
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

func pidAlive(pid int) bool {
	if _, err := os.Stat(fmt.Sprintf("/proc/%d", pid)); err == nil {
		data, _ := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
		return !strings.Contains(string(data), ") Z ")
	}
	return false
}

func TestInsightToolCallsAreAudited(t *testing.T) {
	svc := newTestService(t, map[string]string{"src/pages/index.tsx": "export default function P() { return <h1>x</h1>; }"})
	resps := serve(t, svc,
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`,
		call("get_project_metadata", map[string]any{}),
		call("explain_diagnostic", map[string]any{"code": "KR7115"}),
	)
	if len(resps) != 3 || toolText(t, resps[1]) == "" {
		t.Fatalf("responses = %v", resps)
	}
	data, err := os.ReadFile(filepath.Join(svc.root, ".krate", "mcp-audit.jsonl"))
	if err != nil {
		t.Fatalf("no audit log: %v", err)
	}
	log := string(data)
	for _, tool := range []string{"get_project_metadata", "explain_diagnostic"} {
		if !strings.Contains(log, `"tool":"`+tool+`"`) {
			t.Errorf("audit log has no record of %s:\n%s", tool, log)
		}
	}
	if !strings.Contains(log, `"readOnly":true`) {
		t.Errorf("read-only hint missing from audit records:\n%s", log)
	}
}
