package css

import (
	"strings"
	"testing"
)

func TestChunkCSSSharedAndPerPage(t *testing.T) {
	a := ".shared{color:red}\n.only-a{color:blue}"
	b := ".shared{color:red}\n.only-b{color:green}"
	shared, per := ChunkCSS([]string{a, b})

	if !strings.Contains(shared, ".shared{color:red}") {
		t.Errorf("shared rule missing from shared chunk: %s", shared)
	}
	if strings.Contains(shared, ".only-a") || strings.Contains(shared, ".only-b") {
		t.Errorf("page-only rules leaked into shared chunk: %s", shared)
	}
	if !strings.Contains(per[0], ".only-a") || strings.Contains(per[0], ".shared") {
		t.Errorf("page A remainder wrong: %s", per[0])
	}
	if !strings.Contains(per[1], ".only-b") || strings.Contains(per[1], ".shared") {
		t.Errorf("page B remainder wrong: %s", per[1])
	}
}

func TestChunkCSSMediaKeptWhole(t *testing.T) {
	page := "@media (min-width: 640px){.a{color:red}}.b{color:blue}"
	rules := SplitCSSRules(page)
	if len(rules) != 2 {
		t.Fatalf("expected 2 rules, got %d: %v", len(rules), rules)
	}
	if !strings.HasPrefix(rules[0], "@media") || !strings.Contains(rules[0], ".a{color:red}") {
		t.Errorf("media rule split incorrectly: %v", rules)
	}
}

func TestChunkCSSIgnoresBracesInStrings(t *testing.T) {
	page := `.a{content:"}"}` + `.b{color:red}`
	rules := SplitCSSRules(page)
	if len(rules) != 2 {
		t.Fatalf("brace inside string split a rule: %v", rules)
	}
}
