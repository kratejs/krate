package jsruntime

import (
	"strings"
	"testing"
)

// TestEvaluateExprBareGlobals checks the compile-time evaluator's VM has no
// web API that reaches the network, randomness or request state, and no
// loader that could install one.
func TestEvaluateExprBareGlobals(t *testing.T) {
	for _, name := range []string{
		"fetch", "__go_fetch", "crypto", "__go_random", "__go_digest", "__go_hmac", "__go_url_parse",
		"__go_body_text", "__go_body_b64", "__go_body_multipart", "__go_parse_multipart", "_go_readFile",
		"__krate_load_feature", "process", "require", "setTimeout", "queueMicrotask", "performance",
		"URL", "Request", "Response", "Headers", "TextEncoder", "atob", "Blob", "ReadableStream",
		"AbortController", "DOMException", "Intl",
	} {
		got, err := EvaluateExpr("typeof " + name)
		if err != nil {
			t.Fatalf("typeof %s: %v", name, err)
		}
		if got != "undefined" {
			t.Errorf("typeof %s = %q in the build-time VM, want undefined", name, got)
		}
	}
	for expr, want := range map[string]string{
		`typeof structuredClone`: "function",
		`typeof console.log`:     "function",
		`JSON.stringify(structuredClone({ a: [1, new Map([[1, 2]]).size] }))`:                                                           `{"a":[1,1]}`,
		`(function () { try { structuredClone(() => 1); return "no error" } catch (e) { return e.name } })()`:                           "DataCloneError",
		`(function () { try { globalThis[Symbol.for("krate.web")].need("fetch"); return "loaded" } catch (e) { return "refused" } })()`: "refused",
		`Object.keys(globalThis).join()`: "",
		`[3, 1, 2].sort().join("-") + " " + "abc".toUpperCase() + " " + (255).toString(16)`: "1-2-3 ABC ff",
		`new Date(0).toISOString()`:          "1970-01-01T00:00:00.000Z",
		`JSON.stringify({ d: new Date(0) })`: `{"d":"1970-01-01T00:00:00.000Z"}`,
	} {
		got, err := EvaluateExpr(expr)
		if err != nil {
			t.Errorf("%s: %v", expr, err)
			continue
		}
		if got != want {
			t.Errorf("%s = %q, want %q", expr, got, want)
		}
	}
}

// TestEvaluateExprLocaleBuiltinsThrow checks the locale- and time-zone-
// dependent built-ins throw in the build-time VM, so their result is never
// folded into the output.
func TestEvaluateExprLocaleBuiltinsThrow(t *testing.T) {
	for _, expr := range []string{
		`"a".localeCompare("b")`,
		`"i".toLocaleUpperCase("tr")`,
		`"I".toLocaleLowerCase()`,
		`(1234.5).toLocaleString()`,
		`(12n).toLocaleString()`,
		`[1, 2].toLocaleString()`,
		`new Uint8Array(2).toLocaleString()`,
		`new Date(0).toLocaleString()`,
		`new Date(0).toLocaleDateString("de-DE")`,
		`new Date(0).toLocaleTimeString()`,
		`new Date(0).toString()`,
		`new Date(0).toDateString()`,
		`new Date(0).toTimeString()`,
		`String(new Date(0))`,
		"`${new Date(0)}`",
		`new Date(0) + ""`,
		`({ toString: Date.prototype.toString }).toString()`,
	} {
		_, err := EvaluateExpr(expr)
		if err == nil || !strings.Contains(err.Error(), "depends on the locale or time zone") {
			t.Errorf("%s: err = %v, want the locale stub to throw", expr, err)
		}
	}
	// The full runtime keeps the real built-ins.
	rt, err := New()
	if err != nil {
		t.Fatal(err)
	}
	defer rt.Close()
	if v, err := rt.Execute(`"a".localeCompare("b") < 0 && typeof new Date(0).toString() === "string"`); err != nil || v != true {
		t.Errorf("full runtime locale built-ins: %v %v", v, err)
	}
}

// TestNewBareHasNoNodeGlobals checks NewBare directly (EvaluateExpr wraps it
// in a strict IIFE).
func TestNewBareHasNoNodeGlobals(t *testing.T) {
	rt, err := NewBare()
	if err != nil {
		t.Fatal(err)
	}
	defer rt.Close()
	v, err := rt.Execute(`[typeof process, typeof require, typeof __require_fs].join()`)
	if err != nil || v != "undefined,undefined,undefined" {
		t.Errorf("got %v %v", v, err)
	}
	if got := rt.LoadedFeatures(); strings.Join(got, ",") != "clone,console" {
		t.Errorf("bare VM features %v", got)
	}
}
