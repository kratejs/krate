package windows

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseDropMsg(t *testing.T) {
	m, ok, err := parseDropMsg(`{"k":"drop","x":120,"y":80}`)
	if err != nil || !ok || m.X != 120 || m.Y != 80 {
		t.Fatalf("drop = %+v %v %v", m, ok, err)
	}
	for _, s := range []string{
		`{"k":"open","id":7,"p":"/__krate/signals","h":[["a","b"]]}`,
		`{"k":"cancel","id":7}`,
		`{"x":1}`,
		strings.Repeat(" ", maxDropMsg+1) + `{"k":"drop"}`,
	} {
		if _, ok, err := parseDropMsg(s); ok || err != nil {
			t.Errorf("%.40q: ok=%v err=%v; it is not a drop and not an error", s, ok, err)
		}
	}
	for _, s := range []string{`{`, `{"k":"drop","x":-5,"y":0}`, `{"k":"drop","x":1,"y":99999999}`} {
		if _, ok, err := parseDropMsg(s); ok || err == nil {
			t.Errorf("%q accepted (ok=%v err=%v)", s, ok, err)
		}
	}
}

// The injected script is exercised in node against a fake WebView2 message
// channel: a drop with files posts them along with the position, and nothing
// else (no files, another origin, a frame, no WebView2) posts anything.
func TestDropShimInNode(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		if os.Getenv("KRATE_REQUIRE_E2E") == "1" {
			t.Fatal("node is required")
		}
		t.Skip("node not found")
	}
	harness := `
const assert = (c, m) => { if (!c) { console.error("FAIL " + m); process.exit(1); } };
function run(opts) {
  const posted = [];
  const handlers = [];
  const g = {
    location: new URL(opts.origin || "` + Origin + `/index.html"),
    addEventListener(n, f, capture) { handlers.push({ n, f, capture }); },
    chrome: opts.noWebview ? undefined : { webview: {
      postMessageWithAdditionalObjects: opts.noAdditional ? undefined : (m, objs) => posted.push({ m: JSON.parse(m), objs }),
    }},
  };
  g.window = g;
  g.top = opts.frame ? {} : g;
  new Function("window", "location", "addEventListener", "document", process.env.SHIM.replace(/^\(function\(\)\{/, "").replace(/\}\)\(\);$/, ""))
    .call(g, g, g.location, g.addEventListener.bind(g), {});
  return { posted, handlers, g };
}
{
  const { posted, handlers } = run({});
  assert(handlers.length === 1 && handlers[0].n === "drop" && handlers[0].capture === true, "one capture-phase drop listener");
  const f1 = { name: "a.txt" }, f2 = { name: "b.txt" };
  handlers[0].f({ dataTransfer: { files: { length: 2, 0: f1, 1: f2 } }, clientX: 120.4, clientY: 79.6 });
  assert(posted.length === 1, "one message");
  assert(posted[0].m.k === "drop" && posted[0].m.x === 120 && posted[0].m.y === 80, "position " + JSON.stringify(posted[0].m));
  assert(Array.isArray(posted[0].objs) && posted[0].objs.length === 2 && posted[0].objs[0] === f1, "the files go as an array");
  handlers[0].f({ dataTransfer: { files: { length: 0 } }, clientX: 1, clientY: 1 });
  handlers[0].f({ dataTransfer: null, clientX: 1, clientY: 1 });
  assert(posted.length === 1, "no files, nothing posted");
  handlers[0].f({ dataTransfer: { files: { length: 1, 0: f1 } }, clientX: -4, clientY: NaN });
  assert(posted.length === 2 && posted[1].m.x === 0 && posted[1].m.y === 0, "odd positions are clamped");
}
for (const [name, opts] of [["another origin", { origin: "https://example.com/" }], ["a frame", { frame: true }], ["no webview", { noWebview: true }], ["an old runtime", { noAdditional: true }]]) {
  const { handlers } = run(opts);
  assert(handlers.length === 0, name + " installs nothing");
}
console.log("ok");
`
	f := filepath.Join(t.TempDir(), "h.js")
	if err := os.WriteFile(f, []byte(harness), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(node, f)
	cmd.Env = append(os.Environ(), "SHIM="+dropShim)
	out, err := cmd.CombinedOutput()
	if err != nil || !strings.Contains(string(out), "ok") {
		t.Fatalf("node: %v\n%s", err, out)
	}
}
