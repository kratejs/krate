package hostjs

import (
	"testing"

	"github.com/kratejs/krate/packages/native/krate"
	"github.com/kratejs/krate/packages/native/wire"
)

// BenchmarkCall measures one host function round trip, from the generated-glue
// entry point (scheme.Func) through the VM goroutine and back.
func BenchmarkCall(b *testing.B) {
	h, err := New(Options{Log: func(string) {}})
	if err != nil {
		b.Fatal(err)
	}
	defer h.Close()
	js, sm := bundleTS(b, "src/lib/host.ts", `declare const __krateHost: any;
__krateHost.register("b.add", (x: number, y: number) => x + y, "func");
__krateHost.register("b.echo", (v: any) => v, "func");`)
	if err := h.Load(js, sm, "host.js"); err != nil {
		b.Fatal(err)
	}
	args := wire.NewEncoder()
	args.BeginArray()
	args.Int(1)
	args.Int(2)
	args.EndArray()
	argFrame := args.Frame()
	fn := h.funcOf("b.add")
	ctx := krate.Background()
	b.ResetTimer()
	for b.Loop() {
		d, _ := wire.NewDecoder(argFrame)
		out := wire.NewEncoder()
		if err := fn(ctx, d, out); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkCallObject(b *testing.B) {
	h, err := New(Options{Log: func(string) {}})
	if err != nil {
		b.Fatal(err)
	}
	defer h.Close()
	js, sm := bundleTS(b, "src/lib/host.ts", `declare const __krateHost: any;
__krateHost.register("b.echo", (v: any) => v, "func");`)
	if err := h.Load(js, sm, "host.js"); err != nil {
		b.Fatal(err)
	}
	args := wire.NewEncoder()
	args.Raw([]byte(`[{"id":7,"title":"Buy milk","tags":["a","b"],"done":false,"n":{"x":1.5}}]`))
	argFrame := args.Frame()
	fn := h.funcOf("b.echo")
	ctx := krate.Background()
	b.ResetTimer()
	for b.Loop() {
		d, _ := wire.NewDecoder(argFrame)
		out := wire.NewEncoder()
		if err := fn(ctx, d, out); err != nil {
			b.Fatal(err)
		}
	}
}
