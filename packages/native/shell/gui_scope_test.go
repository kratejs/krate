package shell

import (
	"fmt"
	"testing"
)

// The page of a scoped window cannot reach a wider scope by sending another
// window's id in x-krate-window: the shell binds each request to the native
// view it came from. The same page in an unscoped window is allowed, so the
// denial is the scope's doing. The windows are opened by the main page itself,
// through the SDK, as an app would.
func TestGUIScopedWindowCannotForgeAWiderScope(t *testing.T) {
	if guiCtl == nil {
		t.Skip("set KRATE_NATIVE_GUI=1 to run GUI tests")
	}
	check := func(tag, want string) {
		t.Helper()
		m := guiSink.wait(t, "forge", func(m map[string]any) bool { return m["tag"] == tag })
		res, _ := m["result"].(map[string]any)
		for _, k := range []string{"plain", "forgedMain", "forgedEmpty", "forgedOther"} {
			if res[k] != want {
				t.Errorf("%s window: %s = %v, want %q (all %v)", tag, k, res[k], want, res)
			}
		}
	}
	const script = `(async function () {
  function frame(o) {
    var j = new TextEncoder().encode(JSON.stringify(o));
    var b = new Uint8Array(4 + j.length + 2);
    new DataView(b.buffer).setUint32(0, j.length);
    b.set(j, 4);
    return b;
  }
  async function call(fn, args) {
    var r = await fetch("/__krate/call", {method: "POST", body: frame({calls: [{id: 1, fn: fn, args: args}]})});
    var buf = new Uint8Array(await r.arrayBuffer());
    var n = new DataView(buf.buffer).getUint32(0);
    var res = JSON.parse(new TextDecoder().decode(buf.subarray(4, 4 + n)));
    if (res.err) throw new Error(res.err.code + ": " + res.err.message);
    return res.ok;
  }
  var ids = {};
  try {
    ids.wide = await call("krate/window.open", [{title: "wide", path: "/forge/?tag=wide"}]);
    ids.narrow = await call("krate/window.open", [{title: "narrow", path: "/forge/?tag=narrow", permissions: ["window"]}]);
    ids.view = await call("krate/window.addView", [ids.wide, {path: "/forge/?tag=narrowview", permissions: ["window"]}]);
  } catch (e) { ids.error = String(e); }
  await fetch("/__test/report", {method: "POST", body: JSON.stringify({kind: "forge-ids", ids: ids})});
  window.__forgeCall = call;
})()`
	// Wait for the main page to load (a script run earlier is lost), and leave
	// its report for the tests that expect it.
	calls := guiSink.wait(t, "calls", nil)
	guiSink.pending = append(guiSink.pending, calls)
	if err := guiCtl.Eval(script); err != nil {
		t.Fatal(err)
	}
	ids := guiSink.wait(t, "forge-ids", nil)
	m, _ := ids["ids"].(map[string]any)
	if m["error"] != nil {
		t.Fatalf("opening windows: %v", m["error"])
	}
	check("wide", "ok")
	check("narrow", "permission_denied")
	check("narrowview", "permission_denied")

	// Clean up through the page that opened them.
	if err := guiCtl.Eval(fmt.Sprintf(`(async function(){ try { await window.__forgeCall("krate/window.close", [%q]); await window.__forgeCall("krate/window.close", [%q]); } catch (e) {} })()`,
		m["narrow"], m["wide"])); err != nil {
		t.Fatal(err)
	}
}
