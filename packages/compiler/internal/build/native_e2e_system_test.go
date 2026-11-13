package build

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

// sysStep is the outcome of one SDK call the test page made.
type sysStep struct {
	OK      bool            `json:"ok"`
	Code    string          `json:"code"`
	Message string          `json:"message"`
	Value   json.RawMessage `json:"value"`
}

type sysPage struct {
	Done   bool
	Events []json.RawMessage
	Steps  map[string]sysStep
}

func (p *sysPage) UnmarshalJSON(b []byte) error {
	var all map[string]json.RawMessage
	if err := json.Unmarshal(b, &all); err != nil {
		return err
	}
	p.Steps = map[string]sysStep{}
	for k, v := range all {
		switch k {
		case "done":
			_ = json.Unmarshal(v, &p.Done)
		case "events":
			_ = json.Unmarshal(v, &p.Events)
		default:
			var s sysStep
			if json.Unmarshal(v, &s) == nil {
				p.Steps[k] = s
			}
		}
	}
	return nil
}

// e2eSystemApp copies testdata/native_system into a scratch project that
// under examples/ that links the repo's JS packages, and builds it as a native app.
func e2eSystemApp(t *testing.T, work string) string {
	t.Helper()
	e2eRequireDeps(t)
	root := repoRoot(t)
	// The project lives under examples/ so the compiler finds the runtime
	// bundles the way it does for the real examples; a leading dot keeps it out
	// of the workspace globs, and it is removed afterwards.
	proj, err := os.MkdirTemp(filepath.Join(root, "examples"), ".e2e-system-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(proj) })
	for _, f := range []string{"krate.config.ts", filepath.Join("src", "pages", "index.tsx")} {
		b, err := os.ReadFile(filepath.Join("testdata", "native_system", f))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(filepath.Dir(filepath.Join(proj, f)), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(proj, f), b, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.MkdirAll(filepath.Join(proj, "node_modules", "@krate"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{"app", "core", "runtime"} {
		if err := os.Symlink(filepath.Join(root, "packages", p), filepath.Join(proj, "node_modules", "@krate", p)); err != nil {
			t.Skipf("cannot link the JS packages (symlinks unavailable): %v", err)
		}
	}
	return e2eBuildNative(t, proj, filepath.Join(work, "dist-native"))
}

func (s *e2eShell) systemPage(t *testing.T, wantDone bool) sysPage {
	t.Helper()
	var p sysPage
	deadline := time.Now().Add(60 * time.Second)
	for {
		err := s.query(`globalThis.__sys ?? {done: false}`, &p)
		if err == nil && (p.Done || !wantDone) {
			return p
		}
		if time.Now().After(deadline) {
			var errs string
			_ = s.query(`JSON.stringify(globalThis.__krateErrs ?? [], (k, v) => v instanceof Error ? String(v) + " " + v.stack : v)`, &errs)
			t.Fatalf("the system page did not finish within 60s (last query error: %v, page errors: %s)", err, errs)
		}
		time.Sleep(200 * time.Millisecond)
	}
}

// TestNativeE2ESystemGUI drives the OS integration areas from a page through
// the real shell: displays, theme, power, badge, launch at login and crash
// reports, and then kills the webview's render process to check the crash
// event, the report and the automatic reload.
func TestNativeE2ESystemGUI(t *testing.T) {
	if os.Getenv("KRATE_NATIVE_GUI") != "1" {
		t.Skip("set KRATE_NATIVE_GUI=1 to run the native GUI end-to-end test")
	}
	work := t.TempDir()
	appDir := e2eSystemApp(t, work)
	bin := e2eBuildShell(t, work)

	// Keep the page's login and crash writes out of the real profile.
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(work, "xdg-config"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(work, "xdg-data"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(work, "xdg-cache"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(work, "xdg-state"))
	if runtime.GOOS == "darwin" {
		t.Setenv("HOME", filepath.Join(work, "home"))
		if err := os.MkdirAll(filepath.Join(work, "home"), 0o755); err != nil {
			t.Fatal(err)
		}
	}

	if man, err := os.ReadFile(filepath.Join(appDir, "app.json")); err == nil {
		t.Logf("app.json: %s", man)
	}
	s := e2eStartShell(t, bin, appDir)
	s.waitReady(60 * time.Second)
	p := s.systemPage(t, true)

	// An SDK call either works or says why it cannot; any other error is a bug.
	for name, st := range p.Steps {
		if !st.OK && st.Code != "unsupported" {
			t.Errorf("%s failed: %s %s", name, st.Code, st.Message)
		} else if !st.OK {
			t.Logf("%s: unsupported here: %s", name, st.Message)
		}
	}

	var displays []struct {
		ID      string  `json:"id"`
		Width   int     `json:"width"`
		Scale   float64 `json:"scale"`
		Primary bool    `json:"primary"`
	}
	if st := p.Steps["displays"]; !st.OK {
		t.Errorf("displays: %s", st.Message)
	} else if err := json.Unmarshal(st.Value, &displays); err != nil || len(displays) == 0 || !displays[0].Primary || displays[0].Width <= 0 || displays[0].Scale < 1 {
		t.Errorf("displays = %s (%v)", st.Value, err)
	}
	for _, name := range []string{"primary", "ofWindow", "center"} {
		if !p.Steps[name].OK {
			t.Errorf("%s: %+v", name, p.Steps[name])
		}
	}
	var th struct {
		Scheme string `json:"scheme"`
		System string `json:"system"`
		Source string `json:"source"`
	}
	if st := p.Steps["theme"]; !st.OK {
		t.Errorf("theme: %s", st.Message)
	} else if err := json.Unmarshal(st.Value, &th); err != nil || (th.Scheme != "light" && th.Scheme != "dark") || th.Source != "system" {
		t.Errorf("theme = %s (%v)", st.Value, err)
	}
	if st := p.Steps["power"]; !st.OK {
		t.Errorf("power: %s", st.Message)
	} else {
		var ps struct {
			IdleState string `json:"idleState"`
		}
		if err := json.Unmarshal(st.Value, &ps); err != nil || ps.IdleState == "" {
			t.Errorf("power = %s (%v)", st.Value, err)
		}
	}
	if st := p.Steps["badge"]; st.OK {
		var b struct {
			Shown    string `json:"shown"`
			Progress string `json:"progress"`
			After    string `json:"after"`
		}
		if err := json.Unmarshal(st.Value, &b); err != nil || b.Shown != "3" || b.Progress != "normal" || b.After != "" {
			t.Errorf("badge = %s (%v)", st.Value, err)
		}
	}
	if st := p.Steps["login"]; st.OK {
		var l struct {
			Before, During, After bool
			Method                string
		}
		if err := json.Unmarshal(st.Value, &l); err != nil || l.Before || !l.During || l.After || l.Method == "" {
			t.Errorf("login = %s (%v)", st.Value, err)
		}
	}
	if st := p.Steps["autoReload"]; !st.OK {
		t.Skipf("this platform does not report webview crashes (%s %s); nothing to kill", st.Code, st.Message)
	}

	// Kill the render process. The shell reloads the view because the page
	// asked for it; the reloaded page lists a webview report.
	out, err := s.send("memory", 30*time.Second)
	if err != nil {
		t.Fatalf("memory: %v", err)
	}
	var mem struct {
		Procs []struct {
			PID  int    `json:"pid"`
			Role string `json:"role"`
		} `json:"procs"`
	}
	if err := json.Unmarshal([]byte(out), &mem); err != nil {
		t.Fatal(err)
	}
	killed := 0
	for _, pr := range mem.Procs {
		if pr.Role == "webview:web" && pr.PID > 0 {
			if proc, err := os.FindProcess(pr.PID); err == nil && proc.Kill() == nil {
				killed++
			}
		}
	}
	if killed == 0 {
		t.Skipf("no render process to kill in %+v", mem.Procs)
	}
	t.Logf("killed %d render process(es)", killed)

	var reasons []string
	for deadline := time.Now().Add(60 * time.Second); ; time.Sleep(300 * time.Millisecond) {
		var q struct {
			Done  bool `json:"done"`
			Crash struct {
				OK    bool `json:"ok"`
				Value []struct {
					Kind   string `json:"kind"`
					Reason string `json:"reason"`
				} `json:"value"`
			} `json:"crashList"`
		}
		if err := s.query(`globalThis.__sys ?? {done: false}`, &q); err == nil && q.Done && q.Crash.OK {
			reasons = nil
			for _, r := range q.Crash.Value {
				if r.Kind == "webview" {
					reasons = append(reasons, r.Reason)
				}
			}
			if len(reasons) > 0 {
				break
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("after the render process was killed the page did not come back with a webview crash report (last query error: %v)", err)
		}
	}
	t.Logf("webview crash reports: %v", reasons)
	if _, err := s.send("quit", 10*time.Second); err != nil {
		t.Errorf("quit: %v", err)
	}
}
