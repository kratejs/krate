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
	"testing"
	"time"
)

// The budgets live in packages/native/budgets.json: for each platform, a
// limit for the size of the stripped shell and of three release executables
// (bytes), and for the start-up of the example release executable in a real
// window (cold start in ms, resident memory in KB). A test fails when a
// measurement is over its limit. packages/native/BUDGETS.md says how to move
// a limit.
//
//	KRATE_UPDATE_BUDGETS=1   rewrite measured and max (measured plus headroom)
//	                         of every entry the run measured
//	KRATE_BUDGET_SCALE=2     multiply the start-up limits (slower machines)

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

func budgetPlatform() string { return runtime.GOOS + "-" + runtime.GOARCH }

// budgetCheck compares a measurement with its limit, or in update mode
// records it. scale multiplies the limit.
func budgetCheck(t *testing.T, f *budgetFile, section map[string]map[string]budgetEntry, name string, value int64, unit string, scale float64) {
	t.Helper()
	plat := budgetPlatform()
	if os.Getenv("KRATE_UPDATE_BUDGETS") == "1" {
		if section[plat] == nil {
			section[plat] = map[string]budgetEntry{}
		}
		e := budgetEntry{Measured: value, Max: int64(math.Ceil(float64(value) * (1 + f.Headroom)))}
		section[plat][name] = e
		t.Logf("%s %s: measured %d %s, new limit %d %s", plat, name, value, unit, e.Max, unit)
		return
	}
	e, ok := section[plat][name]
	if !ok {
		t.Fatalf("budgets.json has no %s entry for %s; measured %d %s. Run with KRATE_UPDATE_BUDGETS=1 to record it", name, plat, value, unit)
	}
	limit := int64(float64(e.Max) * scale)
	t.Logf("%s %s: %d %s (limit %d %s, measured when set %d %s)", plat, name, value, unit, limit, unit, e.Measured, unit)
	if value > limit {
		t.Errorf("%s is over its budget: %d %s > %d %s (it measured %d %s when the budget was set). "+
			"If the growth is intended, update the budget (KRATE_UPDATE_BUDGETS=1, see packages/native/BUDGETS.md)", name, value, unit, limit, unit, e.Measured, unit)
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
	if runtime.GOOS != "linux" || runtime.GOARCH != "amd64" {
		t.Skip("the size budgets are recorded for linux-amd64")
	}
	requireRelease(t)
	f := loadBudgets(t)
	sizes := map[string]int64{}

	// The shell, built the way the platform packages build it.
	shell := filepath.Join(t.TempDir(), "krate-shell")
	cmd := exec.Command("go", "build", "-trimpath", "-ldflags=-s -w", "-o", shell, "./cmd/krate-shell")
	cmd.Dir = releaseNativeDir(t)
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0", "GOOS=linux", "GOARCH=amd64")
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
		budgetCheck(t, f, f.Sizes, n, sizes[n], "bytes", 1)
	}
	if os.Getenv("KRATE_UPDATE_BUDGETS") == "1" {
		f.MeasuredWith = fmt.Sprintf("%s, %s, CGO_ENABLED=0, -trimpath -s -w", goVersionString(t), budgetPlatform())
		f.save(t)
	}
}

// TestNativeBudgetGUI measures the example release executable in a real
// window: the time from process start to the first page's load event (the
// median of three cold runs) and the resident memory of the shell and its
// helper processes after the page has loaded. It runs under
// KRATE_NATIVE_GUI=1 on Linux (Xvfb is enough). The executable is built with
// the krate_devcontrol tag, which the test needs to read the page.
func TestNativeBudgetGUI(t *testing.T) {
	if os.Getenv("KRATE_NATIVE_GUI") != "1" {
		t.Skip("set KRATE_NATIVE_GUI=1 to run the native start-up and memory budget test")
	}
	if runtime.GOOS != "linux" {
		t.Skip("the start-up budgets are recorded for linux-amd64")
	}
	if runtime.GOARCH != "amd64" {
		t.Skip("the start-up budgets are recorded for linux-amd64")
	}
	f := loadBudgets(t)
	ex := e2eRequireDeps(t)
	bin, _ := e2eBuildRelease(t, ex, filepath.Join(t.TempDir(), "dist-native"), "krate_devcontrol")

	var starts []int64
	var shellKB, totalKB int
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
		// Let the page and the helper processes settle before reading memory.
		time.Sleep(1500 * time.Millisecond)
		shellKB, totalKB = e2eFootprint(t, s.cmd.Process.Pid)
		if _, err := s.send("quit", 10*time.Second); err != nil {
			t.Errorf("quit: %v", err)
		}
		s.stop()
	}
	sort.Slice(starts, func(i, j int) bool { return starts[i] < starts[j] })
	median := starts[len(starts)/2]

	if f.Startup == nil {
		f.Startup = map[string]map[string]budgetEntry{}
	}
	scale := budgetScale(t)
	budgetCheck(t, f, f.Startup, "coldStartMs", median, "ms", scale)
	budgetCheck(t, f, f.Startup, "rssShellKB", int64(shellKB), "KB", scale)
	budgetCheck(t, f, f.Startup, "rssTotalKB", int64(totalKB), "KB", scale)
	if os.Getenv("KRATE_UPDATE_BUDGETS") == "1" {
		f.save(t)
	}
}
