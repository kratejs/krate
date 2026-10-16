package css

import (
	"strings"
	"testing"
)

// Test prefixes mirror csssignals.CompatModernPrefix / CompatLegacyPrefix
// (kept local so this package does not import csssignals).
const (
	modernPrefix = "@supports selector(:has(*)){/*krate:modern*/"
	legacyPrefix = "@supports not selector(:has(*)){/*krate:legacy*/"
)

const hybridFixture = modernPrefix + `.a:has(.b:checked){color:red}.c{content:"}"}}` + "\n" +
	legacyPrefix + `.a.b-on{display:block}.d{content:"{"}}` + "\n" +
	`.tail{color:blue}`

func TestRemoveAtRuleBlock(t *testing.T) {
	got := RemoveAtRuleBlock(hybridFixture, legacyPrefix)
	if strings.Contains(got, "krate:legacy") {
		t.Errorf("legacy block not removed:\n%s", got)
	}
	if !strings.Contains(got, modernPrefix) || !strings.Contains(got, ".tail{color:blue}") {
		t.Errorf("modern block or tail lost:\n%s", got)
	}
	// The string literal brace in the modern block must not confuse matching.
	if !strings.Contains(got, `.c{content:"}"}}`) {
		t.Errorf("modern string literal damaged:\n%s", got)
	}
}

func TestUnwrapAtRuleBlock(t *testing.T) {
	got := UnwrapAtRuleBlock(RemoveAtRuleBlock(hybridFixture, modernPrefix), legacyPrefix)
	if strings.Contains(got, "krate:modern") || strings.Contains(got, "krate:legacy") {
		t.Errorf("sentinels should be gone:\n%s", got)
	}
	if !strings.Contains(got, ".a.b-on{display:block}") {
		t.Errorf("legacy rules not inlined:\n%s", got)
	}
	if !strings.Contains(got, ".tail{color:blue}") {
		t.Errorf("tail lost:\n%s", got)
	}
}

// TestAuthoredSupportsUntouched verifies an author's own (unsentinelled)
// @supports block is never transformed.
func TestAuthoredSupportsUntouched(t *testing.T) {
	authored := `@supports selector(:has(*)){.x:has(.y){color:red}}`
	if got := RemoveAtRuleBlock(authored, legacyPrefix); got != authored {
		t.Errorf("authored modern block removed: %q", got)
	}
	if got := UnwrapAtRuleBlock(authored, modernPrefix); got != authored {
		t.Errorf("authored modern block unwrapped: %q", got)
	}
	authoredLegacy := `@supports not selector(:has(*)){.x{color:red}}`
	if got := RemoveAtRuleBlock(authoredLegacy, legacyPrefix); got != authoredLegacy {
		t.Errorf("authored legacy block removed: %q", got)
	}
}

func TestUnwrapMissingPrefixIsNoop(t *testing.T) {
	in := ".x{color:red}"
	if got := RemoveAtRuleBlock(in, legacyPrefix); got != in {
		t.Errorf("RemoveAtRuleBlock changed input: %q", got)
	}
	if got := UnwrapAtRuleBlock(in, legacyPrefix); got != in {
		t.Errorf("UnwrapAtRuleBlock changed input: %q", got)
	}
}
