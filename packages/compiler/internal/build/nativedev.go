package build

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/kratejs/krate/packages/compiler/internal/config"
	"github.com/kratejs/krate/packages/compiler/internal/diag"
	"github.com/kratejs/krate/packages/compiler/internal/native"
	"github.com/kratejs/krate/packages/compiler/internal/native/runtimes"
)

// NativeChangeClassifier decides what a changed file means for a running
// `krate native dev` session.
type NativeChangeClassifier struct {
	Root string
	// HostDeps are the absolute files the TypeScript host bundle was built from.
	HostDeps map[string]bool
	// WholeHost are modules that are entirely host code (no UI in them).
	WholeHost map[string]bool
	// IsUIGo reports a .go file the UI build consumes (a Go component).
	IsUIGo func(path string) bool
}

// NativePlan is what a batch of changed files requires.
type NativePlan struct {
	// Config means a config or env file changed: rebuild everything and
	// restart the shell.
	Config bool
	// GoHost means Go sources changed: regenerate glue, rebuild the Go host
	// and restart it.
	GoHost bool
	// HostJS means TypeScript host sources changed: rebuild the host bundle
	// and reload it.
	HostJS bool
	// UI lists the files for the incremental UI rebuild.
	UI []string
}

// PlanNativeChanges classifies a batch of changed files.
func PlanNativeChanges(c NativeChangeClassifier, changed []string) NativePlan {
	var plan NativePlan
	for _, p := range changed {
		base := filepath.Base(p)
		switch {
		case restartTrigger(c.Root, p) != "":
			plan.Config = true
		case filepath.Dir(p) == filepath.Clean(c.Root) && (base == "go.mod" || base == "go.sum"):
			plan.GoHost = true
		case strings.HasSuffix(base, ".go"):
			if strings.HasSuffix(base, "_test.go") {
				continue
			}
			plan.GoHost = true
			if c.IsUIGo != nil && c.IsUIGo(p) {
				plan.UI = append(plan.UI, p)
			}
		case c.WholeHost[p]:
			plan.HostJS = true
		case c.HostDeps[p]:
			plan.HostJS = true
			plan.UI = append(plan.UI, p)
		default:
			plan.UI = append(plan.UI, p)
		}
	}
	return plan
}

func isScriptFile(p string) bool {
	switch strings.ToLower(filepath.Ext(p)) {
	case ".ts", ".tsx", ".js", ".jsx", ".mjs", ".mts":
		return true
	}
	return false
}

// NativeHMRMessage converts a hub event to the message the shell relays on
// /__krate/hmr. It mirrors what the SSE endpoint of `krate dev` sends.
func NativeHMRMessage(ev DevEvent) []byte {
	var msg map[string]any
	switch {
	case ev.Type == "client-error" && ev.ClientError != nil:
		msg = map[string]any{"type": "client-error", "clientError": ev.ClientError}
	case len(ev.Diagnostics) > 0:
		msg = map[string]any{"type": "build-error", "diagnostics": ev.Diagnostics, "buildOk": false}
	case len(ev.Routes) > 0:
		msg = map[string]any{"type": "reload", "pages": ev.Routes, "cssOnly": ev.CSSOnly}
	default:
		msg = map[string]any{"type": "reload", "cssOnly": ev.CSSOnly}
	}
	data, _ := json.Marshal(msg)
	return data
}

// LocateNativeShell finds the shell binary for a project: the explicit path,
// $KRATE_SHELL, or (inside a Krate checkout) a build of packages/native/cmd/
// krate-shell kept in <root>/.krate/native/bin and rebuilt when its sources
// are newer. Released @krate/core packages will ship a prebuilt shell; until
// then outside a checkout the shell must be given explicitly.
func LocateNativeShell(root, override string) (string, error) {
	if override == "" || override == "auto" {
		override = os.Getenv("KRATE_SHELL")
	}
	if override != "" && override != "auto" {
		if st, err := os.Stat(override); err != nil || st.IsDir() {
			return "", fmt.Errorf("the shell binary %s does not exist", override)
		}
		return override, nil
	}
	mod := FindMonorepoNative(root)
	if mod == "" {
		return "", errors.New("no shell binary: pass --shell <path> or set KRATE_SHELL (the prebuilt shell ships with @krate/core in a later release)")
	}
	if _, err := os.Stat(filepath.Join(mod, "cmd", "krate-shell")); err != nil {
		return "", errors.New("no shell binary: packages/native/cmd/krate-shell does not exist in this checkout")
	}
	exe := "krate-shell"
	if runtime.GOOS == "windows" {
		exe += ".exe"
	}
	bin := filepath.Join(root, ".krate", "native", "bin", exe)
	if !nativeShellStale(mod, bin) {
		return bin, nil
	}
	if err := os.MkdirAll(filepath.Dir(bin), 0o755); err != nil {
		return "", err
	}
	printf("  %s>%s Building the native shell\n", cGreen, cReset)
	cmd := exec.Command("go", "build", "-trimpath", "-o", bin, "./cmd/krate-shell")
	cmd.Dir = mod
	var buf bytes.Buffer
	cmd.Stdout, cmd.Stderr = &buf, &buf
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("building the native shell: %s", trimTail(strings.TrimSpace(buf.String()), 8))
	}
	return bin, nil
}

// nativeShellStale reports whether bin is missing or older than any Go source
// of the native module.
func nativeShellStale(mod, bin string) bool {
	st, err := os.Stat(bin)
	if err != nil {
		return true
	}
	stale := false
	_ = filepath.WalkDir(mod, func(p string, d os.DirEntry, err error) error {
		if err != nil || stale {
			return nil
		}
		if d.IsDir() {
			if n := d.Name(); n == "node_modules" || n == "testdata" || strings.HasPrefix(n, ".") {
				return filepath.SkipDir
			}
			return nil
		}
		n := d.Name()
		if strings.HasSuffix(n, ".go") || n == "go.mod" || n == "go.sum" || strings.HasSuffix(n, ".js") || strings.HasSuffix(n, ".m") || strings.HasSuffix(n, ".h") {
			if info, err := d.Info(); err == nil && info.ModTime().After(st.ModTime()) {
				stale = true
			}
		}
		return nil
	})
	return stale
}

// nativeShell is a running krate-shell --dev process.
type nativeShell struct {
	cmd     *exec.Cmd
	stdin   io.WriteCloser
	mu      sync.Mutex // serializes commands
	replies chan string
	url     chan string
	ready   chan struct{}
	exited  chan struct{}
}

// shellTaggedPrefixes are the stderr prefixes the shell already adds.
var shellTaggedPrefixes = []string{"[gohost] ", "[host] ", "[node] ", "[bun] ", "[deno] ", "[webview] ", "krate-shell: "}

func startNativeShell(path, appDir string, browser bool, addr string, stderr io.Writer) (*nativeShell, error) {
	args := []string{"--dev"}
	if browser {
		args = append(args, "--browser")
		if addr != "" {
			args = append(args, "--addr", addr)
		}
	}
	args = append(args, appDir)
	cmd := exec.Command(path, args...)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	errPipe, err := cmd.StderrPipe()
	if err != nil {
		return nil, err
	}
	s := &nativeShell{
		cmd: cmd, stdin: stdin,
		replies: make(chan string, 16), url: make(chan string, 1),
		ready: make(chan struct{}), exited: make(chan struct{}),
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("starting the shell: %w", err)
	}
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		sc := bufio.NewScanner(stdout)
		sc.Buffer(make([]byte, 64*1024), 1<<20)
		readyOnce := false
		for sc.Scan() {
			line := sc.Text()
			switch {
			case strings.HasPrefix(line, "url "):
				select {
				case s.url <- strings.TrimPrefix(line, "url "):
				default:
				}
			case line == "ready" && !readyOnce:
				readyOnce = true
				close(s.ready)
			case line == "ok" || strings.HasPrefix(line, "error"):
				select {
				case s.replies <- line:
				default:
				}
			}
		}
	}()
	go func() {
		defer wg.Done()
		sc := bufio.NewScanner(errPipe)
		sc.Buffer(make([]byte, 64*1024), 1<<20)
		for sc.Scan() {
			line := sc.Text()
			tagged := false
			for _, p := range shellTaggedPrefixes {
				if strings.HasPrefix(line, p) {
					tagged = true
				}
			}
			if !tagged {
				line = "[shell] " + line
			}
			fmt.Fprintf(stderr, "  %s%s%s\n", cGray, line, cReset)
		}
	}()
	go func() {
		wg.Wait()
		_ = cmd.Wait()
		close(s.exited)
	}()
	return s, nil
}

// send runs one control command and returns the shell's error text, "" on ok.
func (s *nativeShell) send(line string, timeout time.Duration) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	select {
	case <-s.exited:
		return errors.New("the shell has exited")
	default:
	}
	for len(s.replies) > 0 {
		<-s.replies
	}
	if _, err := io.WriteString(s.stdin, strings.ReplaceAll(line, "\n", " ")+"\n"); err != nil {
		return err
	}
	select {
	case r := <-s.replies:
		if r == "ok" {
			return nil
		}
		return errors.New(strings.TrimSpace(strings.TrimPrefix(r, "error")))
	case <-s.exited:
		return errors.New("the shell has exited")
	case <-time.After(timeout):
		return fmt.Errorf("timed out waiting for the shell to answer %q", strings.SplitN(line, " ", 2)[0])
	}
}

// quit asks the shell to exit and kills it if it does not.
func (s *nativeShell) quit() {
	select {
	case <-s.exited:
		return
	default:
	}
	_ = s.send("quit", 2*time.Second)
	_ = s.stdin.Close()
	select {
	case <-s.exited:
	case <-time.After(3 * time.Second):
		if s.cmd.Process != nil {
			_ = s.cmd.Process.Kill()
		}
		<-s.exited
	}
}

// NativeDevOptions configure RunNativeDev.
type NativeDevOptions struct {
	// LoadConfig loads the project config; it is called again after a config
	// or env change.
	LoadConfig func() (root string, cfg *config.Config, env map[string]string, err error)
	// Browser serves the app on a loopback port instead of opening a window.
	Browser bool
	// Port is the browser-mode port (0 picks one).
	Port int
	// Shell is the shell binary (see LocateNativeShell).
	Shell   string
	Target  string
	Verbose bool
	// Stderr receives the shell's forwarded output. Default os.Stderr.
	Stderr io.Writer
	// OnURL, if set, receives the browser-mode URL once printed.
	OnURL func(url string)
	Ctx   context.Context
}

// RunNativeDev builds the project as a native app and runs it under the shell
// with hot reload until the window closes, ctx is done, or the shell dies.
func RunNativeDev(o NativeDevOptions) error {
	if o.Stderr == nil {
		o.Stderr = os.Stderr
	}
	if o.Ctx == nil {
		o.Ctx = context.Background()
	}
	for {
		again, err := runNativeDevOnce(o)
		if err != nil || !again {
			return err
		}
		printf("%s%s  Restarting native dev with the new settings...%s\n", cBold, cCyan, cReset)
	}
}

// nativeDev is one dev session (between config reloads).
type nativeDev struct {
	b       *Builder
	o       NativeDevOptions
	hub     *DevHub
	outDir  string
	appDir  string
	goos    string
	goarch  string
	shell   *nativeShell
	hostSum [32]byte
	goSum   string
	deps    NativeChangeClassifier
	// failed is true while a host build error is on screen.
	failed bool
}

func runNativeDevOnce(o NativeDevOptions) (bool, error) {
	root, cfg, env, err := o.LoadConfig()
	if err != nil {
		return false, err
	}
	ctx, cancel := context.WithCancel(o.Ctx)
	defer cancel()

	b := New(root, cfg)
	b.Verbose = o.Verbose
	b.Env = env
	b.DevMode = true
	b.SetContext(ctx)
	hub := NewDevHub()
	nd := &nativeDev{b: b, o: o, hub: hub}
	nd.outDir = filepath.Join(root, ".krate", "native", "dev")
	b.DevReload = func(reason string) bool {
		printf("  %s%s changed; reloading configuration...%s\n", cYellow, reason, cReset)
		if _, _, _, err := o.LoadConfig(); err != nil {
			fmt.Fprintf(os.Stderr, "%sConfig error:%s %v\n", cRed, cReset, err)
			hub.Publish(DevEvent{Type: "reload", Diagnostics: []diag.Diagnostic{{File: reason, Message: "config: " + err.Error(), Hint: "fix " + reason + "; native dev keeps running with the previous settings"}}})
			return false
		}
		return true
	}

	printf("%s%s  Building native app (dev)...%s\n", cBold, cCyan, cReset)
	res, err := b.BuildNativeApp(NativeAppOptions{OutDir: nd.outDir, Target: o.Target, Dev: true})
	if err != nil {
		return false, fmt.Errorf("initial build failed: %s", BuildErrorSummary(err))
	}
	nd.appDir = res.AppDir
	nd.goos, nd.goarch, _ = parseNativeTarget(o.Target)
	for _, n := range res.Notes {
		printf("  %s! %s%s\n", cYellow, n, cReset)
	}
	nd.captureState()

	addr := ""
	if o.Port > 0 {
		addr = fmt.Sprintf("127.0.0.1:%d", o.Port)
	}
	sh, err := startNativeShell(o.Shell, nd.appDir, o.Browser, addr, o.Stderr)
	if err != nil {
		return false, err
	}
	nd.shell = sh
	defer sh.quit()
	select {
	case <-sh.ready:
	case <-sh.exited:
		return false, errors.New("the shell exited before it was ready")
	case <-time.After(60 * time.Second):
		return false, errors.New("the shell did not become ready in 60s")
	case <-ctx.Done():
		return false, nil
	}
	if o.Browser {
		select {
		case u := <-sh.url:
			printf("%s%s  Native dev (browser): %s%s\n", cBold, cGreen, u, cReset)
			if o.OnURL != nil {
				o.OnURL(u)
			}
		default:
		}
	} else {
		printf("%s%s  Native dev is running. Close the window or press Ctrl+C to stop.%s\n", cBold, cGreen, cReset)
	}

	// Relay hub events to the shell's /__krate/hmr stream.
	events, unsub := hub.Subscribe()
	defer unsub()
	go func() {
		for ev := range events {
			if err := sh.send("hmr "+string(NativeHMRMessage(ev)), 10*time.Second); err != nil && ctx.Err() == nil {
				fmt.Fprintf(os.Stderr, "  %sHMR relay: %v%s\n", cYellow, err, cReset)
			}
		}
	}()

	b.DevHandleChanges = nd.handleChanges
	errc := make(chan error, 1)
	go func() { errc <- Watch(b, 75*time.Millisecond, hub) }()

	select {
	case err := <-errc:
		if errors.Is(err, ErrDevRestart) {
			return true, nil
		}
		return false, err
	case <-sh.exited:
		cancel()
		<-errc
		return false, nil
	case <-ctx.Done():
		<-errc
		return false, nil
	}
}

// captureState records the artifacts of the build so later batches can tell
// what changed.
func (nd *nativeDev) captureState() {
	nd.hostSum = nd.hostBundleSum()
	nd.goSum = nd.goDigest()
	nd.deps = NativeChangeClassifier{Root: nd.b.Root, HostDeps: map[string]bool{}, WholeHost: map[string]bool{}, IsUIGo: nd.b.isManagedGoFile}
	if p, err := native.Analyze(nd.b.Root, nd.b.Cfg); err == nil {
		for _, m := range p.Modules {
			if strings.HasSuffix(m.Rel, ".go") {
				continue
			}
			nd.deps.HostDeps[m.Path] = true
			if m.Whole {
				nd.deps.WholeHost[m.Path] = true
			}
		}
	}
	if h := nd.b.NativeHost(); h != nil {
		for _, g := range h.Groups {
			for _, s := range sourceMapSources(g.SourceMap) {
				nd.deps.HostDeps[s] = true
			}
		}
	}
}

// sourceMapSources lists the absolute source files of a source map.
func sourceMapSources(mapPath string) []string {
	data, err := os.ReadFile(mapPath)
	if err != nil {
		return nil
	}
	var m struct {
		Sources []string `json:"sources"`
	}
	if json.Unmarshal(data, &m) != nil {
		return nil
	}
	var out []string
	for _, s := range m.Sources {
		if strings.Contains(s, "://") {
			continue
		}
		if !filepath.IsAbs(s) {
			s = filepath.Join(filepath.Dir(mapPath), filepath.FromSlash(s))
		}
		out = append(out, filepath.Clean(s))
	}
	return out
}

func (nd *nativeDev) hostBundleSum() [32]byte {
	h := nd.b.NativeHost()
	if h == nil {
		return [32]byte{}
	}
	// One digest over every bundle the app directory holds: the QuickJS one
	// and those of the child runtimes.
	sum := sha256.New()
	any := false
	for _, g := range h.Groups {
		if rt := runtimes.Runtime(g.Runtime); rt != runtimes.QuickJS && !rt.Child() {
			continue
		}
		if data, err := os.ReadFile(g.Bundle); err == nil {
			fmt.Fprintln(sum, g.Runtime, len(data))
			sum.Write(data)
			any = true
		}
	}
	if !any {
		return [32]byte{}
	}
	var out [32]byte
	copy(out[:], sum.Sum(nil))
	return out
}

// goDigest summarizes everything the Go host program is generated from, apart
// from the user's own files: the glue packages and the registered ids.
func (nd *nativeDev) goDigest() string {
	g := nd.b.NativeGo()
	if g == nil {
		return ""
	}
	h := sha256.New()
	for _, gl := range g.Glue {
		fmt.Fprintln(h, gl.ImportPath)
		if data, err := os.ReadFile(gl.File); err == nil {
			h.Write(data)
		}
	}
	for _, m := range g.Modules {
		fmt.Fprintln(h, m.ID, m.Funcs, m.Streams, m.Signals)
	}
	return fmt.Sprintf("%x", h.Sum(nil))
}

func equalOverlays(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if w, ok := b[k]; !ok || w != v {
			return false
		}
	}
	return true
}

func (nd *nativeDev) reportErr(source string, err error) {
	diags := DiagnosticsFromError(err)
	if len(diags) == 0 {
		diags = []diag.Diagnostic{{Message: err.Error()}}
	}
	fmt.Fprintf(os.Stderr, "  %s%s error: %s%s\n", cRed, source, BuildErrorSummary(err), cReset)
	nd.failed = true
	nd.hub.Publish(DevEvent{Type: "reload", Diagnostics: diags})
}

// handleChanges is the dev watcher's batch handler: host code first (so the
// reloaded UI finds the new host), then the UI.
func (nd *nativeDev) handleChanges(changed []string, hub *DevHub) {
	defer func() {
		if rec := recover(); rec != nil {
			fmt.Fprintf(os.Stderr, "  %sNative dev rebuild error:%s %v\n", cRed, cReset, rec)
		}
	}()
	b := nd.b
	plan := PlanNativeChanges(nd.deps, changed)
	if len(plan.UI) == 0 && !plan.GoHost && !plan.HostJS {
		return
	}
	needRefresh := plan.GoHost || plan.HostJS
	if !needRefresh && (b.NativeHost() != nil || b.NativeGo() != nil) {
		for _, p := range plan.UI {
			if isScriptFile(p) {
				needRefresh = true
				break
			}
		}
	}

	fullUI := false
	if needRefresh {
		oldOverlays := b.nativeOverlays
		savedHost, savedGo, oldBaked := b.nativeHost, b.nativeGo, b.nativeBaked
		if err := b.prepareNativeHost(); err != nil {
			b.nativeHost, b.nativeGo, b.nativeOverlays, b.nativeBaked = savedHost, savedGo, oldOverlays, oldBaked
			nd.reportErr("Host code", err)
			return
		}
		if !equalOverlays(oldOverlays, b.nativeOverlays) || !reflect.DeepEqual(oldBaked, b.nativeBaked) {
			fullUI = true
		}
		nd.captureDeps()
		if err := nd.syncHost(plan.GoHost); err != nil {
			nd.reportErr("Host build", err)
			return
		}
	}

	wasFailed := nd.failed
	nd.failed = false
	switch {
	case fullUI:
		printf("  %sHost API changed; rebuilding the UI...%s\n", cYellow, cReset)
		var diags []diag.Diagnostic
		if err := b.BuildAll(); err != nil {
			fmt.Fprintf(os.Stderr, "  %sError: %v%s\n", cRed, err, cReset)
			diags = DiagnosticsFromError(err)
		}
		nd.syncFallbacks()
		hub.Publish(DevEvent{Type: "reload", Diagnostics: diags})
	case len(plan.UI) > 0:
		b.processChanges(plan.UI, hub)
		nd.syncFallbacks()
	case wasFailed:
		// Host code recovered and no page changed: clear the error without
		// reloading any page (no route matches the marker).
		hub.Publish(DevEvent{Type: "reload", Routes: []string{"/__krate_clear"}})
	}
}

func (nd *nativeDev) captureDeps() {
	hostSum, goSum := nd.hostSum, nd.goSum
	nd.captureState()
	nd.hostSum, nd.goSum = hostSum, goSum
}

// syncHost copies a changed host bundle into the app directory, rebuilds the
// Go host when needed, and tells the shell.
func (nd *nativeDev) syncHost(goEdited bool) error {
	b := nd.b
	manifestDirty := false
	reload := false
	if h := b.NativeHost(); h != nil && len(h.Groups) > 0 {
		sum := nd.hostBundleSum()
		dst := filepath.Join(nd.appDir, "host", "host.js")
		_, statErr := os.Stat(dst)
		if h.HostBundle == "" {
			statErr = nil
		}
		if sum != nd.hostSum || statErr != nil {
			if h.HostBundle != "" {
				if err := copyNativeFile(h.HostBundle, dst); err != nil {
					return err
				}
				if h.SourceMap != "" {
					if _, err := os.Stat(h.SourceMap); err == nil {
						_ = copyNativeFile(h.SourceMap, filepath.Join(nd.appDir, "host", "host.js.map"))
					}
				}
			}
			// The child runtimes' scripts are rewritten by rewriteHostManifest.
			nd.hostSum = sum
			reload = true
			manifestDirty = true
		}
	}
	restart := false
	if g := b.NativeGo(); g != nil && len(g.Glue) > 0 {
		digest := nd.goDigest()
		if goEdited || digest != nd.goSum {
			mainPath, err := b.writeGoHostMain(g)
			if err != nil {
				return err
			}
			_ = mainPath
			bin := filepath.Join(nd.appDir, "gohost", "krate-gohost")
			if nd.goos == "windows" {
				bin += ".exe"
			}
			next := bin + ".next"
			if err := b.compileGoHost(next, nd.goos, nd.goarch, false); err != nil {
				return err
			}
			if err := os.Rename(next, bin); err != nil {
				_ = os.Remove(next)
				return fmt.Errorf("replacing the Go host binary (it may still be running): %w", err)
			}
			nd.goSum = digest
			restart = true
			manifestDirty = true
		}
	}
	if manifestDirty {
		if err := nd.rewriteHostManifest(); err != nil {
			return err
		}
	}
	if reload {
		printf("  %sReloading host code...%s\n", cCyan, cReset)
		if err := nd.shell.send("reload-hostjs", 30*time.Second); err != nil {
			nd.hub.PublishServerError("host", ClientError{Message: "reloading host code failed: " + err.Error(), Kind: "host"})
			fmt.Fprintf(os.Stderr, "  %sreload-hostjs: %v%s\n", cRed, err, cReset)
		} else {
			nd.hub.ClearServerError("host")
		}
	}
	if restart {
		printf("  %sRestarting the Go host...%s\n", cCyan, cReset)
		if err := nd.shell.send("restart-gohost", 60*time.Second); err != nil {
			nd.hub.PublishServerError("gohost", ClientError{Message: "restarting the Go host failed: " + err.Error(), Kind: "host"})
			fmt.Fprintf(os.Stderr, "  %srestart-gohost: %v%s\n", cRed, err, cReset)
		} else {
			nd.hub.ClearServerError("gohost")
		}
	}
	return nil
}

// rewriteHostManifest updates the hostjs and gohost sections of app.json.
func (nd *nativeDev) rewriteHostManifest() error {
	path := filepath.Join(nd.appDir, "app.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var m NativeManifest
	if err := json.Unmarshal(data, &m); err != nil {
		return err
	}
	m.HostJS = nil
	if h := nd.b.NativeHost(); h != nil && h.HostBundle != "" {
		m.HostJS = &NativeHostJSMeta{Bundle: "host/host.js"}
		if _, err := os.Stat(filepath.Join(nd.appDir, "host", "host.js.map")); err == nil && h.SourceMap != "" {
			m.HostJS.Map = "host/host.js.map"
		}
	}
	m.HostChildren = nil
	if h := nd.b.NativeHost(); h != nil {
		children, _, err := writeHostChildren(h, nd.appDir, nativeEffective(nd.b.Cfg).Permissions)
		if err != nil {
			return err
		}
		m.HostChildren = children
	}
	m.GoHost = nil
	if g := nd.b.NativeGo(); g != nil && len(g.Glue) > 0 {
		bin := filepath.Join("gohost", "krate-gohost")
		if nd.goos == "windows" {
			bin += ".exe"
		}
		gh := &NativeGoHostMeta{Binary: filepath.ToSlash(bin), Funcs: []string{}, Streams: []string{}, Signals: []string{}}
		for _, mod := range g.Modules {
			gh.Funcs = append(gh.Funcs, mod.Funcs...)
			gh.Streams = append(gh.Streams, mod.Streams...)
			gh.Signals = append(gh.Signals, mod.Signals...)
		}
		sort.Strings(gh.Funcs)
		sort.Strings(gh.Streams)
		sort.Strings(gh.Signals)
		m.GoHost = gh
	}
	out, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(out, '\n'), 0o644)
}

// syncFallbacks drops the build manifest the shell does not need and warns
// when the set of dynamic routes changed (the shell reads them at start).
func (nd *nativeDev) syncFallbacks() {
	distDir := filepath.Join(nd.appDir, "dist")
	if _, err := os.Stat(ManifestPath(distDir)); err != nil {
		return
	}
	fb, err := nativeFallbacks(distDir)
	_ = os.RemoveAll(filepath.Join(distDir, ".krate"))
	if err != nil {
		return
	}
	path := filepath.Join(nd.appDir, "app.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}
	var m NativeManifest
	if json.Unmarshal(data, &m) != nil {
		return
	}
	a, _ := json.Marshal(m.Fallbacks)
	c, _ := json.Marshal(fb)
	if bytes.Equal(a, c) {
		return
	}
	m.Fallbacks = fb
	if out, err := json.MarshalIndent(m, "", "  "); err == nil {
		_ = os.WriteFile(path, append(out, '\n'), 0o644)
	}
	printf("  %s! The dynamic routes changed; restart native dev to serve the new ones.%s\n", cYellow, cReset)
}
