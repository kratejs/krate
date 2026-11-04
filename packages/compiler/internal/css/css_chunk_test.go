package css

import (
	"strings"
	"testing"
)

func TestSplitCSSRulesMediaKeptWhole(t *testing.T) {
	page := "@media (min-width: 640px){.a{color:red}}.b{color:blue}"
	rules := SplitCSSRules(page)
	if len(rules) != 2 {
		t.Fatalf("expected 2 rules, got %d: %v", len(rules), rules)
	}
	if !strings.HasPrefix(rules[0], "@media") || !strings.Contains(rules[0], ".a{color:red}") {
		t.Errorf("media rule split incorrectly: %v", rules)
	}
}

func TestSplitCSSRulesIgnoresBracesInStrings(t *testing.T) {
	page := `.a{content:"}"}` + `.b{color:red}`
	rules := SplitCSSRules(page)
	if len(rules) != 2 {
		t.Fatalf("brace inside string split a rule: %v", rules)
	}
}

func TestSplitCSSRulesSkipsEscapes(t *testing.T) {
	rules := SplitCSSRules(`.a\{{color:red}.b{color:blue}`)
	if len(rules) != 2 || rules[0] != `.a\{{color:red}` || rules[1] != `.b{color:blue}` {
		t.Fatalf("escaped brace mis-split: %q", rules)
	}
	rules = SplitCSSRules(`.c\'x{color:red}.d{color:blue}`)
	if len(rules) != 2 {
		t.Fatalf("escaped quote opened a string: %q", rules)
	}
}
