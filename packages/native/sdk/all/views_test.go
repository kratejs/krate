package all

import (
	"strings"
	"testing"
	"time"

	"github.com/kratejs/krate/packages/native/platform"
	"github.com/kratejs/krate/packages/native/platform/caps"
	"github.com/kratejs/krate/packages/native/scheme"
	"github.com/kratejs/krate/packages/native/sdk"
	"github.com/kratejs/krate/packages/native/sdk/internal/fakeplat"
	"github.com/kratejs/krate/packages/native/sdk/internal/sdktest"
	"github.com/kratejs/krate/packages/native/sdk/permission"
	"github.com/kratejs/krate/packages/native/wire"
)

type viewRig struct {
	set  permission.Set
	host *sdk.Host
	plat *fakeplat.App
	main *fakeplat.Window
	c    *sdktest.Caller
}

func newViewRig(t *testing.T) *viewRig {
	t.Helper()
	isolate(t)
	set, err := permission.Parse([]string{"kv", "db", "window"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Register(scheme.NewRegistry(), Options{AppID: "com.acme.views"}); err != nil {
		t.Fatal(err)
	}
	reg := scheme.NewRegistry()
	RegisterAreas(reg, set)
	host := sdk.NewHost()
	plat := fakeplat.New()
	host.Attach(plat)
	t.Cleanup(host.Close)
	RegisterUI(reg, host, set, nil)
	w, err := plat.NewWindow(platform.WindowOptions{Title: "main", Path: "/"})
	if err != nil {
		t.Fatal(err)
	}
	host.AddWindow("main", w)
	return &viewRig{set: set, host: host, plat: plat, main: w.(*fakeplat.Window), c: sdktest.New(t, reg)}
}

func (r *viewRig) addView(caller, window string, fields func(e *wire.Encoder)) sdktest.Result {
	return r.c.CallFrom(caller, "krate/window.addView", func(e *wire.Encoder) {
		e.String(window)
		e.BeginObject()
		if fields != nil {
			fields(e)
		}
		e.EndObject()
	})
}

func perms(list ...string) func(e *wire.Encoder) {
	return func(e *wire.Encoder) {
		e.Key("permissions")
		e.BeginArray()
		for _, p := range list {
			e.String(p)
		}
		e.EndArray()
	}
}

func unquote(s string) string { return strings.Trim(s, `"`) }

// A view has a scope of its own: one opened with a list gets exactly that list
// (a subset of the caller's), one opened without inherits the caller's scope,
// and the scope goes away with the view and with its window.
func TestViewScopes(t *testing.T) {
	r := newViewRig(t)
	kvGet := func(from string) sdktest.Result {
		return r.c.CallFrom(from, "krate/kv.get", func(e *wire.Encoder) { e.String("k") })
	}

	res := r.addView("main", "main", func(e *wire.Encoder) { e.Key("id"); e.String("narrow"); perms("window")(e) })
	if res.Failed || unquote(res.OK) != "narrow" {
		t.Fatalf("addView: %+v", res)
	}
	if got, ok := r.set.ScopeOf("narrow"); !ok || len(got) != 1 || got[0] != "window" {
		t.Fatalf("scope of the view: %v %v", got, ok)
	}
	if res := kvGet("narrow"); res.ErrCode != "permission_denied" {
		t.Fatalf("kv from the narrow view: %+v", res)
	}
	if res := kvGet("main"); res.ErrCode == "permission_denied" {
		t.Fatalf("the primary view lost kv: %+v", res)
	}
	// The page of the view was opened on a scoped route.
	pv := r.plat.Windows[0].Views()
	if len(pv) != 2 || !strings.Contains(pv[1].Path, "__krate_win=narrow") {
		t.Fatalf("views %+v: the scoped view should load a route naming its id", pv)
	}

	// A narrow view cannot add a wider one; without a list it inherits.
	if res := r.addView("narrow", "main", perms("window", "kv")); res.ErrCode != "permission_denied" {
		t.Fatalf("widening from a view: %+v", res)
	}
	res = r.addView("narrow", "main", nil)
	if res.Failed {
		t.Fatalf("inherit: %+v", res)
	}
	child := unquote(res.OK)
	if got, ok := r.set.ScopeOf(child); !ok || len(got) != 1 || got[0] != "window" {
		t.Fatalf("inherited scope of %s: %v %v", child, got, ok)
	}

	// Removing a view drops its scope; so does closing the window.
	if res := r.c.CallFrom("main", "krate/window.removeView", func(e *wire.Encoder) { e.String(child) }); res.Failed {
		t.Fatalf("remove: %+v", res)
	}
	if _, ok := r.set.ScopeOf(child); ok {
		t.Error("the removed view kept its scope")
	}
	r.plat.EmitWindow(r.main, caps.WindowClosed)
	if _, ok := r.set.ScopeOf("narrow"); ok {
		t.Error("a closed window's view kept its scope")
	}
	if _, ok := r.host.ViewWindow(r.main.PrimaryView()); ok {
		t.Error("a closed window's primary view is still mapped")
	}
}

// The shell binds a request to a view through ViewWindow: registered views map
// to their ids, views the platform does not know do not, and a view the
// platform knows but the SDK has not registered yet is waited for.
func TestViewWindowMapping(t *testing.T) {
	r := newViewRig(t)
	if id, ok := r.host.ViewWindow(r.main.PrimaryView()); !ok || id != "main" {
		t.Fatalf("primary view maps to %q %v", id, ok)
	}
	res := r.addView("main", "main", nil)
	if res.Failed {
		t.Fatal(res)
	}
	id := unquote(res.OK)
	var ref caps.ViewRef
	for _, v := range r.main.Views() {
		if !v.Primary {
			ref = v.Ref
		}
	}
	if got, ok := r.host.ViewWindow(ref); !ok || got != id {
		t.Fatalf("added view maps to %q %v, want %q", got, ok, id)
	}
	start := time.Now()
	if _, ok := r.host.ViewWindow(9999); ok || time.Since(start) > time.Second {
		t.Fatalf("an unknown view must be refused at once (took %v)", time.Since(start))
	}
	if _, ok := r.host.ViewWindow(0); ok {
		t.Fatal("view 0 maps to a window")
	}
}

// Events a platform sends for a view before the SDK registered it (the first
// load can finish before AddView returned) are held and delivered, in order,
// when it is.
func TestViewEventsAreHeldUntilRegistered(t *testing.T) {
	r := newViewRig(t)
	got := make(chan string, 8)
	cancel := subscribeViewEvents(t, r, got)
	defer cancel()

	res := r.addView("main", "main", func(e *wire.Encoder) { e.Key("id"); e.String("late") })
	if res.Failed {
		t.Fatal(res)
	}
	var ref caps.ViewRef
	for _, v := range r.main.Views() {
		if !v.Primary {
			ref = v.Ref
		}
	}
	r.plat.EmitView(caps.ViewEvent{View: ref, Window: r.main.Ref(), Kind: caps.ViewNavigated, Path: "/late"})
	for _, want := range []string{"late:added", "late:navigated"} {
		select {
		case s := <-got:
			if s != want {
				t.Fatalf("event %q, want %q", s, want)
			}
		case <-time.After(3 * time.Second):
			t.Fatalf("no event %q", want)
		}
	}
	// An event of a view nobody registered is dropped after being held, not
	// delivered to someone else.
	r.plat.EmitView(caps.ViewEvent{View: 424242, Window: r.main.Ref(), Kind: caps.ViewNavigated})
	select {
	case s := <-got:
		t.Fatalf("unexpected event %q", s)
	case <-time.After(100 * time.Millisecond):
	}
}

// subscribeViewEvents streams krate/window.viewEvents into out as "view:kind".
func subscribeViewEvents(t *testing.T, r *viewRig, out chan<- string) (cancel func()) {
	t.Helper()
	cancel, err := windowOnViewEvent(func(view, kind string) { out <- view + ":" + kind })
	if err != nil {
		t.Fatal(err)
	}
	// The stream is open once the topic has a subscriber.
	time.Sleep(100 * time.Millisecond)
	return cancel
}
