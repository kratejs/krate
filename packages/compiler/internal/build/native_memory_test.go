package build

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// The memory measurement is driven through the shell's dev control channel.
// These tests run it against a scripted stand-in (this test binary, started
// with KRATE_FAKE_SHELL set), so they need no display. The real thing runs in
// TestNativeBudgetGUI and `krate native profile --memory`.

func TestMain(m *testing.M) {
	if mode := os.Getenv("KRATE_FAKE_SHELL"); mode != "" {
		os.Exit(fakeShell(mode))
	}
	os.Exit(m.Run())
}

// fakeShell speaks the dev control protocol. KRATE_FAKE_LOG names a file that
// receives every command it was sent.
func fakeShell(mode string) int {
	logf, _ := os.OpenFile(os.Getenv("KRATE_FAKE_LOG"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if logf != nil {
		defer logf.Close()
	}
	fmt.Println("ready")
	// PSS and private dirty in MB per role: the app has a Go host, the floor
	// does not, and the app's web process grows after a navigation.
	base := map[string][2]int{"shell": {80, 40}, "webview:web": {100, 38}, "webview:network": {45, 25}}
	if mode == "app" {
		base["gohost"] = [2]int{10, 4}
		base["shell"] = [2]int{83, 41}
	}
	navs := 0
	hidden := false
	sc := bufioScanner(os.Stdin)
	for sc.Scan() {
		line := sc.Text()
		if logf != nil {
			fmt.Fprintln(logf, mode+" "+line)
		}
		cmd, _, _ := strings.Cut(line, " ")
		switch cmd {
		case "query":
			switch {
			case strings.Contains(line, "krate/window.hide"):
				hidden = true
				fmt.Println(`ok "ok"`)
			default:
				fmt.Println("ok true")
			}
		case "eval":
			navs++
			fmt.Println("ok")
		case "memory":
			type proc struct {
				PID          int    `json:"pid"`
				Name         string `json:"name"`
				Role         string `json:"role"`
				PSS          uint64 `json:"pss"`
				PrivateDirty uint64 `json:"privateDirty"`
			}
			var procs []proc
			pid := 100
			for role, v := range base {
				pss, pd := v[0], v[1]
				if role == "webview:web" {
					pss += navs / 2
				}
				if hidden && role == "webview:web" {
					pss -= 20
				}
				pid++
				procs = append(procs, proc{pid, role, role, uint64(pss) << 20, uint64(pd) << 20})
			}
			// A second web process: roles are summed.
			procs = append(procs, proc{999, "web2", "webview:web", 5 << 20, 1 << 20})
			data, _ := json.Marshal(map[string]any{"os": "linux", "source": "smaps_rollup", "procs": procs, "notes": []string{"fake note"}})
			fmt.Println("ok " + string(data))
		case "quit":
			fmt.Println("ok")
			return 0
		default:
			fmt.Println("error unknown command")
		}
	}
	return 0
}

func TestMeasureNativeMemoryDeltas(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	logPath := filepath.Join(t.TempDir(), "commands.log")
	t.Setenv("KRATE_FAKE_LOG", logPath)
	// The stand-in is told its mode by an argument-free env var, so the two
	// programs are two copies of this binary started through wrappers.
	appBin, floorBin := fakeShellProgram(t, exe, "app"), fakeShellProgram(t, exe, "floor")

	rep, err := MeasureNativeMemory(NativeMemoryOptions{
		App: NativeMemoryProgram{Path: appBin}, Floor: NativeMemoryProgram{Path: floorBin},
		WindowWidth: 960, WindowHeight: 640,
		Routes: []string{"/", "/tasks/"}, Rounds: 3,
		Settle: time.Millisecond, Idle: time.Millisecond, Pause: time.Millisecond, Hide: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if rep.OS == "" || rep.Source != "smaps_rollup" || rep.Window.Width != 960 {
		t.Fatalf("header: %+v", rep)
	}
	var names []string
	for _, s := range rep.Samples {
		names = append(names, s.Name)
	}
	if got := strings.Join(names, ","); got != "afterLoad,afterIdle,afterStorm,hidden" {
		t.Fatalf("samples: %s", got)
	}

	load := rep.Sample(NativeMemAfterLoad)
	mb := uint64(1 << 20)
	// web: 100 + 5 (second process) = 105 in both runs, so no delta.
	if got := load.App.Roles["webview:web"]; got.PSS != 105*mb || got.PrivateDirty != 39*mb {
		t.Errorf("app web role: %+v", got)
	}
	d := load.AppMinusFloor
	if d == nil || load.Floor == nil {
		t.Fatalf("no floor on afterLoad: %+v", load)
	}
	if d.Roles["webview:web"].PSS != 0 || d.Roles["shell"].PSS != 3*int64(mb) || d.Roles["gohost"].PSS != 10*int64(mb) {
		t.Errorf("role deltas: %+v", d.Roles)
	}
	if _, ok := load.Floor.Roles["gohost"]; ok {
		t.Errorf("the floor has a gohost role: %+v", load.Floor.Roles)
	}
	// Total delta is the sum of the role deltas: 3 + 10 MB of PSS, 1 + 4 MB private dirty.
	if d.Total.PSS != 13*int64(mb) || d.Total.PrivateDirty != 5*int64(mb) {
		t.Errorf("total delta: %+v", d.Total)
	}
	if load.App.Total.PSS != load.Floor.Total.PSS+uint64(d.Total.PSS) {
		t.Errorf("app total %d != floor %d + delta %d", load.App.Total.PSS, load.Floor.Total.PSS, d.Total.PSS)
	}

	// The storm: 3 rounds over 2 routes, then back to the first, in each run;
	// the stand-in grows the web process by navs/2 MB, equally for both.
	storm := rep.Sample(NativeMemAfterStorm)
	if storm.Floor == nil || storm.AppMinusFloor.Roles["webview:web"].PSS != 0 {
		t.Errorf("storm: floor %v delta %+v", storm.Floor != nil, storm.AppMinusFloor)
	}
	if storm.App.Roles["webview:web"].PSS <= load.App.Roles["webview:web"].PSS {
		t.Errorf("the storm did not run: %+v", storm.App.Roles)
	}
	if h := rep.Sample(NativeMemHidden); h == nil || h.App.Roles["webview:web"].PSS >= storm.App.Roles["webview:web"].PSS {
		t.Errorf("hidden: %+v", h)
	}
	if len(rep.Notes) == 0 || !strings.Contains(strings.Join(rep.Notes, "\n"), "fake note") {
		t.Errorf("notes: %v", rep.Notes)
	}

	log, _ := os.ReadFile(logPath)
	nav := 0
	for _, l := range strings.Split(string(log), "\n") {
		if strings.HasPrefix(l, "app eval location.assign") {
			nav++
		}
	}
	if nav != 3*2+1 {
		t.Errorf("the app navigated %d times, want 7:\n%s", nav, log)
	}
	floorNav := strings.Count(string(log), "floor eval location.assign(\"/\")")
	if floorNav != 7 {
		t.Errorf("the floor navigated %d times, want 7", floorNav)
	}
}

// fakeShellProgram returns a program that runs this test binary as the
// stand-in shell in the given mode.
func fakeShellProgram(t *testing.T, exe, mode string) string {
	t.Helper()
	dir := t.TempDir()
	link := filepath.Join(dir, "fake-"+mode)
	script := fmt.Sprintf("#!/bin/sh\nKRATE_FAKE_SHELL=%s exec %q \"$@\"\n", mode, exe)
	if err := os.WriteFile(link, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat("/bin/sh"); err != nil {
		t.Skip("needs /bin/sh")
	}
	return link
}

func TestMeasureNativeMemoryWithoutFloor(t *testing.T) {
	exe, _ := os.Executable()
	rep, err := MeasureNativeMemory(NativeMemoryOptions{
		App:    NativeMemoryProgram{Path: fakeShellProgram(t, exe, "app")},
		Settle: time.Millisecond,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Samples) != 1 || rep.Samples[0].Floor != nil || rep.Samples[0].AppMinusFloor != nil {
		t.Fatalf("samples: %+v", rep.Samples)
	}
	if n := strings.Join(rep.Notes, "\n"); !strings.Contains(n, "no floor run") || !strings.Contains(n, "storm was skipped") {
		t.Errorf("notes: %v", rep.Notes)
	}
}

func TestMemoryMinus(t *testing.T) {
	a := NativeMemoryPoint{Roles: map[string]NativeMemoryRole{
		"shell": {PSS: 90, PrivateDirty: 50}, "gohost": {PSS: 10, PrivateDirty: 5}}, Total: NativeMemoryRole{PSS: 100, PrivateDirty: 55}}
	b := NativeMemoryPoint{Roles: map[string]NativeMemoryRole{
		"shell": {PSS: 95, PrivateDirty: 40}, "webview:gpu": {PSS: 7, PrivateDirty: 3}}, Total: NativeMemoryRole{PSS: 102, PrivateDirty: 43}}
	d := MemoryMinus(a, b)
	want := map[string]NativeMemoryDelta{
		"shell": {PSS: -5, PrivateDirty: 10}, "gohost": {PSS: 10, PrivateDirty: 5}, "webview:gpu": {PSS: -7, PrivateDirty: -3}}
	if len(d.Roles) != len(want) {
		t.Fatalf("roles: %+v", d.Roles)
	}
	for k, v := range want {
		if d.Roles[k] != v {
			t.Errorf("%s: got %+v, want %+v", k, d.Roles[k], v)
		}
	}
	if d.Total != (NativeMemoryDelta{PSS: -2, PrivateDirty: 12}) {
		t.Errorf("total: %+v", d.Total)
	}
}

func TestWriteNativeMemoryReportShape(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "report.json"), []byte(`{"appId":"a.b","layout":"executable","extra":{"keep":true}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	f := NativeMemoryPoint{Roles: map[string]NativeMemoryRole{"shell": {PSS: 5, PrivateDirty: 2}}, Total: NativeMemoryRole{PSS: 5, PrivateDirty: 2}}
	a := NativeMemoryPoint{Roles: map[string]NativeMemoryRole{"shell": {PSS: 8, PrivateDirty: 3}}, Total: NativeMemoryRole{PSS: 8, PrivateDirty: 3}}
	d := MemoryMinus(a, f)
	rep := &NativeMemoryReport{OS: "linux", Source: "smaps_rollup", Samples: []NativeMemorySample{{Name: "afterLoad", App: a, Floor: &f, AppMinusFloor: &d}}}
	rep.Window.Width, rep.Window.Height = 960, 640
	if err := WriteNativeMemory(dir, rep); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(filepath.Join(dir, "report.json"))
	var doc struct {
		AppID  string          `json:"appId"`
		Extra  json.RawMessage `json:"extra"`
		Memory struct {
			OS      string `json:"os"`
			Source  string `json:"source"`
			Window  struct{ Width, Height int }
			Samples []struct {
				Name string `json:"name"`
				App  struct {
					Roles map[string]map[string]uint64 `json:"roles"`
					Total map[string]uint64            `json:"total"`
				} `json:"app"`
				Floor         *struct{} `json:"floor"`
				AppMinusFloor struct {
					Roles map[string]map[string]int64 `json:"roles"`
					Total map[string]int64            `json:"total"`
				} `json:"appMinusFloor"`
			} `json:"samples"`
		} `json:"memory"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("%v\n%s", err, data)
	}
	if doc.AppID != "a.b" || !strings.Contains(string(doc.Extra), "keep") {
		t.Errorf("the rest of report.json was not kept: %s", data)
	}
	m := doc.Memory
	if m.OS != "linux" || m.Source != "smaps_rollup" || m.Window.Width != 960 || len(m.Samples) != 1 {
		t.Fatalf("memory: %s", data)
	}
	s := m.Samples[0]
	if s.Name != "afterLoad" || s.App.Roles["shell"]["pss"] != 8 || s.App.Roles["shell"]["privateDirty"] != 3 ||
		s.App.Total["pss"] != 8 || s.Floor == nil || s.AppMinusFloor.Roles["shell"]["pss"] != 3 || s.AppMinusFloor.Total["privateDirty"] != 1 {
		t.Errorf("sample shape: %s", data)
	}
	// NativeReport carries the same section.
	var nr NativeReport
	if err := json.Unmarshal(data, &nr); err != nil || nr.Memory == nil || nr.Memory.Samples[0].AppMinusFloor.Total.PSS != 3 {
		t.Errorf("NativeReport.Memory: %v %+v", err, nr.Memory)
	}
}

func TestNativeStaticRoutes(t *testing.T) {
	dir := t.TempDir()
	for _, f := range []string{"dist/index.html", "dist/about.html", "dist/tasks/index.html", "dist/tasks/[id]/index.html", "dist/404.html", "dist/_assets/app.js"} {
		p := filepath.Join(dir, filepath.FromSlash(f))
		_ = os.MkdirAll(filepath.Dir(p), 0o755)
		if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	got, err := NativeStaticRoutes(dir)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(got, " ") != "/ /about /tasks/" {
		t.Errorf("routes: %v", got)
	}
}

func TestFormatNativeMemory(t *testing.T) {
	f := NativeMemoryPoint{Roles: map[string]NativeMemoryRole{"shell": {PSS: 80 << 20, PrivateDirty: 40 << 20}}, Total: NativeMemoryRole{PSS: 80 << 20, PrivateDirty: 40 << 20}}
	a := NativeMemoryPoint{Roles: map[string]NativeMemoryRole{"shell": {PSS: 83 << 20, PrivateDirty: 41 << 20}, "gohost": {PSS: 10 << 20}}, Total: NativeMemoryRole{PSS: 93 << 20, PrivateDirty: 41 << 20}}
	d := MemoryMinus(a, f)
	out := FormatNativeMemory(&NativeMemoryReport{OS: "linux", Source: "smaps_rollup", Notes: []string{"n1"},
		Samples: []NativeMemorySample{{Name: "afterLoad", App: a, Floor: &f, AppMinusFloor: &d}}})
	for _, want := range []string{"afterLoad", "app - floor", "gohost", "83.0 / 41.0", "+3.0 / +1.0", "+13.0 / +1.0", "note: n1"} {
		if !strings.Contains(out, want) {
			t.Errorf("table lacks %q:\n%s", want, out)
		}
	}
}

func bufioScanner(f *os.File) *bufio.Scanner {
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64*1024), 16<<20)
	return sc
}

func TestMemoryBudgetMax(t *testing.T) {
	f := &budgetFile{Headroom: 0.25}
	for _, tc := range []struct {
		name    string
		v, want int64
	}{
		{"app.total.pss", 200 << 20, 250 << 20},
		{"app.total.privateDirty", 100 << 20, 125 << 20},
		{"appMinusFloor.total.pss", 10 << 20, 10<<20 + memoryMinSlackPSS},
		{"appMinusFloor.total.privateDirty", 4 << 20, 4<<20 + memoryMinSlackPrivate},
		{"appMinusFloor.shell.pss", -2 << 20, -2<<20 + memoryMinSlackPSS},
		{"appMinusFloor.gohost.privateDirty", 0, memoryMinSlackPrivate},
	} {
		if got := memBudgetMax(f, tc.name, tc.v); got != tc.want {
			t.Errorf("memBudgetMax(%s, %d) = %d, want %d", tc.name, tc.v, got, tc.want)
		}
	}
}

func TestMemoryValuesNames(t *testing.T) {
	f := NativeMemoryPoint{Roles: map[string]NativeMemoryRole{"shell": {PSS: 5, PrivateDirty: 2}}, Total: NativeMemoryRole{PSS: 5, PrivateDirty: 2}}
	a := NativeMemoryPoint{Roles: map[string]NativeMemoryRole{"shell": {PSS: 8, PrivateDirty: 3}, "gohost": {PSS: 4, PrivateDirty: 1}}, Total: NativeMemoryRole{PSS: 12, PrivateDirty: 4}}
	d := MemoryMinus(a, f)
	v := memoryValues(&NativeMemorySample{Name: "afterLoad", App: a, Floor: &f, AppMinusFloor: &d})
	want := map[string]int64{
		"app.shell.pss": 8, "app.shell.privateDirty": 3, "app.gohost.pss": 4, "app.gohost.privateDirty": 1,
		"app.total.pss": 12, "app.total.privateDirty": 4,
		"floor.shell.pss": 5, "floor.shell.privateDirty": 2, "floor.total.pss": 5, "floor.total.privateDirty": 2,
		"appMinusFloor.shell.pss": 3, "appMinusFloor.shell.privateDirty": 1,
		"appMinusFloor.gohost.pss": 4, "appMinusFloor.gohost.privateDirty": 1,
		"appMinusFloor.total.pss": 7, "appMinusFloor.total.privateDirty": 2,
	}
	if len(v) != len(want) {
		t.Fatalf("values: %v", v)
	}
	for k, w := range want {
		if v[k] != w {
			t.Errorf("%s = %d, want %d", k, v[k], w)
		}
	}
}

// budgets.json holds the memory entries the GUI test reads and no leftover
// resident-set-size entries.
func TestBudgetsFileMemoryKeys(t *testing.T) {
	f := loadBudgets(t)
	m := f.Memory["linux-amd64"]
	for _, k := range []string{"appMinusFloor.total.pss", "appMinusFloor.total.privateDirty", "app.total.pss", "floor.total.pss", "floor.total.privateDirty"} {
		e, ok := m[k]
		if !ok {
			t.Errorf("budgets.json memory has no %s", k)
			continue
		}
		if e.Max < e.Measured {
			t.Errorf("%s: max %d below measured %d", k, e.Max, e.Measured)
		}
	}
	for k := range f.Startup["linux-amd64"] {
		if strings.HasPrefix(k, "rss") {
			t.Errorf("startup.%s is a summed-RSS entry; memory budgets are PSS and private dirty", k)
		}
	}
}

func TestMedianValues(t *testing.T) {
	got := medianValues([]map[string]int64{
		{"a": 10, "b": 5}, {"a": 30, "b": -5}, {"a": 20},
	})
	if got["a"] != 20 || got["b"] != 0 {
		t.Errorf("medians: %v", got)
	}
}
