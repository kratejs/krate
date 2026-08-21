package build

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCompileRuntimeComponents(t *testing.T) {
	// Create a temp project directory
	tmpDir := t.TempDir()
	srcDir := filepath.Join(tmpDir, "src")
	os.MkdirAll(srcDir, 0755)
	outDir := filepath.Join(tmpDir, "dist")

	// Create a simple runtime component (plain JS, no JSX, to avoid needing the full shim in test)
	componentSrc := `export default function Greeting(props) {
  return '<div class="greeting"><h1>Hello, ' + (props.name || 'World') + '!</h1></div>';
}
`
	componentPath := filepath.Join(srcDir, "Greeting.runtime.tsx")
	if err := os.WriteFile(componentPath, []byte(componentSrc), 0644); err != nil {
		t.Fatal(err)
	}

	// Compile it
	bundles := CompileRuntimeComponents(tmpDir, outDir, nil, nil, nil)
	if len(bundles) != 1 {
		t.Fatalf("expected 1 bundle, got %d", len(bundles))
	}

	bundle := bundles[0]
	if bundle.Name != "Greeting" {
		t.Errorf("expected name 'Greeting', got %q", bundle.Name)
	}
	if bundle.BundlePath == "" {
		t.Error("expected non-empty BundlePath")
	}

	// Verify the output file exists
	bundleFile := filepath.Join(outDir, bundle.BundlePath)
	if _, err := os.Stat(bundleFile); os.IsNotExist(err) {
		t.Fatalf("bundle file does not exist: %s", bundleFile)
	}

	// Verify the bundle contains __krate_render
	data, err := os.ReadFile(bundleFile)
	if err != nil {
		t.Fatal(err)
	}
	bundleContent := string(data)
	if !strings.Contains(bundleContent, "__krate_render") {
		t.Error("bundle does not contain __krate_render function")
	}
	if !strings.Contains(bundleContent, "greeting") {
		t.Error("bundle does not contain component content")
	}

	t.Logf("Bundle written to: %s (%d bytes)", bundleFile, len(data))
}

func TestCompileRuntimeComponentsWithDirective(t *testing.T) {
	tmpDir := t.TempDir()
	srcDir := filepath.Join(tmpDir, "src")
	os.MkdirAll(srcDir, 0755)
	outDir := filepath.Join(tmpDir, "dist")

	// Create a runtime component using the @runtime directive
	componentSrc := `// @runtime
export default function Badge(props) {
  return '<span class="badge">' + (props.label || '') + '</span>';
}
`
	componentPath := filepath.Join(srcDir, "Badge.tsx")
	if err := os.WriteFile(componentPath, []byte(componentSrc), 0644); err != nil {
		t.Fatal(err)
	}

	bundles := CompileRuntimeComponents(tmpDir, outDir, nil, nil, nil)
	if len(bundles) != 1 {
		t.Fatalf("expected 1 bundle, got %d", len(bundles))
	}
	if bundles[0].Name != "Badge" {
		t.Errorf("expected name 'Badge', got %q", bundles[0].Name)
	}
}

func TestCompileRuntimeComponentsEmpty(t *testing.T) {
	tmpDir := t.TempDir()
	os.MkdirAll(filepath.Join(tmpDir, "src"), 0755)
	outDir := filepath.Join(tmpDir, "dist")

	bundles := CompileRuntimeComponents(tmpDir, outDir, nil, nil, nil)
	if len(bundles) != 0 {
		t.Errorf("expected 0 bundles for empty src/, got %d", len(bundles))
	}
}

func TestCompileRuntimeComponentsSkipsNonRuntime(t *testing.T) {
	tmpDir := t.TempDir()
	srcDir := filepath.Join(tmpDir, "src")
	os.MkdirAll(srcDir, 0755)
	outDir := filepath.Join(tmpDir, "dist")

	// Regular component (no directive, no .runtime suffix)
	regularSrc := `export default function Regular() { return '<div>'; }`
	os.WriteFile(filepath.Join(srcDir, "Regular.tsx"), []byte(regularSrc), 0644)

	// Server component (not runtime)
	serverSrc := `// @server
export default function Server() { return '<div>'; }`
	os.WriteFile(filepath.Join(srcDir, "Server.tsx"), []byte(serverSrc), 0644)

	bundles := CompileRuntimeComponents(tmpDir, outDir, nil, nil, nil)
	if len(bundles) != 0 {
		t.Errorf("expected 0 runtime bundles, got %d", len(bundles))
	}
}

func TestCompileRuntimeComponentWithJSX(t *testing.T) {
	tmpDir := t.TempDir()
	srcDir := filepath.Join(tmpDir, "src")
	os.MkdirAll(srcDir, 0755)
	outDir := filepath.Join(tmpDir, "dist")

	// Create a runtime component with JSX (uses the automatic JSX transform)
	componentSrc := `export default function Card(props) {
  return <div class="card"><h2>{props.title}</h2><p>{props.body}</p></div>;
}
`
	componentPath := filepath.Join(srcDir, "Card.runtime.tsx")
	if err := os.WriteFile(componentPath, []byte(componentSrc), 0644); err != nil {
		t.Fatal(err)
	}

	bundles := CompileRuntimeComponents(tmpDir, outDir, nil, nil, nil)
	if len(bundles) != 1 {
		t.Fatalf("expected 1 bundle, got %d", len(bundles))
	}

	// The bundle should be a self-contained IIFE
	data, err := os.ReadFile(filepath.Join(outDir, bundles[0].BundlePath))
	if err != nil {
		t.Fatal(err)
	}
	content := string(data)
	if !strings.Contains(content, "__krate_render") {
		t.Error("JSX bundle missing __krate_render")
	}
}

func TestCompileRuntimeComponentsMultiple(t *testing.T) {
	tmpDir := t.TempDir()
	srcDir := filepath.Join(tmpDir, "src")
	compDir := filepath.Join(srcDir, "components")
	os.MkdirAll(compDir, 0755)
	outDir := filepath.Join(tmpDir, "dist")

	components := map[string]string{
		"A.runtime.tsx": `export default function A(p) { return '<div>A</div>'; }`,
		"B.runtime.tsx": `export default function B(p) { return '<div>B</div>'; }`,
		"C.tsx":         `export default function C(p) { return '<div>C</div>'; }`, // not runtime
	}

	for name, src := range components {
		os.WriteFile(filepath.Join(compDir, name), []byte(src), 0644)
	}

	bundles := CompileRuntimeComponents(tmpDir, outDir, nil, nil, nil)
	if len(bundles) != 2 {
		t.Fatalf("expected 2 runtime bundles, got %d", len(bundles))
	}

	names := make(map[string]bool)
	for _, b := range bundles {
		names[b.Name] = true
	}
	if !names["A"] || !names["B"] {
		t.Errorf("expected bundles for A and B, got %v", names)
	}
}
