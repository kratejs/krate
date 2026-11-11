package all

import (
	"testing"

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
	r = c.CallFrom("w1", "krate/clipboard.Clear", nil)
	if r.ErrCode != "permission_denied" {
		t.Fatalf("clipboard from the scoped window: %+v", r)
	}
	if r := c.CallFrom("main", "krate/db.query", func(e *wire.Encoder) { e.String("select 1") }); r.ErrCode == "permission_denied" {
		t.Fatalf("an unscoped window lost db: %+v", r)
	}
	if r := c.CallFrom("main", "krate/clipboard.Clear", nil); r.ErrCode == "permission_denied" {
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
		return c.CallFrom(caller, "krate/window.Open", func(e *wire.Encoder) {
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
