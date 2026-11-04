package css

import (
	"encoding/json"
	"os"
	"sort"
	"strings"
	"testing"
)

// indexOfRule returns the offset of the first selector that starts with the
// escaped class, or -1.
func indexOfRule(out, cls string) int {
	needle := "." + EscapeClass(cls)
	for from := 0; ; {
		i := strings.Index(out[from:], needle)
		if i < 0 {
			return -1
		}
		i += from
		end := i + len(needle)
		if end >= len(out) || !strings.ContainsRune("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789_-\\", rune(out[end])) {
			return i
		}
		from = end
	}
}

// TestUtilityOrderMatchesTailwind compares the emitted order of ~100 common
// utilities with Tailwind v4.3.3 (testdata/tw_util_order.json).
func TestUtilityOrderMatchesTailwind(t *testing.T) {
	raw, err := os.ReadFile("testdata/tw_util_order.json")
	if err != nil {
		t.Fatal(err)
	}
	var golden struct{ Order []string }
	if err := json.Unmarshal(raw, &golden); err != nil {
		t.Fatal(err)
	}
	classes := map[string]bool{}
	for _, c := range golden.Order {
		classes[c] = true
	}
	out := NewTailwindGenerator().Generate(classes)
	type pos struct {
		idx int
		cls string
	}
	var got []pos
	for _, c := range golden.Order {
		i := indexOfRule(out, c)
		if i < 0 {
			t.Logf("no rule for %q", c)
			continue
		}
		got = append(got, pos{i, c})
	}
	sort.Slice(got, func(i, j int) bool { return got[i].idx < got[j].idx })
	rank := map[string]int{}
	for i, c := range golden.Order {
		rank[c] = i
	}
	for i := 1; i < len(got); i++ {
		if rank[got[i-1].cls] > rank[got[i].cls] {
			t.Errorf("%q is emitted before %q; Tailwind emits it after", got[i-1].cls, got[i].cls)
		}
	}
}

// TestUtilityOrderCascade covers the shorthand-before-longhand pairs that
// used to sort alphabetically, so the longhand must be emitted later.
func TestUtilityOrderCascade(t *testing.T) {
	pairs := [][2]string{
		{"px-2", "pl-4"}, {"py-2", "pt-4"}, {"my-2", "mt-4"}, {"mx-4", "ml-2"},
		{"size-4", "h-8"}, {"border-x-2", "border-l-4"}, {"p-4", "px-2"},
		{"m-4", "mx-2"}, {"rounded-lg", "rounded-t-md"}, {"gap-4", "gap-x-2"},
		{"inset-0", "top-2"}, {"overflow-hidden", "overflow-x-auto"},
		{"transition", "duration-300"}, {"transition", "ease-in"},
	}
	for _, p := range pairs {
		out := gen(t, p[1], p[0])
		a, b := indexOfRule(out, p[0]), indexOfRule(out, p[1])
		if a < 0 || b < 0 {
			t.Errorf("%v: missing rule in\n%s", p, out)
			continue
		}
		if a > b {
			t.Errorf("%q must be emitted before %q:\n%s", p[0], p[1], out)
		}
	}
}

func TestUtilityOrderNumeric(t *testing.T) {
	out := gen(t, "p-10", "p-2", "p-1")
	i1, i2, i10 := indexOfRule(out, "p-1"), indexOfRule(out, "p-2"), indexOfRule(out, "p-10")
	if !(i1 < i2 && i2 < i10) {
		t.Errorf("p-1, p-2, p-10 must sort numerically:\n%s", out)
	}
}
