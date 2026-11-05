package css

import (
	"fmt"
	"sort"
	"strings"
)

var twPropertyIndex = func() map[string]int {
	m := make(map[string]int, len(twPropertyOrder))
	for i, p := range twPropertyOrder {
		m[p] = i
	}
	return m
}()

// ruleSortKey builds the string that orders rules like Tailwind v4: first by
// the variants used (the highest-ranked variant decides, then the next), then
// by the declared properties' position in Tailwind's property order, then by
// declaration count (more first), then by class name with numbers compared
// numerically. A plain byte comparison of the key gives that order.
func ruleSortKey(cls string, variantRanks []string, css string) string {
	uniq := map[string]bool{}
	var ranks []string
	for _, r := range variantRanks {
		if !uniq[r] {
			uniq[r] = true
			ranks = append(ranks, r)
		}
	}
	sort.Sort(sort.Reverse(sort.StringSlice(ranks)))

	var b strings.Builder
	for _, r := range ranks {
		b.WriteString(r)
		b.WriteByte(1)
	}
	// "!" sorts below every rank, so a rule with fewer variants comes first.
	b.WriteByte('!')

	idx, count := declProps(css)
	for _, i := range idx {
		fmt.Fprintf(&b, "%03d", i)
	}
	// A property list that ends first sorts after the longer one.
	b.WriteString("999")
	fmt.Fprintf(&b, "%03d", 999-count)
	b.WriteString(naturalKey(cls))
	return b.String()
}

// declProps returns the sorted property-order indexes of the declarations in
// a block and the declaration count.
func declProps(css string) ([]int, int) {
	seen := map[int]bool{}
	var idx []int
	count := 0
	for _, d := range splitDecls(css) {
		if _, _, nested := nestedAtRule(d); nested || strings.Contains(d, "{") {
			continue
		}
		name, val, ok := strings.Cut(d, ":")
		if !ok || strings.TrimSpace(val) == "" {
			continue
		}
		count++
		if i, ok := twPropertyIndex[strings.TrimSpace(name)]; ok && !seen[i] {
			seen[i] = true
			idx = append(idx, i)
		}
	}
	sort.Ints(idx)
	return idx, count
}

// naturalKey pads digit runs so a byte comparison orders numbers numerically.
func naturalKey(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); {
		if s[i] < '0' || s[i] > '9' {
			b.WriteByte(s[i])
			i++
			continue
		}
		j := i
		for j < len(s) && s[j] >= '0' && s[j] <= '9' {
			j++
		}
		run := strings.TrimLeft(s[i:j], "0")
		b.WriteString(strings.Repeat("0", max(0, 20-len(run))))
		b.WriteString(run)
		i = j
	}
	return b.String()
}
