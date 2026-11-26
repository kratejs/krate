package build

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/config"
)

// TestModuleSetupStatementsReplay: top-level package setup code of client
// modules (a vendor call, one whose argument calls a local helper, a member
// assignment on a package binding) runs in the page script before the
// component scopes, dependencies first. Statements rooted in local code and
// static modules are left out, and a statement reading a binding that cannot
// be brought into the page script is reported.
func TestModuleSetupStatementsReplay(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeProjectFile(t, root, "node_modules/plug/package.json", `{"name":"plug","version":"1.0.0","type":"module","main":"index.js"}`)
	writeProjectFile(t, root, "node_modules/plug/index.js", `
		export function register(n) { (globalThis.__reg ||= []).push('PLUG_MARK:' + n); }
		export const settings = { on: false };
	`)
	writeProjectFile(t, root, "src/lib/setup.ts", `
		import { register, settings } from 'plug';
		function local(n: string) { return 'local:' + n; }
		register(local('setup'));
		settings.on = true;
		export const ready = true;
	`)
	writeProjectFile(t, root, "src/lib/broken.ts", `
		import { register } from 'plug';
		import { ghost } from './nothing-here';
		register(ghost);
		export const b = 1;
	`)
	writeProjectFile(t, root, "src/lib/nothing-here.ts", `export class Holder {}
export * from 'plug'`)
	writeProjectFile(t, root, "src/components/Info.static.tsx", `
		import { register } from 'plug';
		register('static');
		export default function Info() { return <p>info</p>; }
	`)
	writeProjectFile(t, root, "src/components/Widget.tsx", `
		import { createSignal } from '@krate/runtime';
		import { register } from 'plug';
		import '../lib/setup';
		register('widget');
		function Item() { return <i />; }
		export function Widget() {
			const [n, setN] = createSignal(0);
			return <button id="w" onClick={() => setN(n() + 1)}>{n()}</button>;
		}
		// Local code is not package setup and is left to the existing paths.
		Widget.Item = Item;
		Item();
	`)
	writeProjectFile(t, root, "src/pages/index.tsx", `
		import { register } from 'plug';
		import { Widget } from '../components/Widget';
		import Info from '../components/Info.static';
		import { b } from '../lib/broken';
		register('page');
		export default function Page() { return <main><Widget /><Info />{b}</main>; }
	`)
	writeProjectFile(t, root, "src/pages/plain.tsx", `
		import { createSignal } from '@krate/runtime';
		export default function Plain() {
			const [n, setN] = createSignal(0);
			return <button onClick={() => setN(n() + 1)}>{n()}</button>;
		}
	`)
	cfg := config.Default()
	cfg.PagesDir = filepath.Join(root, "src", "pages")
	cfg.OutDir = filepath.Join(root, "dist")
	cfg.Minify = false
	out, err := captureBuild(New(root, cfg))
	if err != nil {
		t.Fatalf("BuildAll: %v\n%s", err, out)
	}
	js := readBuilt(t, mustGlobOne(t, filepath.Join(cfg.OutDir, "index.*.js")))
	order := []string{
		"register(local('setup'))",
		"settings.on = true",
		"register('widget')",
		"register('page')",
		"// component",
	}
	last := -1
	for _, want := range order {
		i := strings.Index(js, want)
		if i < 0 {
			t.Fatalf("page script is missing %q:\n%s", want, js)
		}
		if i < last {
			t.Errorf("%q runs out of module order:\n%s", want, js)
		}
		last = i
	}
	for _, want := range []string{"function local(", `register=__krateVendor["register"]`, "PLUG_MARK", "catch(e){__krateScopeErr(e);}"} {
		if !strings.Contains(js, want) {
			t.Errorf("page script is missing %q:\n%s", want, js)
		}
	}
	for _, bad := range []string{"register('static')", "register(ghost)", "Widget.Item = Item", "Item();})"} {
		if strings.Contains(js, bad) {
			t.Errorf("page script must not replay %q", bad)
		}
	}
	if !strings.Contains(out, "KR8302") || !strings.Contains(out, "ghost") {
		t.Errorf("a statement that cannot be replayed must be reported:\n%s", out)
	}
	// A page without module setup code has no setup block.
	plain := readBuilt(t, mustGlobOne(t, filepath.Join(cfg.OutDir, "plain", "index.*.js")))
	if strings.Contains(plain, "__krateModule(\"#s:") {
		t.Errorf("a page without setup code must not get a setup block:\n%s", plain)
	}
}
