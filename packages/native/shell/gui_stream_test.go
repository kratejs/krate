package shell

import (
	"fmt"
	"testing"
)

// A stream belongs to the view that started it. Stream ids are a counter, so
// a window with an empty scope could otherwise open the stream another window
// started (forging that window's id in x-krate-window for good measure) and
// read what the producer emits with the starter's permissions. The starter
// must still be able to open it afterwards.
func TestGUIStreamCannotBeTakenByAnotherWindow(t *testing.T) {
	if guiCtl == nil {
		t.Skip("set KRATE_NATIVE_GUI=1 to run GUI tests")
	}
	// Eval runs in every view; only the main page acts.
	const start = `(async function () {
  if (location.pathname !== "/") return;
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
    return res;
  }
  var out = {};
  try {
    out.sid = (await call("krate/window.events", [])).stream;
    window.__hijackSid = out.sid;
    out.win = (await call("krate/window.open", [{title: "hijack", path: "/hijack/?sid=" + out.sid, permissions: []}])).ok;
    window.__hijackWin = out.win;
  } catch (e) { out.error = String(e); }
  await fetch("/__test/report", {method: "POST", body: JSON.stringify({kind: "hijack-start", out: out})});
})()`
	calls := guiSink.wait(t, "calls", nil)
	guiSink.pending = append(guiSink.pending, calls)
	if err := guiCtl.Eval(start); err != nil {
		t.Fatal(err)
	}
	st := guiSink.wait(t, "hijack-start", nil)
	out, _ := st["out"].(map[string]any)
	if out["error"] != nil || out["sid"] == nil {
		t.Fatalf("starting the stream: %v", out)
	}
	got := guiSink.wait(t, "hijack", nil)
	if got["status"] != float64(404) {
		t.Errorf("a window with an empty scope opened another window's stream: status %v, want 404", got["status"])
	}
	const owner = `(async function () {
  if (location.pathname !== "/") return;
  var ctrl = new AbortController();
  var status = await Promise.race([
    fetch("/__krate/stream/" + encodeURIComponent(window.__hijackSid), {signal: ctrl.signal}).then(function (r) { return r.status; }, function () { return -1; }),
    new Promise(function (res) { setTimeout(function () { res("open"); }, 3000); })
  ]);
  ctrl.abort();
  try {
    var j = new TextEncoder().encode(JSON.stringify({calls: [{id: 1, fn: "krate/window.close", args: [window.__hijackWin]}]}));
    var b = new Uint8Array(4 + j.length + 2);
    new DataView(b.buffer).setUint32(0, j.length);
    b.set(j, 4);
    await fetch("/__krate/call", {method: "POST", body: b});
  } catch (e) {}
  await fetch("/__test/report", {method: "POST", body: JSON.stringify({kind: "hijack-owner", status: status})});
})()`
	if err := guiCtl.Eval(owner); err != nil {
		t.Fatal(err)
	}
	if o := guiSink.wait(t, "hijack-owner", nil); o["status"] != float64(200) && o["status"] != "open" {
		t.Errorf("the window that started the stream could not open it: %s", fmt.Sprint(o["status"]))
	}
}
