package shell

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/kratejs/krate/packages/native/krate"
	"github.com/kratejs/krate/packages/native/plugin"
	"github.com/kratejs/krate/packages/native/scheme"
	"github.com/kratejs/krate/packages/native/wire"
)

// scopedPage is opened by the plugin flow in a window whose scope is empty. It
// tries a plugin op and an SDK op and reports what it was told.
const scopedPage = `<!doctype html><html><head><meta charset="utf-8"><title>scoped</title></head><body>scoped<script>
const te = new TextEncoder(), td = new TextDecoder();
function frame(obj) {
  const j = te.encode(JSON.stringify(obj));
  const out = new Uint8Array(4 + j.length + 2), dv = new DataView(out.buffer);
  dv.setUint32(0, j.length); out.set(j, 4);
  return out;
}
async function call(fn) {
  const r = await fetch("/__krate/call", {
    method: "POST",
    headers: {"x-krate-window": globalThis.__KRATE_WINDOW__ || ""},
    body: frame({calls: [{id: 1, fn, args: []}]}),
  });
  const b = new Uint8Array(await r.arrayBuffer());
  const n = new DataView(b.buffer).getUint32(0);
  return JSON.parse(td.decode(b.slice(4, 4 + n)));
}
(async () => {
  const rep = {window: globalThis.__KRATE_WINDOW__ || ""};
  try {
    rep.plugin = await call("plugin/gt.hello");
    rep.open = await call("plugin/gt.open");
    rep.sdk = await call("krate/window.list");
  } catch (e) { rep.error = String(e); }
  await fetch("/__test/report", {method: "POST", body: JSON.stringify(Object.assign({kind: "scoped", origin: location.origin}, rep))});
})();
</script></body></html>`

// enableGUIPlugin lists the test plugin gt, and the SDK ids the plugin flow
// uses, in the manifest of the GUI test app.
func enableGUIPlugin(dir string) error {
	path := filepath.Join(dir, ManifestFile)
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		return err
	}
	m["plugins"] = []string{"gt"}
	m["permissions"] = []string{"plugin:gt.read", "plugin:gt.admin", "window", "app"}
	out, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, out, 0o644)
}

// guiPlugin is a plugin that records its lifecycle.
type guiPlugin struct {
	mu         sync.Mutex
	started    bool
	dispatched bool
	created    []string
	closed     []string
	ctx        *plugin.Context
}

func (g *guiPlugin) state() string {
	g.mu.Lock()
	defer g.mu.Unlock()
	return fmt.Sprintf("started=%v dispatched=%v created=%s", g.started, g.dispatched, strings.Join(g.created, ","))
}

func newGUIPlugin() *plugin.Plugin {
	g := &guiPlugin{}
	str := func(s func() string) scheme.Func {
		return func(_ krate.Ctx, _ *wire.Decoder, out *wire.Encoder) error {
			out.String(s())
			return nil
		}
	}
	return &plugin.Plugin{
		Meta: plugin.Meta{
			Name: "gt",
			Permissions: []plugin.PermissionInfo{
				{Name: "read", Doc: "read state"}, {Name: "admin", Doc: "administer"},
			},
			Ops: []plugin.OpInfo{
				{Name: "hello", Permission: "read", Manual: true},
				{Name: "state", Permission: "read", Manual: true},
				{Name: "open", Manual: true},
			},
		},
		Init: func(c *plugin.Context) error {
			g.ctx = c
			c.OnStart(func() error {
				g.mu.Lock()
				g.started = c.App() != nil
				g.mu.Unlock()
				c.Dispatch(func() { g.mu.Lock(); g.dispatched = true; g.mu.Unlock() })
				return nil
			})
			c.OnWindowCreated(func(id string) { g.mu.Lock(); g.created = append(g.created, id); g.mu.Unlock() })
			c.OnWindowClosed(func(id string) { g.mu.Lock(); g.closed = append(g.closed, id); g.mu.Unlock() })
			for op, f := range map[string]scheme.Func{
				"hello": str(func() string { return "hello from gt" }),
				"state": str(g.state),
				"open":  str(func() string { return "open" }),
			} {
				if err := c.Func(op, f); err != nil {
					return err
				}
			}
			return nil
		},
	}
}

func TestGUIPlugins(t *testing.T) {
	if guiCtl == nil {
		t.Skip("set KRATE_NATIVE_GUI=1 to run GUI tests")
	}
	sink := guiSink
	if err := guiCtl.Eval("pluginFlow()"); err != nil {
		t.Fatal(err)
	}
	rep := sink.wait(t, "plugin", nil)
	if rep["error"] != nil {
		t.Fatalf("plugin flow: %v", rep["error"])
	}
	if rep["hello"] != "hello from gt" {
		t.Errorf("plugin op from the main window = %v", rep["hello"])
	}
	st := fmt.Sprint(rep["state"])
	for _, want := range []string{"started=true", "dispatched=true", "created=main"} {
		if !strings.Contains(st, want) {
			t.Errorf("plugin state %q lacks %q", st, want)
		}
	}
	if rep["opened"] == nil || rep["opened"] == "" {
		t.Fatalf("window.open answered %v", rep["opened"])
	}
	sc := sink.wait(t, "scoped", nil)
	if sc["error"] != nil {
		t.Fatalf("scoped window: %v", sc["error"])
	}
	denied := func(what string, v any) {
		t.Helper()
		m, _ := v.(map[string]any)
		e, _ := m["err"].(map[string]any)
		if e == nil || e["code"] != "permission_denied" {
			t.Errorf("%s in a window scoped to nothing: %v, want permission_denied", what, v)
		}
	}
	denied("the gated plugin op", sc["plugin"])
	denied("an SDK op", sc["sdk"])
	if m, _ := sc["open"].(map[string]any); m == nil || m["ok"] != "open" {
		t.Errorf("an ungated plugin op in the scoped window: %v", sc["open"])
	}
	if !strings.HasPrefix(fmt.Sprint(sc["window"]), "w") {
		t.Errorf("scoped window id = %v", sc["window"])
	}
}
