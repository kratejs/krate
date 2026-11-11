package plugin

import (
	"errors"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/kratejs/krate/packages/native/krate"
	"github.com/kratejs/krate/packages/native/scheme"
	"github.com/kratejs/krate/packages/native/sdk/permission"
	"github.com/kratejs/krate/packages/native/wire"
)

func pong(_ krate.Ctx, _ *wire.Decoder, out *wire.Encoder) error {
	out.String("pong")
	return nil
}

func testMeta() Meta {
	return Meta{
		Name:        "demo",
		Version:     "1.0.0",
		Permissions: []PermissionInfo{{Name: "read", Doc: "read things"}},
		Ops:         []OpInfo{{Name: "Ping", Permission: "read"}, {Name: "Open"}, {Name: "Hand", Permission: "read", Manual: true}},
	}
}

func newHost(t *testing.T, plugins []*Plugin, perms []string) (*Host, *scheme.Registry, permission.Set) {
	t.Helper()
	if err := DefinePermissions(plugins); err != nil {
		t.Fatal(err)
	}
	set, err := permission.Parse(perms)
	if err != nil {
		t.Fatal(err)
	}
	reg := scheme.NewRegistry()
	return NewHost(HostOptions{Registry: reg, Perms: set, AppID: "com.example.t"}), reg, set
}

func call(t *testing.T, reg *scheme.Registry, id, window string) error {
	t.Helper()
	f := reg.Snapshot().Funcs[id]
	if f == nil {
		t.Fatalf("%s is not registered", id)
	}
	return f(krate.WithWindow(krate.Background(), window), nil, wire.NewEncoder())
}

func isDenied(err error, perm string) bool {
	var ke *krate.Error
	if !errors.As(err, &ke) || ke.Code != "permission_denied" {
		return false
	}
	m, _ := ke.Data.(map[string]string)
	return m["permission"] == perm
}

func TestValidate(t *testing.T) {
	bad := map[string]func(*Meta){
		"name":            func(m *Meta) { m.Name = "Bad_Name" },
		"short name":      func(m *Meta) { m.Name = "a" },
		"dup op":          func(m *Meta) { m.Ops = append(m.Ops, OpInfo{Name: "Ping"}) },
		"op case":         func(m *Meta) { m.Ops[0].Name = "ping" },
		"unknown perm":    func(m *Meta) { m.Ops[0].Permission = "write" },
		"dup permission":  func(m *Meta) { m.Permissions = append(m.Permissions, PermissionInfo{Name: "read"}) },
		"permission name": func(m *Meta) { m.Permissions[0].Name = "Read!" },
	}
	for name, mut := range bad {
		m := testMeta()
		mut(&m)
		if m.Validate() == nil {
			t.Errorf("%s: Validate accepted %+v", name, m)
		}
	}
	if err := (testMeta()).Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestInstallEnforcesNamespace(t *testing.T) {
	cases := map[string]string{
		"sdk id":         "krate/fs.readFile",
		"other plugin":   "plugin/other.Ping",
		"bare prefix":    "plugin/demo.",
		"undeclared op":  "plugin/demo.Sneaky",
		"manual by glue": "plugin/demo.Hand",
	}
	for name, id := range cases {
		id := id
		p := &Plugin{Meta: testMeta(), Register: func(r *scheme.Registry) { r.Func(id, pong) }}
		h, reg, _ := newHost(t, []*Plugin{p}, nil)
		err := h.Install(p)
		var ne *NamespaceError
		if !errors.As(err, &ne) {
			t.Errorf("%s: Install(%q) = %v, want a NamespaceError", name, id, err)
			continue
		}
		if got := reg.Snapshot(); len(got.Funcs) != 0 {
			t.Errorf("%s: a refused plugin left registrations behind: %v", name, got.Funcs)
		}
	}
	// A stream or signal outside the namespace is refused too.
	p := &Plugin{Meta: testMeta(), Register: func(r *scheme.Registry) {
		r.Stream("krate/window.events", func(krate.Ctx, *wire.Decoder, func(func(*wire.Encoder)) error) error { return nil })
	}}
	h, _, _ := newHost(t, []*Plugin{p}, nil)
	if err := h.Install(p); err == nil {
		t.Error("a stream outside the namespace was accepted")
	}
	p = &Plugin{Meta: testMeta(), Register: func(r *scheme.Registry) {
		r.Signal("krate/x.y", func(func(func(*wire.Encoder))) func() { return func() {} })
	}}
	h, _, _ = newHost(t, []*Plugin{p}, nil)
	if err := h.Install(p); err == nil {
		t.Error("a signal outside the namespace was accepted")
	}
}

func TestInstallRegistersGatedOps(t *testing.T) {
	p := &Plugin{Meta: testMeta(), Register: func(r *scheme.Registry) {
		r.Func("plugin/demo.Ping", pong)
		r.Func("plugin/demo.Open", pong)
	}}
	h, reg, set := newHost(t, []*Plugin{p}, []string{"plugin:demo.read", "window"})
	if err := h.Install(p); err != nil {
		t.Fatal(err)
	}
	if err := call(t, reg, "plugin/demo.Ping", "main"); err != nil {
		t.Errorf("granted call: %v", err)
	}
	// A window scoped to nothing is denied the gated op and may use the open one.
	if err := set.Scope("w2", []string{"window"}); err != nil {
		t.Fatal(err)
	}
	if err := call(t, reg, "plugin/demo.Ping", "w2"); !isDenied(err, "plugin:demo.read") {
		t.Errorf("scoped window: err = %v, want permission_denied for plugin:demo.read", err)
	}
	if err := call(t, reg, "plugin/demo.Open", "w2"); err != nil {
		t.Errorf("ungated op in a scoped window: %v", err)
	}
	// The trusted twin ignores both.
	out, err := reg.Invoke(krate.Background(), "plugin/demo.Ping", mustArgs(t))
	if err != nil || len(out) == 0 {
		t.Errorf("Invoke: %v", err)
	}
}

func mustArgs(t *testing.T) []byte {
	t.Helper()
	e := wire.NewEncoder()
	e.BeginArray()
	e.EndArray()
	fr, err := e.Finish()
	if err != nil {
		t.Fatal(err)
	}
	return fr
}

func TestAppLevelDenied(t *testing.T) {
	p := &Plugin{Meta: testMeta(), Register: func(r *scheme.Registry) { r.Func("plugin/demo.Ping", pong) }}
	h, reg, _ := newHost(t, []*Plugin{p}, []string{"window"})
	if err := h.Install(p); err != nil {
		t.Fatal(err)
	}
	if err := call(t, reg, "plugin/demo.Ping", "main"); !isDenied(err, "plugin:demo.read") {
		t.Errorf("err = %v, want permission_denied", err)
	}
}

func TestSignalGate(t *testing.T) {
	m := Meta{Name: "sig", Permissions: []PermissionInfo{{Name: "watch"}}, Ops: []OpInfo{{Name: "Level", Permission: "watch"}}}
	p := &Plugin{Meta: m, Register: func(r *scheme.Registry) {
		r.Signal("plugin/sig.Level", func(func(func(*wire.Encoder))) func() { return func() {} })
	}}
	h, reg, set := newHost(t, []*Plugin{p}, []string{"plugin:sig.watch", "window"})
	if err := h.Install(p); err != nil {
		t.Fatal(err)
	}
	if err := set.Scope("w9", []string{"window"}); err != nil {
		t.Fatal(err)
	}
	_ = reg
	// The gate is consulted by the signal endpoint; exercise it through the handler.
	srv := scheme.New(scheme.Options{Registry: reg})
	if got := signalStatus(t, srv, "w9"); !strings.Contains(got, "permission_denied") {
		t.Errorf("scoped window got %q, want permission_denied", got)
	}
}

func TestManualOps(t *testing.T) {
	var ctx *Context
	p := &Plugin{Meta: testMeta(), Init: func(c *Context) error {
		ctx = c
		return c.Func("Hand", pong)
	}}
	h, reg, _ := newHost(t, []*Plugin{p}, []string{"plugin:demo.read"})
	if err := h.Install(p); err != nil {
		t.Fatal(err)
	}
	if err := call(t, reg, "plugin/demo.Hand", ""); err != nil {
		t.Errorf("manual op: %v", err)
	}
	for _, op := range []string{"krate/fs.readFile", "../Hand", "plugin/other.Hand", "Ping", "Missing", "Hand"} {
		var ne *NamespaceError
		err := ctx.Func(op, pong)
		if op == "Hand" {
			if err == nil {
				t.Error("registering a manual op twice succeeded")
			}
			continue
		}
		if !errors.As(err, &ne) {
			t.Errorf("Func(%q) = %v, want a NamespaceError", op, err)
		}
	}
	if err := ctx.Stream("Ping", nil); err == nil {
		t.Error("Stream accepted a generated op")
	}
}

func TestLifecycle(t *testing.T) {
	var log []string
	var ctx *Context
	p := &Plugin{Meta: Meta{Name: "life"}, Init: func(c *Context) error {
		ctx = c
		c.OnStart(func() error { log = append(log, "start"); return nil })
		c.OnWindowCreated(func(id string) { log = append(log, "created:"+id) })
		c.OnWindowClosed(func(id string) { log = append(log, "closed:"+id) })
		c.OnQuit(func() { log = append(log, "quit1") })
		c.OnQuit(func() { log = append(log, "quit2") })
		c.Dispatch(func() { log = append(log, "queued") })
		return nil
	}}
	h, _, _ := newHost(t, []*Plugin{p}, nil)
	if err := h.Install(p); err != nil {
		t.Fatal(err)
	}
	if len(log) != 0 {
		t.Fatalf("hooks ran before Start: %v", log)
	}
	h.Start(nil, nil)
	h.Start(nil, nil)
	h.WindowCreated("main")
	h.WindowClosed("main")
	h.Quit()
	h.Quit()
	want := "queued start created:main closed:main quit2 quit1"
	if got := strings.Join(log, " "); got != want {
		t.Errorf("log = %q, want %q", got, want)
	}
	if ctx.App() != nil || len(ctx.Windows()) != 0 {
		t.Error("browser mode has a platform")
	}
	if _, err := ctx.Dialogs(); !krate.IsUnsupported(err) {
		t.Errorf("Dialogs without a platform: %v", err)
	}
	// Dispatch after Start without a platform runs on a goroutine.
	var ran atomic.Bool
	done := make(chan struct{})
	ctx.Dispatch(func() { ran.Store(true); close(done) })
	<-done
	if !ran.Load() {
		t.Error("Dispatch did not run")
	}
}

func TestHookPanicsAreContained(t *testing.T) {
	var logged []string
	p := &Plugin{Meta: Meta{Name: "boom"}, Init: func(c *Context) error {
		c.OnStart(func() error { panic("start") })
		c.OnQuit(func() { panic("quit") })
		return nil
	}}
	if err := DefinePermissions([]*Plugin{p}); err != nil {
		t.Fatal(err)
	}
	h := NewHost(HostOptions{Registry: scheme.NewRegistry(), Logf: func(f string, a ...any) { logged = append(logged, f) }})
	if err := h.Install(p); err != nil {
		t.Fatal(err)
	}
	h.Start(nil, nil)
	h.Quit()
	if len(logged) != 2 {
		t.Errorf("logged %v, want two lines", logged)
	}
}

func TestDuplicateAndInitErrors(t *testing.T) {
	p := &Plugin{Meta: Meta{Name: "dup"}}
	h, _, _ := newHost(t, []*Plugin{p}, nil)
	if err := h.Install(p); err != nil {
		t.Fatal(err)
	}
	if err := h.Install(p); err == nil {
		t.Error("a plugin installed twice")
	}
	q := &Plugin{Meta: Meta{Name: "bad-init"}, Init: func(*Context) error { return errors.New("nope") }}
	if err := h.Install(q); err == nil || !strings.Contains(err.Error(), "nope") {
		t.Errorf("Init error = %v", err)
	}
}
