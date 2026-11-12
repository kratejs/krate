package all

import (
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/kratejs/krate/packages/native/krate"
	"testing"

	"github.com/kratejs/krate/packages/native/platform/caps"
	"github.com/kratejs/krate/packages/native/scheme"
	"github.com/kratejs/krate/packages/native/sdk"
	"github.com/kratejs/krate/packages/native/sdk/internal/fakeplat"
	"github.com/kratejs/krate/packages/native/sdk/internal/sdktest"
	"github.com/kratejs/krate/packages/native/sdk/permission"
	"github.com/kratejs/krate/packages/native/wire"
)

// A scoped window can use only what its scope names, on data ids and UI ids,
// while other windows keep the app's set.
func TestWindowScopeNarrowsCalls(t *testing.T) {
	isolate(t)
	set, err := permission.Parse([]string{"kv", "db", "window", "clipboard"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Register(scheme.NewRegistry(), Options{AppID: "com.acme.scope"}); err != nil {
		t.Fatal(err)
	}
	reg := scheme.NewRegistry()
	RegisterAreas(reg, set)
	host := sdk.NewHost()
	host.Attach(fakeplat.New())
	t.Cleanup(host.Close)
	RegisterUI(reg, host, set, nil)
	if err := set.Scope("w1", []string{"kv"}); err != nil {
		t.Fatal(err)
	}
	c := sdktest.New(t, reg)

	get := func(win string) sdktest.Result {
		return c.CallFrom(win, "krate/kv.get", func(e *wire.Encoder) { e.String("k") })
	}
	if r := get("w1"); r.Failed {
		t.Fatalf("kv from the scoped window: %s %s", r.ErrCode, r.ErrMessage)
	}
	r := c.CallFrom("w1", "krate/db.query", func(e *wire.Encoder) { e.String("select 1") })
	if r.ErrCode != "permission_denied" || r.ErrData != `{"permission":"db"}` {
		t.Fatalf("db from the scoped window: %+v", r)
	}
	r = c.CallFrom("w1", "krate/clipboard.clear", nil)
	if r.ErrCode != "permission_denied" {
		t.Fatalf("clipboard from the scoped window: %+v", r)
	}
	if r := c.CallFrom("main", "krate/db.query", func(e *wire.Encoder) { e.String("select 1") }); r.ErrCode == "permission_denied" {
		t.Fatalf("an unscoped window lost db: %+v", r)
	}
	if r := c.CallFrom("main", "krate/clipboard.clear", nil); r.ErrCode == "permission_denied" {
		t.Fatalf("an unscoped window lost clipboard: %+v", r)
	}
	// An empty scope leaves nothing.
	if err := set.Scope("w2", []string{}); err != nil {
		t.Fatal(err)
	}
	if r := get("w2"); r.ErrCode != "permission_denied" {
		t.Fatalf("empty scope: %+v", r)
	}
}

// Windows opened through window.Open get their scope from the caller: an
// explicit list must be a subset of the caller's, and without a list the new
// window inherits the caller's scope.
func TestWindowOpenScopes(t *testing.T) {
	isolate(t)
	set, _ := permission.Parse([]string{"kv", "db", "window"})
	if _, err := Register(scheme.NewRegistry(), Options{AppID: "com.acme.scope2"}); err != nil {
		t.Fatal(err)
	}
	reg := scheme.NewRegistry()
	RegisterAreas(reg, set)
	host := sdk.NewHost()
	plat := fakeplat.New()
	host.Attach(plat)
	t.Cleanup(host.Close)
	RegisterUI(reg, host, set, nil)
	c := sdktest.New(t, reg)

	open := func(caller string, perms ...string) sdktest.Result {
		return c.CallFrom(caller, "krate/window.open", func(e *wire.Encoder) {
			e.BeginObject()
			e.Key("title")
			e.String("x")
			if perms != nil {
				e.Key("permissions")
				e.BeginArray()
				for _, p := range perms {
					e.String(p)
				}
				e.EndArray()
			}
			e.EndObject()
		})
	}
	r := open("main", "kv", "window")
	if r.Failed {
		t.Fatalf("open: %+v", r)
	}
	// The id is the JSON string the call returned.
	id := r.OK[1 : len(r.OK)-1]
	if got, ok := set.ScopeOf(id); !ok || len(got) != 2 {
		t.Fatalf("scope of %s = %v %v", id, got, ok)
	}
	if r := c.CallFrom(id, "krate/db.query", func(e *wire.Encoder) { e.String("select 1") }); r.ErrCode != "permission_denied" {
		t.Fatalf("db from the opened window: %+v", r)
	}
	// The scoped window cannot open one with more than it has.
	if r := open(id, "kv", "db"); r.ErrCode != "permission_denied" {
		t.Fatalf("widening: %+v", r)
	}
	// Without a list the child inherits the caller's scope.
	r = open(id)
	if r.Failed {
		t.Fatalf("inherit: %+v", r)
	}
	child := r.OK[1 : len(r.OK)-1]
	if got, ok := set.ScopeOf(child); !ok || len(got) != 2 {
		t.Fatalf("inherited scope of %s = %v %v", child, got, ok)
	}
	// A name the app does not grant is refused.
	if r := open("main", "fs"); r.ErrCode != "permission_denied" {
		t.Fatalf("ungranted: %+v", r)
	}
}

// One gating path for every id: the UI is checked against the app's set,
// narrowed by the calling window's scope; trusted in-process callers (host
// code through Registry.Invoke) skip both, for data ids and UI ids alike.
func TestTrustedCallsSkipAppAndWindowGates(t *testing.T) {
	isolate(t)
	set, err := permission.Parse([]string{"kv", "window"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Register(scheme.NewRegistry(), Options{AppID: "com.acme.trusted"}); err != nil {
		t.Fatal(err)
	}
	reg := scheme.NewRegistry()
	RegisterAreas(reg, set)
	host := sdk.NewHost()
	host.Attach(fakeplat.New())
	t.Cleanup(host.Close)
	RegisterUI(reg, host, set, nil)
	if err := set.Scope("w1", []string{}); err != nil {
		t.Fatal(err)
	}
	c := sdktest.New(t, reg)
	args := func(vals ...string) []byte {
		e := wire.NewEncoder()
		e.BeginArray()
		for _, v := range vals {
			e.String(v)
		}
		e.EndArray()
		fr, err := e.Finish()
		if err != nil {
			t.Fatal(err)
		}
		return fr
	}
	denied := func(r sdktest.Result) bool { return r.ErrCode == "permission_denied" }

	// The UI: the app does not grant db or clipboard, whatever the window.
	for _, win := range []string{"main", "w1"} {
		if r := c.CallFrom(win, "krate/db.query", func(e *wire.Encoder) { e.String("select 1") }); !denied(r) {
			t.Errorf("db from %s: %+v", win, r)
		}
		if r := c.CallFrom(win, "krate/clipboard.clear", nil); !denied(r) {
			t.Errorf("clipboard from %s: %+v", win, r)
		}
	}
	// The UI: kv is granted to the app but not to a window with an empty scope.
	if r := c.CallFrom("main", "krate/kv.get", func(e *wire.Encoder) { e.String("k") }); denied(r) {
		t.Errorf("kv from main: %+v", r)
	}
	if r := c.CallFrom("w1", "krate/kv.get", func(e *wire.Encoder) { e.String("k") }); !denied(r) {
		t.Errorf("kv from the empty-scope window: %+v", r)
	}
	// Trusted in-process calls bypass both gates.
	for _, id := range []string{"krate/db.query", "krate/clipboard.clear", "krate/kv.get"} {
		a := args("select 1")
		if id == "krate/clipboard.clear" {
			a = args()
		}
		if _, err := reg.Invoke(krate.Background(), id, a); err != nil {
			var ke *krate.Error
			if errors.As(err, &ke) && ke.Code == "permission_denied" {
				t.Errorf("Invoke(%s) was gated: %v", id, err)
			}
		}
	}
}

// A page cannot name one of its views after a window that does not exist yet.
// If window.open handed out an id a view already held, the window's scope
// would replace the view's, and closing the window would drop the scope of
// that id, leaving the view with every permission of the app.
func TestViewCannotSquatTheNextWindowID(t *testing.T) {
	isolate(t)
	set, _ := permission.Parse([]string{"kv", "db", "window"})
	if _, err := Register(scheme.NewRegistry(), Options{AppID: "com.acme.squat"}); err != nil {
		t.Fatal(err)
	}
	reg := scheme.NewRegistry()
	RegisterAreas(reg, set)
	host := sdk.NewHost()
	plat := fakeplat.New()
	host.Attach(plat)
	t.Cleanup(host.Close)
	RegisterUI(reg, host, set, nil)
	c := sdktest.New(t, reg)

	open := func(caller string, perms ...string) string {
		t.Helper()
		r := c.CallFrom(caller, "krate/window.open", func(e *wire.Encoder) {
			e.BeginObject()
			e.Key("title")
			e.String("x")
			if perms != nil {
				e.Key("permissions")
				e.BeginArray()
				for _, p := range perms {
					e.String(p)
				}
				e.EndArray()
			}
			e.EndObject()
		})
		if r.Failed {
			t.Fatalf("open from %s: %+v", caller, r)
		}
		return r.OK[1 : len(r.OK)-1]
	}
	scoped := open("main", "kv", "window")
	if scoped != "w1" {
		t.Fatalf("first window id %q", scoped)
	}
	// The scoped page names a view of its own window after the next window.
	r := c.CallFrom(scoped, "krate/window.addView", func(e *wire.Encoder) {
		e.String(scoped)
		e.BeginObject()
		e.Key("id")
		e.String("w2")
		e.Key("path")
		e.String("/")
		e.EndObject()
	})
	if r.Failed {
		t.Fatalf("addView: %+v", r)
	}
	db := func(win string) sdktest.Result {
		return c.CallFrom(win, "krate/db.query", func(e *wire.Encoder) { e.String("select 1") })
	}
	if r := db("w2"); r.ErrCode != "permission_denied" {
		t.Fatalf("db from the squatting view before: %+v", r)
	}
	next := open(scoped)
	if next == "w2" {
		t.Errorf("window.open returned %q, the id of an existing view", next)
	}
	win, _, ok := host.Window(next)
	if !ok {
		t.Fatalf("no window %q", next)
	}
	plat.EmitWindow(win.(*fakeplat.Window), caps.WindowClosed)
	if r := db("w2"); r.ErrCode != "permission_denied" {
		t.Fatalf("the squatting view gained db after the window closed: %+v", r)
	}
	if got, ok := set.ScopeOf("w2"); !ok || len(got) != 2 {
		t.Fatalf("scope of the view = %v %v", got, ok)
	}
}

// Two calls that add a view under the same id at the same time: one wins, the
// other fails, and the failure must not drop the scope the winner was given
// (the batch endpoint runs the calls of one request concurrently).
func TestConcurrentViewIDsKeepTheirScope(t *testing.T) {
	isolate(t)
	set, _ := permission.Parse([]string{"kv", "db", "window"})
	if _, err := Register(scheme.NewRegistry(), Options{AppID: "com.acme.race"}); err != nil {
		t.Fatal(err)
	}
	reg := scheme.NewRegistry()
	RegisterAreas(reg, set)
	host := sdk.NewHost()
	plat := fakeplat.New()
	host.Attach(plat)
	t.Cleanup(host.Close)
	RegisterUI(reg, host, set, nil)
	c := sdktest.New(t, reg)
	r := c.CallFrom("main", "krate/window.open", func(e *wire.Encoder) {
		e.BeginObject()
		e.Key("permissions")
		e.BeginArray()
		e.String("window")
		e.EndArray()
		e.EndObject()
	})
	if r.Failed {
		t.Fatalf("open: %+v", r)
	}
	scoped := r.OK[1 : len(r.OK)-1]
	for i := 0; i < 50; i++ {
		id := fmt.Sprintf("race%d", i)
		var wg sync.WaitGroup
		for j := 0; j < 2; j++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				c.CallFrom(scoped, "krate/window.addView", func(e *wire.Encoder) {
					e.String(scoped)
					e.BeginObject()
					e.Key("id")
					e.String(id)
					e.EndObject()
				})
			}()
		}
		wg.Wait()
		vs := c.CallFrom("main", "krate/window.views", func(e *wire.Encoder) { e.String(scoped) })
		if !strings.Contains(vs.OK, `"`+id+`"`) {
			t.Fatalf("no view %s was added: %+v", id, vs)
		}
		if got, ok := set.ScopeOf(id); !ok || len(got) != 1 || got[0] != "window" {
			t.Fatalf("view %s: scope %v %v after a concurrent duplicate; it must keep the caller's", id, got, ok)
		}
		// Keep the layout shallow for the next round.
		if r := c.CallFrom("main", "krate/window.removeView", func(e *wire.Encoder) { e.String(id) }); r.Failed {
			t.Fatalf("removeView: %+v", r)
		}
	}
}
