package hostchild_test

import (
	"net"
	"net/http"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/native/internal/gohosttest"
	"github.com/kratejs/krate/packages/native/krate"
	"github.com/kratejs/krate/packages/native/scheme"
	"github.com/kratejs/krate/packages/native/wire"
)

// serveParent serves a registry like the shell's private SDK socket does and
// returns the value for KRATE_GOHOST_PARENT.
func serveParent(t *testing.T) string {
	t.Helper()
	dir, cleanup, err := gohosttest.TempDir("parent-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(cleanup)
	reg := scheme.NewRegistry()
	reg.Func("krate/p.echo", func(ctx krate.Ctx, a *wire.Decoder, out *wire.Encoder) error {
		if err := a.BeginArray(); err != nil {
			return err
		}
		s, err := a.String()
		if err != nil {
			return err
		}
		if err := a.EndArray(); err != nil {
			return err
		}
		out.String("sdk:" + s)
		return nil
	})
	reg.Func("krate/p.deny", func(krate.Ctx, *wire.Decoder, *wire.Encoder) error {
		return krate.Errorf("permission_denied", "the app does not grant p")
	})
	reg.Stream("krate/p.count", func(ctx krate.Ctx, a *wire.Decoder, emit func(func(*wire.Encoder)) error) error {
		for i := 0; i < 3; i++ {
			if err := emit(func(e *wire.Encoder) { e.Int(int64(i)) }); err != nil {
				return err
			}
		}
		return nil
	})
	ln, err := net.Listen("unix", filepath.Join(dir, "sdk.sock"))
	if err != nil {
		t.Skipf("unix sockets: %v", err)
	}
	srv := &http.Server{Handler: scheme.New(scheme.Options{Registry: reg})}
	go srv.Serve(ln)
	t.Cleanup(func() { srv.Close() })
	return "unix:" + filepath.Join(dir, "sdk.sock")
}

func TestHostCallForwardsToParent(t *testing.T) {
	addr := serveParent(t)
	each(t, func(t *testing.T, r rt) {
		f := setupEnv(t, r, func(src string) string {
			return strings.Replace(src, "console.log(\"version \"", `h.register("t.ViaSDK", (x) => h.call("krate/p.echo", [x]));
h.register("t.ViaDeny", () => h.call("krate/p.deny", []));
h.register("t.ViaMissing", () => h.call("krate/p.nope", []));
h.register("t.ViaLocal", (x) => h.call("t.Echo", [x]));
h.register("t.ViaStream", async () => { const out = []; for await (const v of h.stream("krate/p.count", [])) out.push(v); return out; });
console.log("version "`, 1)
		}, []string{"KRATE_GOHOST_PARENT=" + addr}, []string{"t.ViaSDK", "t.ViaDeny", "t.ViaMissing", "t.ViaLocal", "t.ViaStream"}, nil, nil)

		res := f.post(t, "t.ViaSDK", "", func(e *wire.Encoder) { e.String("hi") })
		if res.err != nil {
			t.Fatalf("ViaSDK: %+v", res.err)
		}
		if s, _ := res.ok.String(); s != "sdk:hi" {
			t.Errorf("ViaSDK = %q", s)
		}
		if res := f.post(t, "t.ViaDeny", "", nil); res.err == nil || res.err.Code != "permission_denied" {
			t.Errorf("ViaDeny: %+v", res.err)
		}
		if res := f.post(t, "t.ViaMissing", "", nil); res.err == nil || res.err.Code != "not_found" {
			t.Errorf("ViaMissing: %+v", res.err)
		}
		res = f.post(t, "t.ViaLocal", "", func(e *wire.Encoder) { e.String("same") })
		if res.err != nil {
			t.Fatalf("ViaLocal: %+v", res.err)
		}
		if s, _ := res.ok.String(); s != "same" {
			t.Errorf("ViaLocal = %q", s)
		}
		res = f.post(t, "t.ViaStream", "", nil)
		if res.err != nil {
			t.Fatalf("ViaStream: %+v", res.err)
		}
		var got []int64
		if err := res.ok.BeginArray(); err != nil {
			t.Fatal(err)
		}
		for res.ok.More() {
			v, _ := res.ok.Int()
			got = append(got, v)
		}
		if len(got) != 3 || got[0] != 0 || got[2] != 2 {
			t.Errorf("ViaStream = %v", got)
		}
	})
}

func TestHostCallWithoutParent(t *testing.T) {
	each(t, func(t *testing.T, r rt) {
		f := setupEnv(t, r, func(src string) string {
			return strings.Replace(src, "console.log(\"version \"", `h.register("t.ViaSDK", (x) => h.call("krate/p.echo", [x]));
console.log("version "`, 1)
		}, nil, []string{"t.ViaSDK"}, nil, nil)
		res := f.post(t, "t.ViaSDK", "", func(e *wire.Encoder) { e.String("hi") })
		if res.err == nil || res.err.Code != "unavailable" {
			t.Errorf("without a parent: %+v", res.err)
		}
	})
}
