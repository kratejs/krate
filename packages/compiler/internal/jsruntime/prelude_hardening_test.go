package jsruntime

import (
	"strings"
	"testing"
	"time"
)

// TestModuleCompileIsolatedFromRequestVM checks that a request VM at its
// memory limit cannot make a feature's first compile fail, and that the feature
// still works in VMs created afterwards (including PreloadAll ones).
func TestModuleCompileIsolatedFromRequestVM(t *testing.T) {
	s, err := prelude()
	if err != nil {
		t.Fatal(err)
	}
	m := s.mods["encoding"]
	m.bc.mu.Lock()
	m.bc.bc = nil
	m.bc.mu.Unlock()

	// Starve the VM from Go: an out-of-memory failure inside a host function
	// call aborts modernc QuickJS under checkptr, so this skips the JS loader.
	rt := mustRuntime(t, Options{})
	rt.vm.SetMemoryLimit(1)
	_ = rt.loadFeature("encoding")
	rt.vm.SetMemoryLimit(rt.memLimit)
	m.bc.mu.Lock()
	compiled := m.bc.bc != nil
	m.bc.mu.Unlock()
	if !compiled {
		t.Error("the starved VM's load did not compile the module")
	}

	fresh := mustRuntime(t, Options{})
	if got := execString(t, fresh, `typeof TextEncoder`); got != "function" {
		t.Errorf("fresh VM after a starved one: typeof TextEncoder = %s", got)
	}
	all := mustRuntime(t, Options{PreloadAll: true})
	if got := execString(t, all, `new TextEncoder().encode("ab").length + ""`); got != "2" {
		t.Errorf("PreloadAll VM: %s", got)
	}
}

func TestSharedBytecodeDoesNotCacheFailure(t *testing.T) {
	var c sharedBytecode
	if _, err := c.get("("); err == nil {
		t.Fatal("compiling a syntax error succeeded")
	}
	if c.bc != nil {
		t.Fatal("failed compile left bytecode behind")
	}
	if bc, err := c.get("1"); err != nil || len(bc) == 0 {
		t.Fatalf("retry: %v", err)
	}
}

// TestLazyLoadsKeepDeadline checks that loading features from a script does not
// push its timeout back: each load used to re-arm the deadline to now+timeout.
func TestLazyLoadsKeepDeadline(t *testing.T) {
	t.Setenv("KRATE_JS_TIMEOUT", "300ms")
	rt := mustRuntime(t, Options{})
	start := time.Now()
	_, err := rt.Execute(`(function(){
	  function spin(ms){ var e = Date.now()+ms; while(Date.now()<e){} }
	  var names = ['URL','TextEncoder','Headers','ReadableStream','Blob','crypto','structuredClone','setTimeout','console','Event'];
	  for (var i=0;i<names.length;i++){ spin(200); void globalThis[names[i]]; }
	})()`)
	elapsed := time.Since(start)
	if err == nil || !strings.Contains(err.Error(), "interrupted") {
		t.Fatalf("err = %v, want an interrupt", err)
	}
	if elapsed > 1200*time.Millisecond {
		t.Errorf("interrupted after %v with a 300ms timeout", elapsed)
	}
	// The configured timeout is back for the next evaluation.
	if _, err := rt.Execute(`(function(){ var e = Date.now()+100; while(Date.now()<e){} })()`); err != nil {
		t.Errorf("next evaluation: %v", err)
	}
}

// TestFailedLazyLoadStaysBounded checks that a load that keeps failing, retried
// in a loop, cannot keep the script alive past its timeout.
func TestFailedLazyLoadStaysBounded(t *testing.T) {
	t.Setenv("KRATE_JS_TIMEOUT", "300ms")
	rt := mustRuntime(t, Options{})
	// Breaking Symbol.for makes every evaluation of a feature module throw.
	got := execString(t, rt, `Symbol.for = function () { return "broken"; };
	  (function(){ try { void AbortController; return "loaded"; } catch (e) { return e.message; } })()`)
	if !strings.Contains(got, "loading the events web APIs failed") {
		t.Fatalf("load did not fail: %s", got)
	}
	start := time.Now()
	_, err := rt.Execute(`(function(){
	  var end = Date.now() + 3000;
	  while (Date.now() < end) { try { void AbortController; } catch (e) {} }
	})()`)
	elapsed := time.Since(start)
	if err == nil || !strings.Contains(err.Error(), "interrupted") {
		t.Fatalf("err = %v, want an interrupt", err)
	}
	if elapsed > 1200*time.Millisecond {
		t.Errorf("interrupted after %v with a 300ms timeout", elapsed)
	}
}

// TestLazyGlobalsOnFrozenGlobal checks that features still load when user code
// froze or sealed the global object before using them, as with PreloadAll.
func TestLazyGlobalsOnFrozenGlobal(t *testing.T) {
	probe := `(function(){ try {
	  return [typeof fetch, typeof URL, new URL("http://a/b?c").pathname, fetch === fetch,
	    typeof console.log, new Response("x").status, typeof AbortController].join();
	} catch (e) { return "threw: " + e.message; } })()`
	want := "function,function,/b,true,function,200,function"
	for _, lock := range []string{"Object.freeze", "Object.seal", "Object.preventExtensions"} {
		for _, o := range []Options{{}, {PreloadAll: true}} {
			rt := mustRuntime(t, o)
			if got := execString(t, rt, lock+"(globalThis); "+probe); got != want {
				t.Errorf("%s, PreloadAll=%v: %s, want %s", lock, o.PreloadAll, got, want)
			}
		}
	}
}

// TestFeatureModuleRollsBack checks that a module failing partway leaves no
// globals behind, so the retry in the next evaluation installs one consistent
// set. Within the failing evaluation the error is rethrown without a retry.
func TestFeatureModuleRollsBack(t *testing.T) {
	rt := mustRuntime(t, Options{})
	got := execString(t, rt, `(function(){
	  var gopd = Object.getOwnPropertyDescriptor, armed = true;
	  Object.getOwnPropertyDescriptor = function (o, n) {
	    if (armed && o === globalThis && n === "AbortController") { armed = false; throw new Error("injected"); }
	    return gopd(o, n);
	  };
	  var first, again;
	  try { void Event; first = "loaded"; } catch (e) { first = e.message; }
	  Object.getOwnPropertyDescriptor = gopd;
	  try { void Event; again = "loaded"; } catch (e) { again = e.message; }
	  var rolledBack = ["DOMException", "Event", "EventTarget", "AbortSignal"].every(function (n) {
	    var d = gopd(globalThis, n); return !!d && typeof d.get === "function";
	  });
	  return [first.indexOf("injected") >= 0, again === first, rolledBack].join();
	})()`)
	if got != "true,true,true" {
		t.Errorf("failing evaluation: %s", got)
	}
	got = execString(t, rt, `(function(){
	  var s = new AbortController().signal;
	  return [s instanceof EventTarget, s instanceof AbortSignal, Object.getPrototypeOf(AbortSignal) === EventTarget].join();
	})()`)
	if got != "true,true,true" {
		t.Errorf("next evaluation: %s", got)
	}
}
