package build

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/kratejs/krate/packages/compiler/internal/config"
)

// TestNativeE2EPluginGUI runs examples/native-plugin under the shell that is
// built for its system-info plugin and checks, through the page, that the
// plugin's calls work: info and memory (calls), cpuLoad (a stream, started by
// the button) and load (a host signal).
func TestNativeE2EPluginGUI(t *testing.T) {
	if os.Getenv("KRATE_NATIVE_GUI") != "1" {
		t.Skip("set KRATE_NATIVE_GUI=1 to run the native GUI end-to-end test")
	}
	requireRelease(t)
	ex := filepath.Join(repoRoot(t), "examples", "native-plugin")
	if _, err := os.Stat(filepath.Join(ex, "node_modules", "@krate", "app", "dist", "index.js")); err != nil {
		requireE2E(t, "examples/native-plugin has no built @krate/app (run pnpm install and pnpm build)")
	}
	t.Setenv("KRATE_NATIVE_DIR", filepath.Join(repoRoot(t), "packages", "native"))
	cfg, err := config.Load(ex)
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}
	b := New(ex, cfg)
	b.DevMode = true // as krate native dev: the page reports its errors to the terminal
	work := t.TempDir()
	res, err := b.BuildNativeApp(NativeAppOptions{OutDir: filepath.Join(work, "dist-native"), Dev: true})
	if err != nil {
		t.Fatalf("native build: %v", err)
	}
	shell, err := b.PluginShell(runtime.GOOS, runtime.GOARCH)
	if err != nil {
		t.Fatalf("PluginShell: %v", err)
	}
	s := e2eStartShell(t, shell, res.AppDir)
	s.waitReady(60 * time.Second)

	type pageState struct {
		OS      string `json:"os"`
		CPUs    string `json:"cpus"`
		Memory  string `json:"memory"`
		Load    string `json:"load"`
		Sampled string `json:"sampled"`
		Error   string `json:"error"`
	}
	read := func() (pageState, error) {
		var st pageState
		err := s.query(`({os: document.getElementById("os")?.textContent ?? "",
			cpus: document.getElementById("cpus")?.textContent ?? "",
			memory: document.getElementById("memory")?.textContent ?? "",
			load: document.getElementById("load")?.textContent ?? "",
			sampled: document.getElementById("sampled")?.textContent ?? "",
			error: document.getElementById("error")?.textContent ?? ""})`, &st)
		return st, err
	}
	var st pageState
	deadline := time.Now().Add(60 * time.Second)
	for ; ; time.Sleep(200 * time.Millisecond) {
		st, err = read()
		if err == nil && strings.HasPrefix(st.OS, runtime.GOOS+"/") && strings.HasSuffix(st.CPUs, " CPUs") && strings.HasSuffix(st.Memory, " MiB") {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("the plugin calls did not answer within 60s: %+v (query error: %v)\nshell stderr:\n%s", st, err, s.errs.String())
		}
	}
	if st.Error != "" {
		t.Errorf("the page shows an error: %q", st.Error)
	}
	if !strings.HasPrefix(st.OS, runtime.GOOS+"/"+runtime.GOARCH+" on ") {
		t.Errorf("info() = %q", st.OS)
	}
	if st.CPUs == "0 CPUs" || st.Memory == "0 MiB" {
		t.Errorf("empty readings: %+v", st)
	}

	// The stream: the button starts cpuLoad and the page shows the readings.
	if err := s.query(`document.getElementById("watch").click(), 1`, nil); err != nil {
		t.Fatalf("clicking the button: %v", err)
	}
	// The dev feed of host traffic shows the stream opened (the reading itself
	// can be a real 0.0% on an idle machine, so the page text proves little).
	for deadline = time.Now().Add(30 * time.Second); ; time.Sleep(250 * time.Millisecond) {
		var feed string
		err = s.query(`fetch("/__krate/dev/events").then(r => r.text())`, &feed)
		if err == nil && strings.Contains(feed, "stream-open") && strings.Contains(feed, "plugin/system-info.cpuLoad") {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("cpuLoad never opened a stream (query error %v); feed:\n%s", err, feed)
		}
	}
	st, err = read()
	if err != nil || st.Error != "" {
		t.Errorf("after the stream started: %+v, %v", st, err)
	}
	e2eNoPageErrors(t, s)
	if _, err := s.send("quit", 10*time.Second); err != nil {
		t.Errorf("quit: %v", err)
	}
}
