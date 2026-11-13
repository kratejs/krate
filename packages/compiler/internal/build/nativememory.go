package build

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"
)

// Memory measurement of a native app. The numbers come from the shell's
// `memory` dev control command (packages/native/memstat): PSS and private
// dirty bytes per process role. The app is measured next to a floor: a blank
// one-line page in the same shell and window size, with no host runtimes.
// Whatever the floor costs is WebKit (or the platform webview), not Krate, so
// app minus floor is the share an app can change and the one budgets.json
// limits.

// NativeMemoryRole is the memory of one role, summed over its processes.
type NativeMemoryRole struct {
	PSS          uint64 `json:"pss"`
	PrivateDirty uint64 `json:"privateDirty"`
}

// NativeMemoryDelta is app minus floor; either can be negative.
type NativeMemoryDelta struct {
	PSS          int64 `json:"pss"`
	PrivateDirty int64 `json:"privateDirty"`
}

// NativeMemoryPoint is the memory of every role at one moment.
type NativeMemoryPoint struct {
	Roles map[string]NativeMemoryRole `json:"roles"`
	Total NativeMemoryRole            `json:"total"`
}

// NativeMemoryDiff is a NativeMemoryPoint of deltas.
type NativeMemoryDiff struct {
	Roles map[string]NativeMemoryDelta `json:"roles"`
	Total NativeMemoryDelta            `json:"total"`
}

// NativeMemorySample is one measurement: the app, the floor sampled at the
// same step, and their difference.
type NativeMemorySample struct {
	// Name is afterLoad, afterIdle, afterStorm or hidden.
	Name          string             `json:"name"`
	App           NativeMemoryPoint  `json:"app"`
	Floor         *NativeMemoryPoint `json:"floor,omitempty"`
	AppMinusFloor *NativeMemoryDiff  `json:"appMinusFloor,omitempty"`
}

// NativeMemoryReport is the "memory" section of dist-native/report.json.
type NativeMemoryReport struct {
	OS string `json:"os"`
	// Source says what PSS and privateDirty mean on this OS (see memstat).
	Source string `json:"source"`
	Window struct {
		Width  int `json:"width"`
		Height int `json:"height"`
	} `json:"window"`
	Samples []NativeMemorySample `json:"samples"`
	// Notes list what was skipped or could not be attributed.
	Notes []string `json:"notes,omitempty"`
}

// Sample returns the sample called name, or nil.
func (r *NativeMemoryReport) Sample(name string) *NativeMemorySample {
	for i := range r.Samples {
		if r.Samples[i].Name == name {
			return &r.Samples[i]
		}
	}
	return nil
}

// Names of the samples.
const (
	NativeMemAfterLoad  = "afterLoad"
	NativeMemAfterIdle  = "afterIdle"
	NativeMemAfterStorm = "afterStorm"
	NativeMemHidden     = "hidden"
)

// shellMemoryReport is the part of memstat.Report (packages/native/memstat)
// the compiler reads from the shell's memory command. The compiler does not
// import memstat so that its darwin and windows builds need no extra modules.
type shellMemoryReport struct {
	OS     string `json:"os"`
	Source string `json:"source"`
	Procs  []struct {
		Role         string `json:"role"`
		PSS          uint64 `json:"pss"`
		PrivateDirty uint64 `json:"privateDirty"`
	} `json:"procs"`
	Notes []string `json:"notes"`
}

func memPointOf(r *shellMemoryReport) NativeMemoryPoint {
	p := NativeMemoryPoint{Roles: map[string]NativeMemoryRole{}}
	for _, pr := range r.Procs {
		a := p.Roles[pr.Role]
		a.PSS += pr.PSS
		a.PrivateDirty += pr.PrivateDirty
		p.Roles[pr.Role] = a
		p.Total.PSS += pr.PSS
		p.Total.PrivateDirty += pr.PrivateDirty
	}
	return p
}

// MemoryMinus returns a - b per role (a role missing on one side counts 0)
// and in total.
func MemoryMinus(a, b NativeMemoryPoint) NativeMemoryDiff {
	d := NativeMemoryDiff{Roles: map[string]NativeMemoryDelta{}}
	sub := func(x, y NativeMemoryRole) NativeMemoryDelta {
		return NativeMemoryDelta{PSS: int64(x.PSS) - int64(y.PSS), PrivateDirty: int64(x.PrivateDirty) - int64(y.PrivateDirty)}
	}
	for role, x := range a.Roles {
		d.Roles[role] = sub(x, b.Roles[role])
	}
	for role, y := range b.Roles {
		if _, ok := a.Roles[role]; !ok {
			d.Roles[role] = sub(NativeMemoryRole{}, y)
		}
	}
	d.Total = sub(a.Total, b.Total)
	return d
}

// WriteNativeFloorApp writes the app directory of the floor run into dir: one
// line of HTML, no host code, a window of the given size.
func WriteNativeFloorApp(dir string, width, height int) error {
	if width <= 0 {
		width = 1024
	}
	if height <= 0 {
		height = 768
	}
	man := map[string]any{
		"appId": "dev.krate.memfloor", "productName": "Krate memory floor", "version": "0.0.0",
		"window": map[string]any{"title": "floor", "width": width, "height": height, "resizable": true, "path": "/"},
		"assets": "dist",
	}
	data, err := json.MarshalIndent(man, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Join(dir, "dist"), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, "dist", "index.html"), []byte("<!doctype html><title>floor</title><p>floor</p>\n"), 0o644); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "app.json"), append(data, '\n'), 0o644)
}

// NativeStaticRoutes lists the prerendered pages of an app directory as
// routes ("/", "/tasks/"): every index.html or name.html under the assets
// directory whose path has no dynamic segment.
func NativeStaticRoutes(appDir string) ([]string, error) {
	assets := "dist"
	if data, err := os.ReadFile(filepath.Join(appDir, "app.json")); err == nil {
		var m struct {
			Assets string `json:"assets"`
		}
		if json.Unmarshal(data, &m) == nil && m.Assets != "" {
			assets = m.Assets
		}
	}
	root := filepath.Join(appDir, filepath.FromSlash(assets))
	var routes []string
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(p, ".html") {
			return err
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if strings.ContainsAny(rel, "[]") || rel == "404.html" {
			return nil
		}
		switch {
		case rel == "index.html":
			routes = append(routes, "/")
		case strings.HasSuffix(rel, "/index.html"):
			routes = append(routes, "/"+strings.TrimSuffix(rel, "index.html"))
		default:
			routes = append(routes, "/"+strings.TrimSuffix(rel, ".html"))
		}
		return nil
	})
	sort.Strings(routes)
	return routes, err
}

// nativeProc is a running shell (or release executable built with the
// krate_devcontrol tag) driven through its dev control channel.
type nativeProc struct {
	cmd   *exec.Cmd
	stdin io.WriteCloser
	lines chan string
	done  chan struct{}
}

func startNativeProc(path string, args []string, stderr io.Writer) (*nativeProc, error) {
	cmd := exec.Command(path, append([]string{"--dev"}, args...)...)
	cmd.Stderr = stderr
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	p := &nativeProc{cmd: cmd, stdin: stdin, lines: make(chan string, 64), done: make(chan struct{})}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("starting %s: %w", path, err)
	}
	go func() {
		sc := bufio.NewScanner(stdout)
		sc.Buffer(make([]byte, 64*1024), 16<<20)
		for sc.Scan() {
			p.lines <- sc.Text()
		}
		close(p.lines)
	}()
	go func() { _ = cmd.Wait(); close(p.done) }()
	return p, nil
}

func (p *nativeProc) next(ctx context.Context, timeout time.Duration) (string, error) {
	select {
	case l, ok := <-p.lines:
		if !ok {
			return "", errors.New("the app closed its control channel (did it exit?)")
		}
		return l, nil
	case <-time.After(timeout):
		return "", fmt.Errorf("no answer from the app within %s", timeout)
	case <-ctx.Done():
		return "", ctx.Err()
	}
}

func (p *nativeProc) waitReady(ctx context.Context, timeout time.Duration) error {
	for {
		l, err := p.next(ctx, timeout)
		if err != nil {
			return err
		}
		switch {
		case l == "ready":
			return nil
		case strings.HasPrefix(l, "url "):
		default:
			return fmt.Errorf("unexpected line before ready: %q (is this a --dev build? release executables need the krate_devcontrol tag)", l)
		}
	}
}

// send runs one control command and returns the text after "ok".
func (p *nativeProc) send(ctx context.Context, cmd string, timeout time.Duration) (string, error) {
	if _, err := fmt.Fprintln(p.stdin, cmd); err != nil {
		return "", err
	}
	l, err := p.next(ctx, timeout)
	if err != nil {
		return "", err
	}
	switch {
	case l == "ok":
		return "", nil
	case strings.HasPrefix(l, "ok "):
		return strings.TrimPrefix(l, "ok "), nil
	case strings.HasPrefix(l, "error "):
		return "", errors.New(strings.TrimPrefix(l, "error "))
	}
	return "", fmt.Errorf("unexpected reply %q", l)
}

func (p *nativeProc) query(ctx context.Context, js string, v any) error {
	js = strings.Join(strings.Fields(strings.ReplaceAll(js, "\n", " ")), " ")
	out, err := p.send(ctx, "query "+js, 60*time.Second)
	if err != nil || v == nil {
		return err
	}
	return json.Unmarshal([]byte(out), v)
}

func (p *nativeProc) stop() {
	select {
	case <-p.done:
		return
	default:
	}
	_, _ = fmt.Fprintln(p.stdin, "quit")
	_ = p.stdin.Close()
	select {
	case <-p.done:
	case <-time.After(8 * time.Second):
		_ = p.cmd.Process.Kill()
		<-p.done
	}
}

func (p *nativeProc) memory(ctx context.Context) (*shellMemoryReport, error) {
	out, err := p.send(ctx, "memory", 30*time.Second)
	if err != nil {
		return nil, err
	}
	var r shellMemoryReport
	if err := json.Unmarshal([]byte(out), &r); err != nil {
		return nil, fmt.Errorf("unreadable memory report: %w", err)
	}
	return &r, nil
}

// waitLoaded blocks until the page's load event has finished.
func (p *nativeProc) waitLoaded(ctx context.Context) error {
	deadline := time.Now().Add(60 * time.Second)
	var last error
	for time.Now().Before(deadline) {
		var done bool
		err := p.query(ctx, `(() => { const n = performance.getEntriesByType("navigation")[0]; return !!n && n.loadEventEnd > 0; })()`, &done)
		if err == nil && done {
			return nil
		}
		last = err
		if ctx.Err() != nil {
			return ctx.Err()
		}
		time.Sleep(100 * time.Millisecond)
	}
	return fmt.Errorf("no load event within 60s (last error: %v)", last)
}

// navigate loads route in the window and waits for it to finish loading.
func (p *nativeProc) navigate(ctx context.Context, route string) error {
	if _, err := p.send(ctx, fmt.Sprintf("eval location.assign(%s)", memJSString(route)), 10*time.Second); err != nil {
		return err
	}
	want := memJSString(route)
	// A query that races the unload of the old document may never be
	// answered (the shell's README says so), so let the navigation start.
	if err := sleepCtx(ctx, 500*time.Millisecond); err != nil {
		return err
	}
	deadline := time.Now().Add(30 * time.Second)
	var last error
	for time.Now().Before(deadline) {
		var ok bool
		err := p.query(ctx, fmt.Sprintf(`(() => { const n = performance.getEntriesByType("navigation")[0]; return document.readyState === "complete" && !!n && n.loadEventEnd > 0 && new URL(%s, location.href).pathname === location.pathname; })()`, want), &ok)
		if err == nil && ok {
			return nil
		}
		last = err
		if ctx.Err() != nil {
			return ctx.Err()
		}
		time.Sleep(50 * time.Millisecond)
	}
	return fmt.Errorf("%s did not finish loading within 30s (last error: %v)", route, last)
}

func memJSString(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

// hideScript calls the SDK's window.hide (or show) for the main window through
// the page's own call endpoint. It answers "ok" or the error the call gave.
func hideScript(id string) string {
	return fmt.Sprintf(`(async () => {
  const j = new TextEncoder().encode(JSON.stringify({calls: [{id: 1, fn: %s, args: ["main"]}]}));
  const out = new Uint8Array(4 + j.length + 2);
  new DataView(out.buffer).setUint32(0, j.length);
  out.set(j, 4);
  const r = await fetch("/__krate/call", {method: "POST", body: out});
  const b = new Uint8Array(await r.arrayBuffer());
  const n = new DataView(b.buffer).getUint32(0);
  const res = JSON.parse(new TextDecoder().decode(b.slice(4, 4 + n)));
  return res.err ? JSON.stringify(res.err) : "ok";
})()`, memJSString(id))
}

// NativeMemoryProgram is a program to measure and the arguments after --dev.
type NativeMemoryProgram struct {
	Path string
	Args []string
}

// NativeMemoryOptions tune MeasureNativeMemory.
type NativeMemoryOptions struct {
	// App is the app: a release executable built with the krate_devcontrol
	// tag, or the shell with the app directory as its argument.
	App NativeMemoryProgram
	// Floor is the shell with the floor app directory as its argument. With
	// no Path there is no floor run and the samples carry no delta.
	Floor NativeMemoryProgram
	// WindowWidth and WindowHeight are recorded in the report.
	WindowWidth, WindowHeight int
	// Routes are the pages the navigation storm visits; none skips the storm
	// (with a note unless NoStorm is set).
	Routes  []string
	NoStorm bool
	// Rounds is how many times the storm visits each route (default 3).
	Rounds int
	// Settle is the idle time after the load event before the first sample
	// (default 5s). Idle is the further wait before the afterIdle sample; 0
	// skips that sample.
	Settle, Idle time.Duration
	// Pause is the wait after the storm and after hiding the window before
	// sampling (default 3s).
	Pause time.Duration
	// Hide adds the hidden-window sample.
	Hide bool
	// Log receives progress lines, Stderr the app's own output.
	Log    io.Writer
	Stderr io.Writer
	Ctx    context.Context
}

func sleepCtx(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return nil
	}
	select {
	case <-time.After(d):
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

type memRun struct {
	source string
	points []namedPoint
	notes  []string
}

type namedPoint struct {
	name  string
	point NativeMemoryPoint
}

// MeasureNativeMemory runs the app and then the floor, taking the same
// samples of each, and returns the report with deltas.
func MeasureNativeMemory(opts NativeMemoryOptions) (*NativeMemoryReport, error) {
	ctx := opts.Ctx
	if ctx == nil {
		ctx = context.Background()
	}
	if opts.Settle <= 0 {
		opts.Settle = 5 * time.Second
	}
	if opts.Rounds <= 0 {
		opts.Rounds = 3
	}
	if opts.Pause <= 0 {
		opts.Pause = 3 * time.Second
	}
	logf := func(format string, a ...any) {
		if opts.Log != nil {
			fmt.Fprintf(opts.Log, format+"\n", a...)
		}
	}
	rep := &NativeMemoryReport{OS: runtime.GOOS}
	rep.Window.Width, rep.Window.Height = opts.WindowWidth, opts.WindowHeight

	logf("measuring the app")
	app, err := runMemory(ctx, opts, opts.App, opts.Routes, logf)
	if err != nil {
		return nil, fmt.Errorf("measuring the app: %w", err)
	}
	rep.Source = app.source
	rep.Notes = append(rep.Notes, app.notes...)

	var floor *memRun
	if opts.Floor.Path != "" {
		logf("measuring the floor (a blank page in the same shell)")
		floor, err = runMemory(ctx, opts, opts.Floor, floorRoutes(opts.Routes), logf)
		if err != nil {
			rep.Notes = append(rep.Notes, "the floor run failed: "+err.Error())
			floor = nil
		} else {
			for _, n := range floor.notes {
				rep.Notes = append(rep.Notes, "floor: "+n)
			}
		}
	} else {
		rep.Notes = append(rep.Notes, "no floor run: no shell binary was available")
	}
	for _, ap := range app.points {
		s := NativeMemorySample{Name: ap.name, App: ap.point}
		if floor != nil {
			for _, fp := range floor.points {
				if fp.name == ap.name {
					f := fp.point
					d := MemoryMinus(ap.point, f)
					s.Floor, s.AppMinusFloor = &f, &d
				}
			}
		}
		rep.Samples = append(rep.Samples, s)
	}
	return rep, nil
}

// floorRoutes makes the floor visit its one page as often as the app visits
// its routes, so both processes reload equally often during the storm.
func floorRoutes(routes []string) []string {
	out := make([]string, len(routes))
	for i := range out {
		out[i] = "/"
	}
	return out
}

func runMemory(ctx context.Context, opts NativeMemoryOptions, prog NativeMemoryProgram, routes []string, logf func(string, ...any)) (*memRun, error) {
	p, err := startNativeProc(prog.Path, prog.Args, opts.Stderr)
	if err != nil {
		return nil, err
	}
	defer p.stop()
	if err := p.waitReady(ctx, 60*time.Second); err != nil {
		return nil, err
	}
	if err := p.waitLoaded(ctx); err != nil {
		return nil, err
	}
	run := &memRun{}
	take := func(name string) error {
		r, err := p.memory(ctx)
		if err != nil {
			return fmt.Errorf("sampling %s: %w", name, err)
		}
		run.points = append(run.points, namedPoint{name, memPointOf(r)})
		run.source = r.Source
		for _, n := range r.Notes {
			if !containsString(run.notes, n) {
				run.notes = append(run.notes, n)
			}
		}
		logf("  sampled %s", name)
		return nil
	}
	if err := sleepCtx(ctx, opts.Settle); err != nil {
		return nil, err
	}
	if err := take(NativeMemAfterLoad); err != nil {
		return nil, err
	}
	if opts.Idle > 0 {
		if err := sleepCtx(ctx, opts.Idle); err != nil {
			return nil, err
		}
		if err := take(NativeMemAfterIdle); err != nil {
			return nil, err
		}
	}
	if len(routes) > 0 {
		for round := 0; round < opts.Rounds; round++ {
			for _, r := range routes {
				if err := p.navigate(ctx, r); err != nil {
					return nil, err
				}
			}
		}
		if err := p.navigate(ctx, routes[0]); err != nil {
			return nil, err
		}
		if err := sleepCtx(ctx, opts.Pause); err != nil {
			return nil, err
		}
		if err := take(NativeMemAfterStorm); err != nil {
			return nil, err
		}
	} else if !opts.NoStorm {
		run.notes = append(run.notes, "no prerendered routes: the navigation storm was skipped")
	}
	if opts.Hide {
		var res string
		if err := p.query(ctx, hideScript("krate/window.hide"), &res); err != nil || res != "ok" {
			if err == nil {
				err = errors.New(res)
			}
			why := "window.hide failed: " + err.Error()
			if strings.Contains(why, "not_found") {
				why = "the app does not link the window SDK area (nothing in it imports @krate/app/window), so krate/window.hide does not exist"
			}
			run.notes = append(run.notes, "the hidden-window sample was skipped: "+why)
		} else {
			if err := sleepCtx(ctx, opts.Pause); err != nil {
				return nil, err
			}
			if err := take(NativeMemHidden); err != nil {
				return nil, err
			}
		}
	}
	return run, nil
}

func containsString(s []string, v string) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}

// FormatNativeMemory renders the report as text tables, one per sample, in MB.
func FormatNativeMemory(r *NativeMemoryReport) string {
	var sb strings.Builder
	mb := func(v uint64) string { return fmt.Sprintf("%.1f", float64(v)/(1<<20)) }
	smb := func(v int64) string { return fmt.Sprintf("%+.1f", float64(v)/(1<<20)) }
	fmt.Fprintf(&sb, "memory (%s, %s; MB; pss / private dirty)\n", r.OS, r.Source)
	for _, s := range r.Samples {
		roles := map[string]bool{}
		for k := range s.App.Roles {
			roles[k] = true
		}
		if s.Floor != nil {
			for k := range s.Floor.Roles {
				roles[k] = true
			}
		}
		names := make([]string, 0, len(roles))
		for k := range roles {
			names = append(names, k)
		}
		sort.Strings(names)
		fmt.Fprintf(&sb, "\n  %s\n", s.Name)
		if s.Floor == nil {
			fmt.Fprintf(&sb, "    %-18s %16s\n", "role", "app")
		} else {
			fmt.Fprintf(&sb, "    %-18s %16s %16s %16s\n", "role", "app", "floor", "app - floor")
		}
		row := func(label string, a NativeMemoryRole, f *NativeMemoryRole, d *NativeMemoryDelta) {
			line := fmt.Sprintf("    %-18s %16s", label, mb(a.PSS)+" / "+mb(a.PrivateDirty))
			if f != nil && d != nil {
				line += fmt.Sprintf(" %16s %16s", mb(f.PSS)+" / "+mb(f.PrivateDirty), smb(d.PSS)+" / "+smb(d.PrivateDirty))
			}
			sb.WriteString(line + "\n")
		}
		for _, n := range names {
			a := s.App.Roles[n]
			if s.Floor == nil {
				row(n, a, nil, nil)
				continue
			}
			f := s.Floor.Roles[n]
			d := s.AppMinusFloor.Roles[n]
			row(n, a, &f, &d)
		}
		if s.Floor == nil {
			row("total", s.App.Total, nil, nil)
		} else {
			row("total", s.App.Total, &s.Floor.Total, &s.AppMinusFloor.Total)
		}
	}
	for _, n := range r.Notes {
		fmt.Fprintf(&sb, "\n  note: %s", n)
	}
	if len(r.Notes) > 0 {
		sb.WriteString("\n")
	}
	return sb.String()
}

// WriteNativeMemory stores r as the "memory" section of outDir/report.json,
// keeping every other key of the file.
func WriteNativeMemory(outDir string, r *NativeMemoryReport) error {
	path := filepath.Join(outDir, "report.json")
	doc := map[string]json.RawMessage{}
	if data, err := os.ReadFile(path); err == nil {
		if err := json.Unmarshal(data, &doc); err != nil {
			return fmt.Errorf("%s is not JSON: %w", path, err)
		}
	} else if !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	mem, err := json.Marshal(r)
	if err != nil {
		return err
	}
	doc["memory"] = mem
	data, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o644)
}

// NativeMemoryProfileOptions tune MemoryNative.
type NativeMemoryProfileOptions struct {
	// OutDir is the native output directory (default dist-native).
	OutDir string
	// Shell is the shell binary (default: the one `krate native dev` uses).
	Shell string
	// Idle is the wait of the afterIdle sample; 0 skips that sample.
	Idle time.Duration
	// Settle is the wait after the load event before the first sample (default 5s).
	Settle time.Duration
	// Log and Stderr as in NativeMemoryOptions.
	Log    io.Writer
	Stderr io.Writer
	Ctx    context.Context
}

// MemoryNative builds the app (a release executable with the dev control
// channel when Go is available, the app directory otherwise), measures its
// memory against the floor and writes the "memory" section of report.json.
func (b *Builder) MemoryNative(opts NativeMemoryProfileOptions) (*NativeMemoryReport, error) {
	rel, err := b.BuildNativeRelease(NativeReleaseOptions{OutDir: opts.OutDir, ExtraTags: []string{"krate_devcontrol"}})
	if err != nil {
		return nil, err
	}
	mo := NativeMemoryOptions{
		WindowWidth: rel.Manifest.Window.Width, WindowHeight: rel.Manifest.Window.Height,
		Settle: opts.Settle, Idle: opts.Idle, Hide: true,
		Log: opts.Log, Stderr: opts.Stderr, Ctx: opts.Ctx,
	}
	shell, shellErr := LocateNativeShell(b.Root, opts.Shell)
	if rel.Compiled {
		mo.App = NativeMemoryProgram{Path: rel.Binaries[0].Path}
	} else {
		if shellErr != nil {
			return nil, fmt.Errorf("no program to measure: %w (install Go to build a release executable, or pass --shell <path>)", shellErr)
		}
		mo.App = NativeMemoryProgram{Path: shell, Args: []string{rel.AppDir}}
	}
	appDir := rel.AppDir
	if appDir == "" {
		// A compiled release embeds the app staged here.
		appDir = filepath.Join(b.nativeOutDir(), "release", "stage", "app")
	}
	if routes, err := NativeStaticRoutes(appDir); err == nil {
		mo.Routes = routes
	}
	if shellErr == nil {
		tmp := filepath.Join(b.nativeOutDir(), "memfloor")
		_ = os.RemoveAll(tmp)
		defer os.RemoveAll(tmp)
		if err := WriteNativeFloorApp(tmp, mo.WindowWidth, mo.WindowHeight); err != nil {
			return nil, err
		}
		mo.Floor = NativeMemoryProgram{Path: shell, Args: []string{tmp}}
	}
	rep, err := MeasureNativeMemory(mo)
	if err != nil {
		return nil, err
	}
	if err := WriteNativeMemory(rel.OutDir, rep); err != nil {
		return nil, err
	}
	return rep, nil
}
