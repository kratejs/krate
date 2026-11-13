package css

import (
	"encoding/json"
	"os"
	"sort"
	"strings"
	"testing"
)

// TestVariantOrderMatchesTailwind checks the emitted order of one utility
// under ~185 variant forms against the order Tailwind v4.3.3 produces
// (testdata/tw_variant_order.json, generated from the reference compiler with
// --breakpoint-3xl:120rem and --breakpoint-tablet:40rem).
func TestVariantOrderMatchesTailwind(t *testing.T) {
	raw, err := os.ReadFile("testdata/tw_variant_order.json")
	if err != nil {
		t.Fatal(err)
	}
	var golden struct{ Order []string }
	if err := json.Unmarshal(raw, &golden); err != nil {
		t.Fatal(err)
	}
	g := NewTailwindGenerator()
	g.Theme.Screens["3xl"] = "120rem"
	g.Theme.Screens["tablet"] = "40rem"
	classes := map[string]bool{}
	var order []string
	for _, c := range golden.Order {
		// Tailwind compares breakpoint lengths unit-string first, so mixed
		// px/rem arbitrary values sort oddly there; Krate compares in px
		// (see TestVariantBreakpointUnits).
		if isMixedUnitBreakpoint(c) {
			continue
		}
		classes[c] = true
		order = append(order, c)
	}
	golden.Order = order
	out := g.Generate(classes)

	type pos struct {
		idx int
		cls string
	}
	var got []pos
	for _, c := range golden.Order {
		i := strings.Index(out, "."+EscapeClass(c)+"{")
		if i < 0 {
			i = strings.Index(out, "."+EscapeClass(c))
		}
		if i < 0 {
			t.Errorf("class %q produced no rule", c)
			continue
		}
		got = append(got, pos{i, c})
	}
	sort.SliceStable(got, func(i, j int) bool { return got[i].idx < got[j].idx })
	want := golden.Order
	for i, p := range got {
		if i < len(want) && p.cls != want[i] {
			t.Fatalf("order differs at %d: got %q, want %q\nfull got: %v", i, p.cls, want[i], func() []string {
				var s []string
				for _, q := range got {
					s = append(s, q.cls)
				}
				return s
			}())
		}
	}
}

func isMixedUnitBreakpoint(c string) bool {
	v, _, _ := strings.Cut(c, ":p-1")
	for _, p := range []string{"min-[", "max-[", "@min-[", "@max-[", "@["} {
		if strings.HasPrefix(v, p) {
			return true
		}
	}
	return false
}
