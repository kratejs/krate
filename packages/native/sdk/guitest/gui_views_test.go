//go:build linux

package guitest

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/kratejs/krate/packages/native/platform/caps"
	"github.com/kratejs/krate/packages/native/platform/linux"
	"github.com/kratejs/krate/packages/native/sdk/window"
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
	forgeSeen    []forgeReport
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

func waitForge(t *testing.T, tag string) map[string]string {
	t.Helper()
	deadline := time.After(20 * time.Second)
	for {
		for i, r := range forgeSeen {
			if r.Tag == tag {
				forgeSeen = append(forgeSeen[:i], forgeSeen[i+1:]...)
				return r.Result
			}
		}
		select {
		case r := <-forgeReports:
			forgeSeen = append(forgeSeen, r)
		case <-deadline:
			t.Fatalf("timed out waiting for the forge report %q", tag)
		}
	}
}

func wantAll(t *testing.T, who string, got map[string]string, want string) {
	t.Helper()
	for _, k := range []string{"plain", "forgedMain", "forgedEmpty", "forgedOther"} {
		if got[k] != want {
			t.Errorf("%s: %s = %q, want %q (all: %v)", who, k, got[k], want, got)
		}
	}
}

// A page in a scoped window cannot claim another window's identity: the shell
// binds every request to the native view it came from and ignores the
// x-krate-window header the page sets.
func TestScopedWindowCannotForgeAWiderScope(t *testing.T) {
	need(t)
	// A wide window: unscoped, so the app's whole set applies.
	wide, err := window.Open(window.Options{Title: "wide", Path: "/forge?tag=wide"})
	if err != nil {
		t.Fatal(err)
	}
	defer wide.Close()
	wantAll(t, "wide window", waitForge(t, "wide"), "ok")

	// A narrow window: it may open windows but not read the key-value store.
	narrow, err := window.Open(window.Options{Title: "narrow", Path: "/forge?tag=narrow", Permissions: []string{"window"}, HasPermissions: true})
	if err != nil {
		t.Fatal(err)
	}
	defer narrow.Close()
	wantAll(t, "narrow window", waitForge(t, "narrow"), "permission_denied")

	// The forged ids name windows that do exist ("main" and the wide window):
	// the same page there is allowed, so the denial above is the scope's.
	if wide.ID == "" || narrow.ID == wide.ID {
		t.Fatalf("window ids %q %q", wide.ID, narrow.ID)
	}
}

// Views have their own scope: a narrow view of a wide window is denied, its
// neighbour is not, and a forged header changes neither.
func TestViewScopeIsBoundNatively(t *testing.T) {
	need(t)
	w, err := window.Open(window.Options{Title: "pair", Path: "/forge?tag=pair-main"})
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	wantAll(t, "primary view", waitForge(t, "pair-main"), "ok")

	narrow, err := w.AddView(window.ViewOptions{Path: "/forge?tag=pair-narrow", Permissions: []string{"window"}, HasPermissions: true, Side: window.Right})
	if err != nil {
		t.Fatal(err)
	}
	wantAll(t, "narrow view", waitForge(t, "pair-narrow"), "permission_denied")

	// A view added without a list inherits the scope of whoever added it; host
	// code is unscoped, so it is wide.
	wide, err := w.AddView(window.ViewOptions{Path: "/forge?tag=pair-wide", Side: window.Bottom})
	if err != nil {
		t.Fatal(err)
	}
	wantAll(t, "wide view", waitForge(t, "pair-wide"), "ok")

	// Views are listed, with the primary first.
	vs, err := w.Views()
	if err != nil || len(vs) != 3 || !vs[0].Primary || vs[0].ID != w.ID || vs[1].ID != narrow.ID || vs[2].ID != wide.ID {
		t.Fatalf("views %+v %v", vs, err)
	}
	// Removing the narrow view drops its scope with it: its id is gone.
	if err := narrow.Remove(); err != nil {
		t.Fatal(err)
	}
	eventually(t, "view gone", func() bool {
		vs, _ := w.Views()
		return len(vs) == 2
	})
	if err := narrow.Remove(); err == nil {
		t.Error("removing a removed view succeeded")
	}
	if err := (&window.View{ID: w.ID}).Remove(); err == nil || !strings.Contains(err.Error(), "primary") {
		t.Errorf("removing the primary view: %v", err)
	}
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
	chatSeen    []chatReport
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

func waitChat(t *testing.T, tag, kind string) chatReport {
	t.Helper()
	deadline := time.After(20 * time.Second)
	for {
		for i, r := range chatSeen {
			if r.Tag == tag && r.Kind == kind {
				chatSeen = append(chatSeen[:i], chatSeen[i+1:]...)
				return r
			}
		}
		select {
		case r := <-chatReports:
			chatSeen = append(chatSeen, r)
		case <-deadline:
			t.Fatalf("timed out waiting for %s of %q (seen %+v)", kind, tag, chatSeen)
		}
	}
}

func TestViewMessagingEventsAndLayout(t *testing.T) {
	need(t)
	var events []window.ViewEvent
	var evMu sync.Mutex
	cancel, err := window.OnViewEvent(func(e window.ViewEvent) {
		evMu.Lock()
		events = append(events, e)
		evMu.Unlock()
	})
	if err != nil {
		t.Fatal(err)
	}
	defer cancel()
	kindsOf := func(view string) (out []window.ViewEventKind) {
		evMu.Lock()
		defer evMu.Unlock()
		for _, e := range events {
			if e.View == view {
				out = append(out, e.Kind)
			}
		}
		return
	}

	w, err := window.Open(window.Options{Title: "chat", Path: "/chat?tag=a", Width: 900, Height: 500})
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	waitChat(t, "a", "ready")

	// b posts to a at load, with a forged header.
	b, err := w.AddView(window.ViewOptions{ID: "side", Path: "/chat?tag=b&to=" + w.ID, Size: 0.4, MinWidth: 150})
	if err != nil {
		t.Fatal(err)
	}
	if b.ID != "side" {
		t.Fatalf("view id %q", b.ID)
	}
	waitChat(t, "b", "ready")
	got := waitChat(t, "a", "message")
	if got.From != "side" || string(got.Data) != `{"hello":"b"}` {
		t.Fatalf("message to the primary view: from %q data %s; want from \"side\" (the sender's view, whatever header it set)", got.From, got.Data)
	}
	// Host code posts to b; from is empty.
	if err := b.Post(map[string]any{"n": 3, "list": []int{1, 2}}); err != nil {
		t.Fatal(err)
	}
	got = waitChat(t, "b", "message")
	if got.From != "" || string(got.Data) != `{"list":[1,2],"n":3}` {
		t.Fatalf("message from host code: from %q data %s", got.From, got.Data)
	}
	// Posting to a view that is not there fails; so does non-JSON data.
	if err := (&window.View{ID: "nope"}).Post(1); err == nil {
		t.Error("post to an unknown view succeeded")
	}
	if err := b.Post(make(chan int)); err == nil {
		t.Error("post of a non-JSON value succeeded")
	}
	// A duplicate or malformed id is refused.
	if _, err := w.AddView(window.ViewOptions{ID: "side"}); err == nil {
		t.Error("duplicate view id accepted")
	}
	if _, err := w.AddView(window.ViewOptions{ID: "bad id"}); err == nil {
		t.Error("malformed view id accepted")
	}
	if _, err := w.AddView(window.ViewOptions{Side: "diagonal"}); err == nil {
		t.Error("bad side accepted")
	}

	c, err := w.AddView(window.ViewOptions{Path: "/chat?tag=c", Beside: b.ID, Side: window.Bottom, Size: 0.5, MinHeight: 80})
	if err != nil {
		t.Fatal(err)
	}
	waitChat(t, "c", "ready")

	// The layout reads back as ids: a | (side over c).
	eventually(t, "layout", func() bool {
		l, err := w.Layout()
		return err == nil && l.Direction == "horizontal" && l.First != nil && l.First.View == w.ID &&
			l.Second != nil && l.Second.Direction == "vertical" && l.Second.First.View == b.ID && l.Second.Second.View == c.ID &&
			l.Second.Second.MinHeight == 80
	})

	// A divider drag, synthesized through GTK, shows in the ratio.
	if err := linux.DragDivider(plat, host.WindowRef(w.ID), "", 300); err != nil {
		t.Fatal(err)
	}
	eventually(t, "ratio after the drag", func() bool {
		l, err := w.Layout()
		return err == nil && l.Ratio > 300.0/920-0.02 && l.Ratio < 300.0/880+0.02
	})

	// Replace the layout; the ids in it must be exactly the window's views.
	want := window.Split("vertical", 0.3, window.Pane(c.ID), window.Split("horizontal", 0.5, window.Pane(w.ID), window.Pane(b.ID)))
	if err := w.SetLayout(want); err != nil {
		t.Fatal(err)
	}
	eventually(t, "new layout", func() bool {
		l, err := w.Layout()
		return err == nil && l.Direction == "vertical" && l.First.View == c.ID && l.Second.First.View == w.ID && l.Second.Second.View == b.ID
	})
	for name, bad := range map[string]*window.Layout{
		"missing": window.Split("horizontal", 0, window.Pane(w.ID), window.Pane(b.ID)),
		"unknown": window.Split("horizontal", 0, window.Pane(w.ID), window.Split("horizontal", 0, window.Pane(b.ID), window.Pane("ghost"))),
		"axis":    window.Split("diagonal", 0, window.Pane(w.ID), window.Split("horizontal", 0, window.Pane(b.ID), window.Pane(c.ID))),
	} {
		if err := w.SetLayout(bad); err == nil {
			t.Errorf("%s layout accepted", name)
		}
	}
	if err := c.Focus(); err != nil {
		t.Error(err)
	}
	if err := b.Navigate("/chat?tag=b2"); err != nil {
		t.Error(err)
	}
	waitChat(t, "b2", "ready")

	// Removal reports, and the neighbour takes the space.
	if err := b.Remove(); err != nil {
		t.Fatal(err)
	}
	eventually(t, "removed event", func() bool {
		k := kindsOf("side")
		return len(k) >= 3 && k[0] == window.ViewAdded && k[len(k)-1] == window.ViewRemoved
	})
	vs, _ := w.Views()
	if len(vs) != 2 {
		t.Fatalf("views after removal: %+v", vs)
	}
	sawNav := false
	for _, k := range kindsOf("side") {
		sawNav = sawNav || k == window.ViewNavigated
	}
	if !sawNav {
		t.Errorf("no navigated event for side: %v", kindsOf("side"))
	}

	// Closing the window reports its remaining views closed.
	w.Close()
	eventually(t, "closed events", func() bool {
		k := kindsOf(c.ID)
		return len(k) > 0 && k[len(k)-1] == window.ViewClosed
	})
}
