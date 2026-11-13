package build

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// A memory limit is never closer than this to what was measured: runs on a
// busy machine vary by several MB of private dirty memory and by more of PSS,
// which also depends on how many processes share a page.
const (
	memoryMinSlackPrivate = 8 << 20
	memoryMinSlackPSS     = 16 << 20
)

// The budgets live in packages/native/budgets.json: for each platform, a
// limit for the size of the stripped shell and of three release executables
// (bytes), for the cold start of the example release executable in a real
// window (ms), and for its memory (PSS and private dirty bytes per process
// role, for the app, for the blank-page floor and for app minus floor). A
// test fails when a size, the cold start or an app-minus-floor value is over
// its limit; absolute memory values only warn, because they move with the
// WebKit version and the runner. packages/native/BUDGETS.md says how to move
// a limit.
//
//	KRATE_UPDATE_BUDGETS=1   rewrite measured and max (measured plus headroom)
//	                         of every entry the run measured
//	KRATE_BUDGET_SCALE=2     multiply the cold-start limit (slower machines);
//	                         memory limits are not scaled

type budgetEntry struct {
	Measured int64 `json:"measured"`
	Max      int64 `json:"max"`
}

type budgetFile struct {
	Comment      string                            `json:"comment"`
	Headroom     float64                           `json:"headroom"`
	MeasuredWith string                            `json:"measuredWith"`
	Sizes        map[string]map[string]budgetEntry `json:"sizes"`
	Startup      map[string]map[string]budgetEntry `json:"startup"`
	// Memory entries are named <set>.<role>.<metric> in bytes: set is app,
	// floor or appMinusFloor, role is total or a memstat role, metric is pss
	// or privateDirty. appMinusFloor values can be negative.
	Memory map[string]map[string]budgetEntry `json:"memory"`
}

func budgetPath(t *testing.T) string {
	return filepath.Join(repoRoot(t), "packages", "native", "budgets.json")
}

func loadBudgets(t *testing.T) *budgetFile {
	t.Helper()
	data, err := os.ReadFile(budgetPath(t))
	if err != nil {
		t.Fatal(err)
	}
	var f budgetFile
	if err := json.Unmarshal(data, &f); err != nil {
		t.Fatalf("budgets.json: %v", err)
	}
	if f.Headroom <= 0 {
		t.Fatal("budgets.json: headroom must be positive")
	}
	return &f
}

func (f *budgetFile) save(t *testing.T) {
	t.Helper()
	data, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(budgetPath(t), append(data, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
}

// budgetPlatformOverride lets the unit tests pretend to run elsewhere.
var budgetPlatformOverride string

func budgetPlatform() string {
	if budgetPlatformOverride != "" {
		return budgetPlatformOverride
	}
	return runtime.GOOS + "-" + runtime.GOARCH
}

// budgetStrictPlatform is the platform whose entries must all exist: a
// missing one is a failure. Any other platform is checked against the entries
// it has; with none at all it only reports what it measured, in a form that
// can be pasted into budgets.json (see budgetRecordText).
const budgetStrictPlatform = "linux-amd64"

// budgetEnforced reports whether a section holds limits for the platform.
func budgetEnforced(section map[string]map[string]budgetEntry, plat string) bool {
	return plat == budgetStrictPlatform || len(section[plat]) > 0
}

// budgetStatus is the outcome of comparing one value with its limit.
type budgetStatus int

const (
	budgetOK budgetStatus = iota
	budgetOver
	budgetMissing // enforced platform, no entry for this name
	budgetReportOnly
)

// budgetVerdict compares a measurement with its entry. scale multiplies the
// limit. It is pure so that it can be tested for every platform.
func budgetVerdict(section map[string]map[string]budgetEntry, plat, name string, value int64, scale float64) (budgetEntry, int64, budgetStatus) {
	if !budgetEnforced(section, plat) {
		return budgetEntry{}, 0, budgetReportOnly
	}
	e, ok := section[plat][name]
	if !ok {
		return budgetEntry{}, 0, budgetMissing
	}
	limit := int64(float64(e.Max) * scale)
	if value > limit {
		return e, limit, budgetOver
	}
	return e, limit, budgetOK
}

// budgetRecorded collects, for a platform without limits, the entries that
// the run would write in update mode.
var budgetRecorded struct {
	sync.Mutex
	sizes, startup, memory map[string]budgetEntry
}

func budgetRecord(section string, name string, e budgetEntry) {
	budgetRecorded.Lock()
	defer budgetRecorded.Unlock()
	var m *map[string]budgetEntry
	switch section {
	case "sizes":
		m = &budgetRecorded.sizes
	case "startup":
		m = &budgetRecorded.startup
	default:
		m = &budgetRecorded.memory
	}
	if *m == nil {
		*m = map[string]budgetEntry{}
	}
	(*m)[name] = e
}

// budgetRecordText renders the recorded entries as the fragment of
// budgets.json for the platform, between two marker lines that are easy to
// find in a CI log. It returns "" when nothing was recorded.
func budgetRecordText(plat string) string {
	budgetRecorded.Lock()
	defer budgetRecorded.Unlock()
	doc := map[string]map[string]map[string]budgetEntry{}
	for sec, m := range map[string]map[string]budgetEntry{"sizes": budgetRecorded.sizes, "startup": budgetRecorded.startup, "memory": budgetRecorded.memory} {
		if len(m) > 0 {
			doc[sec] = map[string]map[string]budgetEntry{plat: m}
		}
	}
	if len(doc) == 0 {
		return ""
	}
	data, _ := json.MarshalIndent(doc, "", "  ")
	return "BUDGET-RECORD-BEGIN " + plat + "\n" + string(data) + "\nBUDGET-RECORD-END " + plat + "\n"
}

// flushBudgetRecord prints what a report-only platform measured, to the log
// and to the GitHub step summary when there is one.
func flushBudgetRecord(t *testing.T) {
	t.Helper()
	text := budgetRecordText(budgetPlatform())
	if text == "" {
		return
	}
	fmt.Print("\n" + text)
	fmt.Printf("::notice title=native budgets %s::%s has no budget entries yet; the measured values are printed between BUDGET-RECORD markers (paste them into packages/native/budgets.json)\n", budgetPlatform(), budgetPlatform())
	if sum := os.Getenv("GITHUB_STEP_SUMMARY"); sum != "" {
		if f, err := os.OpenFile(sum, os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0o644); err == nil {
			fmt.Fprintf(f, "### Native budgets, %s (not enforced yet)\n\n```json\n%s```\n", budgetPlatform(), text)
			f.Close()
		}
	}
}

// budgetCheck compares a measurement with its limit, or in update mode
// records it. scale multiplies the limit. sectionName names the file section
// ("sizes" or "startup").
func budgetCheck(t *testing.T, f *budgetFile, section map[string]map[string]budgetEntry, sectionName, name string, value int64, unit string, scale float64) {
	t.Helper()
	plat := budgetPlatform()
	newEntry := budgetEntry{Measured: value, Max: int64(math.Ceil(float64(value) * (1 + f.Headroom)))}
	if os.Getenv("KRATE_UPDATE_BUDGETS") == "1" {
		if section[plat] == nil {
			section[plat] = map[string]budgetEntry{}
		}
		section[plat][name] = newEntry
		t.Logf("%s %s: measured %d %s, new limit %d %s", plat, name, value, unit, newEntry.Max, unit)
		return
	}
	e, limit, st := budgetVerdict(section, plat, name, value, scale)
	switch st {
	case budgetReportOnly:
		budgetRecord(sectionName, name, newEntry)
		t.Logf("%s %s: %d %s (no budget recorded for %s yet; reported only)", plat, name, value, unit, plat)
	case budgetMissing:
		t.Fatalf("budgets.json has no %s entry for %s; measured %d %s. Run with KRATE_UPDATE_BUDGETS=1 to record it", name, plat, value, unit)
	default:
		t.Logf("%s %s: %d %s (limit %d %s, measured when set %d %s)", plat, name, value, unit, limit, unit, e.Measured, unit)
		if st == budgetOver {
			t.Errorf("%s is over its budget: %d %s > %d %s (it measured %d %s when the budget was set). "+
				"If the growth is intended, update the budget (KRATE_UPDATE_BUDGETS=1, see packages/native/BUDGETS.md)", name, value, unit, limit, unit, e.Measured, unit)
		}
	}
}

func budgetScale(t *testing.T) float64 {
	t.Helper()
	s := os.Getenv("KRATE_BUDGET_SCALE")
	if s == "" {
		return 1
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil || v <= 0 {
		t.Fatalf("KRATE_BUDGET_SCALE=%q is not a positive number", s)
	}
	return v
}

func goVersionString(t *testing.T) string {
	t.Helper()
	out, err := exec.Command("go", "env", "GOVERSION").Output()
	if err != nil {
		return runtime.Version()
	}
	return string(out[:len(out)-1])
}

// TestNativeBudgetSizes measures binary sizes, which need no display: the
// stripped shell, a Go-only release, a TypeScript-only release and the
// examples/native-tasks release.
func TestNativeBudgetSizes(t *testing.T) {
	requireRelease(t)
	f := loadBudgets(t)
	sizes := map[string]int64{}

	// The shell, built the way the platform packages build it.
	shell := filepath.Join(t.TempDir(), "krate-shell")
	if runtime.GOOS == "windows" {
		shell += ".exe"
	}
	cmd := exec.Command("go", "build", "-trimpath", "-ldflags=-s -w", "-o", shell, "./cmd/krate-shell")
	cmd.Dir = releaseNativeDir(t)
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0", "GOOS="+runtime.GOOS, "GOARCH="+runtime.GOARCH)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("building krate-shell: %v\n%s", err, out)
	}
	st, err := os.Stat(shell)
	if err != nil {
		t.Fatal(err)
	}
	sizes["shell"] = st.Size()

	release := func(root string) int64 {
		res, err := releaseBuilder(t, root, nil).BuildNativeRelease(NativeReleaseOptions{OutDir: filepath.Join(root, "out")})
		if err != nil {
			t.Fatalf("release: %v", err)
		}
		return res.Binaries[0].Size
	}
	sizes["goOnlyRelease"] = release(goOnlyProject(t))
	sizes["tsOnlyRelease"] = release(tsOnlyProject(t, ""))

	ex := e2eRequireDeps(t)
	_, res := e2eBuildRelease(t, ex, filepath.Join(t.TempDir(), "dist-native"))
	sizes["nativeTasksRelease"] = res.Binaries[0].Size

	names := make([]string, 0, len(sizes))
	for n := range sizes {
		names = append(names, n)
	}
	sort.Strings(names)
	if f.Sizes == nil {
		f.Sizes = map[string]map[string]budgetEntry{}
	}
	for _, n := range names {
		budgetCheck(t, f, f.Sizes, "sizes", n, sizes[n], "bytes", 1)
	}
	flushBudgetRecord(t)
	if os.Getenv("KRATE_UPDATE_BUDGETS") == "1" {
		f.MeasuredWith = fmt.Sprintf("%s, %s, CGO_ENABLED=0, -trimpath -s -w", goVersionString(t), budgetPlatform())
		f.save(t)
	}
}

// memBudgetMax is the limit for a measured memory value (name is its entry,
// ending in .pss or .privateDirty): the value plus the headroom, but at least
// the metric's minimum slack above it, so that a small or negative delta does
// not get an impossibly tight limit.
func memBudgetMax(f *budgetFile, name string, v int64) int64 {
	slack := int64(math.Ceil(math.Abs(float64(v)) * f.Headroom))
	min := int64(memoryMinSlackPrivate)
	if strings.HasSuffix(name, ".pss") {
		min = memoryMinSlackPSS
	}
	if slack < min {
		slack = min
	}
	return v + slack
}

// memoryValues flattens the afterLoad sample of a memory report into the
// names of budgets.json.
func memoryValues(s *NativeMemorySample) map[string]int64 {
	out := map[string]int64{}
	put := func(set, role string, pss, pd int64) {
		out[set+"."+role+".pss"] = pss
		out[set+"."+role+".privateDirty"] = pd
	}
	for role, r := range s.App.Roles {
		put("app", role, int64(r.PSS), int64(r.PrivateDirty))
	}
	put("app", "total", int64(s.App.Total.PSS), int64(s.App.Total.PrivateDirty))
	if s.Floor != nil {
		for role, r := range s.Floor.Roles {
			put("floor", role, int64(r.PSS), int64(r.PrivateDirty))
		}
		put("floor", "total", int64(s.Floor.Total.PSS), int64(s.Floor.Total.PrivateDirty))
		for role, r := range s.AppMinusFloor.Roles {
			put("appMinusFloor", role, r.PSS, r.PrivateDirty)
		}
		put("appMinusFloor", "total", s.AppMinusFloor.Total.PSS, s.AppMinusFloor.Total.PrivateDirty)
	}
	return out
}

// medianValues is the per-name median over runs; a name a run lacks counts 0.
func medianValues(runs []map[string]int64) map[string]int64 {
	names := map[string]bool{}
	for _, r := range runs {
		for n := range r {
			names[n] = true
		}
	}
	out := map[string]int64{}
	for n := range names {
		vs := make([]int64, len(runs))
		for i, r := range runs {
			vs[i] = r[n]
		}
		sort.Slice(vs, func(i, j int) bool { return vs[i] < vs[j] })
		out[n] = vs[len(vs)/2]
	}
	return out
}

// memResult is the outcome for one recorded memory entry.
type memResult struct {
	name     string
	value    int64
	entry    budgetEntry
	over     bool
	enforced bool // appMinusFloor entries fail the test, absolute ones warn
}

// evalMemory compares measured values with a platform's entries. Names that
// are absent from vals count 0 (a process role that is gone).
func evalMemory(entries map[string]budgetEntry, vals map[string]int64) []memResult {
	names := make([]string, 0, len(entries))
	for n := range entries {
		names = append(names, n)
	}
	sort.Strings(names)
	var out []memResult
	for _, name := range names {
		e := entries[name]
		v := vals[name]
		out = append(out, memResult{name: name, value: v, entry: e, over: v > e.Max, enforced: strings.HasPrefix(name, "appMinusFloor.")})
	}
	return out
}

// checkMemoryBudgets compares measured values with budgets.json (or, in
// update mode, replaces the platform's memory entries). Only appMinusFloor
// entries fail the test; the absolute ones warn. A platform other than
// linux-amd64 with no memory entries only reports (see budgetRecordText).
func checkMemoryBudgets(t *testing.T, f *budgetFile, vals map[string]int64) {
	t.Helper()
	plat := budgetPlatform()
	if os.Getenv("KRATE_UPDATE_BUDGETS") == "1" {
		if f.Memory == nil {
			f.Memory = map[string]map[string]budgetEntry{}
		}
		f.Memory[plat] = map[string]budgetEntry{}
		for name, v := range vals {
			f.Memory[plat][name] = budgetEntry{Measured: v, Max: memBudgetMax(f, name, v)}
		}
		t.Logf("recorded %d memory entries for %s", len(vals), plat)
		return
	}
	if !budgetEnforced(f.Memory, plat) {
		names := make([]string, 0, len(vals))
		for n := range vals {
			names = append(names, n)
		}
		sort.Strings(names)
		for _, name := range names {
			budgetRecord("memory", name, budgetEntry{Measured: vals[name], Max: memBudgetMax(f, name, vals[name])})
			t.Logf("%s memory %s: %d bytes (no budget recorded yet; reported only)", plat, name, vals[name])
		}
		return
	}
	entries := f.Memory[plat]
	for _, must := range []string{"appMinusFloor.total.pss", "appMinusFloor.total.privateDirty"} {
		if _, ok := entries[must]; !ok {
			t.Fatalf("budgets.json has no memory %s entry for %s. Run with KRATE_UPDATE_BUDGETS=1 to record it", must, plat)
		}
	}
	for _, r := range evalMemory(entries, vals) {
		t.Logf("%s memory %s: %d bytes (limit %d, measured when set %d)", plat, r.name, r.value, r.entry.Max, r.entry.Measured)
		if !r.over {
			continue
		}
		msg := fmt.Sprintf("memory %s is over its budget: %d bytes > %d (it measured %d when the budget was set)", r.name, r.value, r.entry.Max, r.entry.Measured)
		if r.enforced {
			t.Errorf("%s. If the growth is intended, update the budget (KRATE_UPDATE_BUDGETS=1, see packages/native/BUDGETS.md)", msg)
		} else {
			t.Logf("WARNING: %s (absolute values move with the WebKit version and the runner, so this only warns)", msg)
			fmt.Printf("::warning::%s\n", msg)
		}
	}
}

// TestNativeBudgetGUI measures the example release executable in a real
// window: the time from process start to the first page's load event (the
// fastest of three cold runs) and its memory after the page has loaded and
// settled, per process role, next to the floor (a blank page in the same
// shell and window size). It runs under KRATE_NATIVE_GUI=1 on Linux (Xvfb is
// enough). The executable is built with the krate_devcontrol tag, which the
// test needs to read the page and sample memory.
func TestNativeBudgetGUI(t *testing.T) {
	if os.Getenv("KRATE_NATIVE_GUI") != "1" {
		t.Skip("set KRATE_NATIVE_GUI=1 to run the native start-up and memory budget test")
	}
	f := loadBudgets(t)
	ex := e2eRequireDeps(t)
	work := t.TempDir()
	bin, res := e2eBuildRelease(t, ex, filepath.Join(work, "dist-native"), "krate_devcontrol")

	var starts []int64
	for i := 0; i < 3; i++ {
		s := e2eStartShell(t, bin, "")
		s.waitReady(60 * time.Second)
		var loadedAt float64
		deadline := time.Now().Add(45 * time.Second)
		for {
			// loadEventEnd is 0 until the load event has finished.
			err := s.query(`(() => { const n = performance.getEntriesByType("navigation")[0]; return n && n.loadEventEnd > 0 ? performance.timeOrigin + n.loadEventEnd : 0; })()`, &loadedAt)
			if err == nil && loadedAt > 0 {
				break
			}
			if time.Now().After(deadline) {
				t.Fatalf("no load event within 45s (last error: %v)", err)
			}
			time.Sleep(50 * time.Millisecond)
		}
		ms := int64(loadedAt) - s.start.UnixMilli()
		if ms <= 0 {
			t.Fatalf("load event %v is not after the process start %v", loadedAt, s.start.UnixMilli())
		}
		starts = append(starts, ms)
		t.Logf("run %d: first page load event %d ms after process start", i+1, ms)
		if _, err := s.send("quit", 10*time.Second); err != nil {
			t.Errorf("quit: %v", err)
		}
		s.stop()
	}
	sort.Slice(starts, func(i, j int) bool { return starts[i] < starts[j] })
	// The fastest run is checked: a shared runner's noise only ever makes a
	// start slower (windows-11-arm once took 1.5s, 6.9s and 6.8s, likely a
	// restart waiting for the previous WebView2 browser process), while a
	// real regression slows every run.
	fastest := starts[0]
	t.Logf("cold start: fastest %d ms, median %d ms", fastest, starts[len(starts)/2])

	// Memory: the app and the floor, each sampled after load plus 5 s.
	floorDir := filepath.Join(work, "floor")
	if err := WriteNativeFloorApp(floorDir, res.Manifest.Window.Width, res.Manifest.Window.Height); err != nil {
		t.Fatal(err)
	}
	shell := e2eBuildShell(t, work)
	// A run's numbers move by several MB (page cache, allocator timing), so
	// the budget uses the median of three runs per value.
	var runs []map[string]int64
	for i := 0; i < 3; i++ {
		rep, err := MeasureNativeMemory(NativeMemoryOptions{
			App:         NativeMemoryProgram{Path: bin},
			Floor:       NativeMemoryProgram{Path: shell, Args: []string{floorDir}},
			WindowWidth: res.Manifest.Window.Width, WindowHeight: res.Manifest.Window.Height,
			Settle: 5 * time.Second, NoStorm: true,
			Log: &testLogWriter{t: t}, Stderr: &testLogWriter{t: t},
		})
		if err != nil {
			t.Fatal(err)
		}
		s := rep.Sample(NativeMemAfterLoad)
		if s == nil || s.Floor == nil {
			t.Fatalf("no afterLoad sample with a floor: %+v (notes %v)", s, rep.Notes)
		}
		t.Logf("run %d\n%s", i+1, FormatNativeMemory(rep))
		runs = append(runs, memoryValues(s))
	}

	if f.Startup == nil {
		f.Startup = map[string]map[string]budgetEntry{}
	}
	budgetCheck(t, f, f.Startup, "startup", "coldStartMs", fastest, "ms", budgetScale(t))
	checkMemoryBudgets(t, f, medianValues(runs))
	flushBudgetRecord(t)
	if os.Getenv("KRATE_UPDATE_BUDGETS") == "1" {
		f.save(t)
	}
}
