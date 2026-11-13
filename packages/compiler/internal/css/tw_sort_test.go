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
