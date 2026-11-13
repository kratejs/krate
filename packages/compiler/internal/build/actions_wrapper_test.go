package build

import (
	"encoding/json"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/config"
)

func TestParseActionExportsAcceptsWrappersAndRejectsLiterals(t *testing.T) {
	good := `"use server";
import { helper } from "./h";
import { createAction } from "./create";
export const save = createAction({ rate: 1 }, async () => 1);
const hidden = createAction({}, async () => 2);
export default createAction({}, async () => 3);
export { hidden as renamed, helper };
export { other, other2 as second } from "./other";
`
	exports, err := parseActionExports(good, "src/actions/a.ts")
	if err != nil {
		t.Fatalf("wrapper-built exports must be accepted: %v", err)
	}
	for _, name := range []string{"save", "default", "renamed", "helper", "other", "second"} {
		if exports[name] == "" {
			t.Errorf("missing export %q in %v", name, exports)
		}
	}

	for name, src := range map[string]string{
		"literal": `export const n = 5;`,
		"object":  `export const cfg = { a: 1 };`,
		"class":   `export class K {}`,
		"local":   "const limit = 10;\nexport { limit };",
		"default": `export default { a: 1 };`,
		"missing": `export { nope };`,
	} {
		_, err := parseActionExports(`"use server";`+"\n"+src, "src/actions/b.ts")
		if err == nil || !strings.Contains(err.Error(), "KR7004") {
			t.Errorf("%s: want a KR7004 error, got %v", name, err)
		}
	}
	if _, err := parseActionExports(`"use server"; export * from "./x";`, "src/actions/c.ts"); err == nil || !strings.Contains(err.Error(), "KR7005") {
		t.Errorf("export * must stay KR7005, got %v", err)
	}
}

// TestWrapperBuiltActionsRunInSidecar compiles a module whose actions come
// from a wrapper, a default wrapper call, a renamed export and a named
// re-export, then calls them through the real action sidecar. An export that
// turns out not to be a function fails when it is called and does not break
// its neighbours.
func TestWrapperBuiltActionsRunInSidecar(t *testing.T) {
	if _, err := exec.LookPath("node"); err != nil {
		requireE2E(t, "node is not installed")
		t.Skip("node is not installed")
	}
	root := t.TempDir()
	writeActionTestFile(t, filepath.Join(root, "src", "actions", "create.ts"), `export function createAction<I, O>(opts: { min: number }, fn: (n: I) => Promise<O>) {
  return async (input: I) => {
    if (typeof input !== "number" || input < opts.min) throw new Error("below minimum");
    return fn(input);
  };
}
export async function plain(n: number) { return n + 100; }
`)
	writeActionTestFile(t, filepath.Join(root, "src", "actions", "math.ts"), `"use server";
import { createAction } from "./create";
export { plain } from "./create";
export const double = createAction({ min: 1 }, async (n: number) => n * 2);
const triple = createAction({ min: 1 }, async (n: number) => n * 3);
export { triple as tripler };
export default createAction({ min: 0 }, async (n: number) => n + 1);
export const notAFunction = JSON.parse("{}");
`)
	cfg := config.Default()
	cfg.OutDir = filepath.Join(root, "dist")
	b := New(root, cfg)
	if err := b.prepareActions(); err != nil {
		t.Fatal(err)
	}
	id := map[string]string{}
	for _, e := range b.actions.Entries {
		id[e.Export] = e.ID
	}
	for _, name := range []string{"double", "tripler", "default", "plain", "notAFunction"} {
		if id[name] == "" {
			t.Fatalf("action %q missing from manifest: %v", name, id)
		}
	}
	srv := serveActionSidecar(t, "node", filepath.Join(root, ".krate", "actions"), false)
	call := func(name string, arg any) sidecarResult {
		body, _ := json.Marshal(map[string]any{"args": []any{arg}})
		return doAction(t, actionRequest(t, srv, "/__krate/action/"+id[name], "application/json", string(body), true))
	}
	for name, want := range map[string]float64{"double": 8, "tripler": 12, "default": 5, "plain": 104} {
		r := call(name, 4)
		if r.status != 200 || r.body["result"] != want {
			t.Errorf("%s(4): got %d %s, want %v", name, r.status, r.raw, want)
		}
	}
	if r := call("double", 0); r.status != 500 {
		t.Errorf("the wrapper's own validation must still run: %d %s", r.status, r.raw)
	}
	if r := call("notAFunction", 1); r.status != 500 {
		t.Errorf("a non-function export must fail when called: %d %s", r.status, r.raw)
	}
}
