package shell

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/cookiejar"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/native/hostchild"
	"github.com/kratejs/krate/packages/native/internal/gohosttest"
	"github.com/kratejs/krate/packages/native/wire"
)

const nodeHostV1 = `import { start } from "./hostchild.mjs";
import { readFileSync } from "node:fs";
const h = globalThis.__krateHost;
console.log("loading host v1");
h.register("n.version", () => "node-v1");
h.register("n.readSelf", () => readFileSync(new URL(import.meta.url), "utf8").length > 0);
h.register("n.tags", () => ["a", "b"], "func");
start();
`

const nodeHostV2 = `import { start } from "./hostchild.mjs";
const h = globalThis.__krateHost;
h.register("n.version", () => "node-v2");
// Registered here, absent from app.json: found through the child's listing.
h.register("n.extra", () => "extra");
start();
`

func nodeApp(t *testing.T, root string) string {
	t.Helper()
	app, err := buildTestApp(root, "<!doctype html><title>idx</title>hello", false, 0)
	if err != nil {
		t.Fatal(err)
	}
	writeAll := func(name, s string) {
		if err := writeFile(filepath.Join(app.dir, filepath.FromSlash(name)), s); err != nil {
			t.Fatal(err)
		}
	}
	writeAll("host/node/hostchild.mjs", hostchild.Source)
	writeAll("host/node/host.mjs", nodeHostV1)
	raw, err := os.ReadFile(filepath.Join(app.dir, ManifestFile))
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	m["hostchildren"] = []map[string]any{{
		"runtime": "node", "entry": "host/node/host.mjs", "command": []string{"node", "{entry}"},
		"funcs": []string{"n.version", "n.readSelf"}, "streams": []string{}, "signals": []string{},
	}}
	b, _ := json.Marshal(m)
	writeAll(ManifestFile, string(b))
	return app.dir
}

func callFn(t *testing.T, c *http.Client, base, fn string) string {
	t.Helper()
	resp, err := c.Post(base+"/__krate/call", "application/x-krate-frames", bytes.NewReader(frameReq(fn, nil)))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	d, err := wire.NewDecoder(b)
	if err != nil {
		t.Fatalf("%s: %v (%q)", fn, err, b)
	}
	if err := d.BeginObject(); err != nil {
		t.Fatal(err)
	}
	for d.More() {
		k, _ := d.Key()
		switch k {
		case "ok":
			raw, _ := d.RawValue()
			return string(raw)
		case "err":
			raw, _ := d.RawValue()
			return "ERR " + string(raw)
		default:
			if err := d.Skip(); err != nil {
				t.Fatal(err)
			}
		}
	}
	t.Fatalf("%s: no result in %q", fn, b)
	return ""
}

func TestHostChildNode(t *testing.T) {
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("node is not on PATH")
	}
	root, cleanup, err := gohosttest.TempDir("shell-hc-")
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	dir := nodeApp(t, root)
	sink := newReportSink()
	ctl, stop := runBrowser(t, dir, sink)
	defer stop()

	jar, _ := cookiejar.New(nil)
	c := &http.Client{Jar: jar}
	if resp, _ := get(t, c, ctl.URL(), nil); resp.StatusCode != 200 {
		t.Fatalf("token visit: %d", resp.StatusCode)
	}
	base := ctl.URL()[:strings.Index(ctl.URL(), "/?")]

	if got := callFn(t, c, base, "n.version"); got != `"node-v1"` {
		t.Fatalf("n.version = %s", got)
	}
	if got := callFn(t, c, base, "n.readSelf"); got != "true" {
		t.Fatalf("n.readSelf (node:fs in the child) = %s", got)
	}
	// An id the child registered but app.json does not list is served too.
	if got := callFn(t, c, base, "n.tags"); got != `["a","b"]` {
		t.Fatalf("n.tags = %s", got)
	}
	// The built-in host JS and the child live side by side.
	if got := callFn(t, c, base, "h.tag"); got != `"host-v1"` {
		t.Fatalf("h.tag = %s", got)
	}

	// Reload restarts the child with the script as it is on disk now.
	if err := os.WriteFile(filepath.Join(dir, "host", "node", "host.mjs"), []byte(nodeHostV2), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := ctl.ReloadHostJS(); err != nil {
		t.Fatal(err)
	}
	if got := callFn(t, c, base, "n.version"); got != `"node-v2"` {
		t.Fatalf("n.version after reload = %s", got)
	}
	if got := callFn(t, c, base, "n.extra"); got != `"extra"` {
		t.Fatalf("n.extra after reload = %s", got)
	}
	if got := callFn(t, c, base, "n.nope"); !strings.Contains(got, "not_found") {
		t.Fatalf("n.nope = %s", got)
	}
}
