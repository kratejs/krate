package plugin

import (
	"errors"
	"strings"
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
		Ops:         []OpInfo{{Name: "ping", Permission: "read"}, {Name: "open"}, {Name: "hand", Permission: "read", Manual: true}},
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

func TestInstallEnforcesNamespace(t *testing.T) {
	cases := map[string]string{
		"sdk id":         "krate/fs.readFile",
		"other plugin":   "plugin/other.ping",
		"bare prefix":    "plugin/demo.",
		"undeclared op":  "plugin/demo.sneaky",
		"manual by glue": "plugin/demo.hand",
		"not lowerCamel": "plugin/demo.Ping",
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
		r.Func("plugin/demo.ping", pong)
		r.Func("plugin/demo.open", pong)
	}}
	h, reg, set := newHost(t, []*Plugin{p}, []string{"plugin:demo.read", "window"})
	if err := h.Install(p); err != nil {
		t.Fatal(err)
	}
	if err := call(t, reg, "plugin/demo.ping", "main"); err != nil {
		t.Errorf("granted call: %v", err)
	}
	// A window scoped to nothing is denied the gated op and may use the open one.
	if err := set.Scope("w2", []string{"window"}); err != nil {
		t.Fatal(err)
	}
	if err := call(t, reg, "plugin/demo.ping", "w2"); !isDenied(err, "plugin:demo.read") {
		t.Errorf("scoped window: err = %v, want permission_denied for plugin:demo.read", err)
	}
	if err := call(t, reg, "plugin/demo.open", "w2"); err != nil {
		t.Errorf("ungated op in a scoped window: %v", err)
	}
	// The trusted twin ignores both.
	out, err := reg.Invoke(krate.Background(), "plugin/demo.ping", mustArgs(t))
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

func TestSignalGate(t *testing.T) {
	m := Meta{Name: "sig", Permissions: []PermissionInfo{{Name: "watch"}}, Ops: []OpInfo{{Name: "level", Permission: "watch"}}}
	p := &Plugin{Meta: m, Register: func(r *scheme.Registry) {
		r.Signal("plugin/sig.level", func(func(func(*wire.Encoder))) func() { return func() {} })
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
