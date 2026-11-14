package jsruntime

import (
	"strings"
	"testing"
)

// Runaway recursion must throw inside the VM, not exhaust the Go stack.
func TestRunawayRecursionThrows(t *testing.T) {
	for _, kb := range []string{"", "32", "100000"} {
		t.Run("stack="+kb, func(t *testing.T) {
			if kb != "" {
				t.Setenv("KRATE_JS_STACK_KB", kb)
			}
			rt, err := New()
			if err != nil {
				t.Fatal(err)
			}
			defer rt.Close()
			for _, src := range []string{
				`(() => { let d = 0; function f() { d++; f() } try { f() } catch (e) { return "caught " + d } })()`,
				`(() => { function f() { return [1].map(f) } try { f() } catch (e) { return "caught" } })()`,
			} {
				v, err := rt.Execute(src)
				if err != nil {
					t.Fatalf("%s: %v", src, err)
				}
				if s, _ := v.(string); !strings.HasPrefix(s, "caught") {
					t.Fatalf("%s = %v, want a caught stack overflow", src, v)
				}
			}
		})
	}
}
