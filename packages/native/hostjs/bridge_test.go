package hostjs

import (
	"testing"

	"github.com/kratejs/krate/packages/native/krate"
	"github.com/kratejs/krate/packages/native/scheme"
	"github.com/kratejs/krate/packages/native/wire"
)

const bridgeSrc = `
declare const __krateHost: any;
async function viaCall(n: number) {
  const r = await __krateHost.call("go.double", [n]);
  return { r, again: await __krateHost.call("go.double", [r]) };
}
async function viaBytes() {
  const out = await __krateHost.call("go.reverse", [new Uint8Array([1, 2, 3])]);
  return Array.from(out as Uint8Array);
}
async function viaFail() {
  try { await __krateHost.call("go.fail", []); } catch (e: any) { return { code: e.code, message: e.message, data: e.data }; }
  return null;
}
async function viaMissing() {
  try { await __krateHost.call("go.nope", []); } catch (e: any) { return e.code; }
  return null;
}
async function viaStream() {
  const got: number[] = [];
  for await (const v of __krateHost.stream("go.count", [3])) got.push(v);
  return got;
}
async function viaSignal() {
  const s = __krateHost.signal("go.sig");
  await new Promise((r) => setTimeout(r, 30));
  const v = s.get();
  s.close();
  return v;
}
async function viaCallee() { return __krateHost.call("host.twice", [4]); }
__krateHost.register("host.twice", (n: number) => n * 2, "func");
__krateHost.register("host.viaCall", viaCall, "func");
__krateHost.register("host.viaBytes", viaBytes, "func");
__krateHost.register("host.viaFail", viaFail, "func");
__krateHost.register("host.viaMissing", viaMissing, "func");
__krateHost.register("host.viaStream", viaStream, "func");
__krateHost.register("host.viaSignal", viaSignal, "func");
__krateHost.register("host.viaCallee", viaCallee, "func");
`

func TestInProcessBridge(t *testing.T) {
	r := newRig(t, bridgeSrc, Options{})
	r.reg.Func("go.double", func(ctx krate.Ctx, a *wire.Decoder, out *wire.Encoder) error {
		if err := a.BeginArray(); err != nil {
			return err
		}
		n, err := a.Int()
		if err != nil {
			return err
		}
		if err := a.EndArray(); err != nil {
			return err
		}
		if ctx.Window() != "" {
			t.Errorf("window = %q, want empty", ctx.Window())
		}
		out.Int(n * 2)
		return nil
	})
	r.reg.Func("go.reverse", func(ctx krate.Ctx, a *wire.Decoder, out *wire.Encoder) error {
		if err := a.BeginArray(); err != nil {
			return err
		}
		b, err := a.Bytes()
		if err != nil {
			return err
		}
		if err := a.EndArray(); err != nil {
			return err
		}
		o := make([]byte, len(b))
		for i := range b {
			o[len(b)-1-i] = b[i]
		}
		out.Bytes(o)
		return nil
	})
	r.reg.Func("go.fail", func(ctx krate.Ctx, a *wire.Decoder, out *wire.Encoder) error {
		return &krate.Error{Code: "denied", Message: "nope", Data: map[string]any{"x": 1}}
	})
	r.reg.Stream("go.count", func(ctx krate.Ctx, a *wire.Decoder, emit func(func(*wire.Encoder)) error) error {
		if err := a.BeginArray(); err != nil {
			return err
		}
		n, _ := a.Int()
		for i := int64(1); i <= n; i++ {
			if err := emit(func(e *wire.Encoder) { e.Int(i) }); err != nil {
				return err
			}
		}
		return nil
	})
	sig := krate.NewSignal("hi")
	r.reg.Signal("go.sig", scheme.SignalOf(sig, func(e *wire.Encoder, s string) { e.String(s) }))

	if res := r.call("host.viaCall", func(e *wire.Encoder) { e.Int(5) }); res.Err != nil {
		t.Fatalf("%+v", res)
	} else if m, _ := res.OK.(map[string]any); m["r"] != float64(10) || m["again"] != float64(20) {
		t.Fatalf("%+v", res.OK)
	}
	if res := r.call("host.viaBytes", nil); str(res.OK) != "[3 2 1]" {
		t.Fatalf("%+v", res)
	}
	if res := r.call("host.viaFail", nil); res.Err != nil {
		t.Fatalf("%+v", res)
	} else if m, _ := res.OK.(map[string]any); m["code"] != "denied" || m["message"] != "nope" || str(m["data"]) != "map[x:1]" {
		t.Fatalf("%+v", res.OK)
	}
	if res := r.call("host.viaMissing", nil); res.OK != "not_found" {
		t.Fatalf("%+v", res)
	}
	if res := r.call("host.viaStream", nil); str(res.OK) != "[1 2 3]" {
		t.Fatalf("%+v", res)
	}
	if res := r.call("host.viaSignal", nil); res.OK != "hi" {
		t.Fatalf("%+v", res)
	}
	if res := r.call("host.viaCallee", nil); res.OK != float64(8) {
		t.Fatalf("%+v", res)
	}
}
