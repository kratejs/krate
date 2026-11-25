package build

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kratejs/krate/packages/compiler/internal/config"
	"github.com/kratejs/krate/packages/compiler/internal/diag"
)

// startActivityWatch builds files in dev mode with RunInitialDevBuild and runs
// Watch on the hub. The returned channel carries the hub's events from before
// the initial build.
func startActivityWatch(t *testing.T, files map[string]string) (string, *Builder, *DevHub, <-chan DevEvent, <-chan error) {
	t.Helper()
	root := t.TempDir()
	writeProject(t, root, files)
	cfg := config.Default()
	cfg.PagesDir = filepath.Join(root, "src", "pages")
	UseDevOutDir(root, cfg)
	cfg.PublicDir = filepath.Join(root, "public")
	b := New(root, cfg)
	b.DevMode = true
	hub := NewDevHub()
	events, cancel := hub.Subscribe()
	t.Cleanup(cancel)
	if err := RunInitialDevBuild(b, hub); err != nil {
		t.Fatalf("initial build: %v", err)
	}
	ctx, stop := context.WithCancel(context.Background())
	b.SetContext(ctx)
	errc := make(chan error, 1)
	done := make(chan struct{})
	go func() {
		defer close(done)
		errc <- Watch(b, 30*time.Millisecond, hub)
	}()
	t.Cleanup(func() {
		stop()
		<-done
	})
	time.Sleep(300 * time.Millisecond)
	return root, b, hub, events, errc
}

// nextEvent returns the next event of a type in types, skipping others.
func nextEvent(t *testing.T, events <-chan DevEvent, types ...string) DevEvent {
	t.Helper()
	deadline := time.After(30 * time.Second)
	for {
		select {
		case ev := <-events:
			for _, ty := range types {
				if ev.Type == ty {
					return ev
				}
			}
		case <-deadline:
			t.Fatalf("timed out waiting for %v", types)
		}
	}
}

func TestDevActivityRecordsBuilds(t *testing.T) {
	t.Parallel()
	root, _, hub, events, _ := startActivityWatch(t, map[string]string{
		"src/pages/index.tsx": devPage("home"),
		"src/pages/about.tsx": `import "./about.css";
export default function About() { return <p class="a">about</p>; }
`,
		"src/pages/about.css": ".a { color: red; }\n",
	})
	// The initial build.
	if ev := nextEvent(t, events, "build-start"); len(ev.Files) != 0 {
		t.Errorf("initial build-start files = %v", ev.Files)
	}
	ev := nextEvent(t, events, "build-end")
	if ev.Build == nil || ev.Build.Kind != BuildKindInitial || !ev.Build.OK || ev.Build.Pages != 2 || ev.Build.ID != 1 {
		t.Fatalf("initial build = %+v", ev.Build)
	}
	if len(ev.Build.Phases) == 0 {
		t.Error("the initial build has no phases")
	}

	// A page edit: build-start, build-end, then the reload.
	if err := os.WriteFile(filepath.Join(root, "src", "pages", "about.tsx"), []byte(`import "./about.css";
export default function About() { return <p class="a">about 2</p>; }
`), 0o644); err != nil {
		t.Fatal(err)
	}
	start := nextEvent(t, events, "build-start")
	if strings.Join(start.Files, ",") != "src/pages/about.tsx" {
		t.Errorf("build-start files = %v", start.Files)
	}
	var order []string
	var rec *BuildRecord
	for len(order) < 2 {
		ev := nextEvent(t, events, "build-end", "reload")
		order = append(order, ev.Type)
		if ev.Build != nil {
			rec = ev.Build
		}
	}
	if strings.Join(order, ",") != "build-end,reload" {
		t.Errorf("event order = %v, want build-end before reload", order)
	}
	if rec == nil || rec.Kind != BuildKindPartial || rec.Pages != 1 || !rec.OK || strings.Join(rec.Routes, ",") != "/about" || rec.DurationMs <= 0 {
		t.Fatalf("partial build = %+v", rec)
	}
	names := map[string]bool{}
	for _, p := range rec.Phases {
		names[p.Name] = true
	}
	if !names["pages"] || !names["html"] {
		t.Errorf("partial build phases = %+v", rec.Phases)
	}

	// A stylesheet edit is css-only.
	if err := os.WriteFile(filepath.Join(root, "src", "pages", "about.css"), []byte(".a { color: blue; }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	nextEvent(t, events, "build-start")
	if ev := nextEvent(t, events, "build-end"); ev.Build.Kind != BuildKindCSSOnly {
		t.Errorf("css build = %+v", ev.Build)
	}

	// A broken page is a failed build.
	if err := os.WriteFile(filepath.Join(root, "src", "pages", "about.tsx"), []byte("export default function About() { return <p>unclosed; }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	nextEvent(t, events, "build-start")
	failed := nextEvent(t, events, "build-end").Build
	if failed.OK || failed.Diagnostics == 0 {
		t.Errorf("broken build = %+v", failed)
	}

	snap := hub.Activity()
	if len(snap.Builds) != 4 || snap.LastGoodAt == 0 || snap.Builds[3].ID != 4 {
		t.Errorf("history = %+v", snap)
	}

	// The history survives a restart.
	next := NewDevHub()
	next.CarryOver(hub)
	if got := next.Activity(); len(got.Builds) != 4 || got.LastGoodAt != snap.LastGoodAt {
		t.Errorf("carried over = %+v", got)
	}
}

func TestDevActivityRing(t *testing.T) {
	t.Parallel()
	hub := NewDevHub()
	events, cancel := hub.Subscribe()
	defer cancel()
	for i := 0; i < buildActivityCap+5; i++ {
		act := hub.BeginActivity("", []string{"x"})
		act.SetKind(BuildKindFull)
		hub.Publish(DevEvent{Type: "reload", Diagnostics: []diag.Diagnostic{{Message: "boom"}}})
		act.End() // already ended by the publish: no second record
		for len(events) > 0 {
			<-events
		}
	}
	snap := hub.Activity()
	if len(snap.Builds) != buildActivityCap || snap.Builds[0].ID != 6 || snap.LastGoodAt != 0 {
		t.Errorf("ring = %d builds from %d, lastGoodAt %d", len(snap.Builds), snap.Builds[0].ID, snap.LastGoodAt)
	}
	// A batch that rebuilt nothing is announced but not kept.
	hub.BeginActivity("", nil).End()
	if got := hub.Activity(); len(got.Builds) != buildActivityCap || got.Building {
		t.Errorf("noop build was recorded: %d", len(got.Builds))
	}
	// Activity events do not change the build state late tabs see.
	if cur, ok := hub.Current(); !ok || cur.BuildOK {
		t.Errorf("current = %+v", cur)
	}
}

// The terminal's r key and the overlay's restart action used to leave the
// watcher running: krate dev waits for both the server and the watcher, so a
// restart hung.
func TestWatchReturnsOnRestartRequest(t *testing.T) {
	t.Parallel()
	_, _, hub, _, errc := startActivityWatch(t, map[string]string{"src/pages/index.tsx": devPage("home")})
	hub.RequestRestart()
	select {
	case err := <-errc:
		if !errors.Is(err, ErrDevRestart) {
			t.Fatalf("Watch returned %v, want ErrDevRestart", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Watch kept running after a restart request")
	}
}

func TestWatchRunsOverlayActions(t *testing.T) {
	t.Parallel()
	root, _, hub, events, _ := startActivityWatch(t, map[string]string{
		"src/pages/index.tsx": devPage("home"),
		"src/pages/about.tsx": devPage("about"),
	})
	nextEvent(t, events, "build-end") // initial

	if err := hub.RequestAction(DevAction{Kind: "rebuild-page", File: filepath.Join(root, "src", "pages", "about.tsx")}); err != nil {
		t.Fatal(err)
	}
	if ev := nextEvent(t, events, "build-end"); ev.Build.Kind != BuildKindPartial || strings.Join(ev.Build.Routes, ",") != "/about" {
		t.Errorf("rebuild-page = %+v", ev.Build)
	}
	nextEvent(t, events, "reload")

	if err := hub.RequestAction(DevAction{Kind: "rebuild-all"}); err != nil {
		t.Fatal(err)
	}
	if ev := nextEvent(t, events, "build-end"); ev.Build.Kind != BuildKindFull || ev.Build.Pages != 2 || !ev.Build.OK {
		t.Errorf("rebuild-all = %+v", ev.Build)
	}
	nextEvent(t, events, "reload")

	cacheDir := filepath.Join(root, ".krate", "cache")
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(cacheDir, "marker")
	if err := os.WriteFile(marker, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := hub.RequestAction(DevAction{Kind: "clear-cache"}); err != nil {
		t.Fatal(err)
	}
	if ev := nextEvent(t, events, "build-end"); ev.Build.Kind != BuildKindFull || !ev.Build.OK {
		t.Errorf("clear-cache = %+v", ev.Build)
	}
	if exists(marker) {
		t.Error("clear-cache left .krate/cache in place")
	}
	if !exists(filepath.Join(DevOutDir(root), "about", "index.html")) {
		t.Error("clear-cache did not rebuild")
	}
}

// The reload stream carries the activity events.
func TestHotReloadStreamsBuildActivity(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeProject(t, root, map[string]string{"src/pages/index.tsx": devPage("home")})
	cfg := config.Default()
	cfg.PagesDir = filepath.Join(root, "src", "pages")
	UseDevOutDir(root, cfg)
	cfg.Server.Host = "127.0.0.1"
	port := freePort(t)
	cfg.DevServer.Port = port
	b := New(root, cfg)
	b.DevMode = true
	if err := b.BuildAll(); err != nil {
		t.Fatal(err)
	}
	hub := NewDevHub()
	errCh := serveInProcess(t, func(ctx context.Context) error { return ServeDevContext(ctx, root, cfg, hub, time.Now()) })
	base := fmt.Sprintf("http://127.0.0.1:%d", port)
	waitFor(t, "dev server up", func() bool {
		select {
		case err := <-errCh:
			t.Fatalf("dev server exited: %v", err)
		default:
		}
		code, _ := httpGet(t, base+"/healthz", "")
		return code == 200
	})
	resp, err := http.Get(base + "/__krate/hotreload")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	lines := make(chan string, 64)
	go func() {
		sc := bufio.NewScanner(resp.Body)
		for sc.Scan() {
			lines <- sc.Text()
		}
		close(lines)
	}()
	readUntil := func(prefix string) string {
		t.Helper()
		deadline := time.After(10 * time.Second)
		for {
			select {
			case l, ok := <-lines:
				if !ok {
					t.Fatalf("stream closed before %q", prefix)
				}
				if strings.HasPrefix(l, prefix) {
					return l
				}
			case <-deadline:
				t.Fatalf("no %q on the stream", prefix)
			}
		}
	}
	readUntil("event: connected")
	// The stream subscribes right after it says connected.
	waitFor(t, "stream subscribed", func() bool {
		hub.mu.Lock()
		defer hub.mu.Unlock()
		return len(hub.subs) > 0
	})
	act := hub.BeginActivity(root, []string{filepath.Join(root, "src", "pages", "index.tsx")})
	readUntil("event: build-start")
	if data := readUntil("data: "); data != `data: {"files":["src/pages/index.tsx"]}` {
		t.Errorf("build-start data = %s", data)
	}
	act.SetKind(BuildKindPartial)
	hub.Publish(DevEvent{Type: "reload", Routes: []string{"/"}})
	readUntil("event: build-end")
	if data := readUntil("data: "); !strings.Contains(data, `"build":{"id":1,`) || !strings.Contains(data, `"kind":"partial"`) || !strings.Contains(data, `"routes":["/"]`) {
		t.Errorf("build-end data = %s", data)
	}
	readUntil("event: reload")
}

func TestNativeHMRMessageActivity(t *testing.T) {
	t.Parallel()
	if got := string(NativeHMRMessage(DevEvent{Type: "build-start", Files: []string{"a.tsx"}})); got != `{"files":["a.tsx"],"type":"build-start"}` {
		t.Errorf("build-start = %s", got)
	}
	got := string(NativeHMRMessage(DevEvent{Type: "build-end", Build: &BuildRecord{ID: 3, Kind: "full", OK: true}}))
	if !strings.Contains(got, `"type":"build-end"`) || !strings.Contains(got, `"id":3`) {
		t.Errorf("build-end = %s", got)
	}
}

func TestCoarseProfilerRecordsOnlyItsGroups(t *testing.T) {
	t.Parallel()
	p := newCoarseProfiler("build", "dev")
	if p.Enabled() {
		t.Fatal("a coarse profiler must report disabled (no profile report, no esbuild stats)")
	}
	p.Phase("page:emit")()
	p.Add("build:pages", 2*time.Millisecond)
	l := p.Laps("dev:")
	l.Lap("1-prepare")
	l.Lap("2-pages")
	p.Add("build:pages", 3*time.Millisecond)
	got := devActivityPhases(p, []BuildPhase{{Name: "api", Ms: 1.25}})
	var names []string
	for _, ph := range got {
		names = append(names, ph.Name)
	}
	if strings.Join(names, ",") != "pages,prepare,api" {
		t.Errorf("phases = %+v", got)
	}
	if got[0].Ms < 5 {
		t.Errorf("pages = %v ms, want the build and dev steps summed", got[0].Ms)
	}
	if NewProfiler(false).Totals() == nil || len(NewProfiler(false).Totals()) != 0 {
		t.Error("a disabled profiler records nothing")
	}
}

// BenchmarkCoarseProfilerPhase measures what the coarse profiler adds to a
// phase call of a group it does not record (the per-page phases).
func BenchmarkCoarseProfilerPhase(b *testing.B) {
	for _, c := range []struct {
		name string
		p    *Profiler
	}{{"disabled", NewProfiler(false)}, {"coarse", newCoarseProfiler("build", "dev")}} {
		b.Run(c.name, func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				c.p.Phase("page:emit")()
			}
		})
	}
}
