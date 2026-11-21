package build

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/evanw/esbuild/pkg/api"
)

// threeResolveFixture is a project with fake @krate/three (with island
// builds) and @react-three/fiber and drei packages.
func threeResolveFixture(t *testing.T, withFiber bool) string {
	t.Helper()
	root := t.TempDir()
	write := func(rel, body string) {
		t.Helper()
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0644); err != nil {
			t.Fatal(err)
		}
	}
	write("node_modules/@krate/three/package.json", `{"name":"@krate/three","type":"module","exports":{".":"./src/index.ts","./drei":"./src/drei.tsx","./island":"./src/island/index.ts","./island/drei":"./src/island/drei.ts"}}`)
	write("node_modules/@krate/three/src/index.ts", `export const which = "krate-source";`)
	write("node_modules/@krate/three/src/drei.tsx", `export const drei = "krate-source-drei";`)
	write("node_modules/@krate/three/src/island/index.ts", `export const which = "island";`)
	write("node_modules/@krate/three/src/island/drei.ts", `export const drei = "island-drei";`)
	if withFiber {
		write("node_modules/@react-three/fiber/package.json", `{"name":"@react-three/fiber","main":"index.js"}`)
		write("node_modules/@react-three/fiber/index.js", `export const which = "fiber";`)
		write("node_modules/@react-three/drei/package.json", `{"name":"@react-three/drei","main":"index.js"}`)
		write("node_modules/@react-three/drei/index.js", `export const drei = "drei";`)
	}
	return root
}

func bundleWith(t *testing.T, root string, plugin api.Plugin) (string, []api.Message) {
	t.Helper()
	res := api.Build(api.BuildOptions{
		Stdin:   &api.StdinOptions{Contents: `import { which } from "@krate/three"; import { drei } from "@krate/three/drei"; console.log(which, drei);`, ResolveDir: root, Loader: api.LoaderTS},
		Bundle:  true,
		Write:   false,
		Format:  api.FormatESModule,
		Plugins: []api.Plugin{plugin},
	})
	if len(res.OutputFiles) == 0 {
		return "", res.Errors
	}
	return string(res.OutputFiles[0].Contents), res.Errors
}

// A Preact island gets the island builds of @krate/three and its drei.
func TestThreeIslandPluginResolvesIslandBuilds(t *testing.T) {
	t.Parallel()
	out, errs := bundleWith(t, threeResolveFixture(t, false), threeIslandPlugin())
	if len(errs) > 0 {
		t.Fatalf("build errors: %v", errs)
	}
	if !strings.Contains(out, `"island"`) || !strings.Contains(out, `"island-drei"`) || strings.Contains(out, "krate-source") {
		t.Fatalf("island builds not used:\n%s", out)
	}
}

// A real-React island runs a @krate/three scene as @react-three/fiber, and
// says what to install when fiber is missing.
func TestThreeRealReactPluginUsesFiber(t *testing.T) {
	t.Parallel()
	out, errs := bundleWith(t, threeResolveFixture(t, true), threeRealReactPlugin())
	if len(errs) > 0 {
		t.Fatalf("build errors: %v", errs)
	}
	if !strings.Contains(out, `"fiber"`) || !strings.Contains(out, `"drei"`) || strings.Contains(out, "krate-source") || strings.Contains(out, "island") {
		t.Fatalf("fiber not used:\n%s", out)
	}
	_, errs = bundleWith(t, threeResolveFixture(t, false), threeRealReactPlugin())
	if len(errs) == 0 || !strings.Contains(errs[0].Text, "real-React island") || !strings.Contains(errs[0].Notes[0].Text, "install @react-three/") {
		t.Fatalf("expected an actionable error without fiber, got %+v", errs)
	}
}
