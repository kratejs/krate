package build

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const funcDefaultPage = `import { createSignal } from "@krate/runtime";
function Btn({ onX = () => "dflt" }) {
	const [n, setN] = createSignal(1);
	return <p class="out" onClick={() => setN(n() + 1)}>{onX()}</p>;
}
export default function Page() {
	return <main><Btn onX={() => "custom"} /><Btn /></main>;
}
`

// A function prop that is passed replaces a function default; the default
// applies only when the prop is omitted.
func TestFunctionPropDefaultOnlyWhenOmitted(t *testing.T) {
	_, out, _ := buildFiles(t, map[string]string{"src/pages/index.tsx": funcDefaultPage}, nil)
	htmlBytes, err := os.ReadFile(filepath.Join(out, "index.html"))
	if err != nil {
		t.Fatal(err)
	}
	html := string(htmlBytes)
	if got := strings.Count(html, "dflt"); got != 1 {
		t.Errorf("the default must render once (for the <Btn /> with no prop), got %d:\n%s", got, html)
	}
	jsFiles, _ := filepath.Glob(filepath.Join(out, "index.*.js"))
	if len(jsFiles) != 1 {
		t.Fatalf("want one hydration bundle, got %v", jsFiles)
	}
	js, _ := os.ReadFile(jsFiles[0])
	// The call site's function reaches the child through the props registry,
	// which the child binds before it reads props.onX.
	if !strings.Contains(string(js), `onX:()=>"custom"`) || !strings.Contains(string(js), "__krate_props[") {
		t.Errorf("the passed function is not registered for hydration:\n%s", js)
	}
}

func TestFunctionPropDefaultBrowser(t *testing.T) {
	evalFixBrowser(t, map[string]string{"src/pages/index.tsx": funcDefaultPage}, []evalFixCheck{
		// The passed function is called at runtime, so its slot is empty until
		// hydration; the omitted prop folds to its default at build time.
		{Path: "/", Selector: "p.out", Static: strp("|dflt"), Hydrated: "custom|dflt"},
	})
}
