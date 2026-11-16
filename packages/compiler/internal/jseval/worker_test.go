package jseval

import (
	"bytes"
	"errors"
	"os/exec"
	"testing"
	"time"

	"github.com/kratejs/krate/packages/compiler/internal/jsworker"
)

// TestEvalOnWorkerMatchesQuickJS runs entries on QuickJS and on each worker
// runtime installed here and requires the same result or error.
func TestEvalOnWorkerMatchesQuickJS(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "data.ts", "let n = 0;\nexport const next = () => ++n;\nexport const table = Array.from({ length: 50 }, (_, i) => ({ i, s: 's' + i }));\n")
	cases := map[string]Options{
		"value": {Source: `import { next, table } from './data';
next();
globalThis.__krateEvalResult = { n: next(), last: table[table.length - 1], keys: Object.keys({ b: 1, a: 2, 1: 3 }) };`},
		"env": {Source: `globalThis.__krateEvalResult = { mode: process.env.MODE, missing: process.env.NOPE ?? null };`, Env: map[string]string{"MODE": "prod"}},
		"await": {Source: `const v = await Promise.resolve(1);
globalThis.__krateEvalResult = v;`, Await: true},
		"async": {Source: `const run = async () => { await null; globalThis.__krateEvalResult = { ok: [1, 2].map(async (x) => x).length }; };
run();`, Await: true},
		"no await": {Source: `Promise.resolve().then(() => { globalThis.__krateEvalResult = 1; });`},
		"thrown": {Source: `const run = async () => { throw new Error("boom"); };
run().catch((e) => { globalThis.__krateEvalError = e.message; });`, Await: true},
		"module throw": {Source: `throw new Error("at load");`},
		"require":      {Source: `const p = require("path"); globalThis.__krateEvalResult = p.join("a", "b");`},
		"json":         {Source: `globalThis.__krateEvalResult = { d: new Date(0).toISOString(), u: new URL("/x?a=1", "https://h.example").href, e: encodeURIComponent("a b") };`},
	}
	for _, n := range []string{"node", "bun"} {
		path, err := exec.LookPath(n)
		if err != nil {
			continue
		}
		t.Run(n, func(t *testing.T) {
			p := jsworker.New(n, path, 1)
			defer p.Close()
			for name, c := range cases {
				c.ResolveDir = dir
				want, werr := Eval(c)
				c.Worker = p
				ran := false
				p.Observe = func(string, time.Duration) { ran = true }
				c.WorkerLabel = "test"
				got, gerr := Eval(c)
				if !ran {
					t.Errorf("%s: the worker did not run", name)
				}
				if (werr == nil) != (gerr == nil) || werr != nil && werr.Error() != gerr.Error() {
					t.Errorf("%s: error %v, quickjs %v", name, gerr, werr)
					continue
				}
				var te, ge *ThrownError
				if errors.As(werr, &te) != errors.As(gerr, &ge) {
					t.Errorf("%s: thrown error kind differs", name)
				}
				if !bytes.Equal(want, got) {
					t.Errorf("%s: got %s, quickjs %s", name, got, want)
				}
			}
		})
	}
}
