package plugin

import "testing"

// TestMergeServeResponse verifies the ServeResponse chain merge only overrides
// fields the plugin actually returned, so a no-op result (no status, no
// headers, no body) preserves the existing response body.
func TestMergeServeResponse(t *testing.T) {
	out := ServeResponseResult{Status: 200, Headers: map[string]string{"content-type": "text/html"}, Body: "<h1>hello</h1>"}

	mergeServeResponse(&out, ServeResponseResult{})
	if out.Body != "<h1>hello</h1>" || out.Status != 200 {
		t.Errorf("zero result changed response: %+v", out)
	}

	mergeServeResponse(&out, ServeResponseResult{Headers: map[string]string{"x-injected": "1"}})
	if out.Body != "<h1>hello</h1>" {
		t.Errorf("headers-only result wiped body: %q", out.Body)
	}
	if out.Headers["x-injected"] != "1" {
		t.Errorf("headers-only result did not apply header: %+v", out.Headers)
	}

	mergeServeResponse(&out, ServeResponseResult{Status: 201, Body: "<p>rewritten</p>"})
	if out.Status != 201 || out.Body != "<p>rewritten</p>" || out.Headers["x-injected"] != "1" {
		t.Errorf("partial result not merged correctly: %+v", out)
	}
}
