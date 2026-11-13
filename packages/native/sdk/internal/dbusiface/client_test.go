package dbusiface

import (
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/kratejs/krate/packages/native/internal/dbus/dbustest"
)

type node struct {
	ID       int32
	Props    map[string]Variant
	Children []Variant
}

func pair(t *testing.T) (*Client, *Client) {
	addr := dbustest.Start(t)
	a, err := Connect(addr)
	if err != nil {
		t.Fatal(err)
	}
	b, err := Connect(addr)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { a.Close(); b.Close() })
	return a, b
}

// echo sends vals to a method that returns them and returns the decoded reply.
func echo(t *testing.T, vals []any) []any {
	t.Helper()
	srv, cli := pair(t)
	if err := srv.RequestName("dev.krate.Echo"); err != nil {
		t.Fatal(err)
	}
	srv.Export("/e", "dev.krate.Echo", map[string]Method{
		"Echo": func(args []any) ([]any, error) { return args, nil },
	})
	got, err := cli.Call("dev.krate.Echo", "/e", "dev.krate.Echo", "Echo", vals...)
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func TestMarshalRoundTrip(t *testing.T) {
	vals := []any{
		byte(7), true, int16(-3), uint16(9), int32(-5), uint32(6), int64(-1 << 40), uint64(1 << 50), 2.5,
		"héllo", ObjectPath("/a/b"), Signature("a{sv}"),
		Variant{Value: "x"}, []byte{1, 2, 3}, []string{"a", "bc"},
		map[string]Variant{"k": {Value: int32(4)}, "z": {Value: []string{"q"}}},
		node{ID: 1, Props: map[string]Variant{"label": {Value: "Root"}}, Children: []Variant{
			{Value: node{ID: 2, Props: map[string]Variant{}, Children: []Variant{}}},
		}},
	}
	// Echo the values through a real bus: encode, send, decode.
	_, cli := pair(t)
	srv := cli
	_ = srv
	got := echo(t, vals)
	if got[0] != byte(7) || got[1] != true || got[2] != int16(-3) || got[6] != int64(-1<<40) || got[8] != 2.5 || got[9] != "héllo" {
		t.Fatalf("basics %v", got[:10])
	}
	if !reflect.DeepEqual(got[13], []byte{1, 2, 3}) || !reflect.DeepEqual(got[14], []any{"a", "bc"}) {
		t.Fatalf("arrays %v %v", got[13], got[14])
	}
	m := got[15].(map[string]any)
	if m["k"].(Variant).Value != int32(4) {
		t.Fatalf("dict %v", m)
	}
	st := got[16].(Struct)
	if st[0] != int32(1) || len(st[2].([]any)) != 1 {
		t.Fatalf("struct %v", st)
	}
	// Decoded structs re-encode (used when echoing replies).
	if _, _, err := encodeArgs([]any{st}); err != nil {
		t.Fatalf("re-encode: %v", err)
	}
	if _, _, err := encodeArgs([]any{1}); err == nil {
		t.Fatal("plain int accepted")
	}
}

func TestCallExportSignalProps(t *testing.T) {
	srv, cli := pair(t)
	if err := srv.RequestName("dev.krate.Test"); err != nil {
		t.Fatal(err)
	}
	if err := cli.RequestName("dev.krate.Test"); err == nil {
		t.Fatal("second owner accepted")
	}
	srv.Export("/obj", "dev.krate.Iface", map[string]Method{
		"Echo": func(args []any) ([]any, error) { return args, nil },
		"Fail": func(args []any) ([]any, error) {
			return nil, &Error{Name: "dev.krate.Error.Nope", Message: "denied"}
		},
		"Sum": func(args []any) ([]any, error) { return []any{args[0].(int32) + args[1].(int32)}, nil },
	})
	srv.ExportProperties("/obj", "dev.krate.Iface", func() map[string]Variant {
		return map[string]Variant{"Title": {Value: "t"}, "N": {Value: uint32(3)}}
	})
	out, err := cli.Call("dev.krate.Test", "/obj", "dev.krate.Iface", "Echo", "a", int32(2), map[string]Variant{"x": {Value: true}})
	if err != nil || len(out) != 3 || out[0] != "a" || out[1] != int32(2) {
		t.Fatalf("echo %v %v", out, err)
	}
	if out, err = cli.Call("dev.krate.Test", "/obj", "dev.krate.Iface", "Sum", int32(40), int32(2)); err != nil || out[0] != int32(42) {
		t.Fatalf("sum %v %v", out, err)
	}
	_, err = cli.Call("dev.krate.Test", "/obj", "dev.krate.Iface", "Fail")
	if de, ok := err.(*Error); !ok || de.Name != "dev.krate.Error.Nope" || de.Message != "denied" {
		t.Fatalf("fail %v", err)
	}
	if _, err = cli.Call("dev.krate.Test", "/obj", "dev.krate.Iface", "Missing"); err == nil {
		t.Fatal("unknown method succeeded")
	}
	all, err := cli.Call("dev.krate.Test", "/obj", "org.freedesktop.DBus.Properties", "GetAll", "dev.krate.Iface")
	if err != nil || all[0].(map[string]any)["Title"].(Variant).Value != "t" {
		t.Fatalf("GetAll %v %v", all, err)
	}
	one, err := cli.Call("dev.krate.Test", "/obj", "org.freedesktop.DBus.Properties", "Get", "dev.krate.Iface", "N")
	if err != nil || one[0].(Variant).Value != uint32(3) {
		t.Fatalf("Get %v %v", one, err)
	}

	var mu sync.Mutex
	var got []Signal
	cancel, err := cli.Subscribe("type='signal',interface='dev.krate.Iface',member='Ping'", func(s Signal) {
		mu.Lock()
		got = append(got, s)
		mu.Unlock()
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := srv.Emit("/obj", "dev.krate.Iface", "Ping", "one", uint32(1)); err != nil {
		t.Fatal(err)
	}
	if err := srv.Emit("/obj", "dev.krate.Iface", "Other"); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		n := len(got)
		mu.Unlock()
		if n == 1 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(got) != 1 || got[0].Name != "Ping" || got[0].Body[0] != "one" || got[0].Path != "/obj" {
		t.Fatalf("signals %+v", got)
	}
	cancel()
}
