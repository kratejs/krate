package dbus_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/kratejs/krate/packages/native/internal/dbus"
	"github.com/kratejs/krate/packages/native/internal/dbus/dbustest"
)

func dial(t *testing.T, addr string) *dbus.Conn {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	c, err := dbus.DialAddress(ctx, addr)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	return c
}

func TestBusRoundTrip(t *testing.T) {
	addr := dbustest.Start(t)
	srv, cli := dial(t, addr), dial(t, addr)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if srv.UniqueName() == "" || srv.UniqueName() == cli.UniqueName() {
		t.Fatalf("unique names: %q %q", srv.UniqueName(), cli.UniqueName())
	}
	if err := srv.RequestName(ctx, "org.krate.Test", 0); err != nil {
		t.Fatal(err)
	}
	srv.Export("/org/krate/Test", "org.krate.Test", map[string]dbus.MethodFunc{
		"Echo": func(m *dbus.Message) (string, []any, error) {
			return "sa{sv}", []any{m.Body[0], map[string]dbus.Variant{"n": {Sig: "x", Value: int64(42)}}}, nil
		},
		"Fail": func(m *dbus.Message) (string, []any, error) {
			return "", nil, dbus.NewError("org.krate.Test.Nope", "refused")
		},
		"Boom": func(m *dbus.Message) (string, []any, error) { panic("kaboom") },
	})
	srv.Export("/org/krate/Test", dbus.PropsIface, map[string]dbus.MethodFunc{
		"Get": func(m *dbus.Message) (string, []any, error) {
			return "v", []any{dbus.V("s", "prop:"+m.Body[1].(string))}, nil
		},
	})

	res, err := cli.Call(ctx, "org.krate.Test", "/org/krate/Test", "org.krate.Test", "Echo", "s", "hello")
	if err != nil {
		t.Fatal(err)
	}
	if res[0] != "hello" {
		t.Fatalf("echo: %#v", res)
	}
	if m := res[1].(map[string]any); m["n"].(dbus.Variant).Value != int64(42) {
		t.Fatalf("dict: %#v", res[1])
	}

	_, err = cli.Call(ctx, "org.krate.Test", "/org/krate/Test", "org.krate.Test", "Fail", "")
	var de *dbus.Error
	if !errors.As(err, &de) || de.Name != "org.krate.Test.Nope" || de.Body[0] != "refused" {
		t.Fatalf("error reply: %v", err)
	}
	_, err = cli.Call(ctx, "org.krate.Test", "/org/krate/Test", "org.krate.Test", "Boom", "")
	if !errors.As(err, &de) || de.Name != "org.freedesktop.DBus.Error.Failed" {
		t.Fatalf("panic reply: %v", err)
	}
	_, err = cli.Call(ctx, "org.krate.Test", "/org/krate/Test", "org.krate.Test", "Missing", "")
	if !errors.As(err, &de) || de.Name != "org.freedesktop.DBus.Error.UnknownMethod" {
		t.Fatalf("unknown method: %v", err)
	}
	_, err = cli.Call(ctx, "org.krate.Nobody", "/", "x.Y", "Z", "")
	if !errors.As(err, &de) || de.Name != "org.freedesktop.DBus.Error.ServiceUnknown" {
		t.Fatalf("unknown service: %v", err)
	}

	v, err := cli.GetProperty(ctx, "org.krate.Test", "/org/krate/Test", "org.krate.Test", "Color")
	if err != nil || v.Value != "prop:Color" {
		t.Fatalf("property: %#v %v", v, err)
	}

	if _, err := cli.Call(ctx, "org.krate.Test", "/org/krate/Test", dbus.IntrospectIfc, "Introspect", ""); err != nil {
		t.Fatalf("introspect: %v", err)
	}
	if _, err := cli.Call(ctx, "org.krate.Test", "/org/krate/Test", dbus.PeerIface, "Ping", ""); err != nil {
		t.Fatalf("ping: %v", err)
	}
}
