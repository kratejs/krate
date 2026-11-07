package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/kratejs/krate/packages/compiler/internal/build"
	"github.com/kratejs/krate/packages/compiler/internal/diag"
	"github.com/kratejs/krate/packages/compiler/internal/execx"
)

// devManager owns the `krate dev` child process started by the dev_server tool.
// A dev server started by the developer in a terminal is found through
// .krate/dev-server.json instead and is never stopped by this tool.
type devManager struct {
	mu        sync.Mutex
	cmd       *exec.Cmd
	startedAt time.Time
	done      chan struct{}
	exitErr   atomic.Value // errBox
	tail      *tailBuffer
}

type errBox struct{ err error }

// tailBuffer keeps the last bytes written to it, so a dev server that dies
// during startup (before it opens its own log) still explains itself.
type tailBuffer struct {
	mu  sync.Mutex
	buf []byte
}

const tailMax = 8 << 10

func (t *tailBuffer) Write(p []byte) (int, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.buf = append(t.buf, p...)
	if len(t.buf) > tailMax {
		t.buf = append([]byte(nil), t.buf[len(t.buf)-tailMax:]...)
	}
	return len(p), nil
}

func (t *tailBuffer) String() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return stripANSI(string(t.buf))
}

// devCommand returns the executable and arguments that start a dev server.
// Tests replace it.
var devCommand = func(root string) (string, []string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", nil, err
	}
	return exe, []string{"dev", root}, nil
}

var devHTTP = &http.Client{
	Timeout: 3 * time.Second,
	CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	},
}

// devTarget is a reachable dev server.
type devTarget struct {
	Rec     build.DevServerRecord
	BaseURL string
}

// findDevServer locates a live dev server for this project. It trusts the
// discovery record only if the process answers on a loopback address.
func (s *Service) findDevServer(ctx context.Context) (devTarget, bool) {
	rec, ok := build.ReadDevServerRecord(s.root)
	if !ok {
		return devTarget{}, false
	}
	host := rec.Host
	if host == "" || host == "localhost" || host == "0.0.0.0" || host == "::" {
		host = "127.0.0.1"
	}
	ip := net.ParseIP(strings.Trim(host, "[]"))
	if ip == nil || !ip.IsLoopback() {
		// Never fetch from a non-loopback address named in a project file.
		return devTarget{}, false
	}
	base := "http://" + net.JoinHostPort(host, strconv.Itoa(rec.Port))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/__krate/status", nil)
	if err != nil {
		return devTarget{}, false
	}
	resp, err := devHTTP.Do(req)
	if err != nil {
		return devTarget{}, false
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return devTarget{}, false
	}
	return devTarget{Rec: rec, BaseURL: base}, true
}

func (s *Service) fetchDevState(ctx context.Context, t devTarget) (build.DevSnapshot, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, t.BaseURL+"/__krate/state", nil)
	if err != nil {
		return build.DevSnapshot{}, err
	}
	resp, err := devHTTP.Do(req)
	if err != nil {
		return build.DevSnapshot{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return build.DevSnapshot{}, fmt.Errorf("dev server answered %d for /__krate/state; it may be an older krate version", resp.StatusCode)
	}
	var snap build.DevSnapshot
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(&snap); err != nil {
		return build.DevSnapshot{}, err
	}
	return snap, nil
}

func (s *Service) managedStatus() map[string]any {
	m := &s.dev
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.cmd == nil {
		return nil
	}
	out := map[string]any{
		"pid":       m.cmd.Process.Pid,
		"startedAt": m.startedAt.UTC().Format(time.RFC3339),
	}
	select {
	case <-m.done:
		out["running"] = false
		if b, ok := m.exitErr.Load().(errBox); ok && b.err != nil {
			out["exit"] = b.err.Error()
		}
		if m.tail != nil {
			if t := strings.TrimSpace(m.tail.String()); t != "" {
				out["output"] = t
			}
		}
	default:
		out["running"] = true
	}
	return out
}

func (s *Service) toolDevServerStatus(ctx context.Context, _ map[string]any) (ToolResult, *rpcError) {
	out := map[string]any{"running": false}
	if t, ok := s.findDevServer(ctx); ok {
		out["running"] = true
		out["url"] = t.Rec.URL
		out["pid"] = t.Rec.PID
		out["startedAt"] = t.Rec.StartedAt.UTC().Format(time.RFC3339)
		if snap, err := s.fetchDevState(ctx, t); err == nil {
			out["buildOk"] = snap.BuildOK
			out["built"] = snap.Built
			out["diagnosticCount"] = len(snap.Diagnostics)
			out["browserErrorCount"] = len(snap.ClientErrors)
			out["rebuilds"] = snap.Rebuilds
			if snap.LastEventAt != nil {
				out["lastBuildAt"] = snap.LastEventAt.UTC().Format(time.RFC3339)
			}
		} else {
			out["stateError"] = err.Error()
		}
		out["logFile"] = ".krate/dev.log"
	} else {
		out["hint"] = "no dev server found; start one with the dev_server tool (action start) or run `krate dev` in a terminal"
	}
	if m := s.managedStatus(); m != nil {
		out["managed"] = m
	}
	return JSONResult(out)
}

type errorItem struct {
	Kind     string `json:"kind"`
	Severity string `json:"severity,omitempty"`
	Code     string `json:"code,omitempty"`
	Message  string `json:"message"`
	File     string `json:"file,omitempty"`
	Line     int    `json:"line,omitempty"`
	Col      int    `json:"col,omitempty"`
	Hint     string `json:"hint,omitempty"`
	Source   string `json:"source,omitempty"`
	Stack    string `json:"stack,omitempty"`
	URL      string `json:"url,omitempty"`
	Time     string `json:"time,omitempty"`
}

func (s *Service) diagItem(d diag.Diagnostic) errorItem {
	file := d.File
	if filepath.IsAbs(file) {
		if rel := s.relToRoot(file); rel != "" {
			file = rel
		} else {
			file = filepath.Base(file)
		}
	}
	sev := "error"
	if d.IsWarning() {
		sev = "warning"
	}
	return errorItem{Kind: "build", Severity: sev, Code: string(d.Code), Message: d.Message,
		File: filepath.ToSlash(file), Line: d.Line, Col: d.Col, Hint: d.Hint, Source: d.Source}
}

// logErrorLines extracts error-looking lines from the dev log, so server-side
// failures (API route errors, SSR crashes) that never reach the browser show
// up in get_errors.
var runtimeErrorMarkers = []string{
	"middleware error", "middleware unavailable", "render error", "renderer unavailable", "page region error",
	"api proxy error", "exited unexpectedly", "not started", "revalidation failed", "component render failed",
	"panic", "unhandled", "uncaught", "internal server error", "plugin error",
}

var buildPhaseMarkers = []string{"compilation error", "bundling page", "build failed", "build error", "warning", "hint:"}

// logErrorLines extracts runtime error lines from the dev log (build errors
// come from the dev server state instead), so failures that never reach the
// browser still show up in get_errors.
func logErrorLines(lines []logLine, since time.Time, max int) []errorItem {
	var out []errorItem
	for i := len(lines) - 1; i >= 0 && len(out) < max; i-- {
		ln := lines[i]
		if !ln.Time.IsZero() && ln.Time.Before(since) {
			break
		}
		low := strings.ToLower(ln.Text)
		skip := strings.HasPrefix(low, "[browser]")
		for _, m := range buildPhaseMarkers {
			if strings.Contains(low, m) {
				skip = true
			}
		}
		if skip {
			continue
		}
		for _, m := range runtimeErrorMarkers {
			if strings.Contains(low, m) {
				it := errorItem{Kind: "server-log", Severity: "error", Message: strings.TrimSpace(ln.Text)}
				if !ln.Time.IsZero() {
					it.Time = ln.Time.Format(time.RFC3339)
				}
				out = append(out, it)
				break
			}
		}
	}
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out
}

func (s *Service) toolGetErrors(ctx context.Context, args map[string]any) (ToolResult, *rpcError) {
	kinds := argString(args, "kind")
	switch kinds {
	case "", "all", "build", "browser", "server":
	default:
		return ErrorResult("kind must be one of: all, build, browser, server"), nil
	}
	want := func(k string) bool { return kinds == "" || kinds == "all" || kinds == k }

	var items []errorItem
	result := map[string]any{}
	t, running := s.findDevServer(ctx)
	if running {
		snap, err := s.fetchDevState(ctx, t)
		if err != nil {
			return ErrorResult("dev server found but its state is unavailable: " + err.Error()), nil
		}
		result["source"] = "dev-server"
		result["url"] = t.Rec.URL
		result["buildOk"] = snap.BuildOK
		if want("build") {
			for _, d := range snap.Diagnostics {
				items = append(items, s.diagItem(d))
			}
		}
		if want("browser") {
			for _, ce := range snap.ClientErrors {
				items = append(items, errorItem{Kind: "browser", Severity: "error", Message: ce.Message,
					Stack: trimStack(ce.Stack), URL: ce.URL, Line: ce.Line, Col: ce.Col, Time: ce.Time.UTC().Format(time.RFC3339)})
			}
		}
		if want("server") {
			window := time.Duration(clampInt(args, "sinceMinutes", 15, 24*60)) * time.Minute
			if lines, err := s.tailDevLog(2000); err == nil {
				items = append(items, logErrorLines(lines, time.Now().Add(-window), 30)...)
			}
		}
	} else {
		// No live server: compile now so the answer still reflects the code on
		// disk. Browser and server errors need a running dev server.
		result["source"] = "build"
		result["running"] = false
		result["note"] = "no dev server is running; browser and runtime errors need `krate dev` (see dev_server). Showing the result of a fresh build."
		if want("build") {
			out, buildErr := s.runBuild(ctx)
			result["buildOk"] = buildErr == nil
			if buildErr != nil {
				ds := build.DiagnosticsFromError(buildErr)
				for _, d := range ds {
					items = append(items, s.diagItem(d))
				}
				if len(ds) == 0 {
					items = append(items, errorItem{Kind: "build", Severity: "error", Message: buildErr.Error()})
				}
				_ = out
			}
		}
	}
	for i := range items {
		items[i].Message = stripANSI(items[i].Message)
	}
	result["total"] = len(items)
	counts := map[string]int{}
	for _, it := range items {
		counts[it.Kind]++
	}
	result["counts"] = counts
	page, next, rerr := paginate(items, args, 25, 200)
	if rerr != nil {
		return ToolResult{}, rerr
	}
	result["errors"] = page
	result["nextCursor"] = next
	return JSONResult(result)
}

func trimStack(stack string) string {
	lines := strings.Split(strings.TrimSpace(stack), "\n")
	if len(lines) > 12 {
		lines = append(lines[:12], fmt.Sprintf("... %d more frames", len(lines)-12))
	}
	return strings.Join(lines, "\n")
}

type logLine struct {
	Time time.Time
	Text string
}

// tailDevLog returns the last n lines of the dev log with their timestamps
// split off (ANSI already removed by the writer).
func (s *Service) tailDevLog(n int) ([]logLine, error) {
	f, err := os.Open(build.DevLogPath(s.root))
	if err != nil {
		return nil, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	const window = 1 << 20
	cut := info.Size() > window
	if cut {
		if _, err := f.Seek(info.Size()-window, io.SeekStart); err != nil {
			return nil, err
		}
	}
	var lines []logLine
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64<<10), 1<<20)
	first := true
	for sc.Scan() {
		if cut && first {
			first = false
			continue // the first line is likely cut mid-way
		}
		first = false
		text := sc.Text()
		ln := logLine{Text: text}
		if len(text) > len(build.DevLogTimeFormat) {
			if t, err := time.Parse(build.DevLogTimeFormat, text[:len(build.DevLogTimeFormat)]); err == nil {
				ln.Time, ln.Text = t, text[len(build.DevLogTimeFormat)+1:]
			}
		}
		lines = append(lines, ln)
		if len(lines) > n*2 {
			lines = lines[len(lines)-n:]
		}
	}
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return lines, nil
}

func (s *Service) toolGetLogs(ctx context.Context, args map[string]any) (ToolResult, *rpcError) {
	n := clampInt(args, "lines", 100, 1000)
	grep := strings.ToLower(argString(args, "grep"))
	level := argString(args, "level")
	switch level {
	case "", "all", "error", "warn":
	default:
		return ErrorResult("level must be one of: all, error, warn"), nil
	}
	var since time.Time
	if v := argString(args, "since"); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			since = time.Now().Add(-d)
		} else if t, err := time.Parse(time.RFC3339, v); err == nil {
			since = t
		} else {
			return ErrorResult("since must be a duration such as 10m or 1h, or an RFC 3339 time such as 2026-11-07T10:00:00Z"), nil
		}
	}
	lines, err := s.tailDevLog(2000)
	if err != nil {
		if os.IsNotExist(err) {
			return ErrorResult("no dev log yet (.krate/dev.log); start the dev server with the dev_server tool or `krate dev`"), nil
		}
		return ErrorResult("reading dev log: " + err.Error()), nil
	}
	var kept []string
	for _, l := range lines {
		if !since.IsZero() && !l.Time.IsZero() && l.Time.Before(since) {
			continue
		}
		low := strings.ToLower(l.Text)
		if grep != "" && !strings.Contains(low, grep) {
			continue
		}
		switch level {
		case "error":
			if !(strings.Contains(low, "error") || strings.Contains(low, "panic") || strings.Contains(low, "[browser]")) {
				continue
			}
		case "warn":
			if !strings.Contains(low, "warn") {
				continue
			}
		}
		if l.Time.IsZero() {
			kept = append(kept, l.Text)
		} else {
			kept = append(kept, l.Time.Format(time.RFC3339)+" "+l.Text)
		}
	}
	truncated := false
	if len(kept) > n {
		kept = kept[len(kept)-n:]
		truncated = true
	}
	text, cut := truncateText(strings.Join(kept, "\n"), 128<<10)
	return JSONResult(map[string]any{
		"file":      ".krate/dev.log",
		"returned":  len(kept),
		"truncated": truncated || cut,
		"lines":     logLines(text),
	})
}

// logLines splits text into lines; empty text has none.
func logLines(text string) []string {
	if text == "" {
		return []string{}
	}
	return strings.Split(text, "\n")
}

func (s *Service) toolDevServer(ctx context.Context, args map[string]any) (ToolResult, *rpcError) {
	action := argString(args, "action")
	switch action {
	case "start", "stop", "restart":
	default:
		return ErrorResult("action must be one of: start, stop, restart"), nil
	}
	if r := s.denyIfReadOnly("dev_server"); r != nil {
		return *r, nil
	}
	if !s.taskAllowed("dev") {
		return ErrorResult(`dev_server runs project code (krate.config, API routes, middleware); add "dev" to allowTasks in .krate/mcp.json to enable it`), nil
	}
	apply := argBool(args, "apply", false)
	exe, cmdArgs, err := devCommand(s.root)
	if err != nil {
		return ErrorResult("locating the krate executable: " + err.Error()), nil
	}
	display := "krate dev ."
	if !apply {
		return JSONResult(map[string]any{
			"dryRun": true,
			"action": action,
			"would":  describeDevAction(action, display),
			"hint":   "pass apply: true to run it",
		})
	}

	m := &s.dev
	m.mu.Lock()
	defer m.mu.Unlock()
	managedRunning := m.cmd != nil && !isDone(m.done)

	if action == "stop" || action == "restart" {
		if managedRunning {
			stopManaged(m)
			managedRunning = false
		} else if _, ext := s.findDevServer(ctx); ext {
			return ErrorResult("the running dev server was not started by this tool; stop it in its own terminal"), nil
		} else if action == "stop" {
			return JSONResult(map[string]any{"stopped": false, "note": "no dev server is running"})
		}
		if action == "stop" {
			return JSONResult(map[string]any{"stopped": true})
		}
	}
	if managedRunning {
		return JSONResult(map[string]any{"started": false, "note": "the dev server started by this tool is already running", "pid": m.cmd.Process.Pid})
	}
	if _, ext := s.findDevServer(ctx); ext {
		return ErrorResult("a dev server is already running (started outside this tool); use it, or stop it first"), nil
	}

	tail := &tailBuffer{}
	cmd := execx.Command(context.Background(), exe, cmdArgs...)
	cmd.Dir = s.root
	cmd.Env = os.Environ()
	cmd.Stdout = tail
	cmd.Stderr = tail
	if err := cmd.Start(); err != nil {
		return ErrorResult("starting dev server: " + err.Error()), nil
	}
	done := make(chan struct{})
	m.cmd, m.done, m.tail, m.startedAt = cmd, done, tail, time.Now()
	m.exitErr.Store(errBox{})
	go func() {
		err := cmd.Wait()
		m.exitErr.Store(errBox{err})
		close(done)
	}()
	s.logMessage("info", "started dev server")

	// Wait for the discovery record so the caller gets the URL.
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case <-done:
			return ErrorResult("dev server exited during startup:\n" + strings.TrimSpace(tail.String())), nil
		case <-ctx.Done():
			return ErrorResult("cancelled while waiting for the dev server"), nil
		case <-time.After(150 * time.Millisecond):
		}
		if rec, ok := build.ReadDevServerRecord(s.root); ok && rec.PID == cmd.Process.Pid {
			return JSONResult(map[string]any{"started": true, "pid": cmd.Process.Pid, "url": rec.URL, "logFile": ".krate/dev.log"})
		}
	}
	return JSONResult(map[string]any{
		"started": true, "pid": cmd.Process.Pid, "ready": false,
		"note": "the process is running but has not reported its URL yet (the first build can be slow); poll dev_server_status",
	})
}

func describeDevAction(action, display string) string {
	switch action {
	case "start":
		return "start `" + display + "` as a child of this MCP server and wait for its URL"
	case "stop":
		return "stop the dev server this tool started"
	default:
		return "stop the dev server this tool started, then start `" + display + "` again"
	}
}

func isDone(ch chan struct{}) bool {
	if ch == nil {
		return true
	}
	select {
	case <-ch:
		return true
	default:
		return false
	}
}

// stopManaged terminates the child and its process tree. The caller holds m.mu.
func stopManaged(m *devManager) {
	if m.cmd == nil || m.cmd.Process == nil {
		return
	}
	done := m.done
	m.mu.Unlock()
	defer m.mu.Lock()
	// Ask first: the server stops its sidecars and removes its discovery record
	// on a signal. Windows has no interrupt to send, so it goes straight to kill.
	if m.cmd.Process.Signal(os.Interrupt) == nil {
		select {
		case <-done:
			return
		case <-time.After(3 * time.Second):
		}
	}
	execx.KillTree(m.cmd)
	select {
	case <-done:
	case <-time.After(5 * time.Second):
	}
}

// Close stops a dev server started by this service. Called when the MCP
// server shuts down so it never leaves an orphaned dev process.
func (s *Service) Close() {
	m := &s.dev
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.cmd != nil && !isDone(m.done) {
		stopManaged(m)
	}
}

func (s *Service) registerDevTools(srv *Server) {
	s.reg(srv, Tool{
		Name: "dev_server_status",
		Description: "Report whether `krate dev` is running for this project: URL, pid, whether the last build passed, and how many build and browser errors are pending. " +
			"Call this first when debugging the running app; start one with dev_server.",
		InputSchema: objSchema(nil),
		Annotations: readOnlyAnnotations("Dev Server Status"),
		Handler:     s.toolDevServerStatus,
	})
	s.reg(srv, Tool{
		Name: "get_errors",
		Description: "Get the current errors of the running app: build errors with KR codes, file:line:col and hints, runtime errors reported by open browser tabs (with stack), and server-side errors from the dev log. " +
			"Reads the live `krate dev` process; without one it runs a fresh build and reports build errors only. Use explain_diagnostic for a KR code.",
		InputSchema: objSchema(map[string]any{
			"kind":         enumSchema("Which errors to return (default all)", "all", "build", "browser", "server"),
			"sinceMinutes": numSchema("How far back server-log errors are read (default 15)"),
			"limit":        numSchema("Maximum errors per page (default 25, max 200)"),
			"cursor":       strSchema("Opaque cursor from a previous get_errors result"),
		}),
		Annotations: rebuildToolAnnotations("Get Errors"),
		Handler:     s.toolGetErrors,
	})
	s.reg(srv, Tool{
		Name: "get_logs",
		Description: "Read the tail of the dev server log (.krate/dev.log): build output, requests, API and SSR errors, and browser errors echoed to the terminal. " +
			"Filter with grep or level. Secrets are redacted. Needs a dev server started with `krate dev` or dev_server.",
		InputSchema: objSchema(map[string]any{
			"lines": numSchema("Number of trailing lines (default 100, max 1000)"),
			"grep":  strSchema("Case-insensitive substring filter"),
			"level": enumSchema("Only error or warn lines (default all)", "all", "error", "warn"),
			"since": strSchema("Only lines at or after this RFC 3339 time"),
		}),
		Annotations: readOnlyAnnotations("Get Logs"),
		Handler:     s.toolGetLogs,
	})
	s.reg(srv, Tool{
		Name: "dev_server",
		Description: "Start, stop or restart `krate dev` as a child of this MCP server so get_errors, get_logs and browser checks see a live app. " +
			"Dry-run by default (apply: true runs it). Runs project code, so it needs \"dev\" in allowTasks and is disabled by readOnlyMode. Only stops a server that this tool started.",
		InputSchema: objSchema(map[string]any{
			"action": enumSchema("What to do", "start", "stop", "restart"),
			"apply":  boolSchema("Actually run the action (default false: describe it)"),
		}, "action"),
		Annotations: additiveToolAnnotations("Manage Dev Server"),
		Handler:     s.toolDevServer,
	})
}
