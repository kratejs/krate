package jsruntime

import (
	"testing"
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

	rt := mustRuntime(t, Options{})
	// The load may still fail to evaluate here; only the compile must not.
	_, _ = rt.Execute(`(function(){
	  var hog = [];
	  try { for (;;) hog.push(new Array(4096).fill(1.5)); } catch (e) {}
	  try { for (;;) hog.push(new Array(64).fill(1.5)); } catch (e) {}
	  hog.length = hog.length - 4;
	  try { void TextEncoder; } catch (e) {}
	})()`)

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
