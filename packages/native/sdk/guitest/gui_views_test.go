//go:build linux

package guitest

import (
	"encoding/json"
	"io"
	"net/http"

	"github.com/kratejs/krate/packages/native/platform/caps"
)

// identify is what the shell does with the identity the platform attaches:
// the SDK maps the view to the window or view id that scopes are kept under.
func identify(r *http.Request) (string, bool) {
	ref, ok := caps.ViewOf(r)
	if !ok {
		return "", false
	}
	if id, ok := host.ViewWindow(ref); ok {
		return id, true
	}
	return "?unknown-view", true
}

type forgeReport struct {
	Tag    string            `json:"tag"`
	Result map[string]string `json:"result"`
}

var (
	forgeReports = make(chan forgeReport, 16)
)

const forgePage = `<!doctype html><title>forge</title><body>forge<script>
(async function () {
  var tag = new URLSearchParams(location.search).get("tag") || "";
  function frame(o) {
    var j = new TextEncoder().encode(JSON.stringify(o));
    var b = new Uint8Array(4 + j.length + 2);
    new DataView(b.buffer).setUint32(0, j.length);
    b.set(j, 4);
    return b;
  }
  // The result is the error code of the first result frame, or "ok".
  async function call(fn, args, header) {
    var h = {};
    if (header !== undefined) h["x-krate-window"] = header;
    var r = await fetch("/__krate/call", {method: "POST", headers: h, body: frame({calls: [{id: 1, fn: fn, args: args}]})});
    var buf = new Uint8Array(await r.arrayBuffer());
    var n = new DataView(buf.buffer).getUint32(0);
    var res = JSON.parse(new TextDecoder().decode(buf.subarray(4, 4 + n)));
    return res.err ? res.err.code : "ok";
  }
  var out = {};
  out.plain = await call("krate/kv.get", ["k"]);
  out.forgedMain = await call("krate/kv.get", ["k"], "main");
  out.forgedEmpty = await call("krate/kv.get", ["k"], "");
  out.forgedOther = await call("krate/kv.get", ["k"], "w999");
  await fetch("/forge-report", {method: "POST", body: JSON.stringify({tag: tag, result: out})});
})();
</script></body>`

func registerForgeHandlers(mux *http.ServeMux) {
	mux.HandleFunc("/forge", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = io.WriteString(w, forgePage)
	})
	mux.HandleFunc("/forge-report", func(w http.ResponseWriter, r *http.Request) {
		var rep forgeReport
		_ = json.NewDecoder(r.Body).Decode(&rep)
		forgeReports <- rep
		w.WriteHeader(204)
	})
}

// ---- messaging, events and layout through the SDK ----

type chatReport struct {
	Tag  string          `json:"tag"`
	Kind string          `json:"kind"`
	From string          `json:"from"`
	Data json.RawMessage `json:"data"`
}

var (
	chatReports = make(chan chatReport, 32)
)

const chatPage = `<!doctype html><title>chat</title><body>chat<script>
(function () {
  var q = new URLSearchParams(location.search), tag = q.get("tag"), to = q.get("to");
  function report(kind, o) {
    fetch("/chat-report", {method: "POST", body: JSON.stringify(Object.assign({tag: tag, kind: kind}, o))});
  }
  function frame(o) {
    var j = new TextEncoder().encode(JSON.stringify(o));
    var b = new Uint8Array(4 + j.length + 2);
    new DataView(b.buffer).setUint32(0, j.length);
    b.set(j, 4);
    return b;
  }
  window.addEventListener("krate:view-message", function (e) {
    report("message", {from: e.detail.from, data: e.detail.data});
  });
  report("ready", {});
  if (to) {
    // Sends with a forged window header; "from" must still be this view.
    setTimeout(function () {
      fetch("/__krate/call", {method: "POST", headers: {"x-krate-window": "main"},
        body: frame({calls: [{id: 1, fn: "krate/window.postMessage", args: [to, {hello: tag}]}]})});
    }, 300);
  }
})();
</script></body>`

func registerChatHandlers(mux *http.ServeMux) {
	mux.HandleFunc("/chat", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = io.WriteString(w, chatPage)
	})
	mux.HandleFunc("/chat-report", func(w http.ResponseWriter, r *http.Request) {
		var rep chatReport
		_ = json.NewDecoder(r.Body).Decode(&rep)
		chatReports <- rep
		w.WriteHeader(204)
	})
}
