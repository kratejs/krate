package build

import (
	"encoding/json"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestNativeE2EProcsGUI runs examples/native-procs in the real webview: the
// main page starts a Node worker and talks to it, hands one end of a channel to
// a second view and another to the worker, opens an offscreen window and reads
// frames of it, and reports the automation endpoint and the extension API. It
// goes through the compiler (worker bundle, derived permissions), the shell,
// the SDK bridge and the pages as an app does.
func TestNativeE2EProcsGUI(t *testing.T) {
	if os.Getenv("KRATE_NATIVE_GUI") != "1" {
		t.Skip("set KRATE_NATIVE_GUI=1 to run the native GUI end-to-end test")
	}
	e2eRequireDeps(t)
	if _, err := exec.LookPath("node"); err != nil {
		requireE2E(t, "node is not on PATH (the example's worker runs on Node)")
	}
	ex := filepath.Join(repoRoot(t), "examples", "native-procs")
	if _, err := os.Stat(filepath.Join(ex, "node_modules", "@krate", "app", "dist", "worker.js")); err != nil {
		requireE2E(t, "examples/native-procs has no built @krate/app (run pnpm install and pnpm build)")
	}
	work := t.TempDir()
	appDir := e2eBuildNativeDev(t, ex, filepath.Join(work, "dist-native"))

	raw, err := os.ReadFile(filepath.Join(appDir, "app.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"workers"`, `"host/workers/echo.mjs"`, `"port"`, `"automation"`} {
		if !strings.Contains(string(raw), want) {
			t.Errorf("app.json lacks %s:\n%s", want, raw)
		}
	}
	bin := e2eBuildShell(t, work)
	s := e2eStartShell(t, bin, appDir)
	s.waitReady(60 * time.Second)

	var res string
	for deadline := time.Now().Add(90 * time.Second); ; time.Sleep(400 * time.Millisecond) {
		err = s.query(`document.getElementById("result")?.textContent ?? ""`, &res)
		if err == nil && res != "" {
			break
		}
		if time.Now().After(deadline) {
			var errs string
			_ = s.query(`JSON.stringify(globalThis.__krateErrs ?? [], (k, v) => v instanceof Error ? String(v) + " " + v.stack : v)`, &errs)
			t.Logf("page errors: %s", errs)
			var prog string
			_ = s.query(`globalThis.__step ?? ""`, &prog)
			t.Logf("last step started: %s", prog)
			t.Fatalf("the page did not report within 90s: %q (last query error: %v)", res, err)
		}
	}
	t.Logf("page result: %s", res)
	if strings.HasPrefix(res, "ERROR") {
		var prog string
		_ = s.query(`globalThis.__step ?? ""`, &prog)
		t.Fatalf("%s (in step %q)", res, prog)
	}
	var got struct {
		Worker     map[string]map[string]any `json:"worker"`
		Pong       map[string]any            `json:"pong"`
		WorkerPort map[string]any            `json:"workerPort"`
		Frame      struct {
			W, H int
			PNG  []int `json:"png"`
		} `json:"frame"`
		Pixel    []int `json:"pixel"`
		Seqs     []int `json:"seqs"`
		Endpoint struct {
			Enabled bool   `json:"enabled"`
			Kind    string `json:"kind"`
			CDP     bool   `json:"cdp"`
		} `json:"endpoint"`
		CDP        string   `json:"cdp"`
		Declared   []string `json:"declared"`
		Extensions string   `json:"extensions"`
	}
	if err := json.Unmarshal([]byte(res), &got); err != nil {
		t.Fatal(err)
	}
	if got.Worker["echo"]["n"] != float64(7) {
		t.Errorf("worker echo = %v", got.Worker)
	}
	if got.Pong["pong"] != "ping" {
		t.Errorf("view to view through a port = %v", got.Pong)
	}
	if got.WorkerPort["workerGot"] != "hello worker" {
		t.Errorf("page to worker through a port = %v", got.WorkerPort)
	}
	if got.Frame.W <= 0 || got.Frame.H <= 0 || len(got.Frame.PNG) != 3 || got.Frame.PNG[0] != 'P' {
		t.Errorf("offscreen png frame = %+v", got.Frame)
	}
	// BGRA of a red page.
	if len(got.Pixel) != 4 || got.Pixel[2] < 250 || got.Pixel[0] > 5 || got.Pixel[1] > 5 {
		t.Errorf("offscreen pixel (B,G,R,A) = %v", got.Pixel)
	}
	if len(got.Seqs) != 3 || got.Seqs[0] != 1 || got.Seqs[2] != 3 {
		t.Errorf("frame sequence = %v", got.Seqs)
	}
	if !got.Endpoint.Enabled || got.Endpoint.Kind == "" {
		t.Errorf("automation endpoint = %+v", got.Endpoint)
	}
	// The endpoint really serves (the WebKit inspector page or the CDP
	// version document) on the configured port; Safari's has no URL.
	if got.Endpoint.Kind == "webkit-inspector" || got.Endpoint.Kind == "cdp" {
		u := "http://127.0.0.1:19321/"
		if got.Endpoint.Kind == "cdp" {
			u += "json/version"
		}
		cl := &http.Client{Timeout: 10 * time.Second}
		if resp, err := cl.Get(u); err != nil {
			t.Errorf("the %s endpoint does not answer at %s: %v", got.Endpoint.Kind, u, err)
		} else {
			_ = resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				t.Errorf("the %s endpoint answered %s at %s", got.Endpoint.Kind, resp.Status, u)
			}
		}
	}
	if got.Endpoint.Kind != "cdp" && got.CDP != "unsupported" {
		t.Errorf("cdp on a non-CDP engine = %q, want unsupported", got.CDP)
	}
	if len(got.Declared) != 0 {
		t.Errorf("declared extensions = %v", got.Declared)
	}
	if got.Extensions != "unsupported" && got.Extensions != "ok" {
		t.Errorf("extensions = %q", got.Extensions)
	}
	e2eNoPageErrors(t, s)
	if _, err := s.send("quit", 10*time.Second); err != nil {
		t.Errorf("quit: %v", err)
	}
}
