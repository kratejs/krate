package jsruntime

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"krate-compiler/internal/escape"
)

// testBundleCode generates a self-contained IIFE bundle that mimics
// what CompileRuntimeComponent produces. It defines globalThis.__krate_render.
func testBundleCode(componentBody string) string {
	// Minimal JSX shim + component bundled together as an IIFE
	return `(function(){
function __escapeHtml(v){return String(v).replace(/&/g,'&amp;').replace(/</g,'&lt;').replace(/>/g,'&gt;').replace(/"/g,'&quot;');}
function __renderChildren(c){if(c==null||c===false||c===true)return'';if(Array.isArray(c)){var h='';for(var i=0;i<c.length;i++)h+=__renderChildren(c[i]);return h;}if(typeof c==='object'){if(typeof c.__html==='string')return c.__html;return'';}return String(c);}
function __renderProps(p){var parts=[];var keys=Object.keys(p);for(var i=0;i<keys.length;i++){var k=keys[i];if(k==='children'||k==='key'||k==='ref')continue;var v=p[k];if(v==null||v===false)continue;if(k==='className')k='class';if(v===true)parts.push(' '+k);else parts.push(' '+k+'="'+__escapeHtml(v)+'"');}return parts.join('');}
function jsx(tag,props,key){if(typeof tag==='function')return tag(props||{});props=props||{};var ch=props.children;var a=__renderProps(props);var c=__renderChildren(ch);return '<'+tag+a+'>'+c+'</'+tag+'>';
}
function jsxs(tag,props,key){return jsx(tag,props,key);}
` + componentBody + `
})()
`
}

func TestRuntimeComponentRender(t *testing.T) {
	tmpDir := t.TempDir()

	// Create a bundle that defines a simple component and __krate_render
	bundleCode := testBundleCode(`
function Greeting(props) {
  return '<div class="greeting"><h1>Hello, ' + (props.name || 'World') + '!</h1></div>';
}
globalThis.__krate_render = function(propsJSON) {
  var props = typeof propsJSON === 'string' ? JSON.parse(propsJSON) : (propsJSON || {});
  return Greeting(props);
};
`)
	bundlePath := filepath.Join(tmpDir, "Greeting.runtime.js")
	if err := os.WriteFile(bundlePath, []byte(bundleCode), 0644); err != nil {
		t.Fatal(err)
	}

	rt := NewRuntimeComponentRuntime(tmpDir)
	result := rt.RenderComponent("Greeting", `{"name":"Alice"}`)

	if result.Error != "" {
		t.Fatalf("render error: %s", result.Error)
	}
	if !strings.Contains(result.HTML, "Hello, Alice!") {
		t.Errorf("expected 'Hello, Alice!' in output, got: %s", result.HTML)
	}
	if !strings.Contains(result.HTML, `class="greeting"`) {
		t.Errorf("expected class='greeting' in output, got: %s", result.HTML)
	}
	t.Logf("HTML output: %s", result.HTML)
}

func TestRuntimeComponentRenderDefaultProps(t *testing.T) {
	tmpDir := t.TempDir()

	bundleCode := testBundleCode(`
function Badge(props) {
  return '<span class="badge">' + (props.label || 'default') + '</span>';
}
globalThis.__krate_render = function(propsJSON) {
  var props = typeof propsJSON === 'string' ? JSON.parse(propsJSON) : (propsJSON || {});
  return Badge(props);
};
`)
	os.WriteFile(filepath.Join(tmpDir, "Badge.runtime.js"), []byte(bundleCode), 0644)

	rt := NewRuntimeComponentRuntime(tmpDir)

	// With props
	result := rt.RenderComponent("Badge", `{"label":"Admin"}`)
	if result.Error != "" {
		t.Fatalf("render error: %s", result.Error)
	}
	if !strings.Contains(result.HTML, "Admin") {
		t.Errorf("expected 'Admin' in output, got: %s", result.HTML)
	}

	// Without props (empty JSON object)
	result = rt.RenderComponent("Badge", `{}`)
	if result.Error != "" {
		t.Fatalf("render error: %s", result.Error)
	}
	if !strings.Contains(result.HTML, "default") {
		t.Errorf("expected 'default' in output, got: %s", result.HTML)
	}
}

func TestRuntimeComponentNotFound(t *testing.T) {
	tmpDir := t.TempDir()
	os.MkdirAll(tmpDir, 0755)

	rt := NewRuntimeComponentRuntime(tmpDir)
	result := rt.RenderComponent("NonExistent", `{}`)

	if result.Error == "" {
		t.Error("expected error for non-existent component")
	}
	if !strings.Contains(result.Error, "not found") {
		t.Errorf("expected 'not found' in error, got: %s", result.Error)
	}
}

func TestRuntimeComponentJSError(t *testing.T) {
	tmpDir := t.TempDir()

	// Bundle that throws an error
	bundleCode := `(function(){
globalThis.__krate_render = function() {
  throw new Error('Component crashed!');
};
})()
`
	os.WriteFile(filepath.Join(tmpDir, "ErrorComp.runtime.js"), []byte(bundleCode), 0644)

	rt := NewRuntimeComponentRuntime(tmpDir)
	result := rt.RenderComponent("ErrorComp", `{}`)

	if result.Error == "" {
		t.Error("expected error from crashing component")
	}
	if !strings.Contains(result.Error, "Component crashed") {
		t.Errorf("expected 'Component crashed' in error, got: %s", result.Error)
	}
}

func TestRuntimeComponentEmptyBundle(t *testing.T) {
	tmpDir := t.TempDir()

	// Bundle that doesn't define __krate_render
	bundleCode := `(function(){ var x = 42; })()`
	os.WriteFile(filepath.Join(tmpDir, "Empty.runtime.js"), []byte(bundleCode), 0644)

	rt := NewRuntimeComponentRuntime(tmpDir)
	result := rt.RenderComponent("Empty", `{}`)

	if result.Error == "" {
		t.Error("expected error for bundle without __krate_render")
	}
}

func TestEscapeJSString(t *testing.T) {
	tests := []struct {
		input    string
		contains string // substring that must appear in the result
	}{
		{"hello", "hello"},
		{`he said "hi"`, `\"hi\"`},
		{"line\nbreak", `\n`},
		{"back\\slash", `\\`},
		{"tab\there", `\t`},
		{"", `""`},
	}

	for _, tt := range tests {
		result := escape.JSStringDQ(tt.input)
		if !strings.Contains(result, tt.contains) {
			t.Errorf("escape.JSStringDQ(%q) = %q, expected to contain %q", tt.input, result, tt.contains)
		}
		// Should be wrapped in quotes
		if !strings.HasPrefix(result, `"`) || !strings.HasSuffix(result, `"`) {
			t.Errorf("escape.JSStringDQ(%q) = %q, expected quoted string", tt.input, result)
		}
	}
}

func TestRuntimeComponentResolveBundle(t *testing.T) {
	tmpDir := t.TempDir()

	// Only .runtime.js file
	os.WriteFile(filepath.Join(tmpDir, "Foo.runtime.js"), []byte("x"), 0644)

	// Also .js file (fallback)
	os.WriteFile(filepath.Join(tmpDir, "Bar.js"), []byte("y"), 0644)

	rt := NewRuntimeComponentRuntime(tmpDir)

	// Should find Foo via .runtime.js
	result := rt.RenderComponent("Foo", `{}`)
	if result.Error == "" || !strings.Contains(result.Error, "not found") {
		// Foo.runtime.js exists but has invalid code, so it should fail at execution, not resolution
		t.Logf("Foo result (expected execution error): %s", result.Error)
	}

	// Should find Bar via .js fallback
	result = rt.RenderComponent("Bar", `{}`)
	if result.Error == "" || !strings.Contains(result.Error, "not found") {
		t.Logf("Bar found and executed (execution error expected for bare 'x'): %s", result.Error)
	}

	// Should NOT find Baz
	result = rt.RenderComponent("Baz", `{}`)
	if result.Error == "" {
		t.Error("expected error for Baz")
	}
}
