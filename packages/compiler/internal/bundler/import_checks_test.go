package bundler

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/diag"
)

func fakeRuntime() map[string]string {
	return map[string]string{
		"node_modules/@krate/runtime/package.json":   `{"name":"@krate/runtime","type":"module","main":"./dist/index.js","exports":{".":"./dist/index.js"}}`,
		"node_modules/@krate/runtime/dist/index.js":  "export { createSignal, onMount as mounted } from './signal.js';\nexport * from './more.js';\nexport const direct = 1;\nexport default {};\n",
		"node_modules/@krate/runtime/dist/signal.js": "export function createSignal() {}\nexport function onMount() {}\n",
		"node_modules/@krate/runtime/dist/more.js":   "export function navigate() {}\nexport function getNavigationState() {}\n",
	}
}

func bundleWith(t *testing.T, page string, extra map[string]string) error {
	t.Helper()
	root := t.TempDir()
	files := fakeRuntime()
	files["src/pages/index.tsx"] = page
	for k, v := range extra {
		files[k] = v
	}
	writeFiles(t, root, files)
	_, err := New(root).Bundle(filepath.Join(root, "src/pages/index.tsx"))
	return err
}

func TestRuntimeImportOfMissingNameIsAnError(t *testing.T) {
	err := bundleWith(t, "import { createSignal, signal } from '@krate/runtime';\nexport default function P() { return <p />; }\n", nil)
	d, ok := diag.FromError(err)
	if !ok || d.Code != diag.CodeRuntimeExportMissing {
		t.Fatalf("err = %v", err)
	}
	if !strings.Contains(d.Message, `"signal"`) || d.Line != 1 || !strings.Contains(d.Hint, "createSignal") {
		t.Errorf("diag = %+v", d)
	}

	err = bundleWith(t, "import { usePathname } from '@krate/runtime';\nexport default function P() { return <p />; }\n", nil)
	if d, ok := diag.FromError(err); !ok || !strings.Contains(d.Hint, "getNavigationState") {
		t.Errorf("usePathname hint: %v", err)
	}
}

func TestRuntimeImportOfRealNamesPasses(t *testing.T) {
	err := bundleWith(t, "import rt, { createSignal, mounted, navigate, direct } from '@krate/runtime';\nimport * as all from '@krate/runtime';\nexport default function P() { return <p />; }\n", nil)
	if err != nil {
		t.Fatalf("valid imports rejected: %v", err)
	}
}

func TestStylesheetImportsOfUnsupportedLanguagesAreErrors(t *testing.T) {
	for _, ext := range []string{".scss", ".sass", ".less", ".styl", ".module.scss"} {
		err := bundleWith(t, "import './a"+ext+"';\nexport default function P() { return <p />; }\n", map[string]string{"src/pages/a" + ext: "a{}"})
		d, ok := diag.FromError(err)
		if !ok || d.Code != diag.CodeUnsupportedStylesheet || d.Line != 1 || d.Hint == "" {
			t.Errorf("%s: err = %v", ext, err)
		}
	}
}
