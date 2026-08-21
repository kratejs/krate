package jsruntime

import (
	"os"
	"path/filepath"
	"testing"
)

func TestStripExports(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "named_export_braces",
			input:    "function GET() {}\nexport {\n  GET\n};",
			expected: "function GET() {}\n",
		},
		{
			name:     "esbuild_minified_named_exports",
			input:    "var n=[{id:'abc123'}];function s(o){}async function d(o){}export{s as GET,d as POST};",
			expected: "var n=[{id:'abc123'}];function s(o){}async function d(o){}var GET = s;\nvar POST = d;",
		},
		{
			name:     "export_multiple_as_default",
			input:    "function a(){}\nfunction b(){}\nexport { a as default, b as helper };",
			expected: "function a(){}\nfunction b(){}\nvar __default = a;\nvar helper = b;",
		},
		{
			name:     "export as default",
			input:    "function handler() {}\nexport { handler as default };",
			expected: "function handler() {}\nvar __default = handler;",
		},
		{
			name:     "export const",
			input:    "export const FOO = 42;",
			expected: "const FOO = 42;",
		},
		{
			name:     "export default named function",
			input:    "export default function handler() {}",
			expected: "function handler() {}\nvar __default = (typeof _default !== 'undefined') ? _default : undefined;",
		},
		{
			name:     "export default anonymous function",
			input:    "export default function() {}",
			expected: "function _default() {}\nvar __default = (typeof _default !== 'undefined') ? _default : undefined;",
		},
		{
			name:     "no exports",
			input:    "const x = 1;\nfunction foo() {}",
			expected: "const x = 1;\nfunction foo() {}",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := stripExports(tt.input)
			if result != tt.expected {
				t.Errorf("stripExports(%q)\n  got:  %q\n  want: %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestAPIRouteExecute(t *testing.T) {
	tmpDir := t.TempDir()
	apiDir := filepath.Join(tmpDir, "api")
	os.MkdirAll(apiDir, 0755)

	// Create a simple API route
	routeCode := `
function GET(request) {
	return {
		status: 200,
		headers: { 'Content-Type': 'application/json' },
		text: function() { return JSON.stringify({ message: 'hello' }); }
	};
}
`
	os.WriteFile(filepath.Join(apiDir, "test.js"), []byte(routeCode), 0644)

	rt := NewAPIRouteRuntime(apiDir)
	result := rt.Execute(APIRequest{
		URL:    "http://localhost:3000/api/test",
		Method: "GET",
		Path:   "/api/test",
		Headers: map[string]string{},
	})

	if result.Status != 200 {
		t.Errorf("Expected 200, got %d", result.Status)
	}
	if result.Body != `{"message":"hello"}` {
		t.Errorf("Expected {message:hello}, got %s", result.Body)
	}
}

func TestAPIRouteNotFound(t *testing.T) {
	tmpDir := t.TempDir()
	apiDir := filepath.Join(tmpDir, "api")
	os.MkdirAll(apiDir, 0755)

	rt := NewAPIRouteRuntime(apiDir)
	result := rt.Execute(APIRequest{
		URL:    "http://localhost:3000/api/nonexistent",
		Method: "GET",
		Path:   "/api/nonexistent",
	})

	if result.Status != 404 {
		t.Errorf("Expected 404, got %d", result.Status)
	}
}

func TestAPIRoutePathTraversalRejected(t *testing.T) {
	tmpDir := t.TempDir()
	apiDir := filepath.Join(tmpDir, "api")
	os.MkdirAll(apiDir, 0755)

	// A legit route inside the API dir.
	os.WriteFile(filepath.Join(apiDir, "secret.js"), []byte("function GET() { return 'secret'; }"), 0644)
	// A decoy outside the API dir that must never be reached.
	os.WriteFile(filepath.Join(tmpDir, "outside.js"), []byte("function GET() { return 'outside'; }"), 0644)

	rt := NewAPIRouteRuntime(apiDir)

	// The real route still resolves.
	ok := rt.Execute(APIRequest{Method: "GET", Path: "/api/secret"})
	if ok.Status != 200 {
		t.Fatalf("expected legit route to resolve, got %d", ok.Status)
	}

	for _, path := range []string{
		"/api/../secret",          // traverse up a level
		"/api/..%2F..%2Fsecret",   // encoded traversal (decoded by resolver)
		"/api/..%2fsecret",        // lowercase encoded traversal
		"/api/%2e%2e/secret",      // dotted-encoded segments
		"/api/../outside",         // escape to the decoy
		"/api/..\\outside",        // backslash separator (Windows)
		"/api/x/../../secret",     // deep traversal back in
	} {
		result := rt.Execute(APIRequest{Method: "GET", Path: path})
		if result.Status != 404 {
			t.Errorf("path %q: expected 404 (rejected), got %d", path, result.Status)
		}
		if result.Body != `{"error":"API Route Not Found"}` {
			t.Errorf("path %q: expected not-found body, got %q", path, result.Body)
		}
	}
}

func TestAPIRouteMethodDispatch(t *testing.T) {
	tmpDir := t.TempDir()
	apiDir := filepath.Join(tmpDir, "api")
	os.MkdirAll(apiDir, 0755)

	routeCode := `
function GET(request) {
	return { status: 200, text: function() { return 'GET'; } };
}
function POST(request) {
	return { status: 201, text: function() { return 'POST'; } };
}
`
	os.WriteFile(filepath.Join(apiDir, "methods.js"), []byte(routeCode), 0644)

	rt := NewAPIRouteRuntime(apiDir)

	// Test GET
	result := rt.Execute(APIRequest{
		Method: "GET",
		Path:   "/api/methods",
	})
	if result.Status != 200 || result.Body != "GET" {
		t.Errorf("GET: expected 200/GET, got %d/%s", result.Status, result.Body)
	}

	// Test POST
	result = rt.Execute(APIRequest{
		Method: "POST",
		Path:   "/api/methods",
	})
	if result.Status != 201 || result.Body != "POST" {
		t.Errorf("POST: expected 201/POST, got %d/%s", result.Status, result.Body)
	}
}

func TestAPIRouteDefaultExport(t *testing.T) {
	tmpDir := t.TempDir()
	apiDir := filepath.Join(tmpDir, "api")
	os.MkdirAll(apiDir, 0755)

	// Simulate esbuild ESM output: export { handler as default }
	routeCode := `
function handler(request) {
	return { status: 200, text: function() { return 'default'; } };
}
export { handler as default };
`
	os.WriteFile(filepath.Join(apiDir, "default.js"), []byte(routeCode), 0644)

	rt := NewAPIRouteRuntime(apiDir)
	result := rt.Execute(APIRequest{
		Method: "GET",
		Path:   "/api/default",
	})

	if result.Status != 200 || result.Body != "default" {
		t.Errorf("Expected 200/default, got %d/%s", result.Status, result.Body)
	}
}

func TestAPIRouteJSONReturn(t *testing.T) {
	tmpDir := t.TempDir()
	apiDir := filepath.Join(tmpDir, "api")
	os.MkdirAll(apiDir, 0755)

	routeCode := `
function GET(request) {
	return { name: "test", value: 42 };
}
`
	os.WriteFile(filepath.Join(apiDir, "json.js"), []byte(routeCode), 0644)

	rt := NewAPIRouteRuntime(apiDir)
	result := rt.Execute(APIRequest{
		Method: "GET",
		Path:   "/api/json",
	})

	if result.Status != 200 {
		t.Errorf("Expected 200, got %d", result.Status)
	}
	// JSON return should be auto-stringified
	if result.Body != `{"name":"test","value":42}` {
		t.Errorf("Expected JSON body, got %s", result.Body)
	}
}

func TestAPIRouteError(t *testing.T) {
	tmpDir := t.TempDir()
	apiDir := filepath.Join(tmpDir, "api")
	os.MkdirAll(apiDir, 0755)

	routeCode := `
function GET(request) {
	throw new Error('handler crashed');
}
`
	os.WriteFile(filepath.Join(apiDir, "error.js"), []byte(routeCode), 0644)

	rt := NewAPIRouteRuntime(apiDir)
	result := rt.Execute(APIRequest{
		Method: "GET",
		Path:   "/api/error",
	})

	if result.Status != 500 {
		t.Errorf("Expected 500, got %d", result.Status)
	}
	if result.Error == "" {
		t.Error("Expected error message")
	}
}

func TestAPIRouteLegacyHandler(t *testing.T) {
	tmpDir := t.TempDir()
	apiDir := filepath.Join(tmpDir, "api")
	os.MkdirAll(apiDir, 0755)

	routeCode := `
function handler(req, res) {
	res.statusCode = 200;
	res.setHeader('Content-Type', 'application/json');
	res.end(JSON.stringify({ message: 'Hello' }));
}
export { handler as default };
`
	os.WriteFile(filepath.Join(apiDir, "legacy.js"), []byte(routeCode), 0644)

	rt := NewAPIRouteRuntime(apiDir)
	result := rt.Execute(APIRequest{
		Method: "GET",
		Path:   "/api/legacy",
	})

	if result.Status != 200 {
		t.Errorf("Expected 200, got %d", result.Status)
	}
	if result.Body != `{"message":"Hello"}` {
		t.Errorf("Expected {message:Hello}, got %s", result.Body)
	}
	if result.Headers["content-type"] != "application/json" {
		t.Errorf("Expected content-type header, got %v", result.Headers)
	}
}

func TestAPIRouteEsbuildMinified(t *testing.T) {
	tmpDir := t.TempDir()
	apiDir := filepath.Join(tmpDir, "api")
	os.MkdirAll(apiDir, 0755)

	// This is exactly what esbuild produces for videos.ts with minification
	routeCode := `var n=[{id:"abc123",title:"Intro to Krate",duration:300},{id:"demo-42",title:"Advanced Patterns",duration:600}];function s(o){let e=new URL(o.url).searchParams.get("id");if(e){let i=n.find(r=>r.id===e);return i?Response.json(i):Response.json({error:"Video not found"},{status:404})}return Response.json({videos:n,count:n.length})}async function d(o){let t=await o.json();if(!t.title||!t.duration)return Response.json({error:"title and duration are required"},{status:400});let e={id:"vid-"+Date.now(),title:t.title,duration:t.duration};return n.push(e),Response.json(e,{status:201})}export{s as GET,d as POST};`
	os.WriteFile(filepath.Join(apiDir, "videos.js"), []byte(routeCode), 0644)

	rt := NewAPIRouteRuntime(apiDir)

	// Test GET /api/videos
	result := rt.Execute(APIRequest{
		Method:  "GET",
		Path:    "/api/videos",
		Headers: map[string]string{},
	})
	t.Logf("GET /api/videos: status=%d body=%s error=%s", result.Status, result.Body, result.Error)
	if result.Status != 200 {
		t.Errorf("Expected 200, got %d", result.Status)
	}

	// Test GET /api/videos?id=demo-42
	result = rt.Execute(APIRequest{
		URL:     "http://localhost:3000/api/videos?id=demo-42",
		Method:  "GET",
		Path:    "/api/videos",
		Headers: map[string]string{},
	})
	t.Logf("GET /api/videos?id=demo-42: status=%d body=%s", result.Status, result.Body)
	if result.Status != 200 {
		t.Errorf("Expected 200, got %d", result.Status)
	}

	// Test GET /api/videos?id=nonexistent
	result = rt.Execute(APIRequest{
		URL:     "http://localhost:3000/api/videos?id=nonexistent",
		Method:  "GET",
		Path:    "/api/videos",
		Headers: map[string]string{},
	})
	t.Logf("GET /api/videos?id=nonexistent: status=%d body=%s", result.Status, result.Body)
	if result.Status != 404 {
		t.Errorf("Expected 404, got %d", result.Status)
	}

	// Test POST /api/videos (async function)
	result = rt.Execute(APIRequest{
		Method:  "POST",
		Path:    "/api/videos",
		Headers: map[string]string{"content-type": "application/json"},
		Body:    `{"title":"New Video","duration":120}`,
	})
	t.Logf("POST /api/videos: status=%d body=%s error=%s", result.Status, result.Body, result.Error)
	if result.Status != 201 {
		t.Errorf("Expected 201, got %d", result.Status)
	}

	// Test POST with missing fields → 400
	result = rt.Execute(APIRequest{
		Method:  "POST",
		Path:    "/api/videos",
		Headers: map[string]string{"content-type": "application/json"},
		Body:    `{"title":"Missing duration"}`,
	})
	t.Logf("POST /api/videos (bad): status=%d body=%s", result.Status, result.Body)
	if result.Status != 400 {
		t.Errorf("Expected 400, got %d", result.Status)
	}
}
