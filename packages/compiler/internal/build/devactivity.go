package build

import (
	"errors"
	"fmt"
	"math"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/kratejs/krate/packages/compiler/internal/bundler"
	"github.com/kratejs/krate/packages/compiler/internal/diag"
)

// buildActivityCap is how many dev builds the hub remembers.
const buildActivityCap = 100

// Build kinds recorded in BuildRecord.Kind. A failed build keeps the kind of
// what it tried to build and has OK false.
const (
	BuildKindInitial       = "initial"
	BuildKindPartial       = "partial"
	BuildKindFull          = "full"
	BuildKindCSSOnly       = "css-only"
	BuildKindPublicOnly    = "public-only"
	BuildKindConfigRestart = "config-restart"
	// BuildKindNoop is a batch that rebuilt nothing (only unmanaged files
	// changed). It is announced with build-end but not kept.
	BuildKindNoop = "noop"
)

// BuildPhase is the time one coarse phase of a dev build took.
type BuildPhase struct {
	Name string  `json:"name"`
	Ms   float64 `json:"ms"`
}

// BuildRecord describes one dev build for the overlay's activity panel.
type BuildRecord struct {
	ID          int          `json:"id"`
	Start       int64        `json:"start"` // unix ms
	DurationMs  float64      `json:"durationMs"`
	Kind        string       `json:"kind"`
	Files       []string     `json:"files"`
	Routes      []string     `json:"routes"`
	Pages       int          `json:"pages"`
	OK          bool         `json:"ok"`
	Diagnostics int          `json:"diagnostics"`
	Phases      []BuildPhase `json:"phases"`
}

// BuildActivity is a dev build in progress. The builder fills in what it
// learns while it runs (SetKind, SetPages); the hub completes the record when
// the build publishes its result, or when End is called.
type BuildActivity struct {
	hub     *DevHub
	started time.Time
	files   []string

	mu     sync.Mutex
	kind   string
	pages  int
	phases func() []BuildPhase
	done   bool
}

// SetKind records what the build turned out to rebuild.
func (a *BuildActivity) SetKind(kind string) {
	if a == nil {
		return
	}
	a.mu.Lock()
	a.kind = kind
	a.mu.Unlock()
}

// SetKindIfUnset records kind unless one was recorded already.
func (a *BuildActivity) SetKindIfUnset(kind string) {
	if a == nil {
		return
	}
	a.mu.Lock()
	if a.kind == "" {
		a.kind = kind
	}
	a.mu.Unlock()
}

// SetPages records how many pages the build rebuilt.
func (a *BuildActivity) SetPages(n int) {
	if a == nil {
		return
	}
	a.mu.Lock()
	a.pages = n
	a.mu.Unlock()
}

// setPhases installs the function that reports the phase timings.
func (a *BuildActivity) setPhases(fn func() []BuildPhase) {
	if a == nil {
		return
	}
	a.mu.Lock()
	a.phases = fn
	a.mu.Unlock()
}

// End completes the build if publishing its result did not already. A build
// that published nothing rebuilt nothing.
func (a *BuildActivity) End() {
	if a == nil || a.hub == nil {
		return
	}
	a.hub.finishActivity(a, nil, 0, false)
}

// BeginActivity records the start of a dev build and announces it to the
// connected tabs (build-start). files are absolute or root-relative paths;
// root makes them relative for display. The build ends when the next reload
// event is published or End is called.
func (h *DevHub) BeginActivity(root string, files []string) *BuildActivity {
	if h == nil {
		return nil
	}
	rel := make([]string, 0, len(files))
	for _, f := range files {
		rel = append(rel, displayRel(root, f))
	}
	sort.Strings(rel)
	a := &BuildActivity{hub: h, started: time.Now(), files: rel}
	h.mu.Lock()
	if prev := h.activity; prev != nil {
		// A build that never ended (a panic in the builder): close it as is.
		h.finishActivityLocked(prev, nil, 0, false)
	}
	h.activity = a
	h.fanOutLocked(DevEvent{Type: "build-start", Files: rel})
	h.mu.Unlock()
	return a
}

// finishActivity completes a, records it and announces it (build-end).
func (h *DevHub) finishActivity(a *BuildActivity, routes []string, diags int, published bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.finishActivityLocked(a, routes, diags, published)
}

func (h *DevHub) finishActivityLocked(a *BuildActivity, routes []string, diags int, published bool) {
	a.mu.Lock()
	if a.done {
		a.mu.Unlock()
		return
	}
	a.done = true
	kind, pages, phases := a.kind, a.pages, a.phases
	a.mu.Unlock()
	if h.activity == a {
		h.activity = nil
	}
	if !published && kind == "" {
		kind = BuildKindNoop
	}
	if kind == "" {
		kind = BuildKindPartial
	}
	rec := BuildRecord{
		Start:       a.started.UnixMilli(),
		DurationMs:  durationMs(time.Since(a.started)),
		Kind:        kind,
		Files:       a.files,
		Routes:      append([]string{}, routes...),
		Pages:       pages,
		OK:          diags == 0,
		Diagnostics: diags,
		Phases:      []BuildPhase{},
	}
	if phases != nil {
		if p := phases(); p != nil {
			rec.Phases = p
		}
	}
	if kind != BuildKindNoop {
		h.buildSeq++
		rec.ID = h.buildSeq
		h.history = append(h.history, rec)
		if over := len(h.history) - buildActivityCap; over > 0 {
			h.history = append([]BuildRecord(nil), h.history[over:]...)
		}
		if rec.OK {
			h.lastGoodAt = time.Now()
		}
	}
	h.fanOutLocked(DevEvent{Type: "build-end", Build: &rec})
}

// BuildActivitySnapshot is the payload of GET /__krate/dev/activity.
type BuildActivitySnapshot struct {
	Builds     []BuildRecord `json:"builds"`
	LastGoodAt int64         `json:"lastGoodAt,omitempty"` // unix ms, 0 when no build passed yet
	Building   bool          `json:"building"`
}

// Activity returns the recorded dev builds, oldest first.
func (h *DevHub) Activity() BuildActivitySnapshot {
	h.mu.Lock()
	defer h.mu.Unlock()
	snap := BuildActivitySnapshot{Builds: append([]BuildRecord{}, h.history...), Building: h.activity != nil}
	if !h.lastGoodAt.IsZero() {
		snap.LastGoodAt = h.lastGoodAt.UnixMilli()
	}
	return snap
}

// CarryOver copies the build history of prev, the hub of the dev server this
// one replaces after a restart, so the activity panel keeps it.
func (h *DevHub) CarryOver(prev *DevHub) {
	if h == nil || prev == nil || h == prev {
		return
	}
	prev.mu.Lock()
	builds := append([]BuildRecord(nil), prev.history...)
	seq, good := prev.buildSeq, prev.lastGoodAt
	prev.mu.Unlock()
	h.mu.Lock()
	h.history = append(builds, h.history...)
	if seq > h.buildSeq {
		h.buildSeq = seq
	}
	if h.lastGoodAt.IsZero() {
		h.lastGoodAt = good
	}
	h.mu.Unlock()
}

// devActivityPhases turns profiler totals into the coarse phases shown in the
// activity panel: the group prefix and step numbers are dropped and steps of
// the same name are summed, in the order first seen.
func devActivityPhases(p *Profiler, extra []BuildPhase) []BuildPhase {
	var out []BuildPhase
	index := map[string]int{}
	add := func(name string, ms float64) {
		if i, ok := index[name]; ok {
			out[i].Ms = roundMs(out[i].Ms + ms)
			return
		}
		index[name] = len(out)
		out = append(out, BuildPhase{Name: name, Ms: roundMs(ms)})
	}
	for _, ph := range p.Totals() {
		name := ph.Name
		if name == "build:total" {
			continue
		}
		if i := strings.IndexByte(name, ':'); i >= 0 {
			name = name[i+1:]
		}
		if i := strings.IndexByte(name, '-'); i > 0 && strings.Trim(name[:i], "0123456789") == "" {
			name = name[i+1:]
		}
		add(name, ph.Ms)
	}
	for _, ph := range extra {
		add(ph.Name, ph.Ms)
	}
	return out
}

// devBuildTimer times the coarse steps of one dev build next to the
// profiler's groups (API compiles, the public/ sync).
type devBuildTimer struct {
	mu     sync.Mutex
	phases []BuildPhase
}

func (t *devBuildTimer) time(name string) func() {
	if t == nil {
		return func() {}
	}
	t0 := time.Now()
	return func() {
		t.mu.Lock()
		t.phases = append(t.phases, BuildPhase{Name: name, Ms: durationMs(time.Since(t0))})
		t.mu.Unlock()
	}
}

func (t *devBuildTimer) list() []BuildPhase {
	if t == nil {
		return nil
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	return append([]BuildPhase(nil), t.phases...)
}

// beginDevActivity starts recording a dev build on the builder: a coarse
// profiler collects the phase timings unless a full profile was asked for.
// The returned function restores the builder's profiler.
func (b *Builder) beginDevActivity(hub *DevHub, files []string) (*BuildActivity, func()) {
	if hub == nil {
		return nil, func() {}
	}
	act := hub.BeginActivity(b.Root, files)
	prevProf, prevAct, prevTimer := b.Prof, b.devAct, b.devTimer
	prof := b.Prof
	if !prof.Enabled() {
		prof = newCoarseProfiler("build", "dev")
		b.Prof = prof
	}
	timer := &devBuildTimer{}
	b.devAct, b.devTimer = act, timer
	act.setPhases(func() []BuildPhase {
		// The builder may have swapped in its own profiler (KRATE_DEV_PROFILE).
		p := prof
		if b.Prof != nil && b.Prof.Enabled() {
			p = b.Prof
		}
		return devActivityPhases(p, timer.list())
	})
	return act, func() {
		b.Prof, b.devAct, b.devTimer = prevProf, prevAct, prevTimer
	}
}

// RunInitialDevBuild runs the first full build of `krate dev` and records it
// in the hub's build activity. A failure is published so the overlay shows
// it.
func RunInitialDevBuild(b *Builder, hub *DevHub) error {
	act, restore := b.beginDevActivity(hub, nil)
	act.SetKind(BuildKindInitial)
	if hub != nil {
		hub.BeginBuild()
	}
	err := b.BuildAll()
	if hub != nil {
		hub.EndBuild()
	}
	act.SetPages(b.builtPageCount())
	if err != nil && hub != nil {
		hub.Publish(DevEvent{Type: "reload", Diagnostics: DiagnosticsFromError(err)})
	} else if hub != nil {
		hub.finishActivity(act, nil, 0, true)
	}
	restore()
	return err
}

// builtPageCount is the number of pages the last full build wrote, read from
// its manifest.
func (b *Builder) builtPageCount() int {
	m, err := readManifestPages(b.Cfg.OutDir)
	if err != nil {
		return 0
	}
	return len(m.Pages)
}

func displayRel(root, p string) string {
	if root != "" && filepath.IsAbs(p) {
		if rel, err := filepath.Rel(root, p); err == nil && !strings.HasPrefix(rel, "..") {
			return filepath.ToSlash(rel)
		}
	}
	return filepath.ToSlash(p)
}

func durationMs(d time.Duration) float64 {
	return roundMs(float64(d) / float64(time.Millisecond))
}

func roundMs(ms float64) float64 {
	return math.Round(ms*10) / 10
}

// DevAction is a rebuild or cache action the overlay asks the watch loop to
// run (POST /__krate/dev/action).
type DevAction struct {
	// Kind is "rebuild-page", "rebuild-all" or "clear-cache".
	Kind string
	// File is the absolute source file of the page to rebuild.
	File string
}

// errNoWatcher means no watch loop takes actions: the server runs without
// one (tests, `krate serve`).
var errNoWatcher = errors.New("no dev watcher is running")

// errActionBusy means the queue of pending actions is full.
var errActionBusy = errors.New("the dev watcher is busy with earlier actions")

// RequestAction queues a for the watch loop. It fails when no loop runs or
// too many actions are already queued.
func (h *DevHub) RequestAction(a DevAction) error {
	h.mu.Lock()
	watching := h.watchers > 0
	h.mu.Unlock()
	if !watching {
		return errNoWatcher
	}
	select {
	case h.actions <- a:
		return nil
	default:
		return errActionBusy
	}
}

// watchActions registers a watch loop as the receiver of actions. The
// returned function unregisters it.
func (h *DevHub) watchActions() (<-chan DevAction, func()) {
	h.mu.Lock()
	h.watchers++
	h.mu.Unlock()
	return h.actions, func() {
		h.mu.Lock()
		h.watchers--
		h.mu.Unlock()
	}
}

// clearDevCaches forgets every cache a build could reuse: the compiler cache
// on disk (.krate/cache) and the parsed modules kept in memory. The next
// build starts cold.
func (b *Builder) clearDevCaches() error {
	if _, err := CleanWith(b.Root, b.Cfg, CleanOptions{Cache: true}); err != nil {
		return err
	}
	b.moduleCache = bundler.NewModuleCache()
	b.cacheContentHash = ""
	if c := b.diskCache; c != nil {
		b.diskCache = newBuildDiskCache(b.Root, c.cfgHash, c.enabled)
	}
	b.islandMemo, b.ssrMemo = nil, nil
	b.resetBuildCaches()
	return nil
}

// rebuildAllForDev runs a full build and the API builds for an overlay
// action and publishes the result.
func (b *Builder) rebuildAllForDev(hub *DevHub, clearCache bool) {
	act, restore := b.beginDevActivity(hub, nil)
	defer restore()
	act.SetKind(BuildKindFull)
	if hub != nil {
		hub.BeginBuild()
		defer hub.EndBuild()
	}
	defer boostDevGC()()
	var diags []diag.Diagnostic
	if clearCache {
		printf("  %sClearing the build cache and rebuilding all...%s\n", cYellow, cReset)
		if err := b.clearDevCaches(); err != nil {
			fmt.Fprintf(b.stderr(), "  %sError: %v%s\n", cRed, err, cReset)
			diags = append(diags, DiagnosticsFromError(err)...)
		}
	} else {
		printf("  %sRebuilding all...%s\n", cYellow, cReset)
	}
	if err := b.BuildAll(); err != nil {
		fmt.Fprintf(b.stderr(), "  %sError: %v%s\n", cRed, err, cReset)
		diags = append(diags, DiagnosticsFromError(err)...)
	}
	stop := b.devTimer.time("api")
	if err := b.BuildAllAPI(); err != nil {
		fmt.Fprintf(b.stderr(), "  %sError: %v%s\n", cRed, err, cReset)
		diags = append(diags, DiagnosticsFromError(err)...)
	}
	stop()
	act.SetPages(b.builtPageCount())
	if hub != nil {
		hub.Publish(DevEvent{Type: "reload", Diagnostics: diags})
	}
	act.End()
}
