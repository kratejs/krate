package resolver

import (
	"encoding/json"
	"path/filepath"
	"testing"
)

func TestVendorPrefixes(t *testing.T) {
	cases := []struct {
		raw     string
		want    []string
		wantErr bool
	}{
		{raw: ``},
		{raw: `"./krate.config.ts"`},
		{raw: `{"plugin":"x"}`},
		{raw: `{"vendor":["src/engine/","./lib/x.js"]}`, want: []string{"src/engine/", "lib/x.js"}},
		{raw: `{"vendor":"src/engine/"}`, wantErr: true},
		{raw: `{"vendor":["../outside/"]}`, wantErr: true},
		{raw: `{"vendor":["/abs/"]}`, wantErr: true},
		{raw: `{"vendor":[""]}`, wantErr: true},
	}
	for _, c := range cases {
		got, err := VendorPrefixes(json.RawMessage(c.raw))
		if (err != nil) != c.wantErr {
			t.Errorf("%s: err = %v, wantErr %v", c.raw, err, c.wantErr)
			continue
		}
		if len(got) != len(c.want) {
			t.Errorf("%s: got %v, want %v", c.raw, got, c.want)
			continue
		}
		for i := range got {
			if got[i] != c.want[i] {
				t.Errorf("%s: got %v, want %v", c.raw, got, c.want)
			}
		}
	}
}

// Files under krate.vendor are compiled dependencies, in a @krate/* package
// too; the rest of the package stays Krate source.
func TestKrateVendorIsCompiledDependency(t *testing.T) {
	ResetCaches()
	t.Cleanup(ResetCaches)
	root := t.TempDir()
	pkg := filepath.Join(root, "node_modules", "@krate", "three")
	writeFile(t, filepath.Join(pkg, "package.json"), `{"name":"@krate/three","krate":{"vendor":["src/engine/"]},"exports":{".":"./src/index.ts","./engine":"./src/engine/index.ts"}}`)
	writeFile(t, filepath.Join(pkg, "src", "index.ts"), `export { Canvas } from './Canvas';`)
	writeFile(t, filepath.Join(pkg, "src", "Canvas.tsx"), `import { createRoot } from '@krate/three/engine';`)
	writeFile(t, filepath.Join(pkg, "src", "engine", "index.ts"), `export function createRoot() {}`)
	other := filepath.Join(root, "node_modules", "lib")
	writeFile(t, filepath.Join(other, "package.json"), `{"name":"lib","krate":{"vendor":["dist/"]}}`)

	engine := filepath.Join(pkg, "src", "engine", "index.ts")
	if !IsCompiledDependency(engine) {
		t.Error("a krate.vendor file of a @krate package must be a compiled dependency")
	}
	if IsCompiledDependency(filepath.Join(pkg, "src", "Canvas.tsx")) {
		t.Error("the rest of a @krate package stays Krate source")
	}
	if !IsCompiledDependency(filepath.Join(other, "dist", "a.ts")) {
		t.Error("a krate.vendor .ts file of a package with a krate field must be a compiled dependency")
	}
	if IsCompiledDependency(filepath.Join(other, "src", "a.ts")) {
		t.Error("a package with a krate field stays Krate source outside krate.vendor")
	}
	// The package imports its own engine by name (Node's self-reference), and
	// the path stays under node_modules.
	if got := NodeModule(filepath.Join(pkg, "src"), "@krate/three/engine"); got != engine {
		t.Errorf("self-reference resolved to %q, want %q", got, engine)
	}
}
