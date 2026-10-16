package css

import (
	"strings"
	"testing"
)

// TestCascadeOrderBaseBeforeVariants verifies base utilities are emitted before
// media/pseudo variants so later utilities win (the previous sort emitted every
// @media rule first, inverting responsive and dark-mode overrides).
func TestCascadeOrderBaseBeforeVariants(t *testing.T) {
	out := gen(t, "text-sm", "sm:text-lg")
	iBase := strings.Index(out, ".text-sm{")
	iSm := strings.Index(out, "@media (min-width: 640px)")
	if iBase < 0 || iSm < 0 {
		t.Fatalf("expected base and responsive rules:\n%s", out)
	}
	if iBase > iSm {
		t.Errorf("base rule must come before responsive rule:\n%s", out)
	}
}

func TestCascadeOrderResponsiveAscending(t *testing.T) {
	out := gen(t, "lg:flex", "sm:flex", "md:flex")
	iSm := strings.Index(out, "min-width: 640px")
	iMd := strings.Index(out, "min-width: 768px")
	iLg := strings.Index(out, "min-width: 1024px")
	if iSm < 0 || iMd < 0 || iLg < 0 {
		t.Fatalf("missing breakpoints:\n%s", out)
	}
	if !(iSm < iMd && iMd < iLg) {
		t.Errorf("breakpoints must be ascending (sm<md<lg):\n%s", out)
	}
}

func TestCascadeOrderDarkAfterBase(t *testing.T) {
	out := gen(t, "bg-white", "dark:bg-black")
	iBase := strings.Index(out, ".bg-white{")
	iDark := strings.Index(out, "prefers-color-scheme")
	if iBase < 0 || iDark < 0 {
		t.Fatalf("expected base and dark rules:\n%s", out)
	}
	if iBase > iDark {
		t.Errorf("base rule must come before dark rule:\n%s", out)
	}
}

func TestCascadeOrderPseudoAfterBase(t *testing.T) {
	out := gen(t, "bg-red-500", "hover:bg-blue-500")
	iBase := strings.Index(out, ".bg-red-500{")
	iHover := strings.Index(out, ":hover")
	if iBase < 0 || iHover < 0 {
		t.Fatalf("expected base and hover rules:\n%s", out)
	}
	if iBase > iHover {
		t.Errorf("base rule must come before hover rule:\n%s", out)
	}
}

// TestPeerVariantUsesSiblingCombinator verifies peer-* uses `~`, not a
// descendant combinator.
func TestPeerVariantUsesSiblingCombinator(t *testing.T) {
	out := gen(t, "peer-checked:block")
	if !strings.Contains(out, ".peer:checked ~ ") {
		t.Errorf("peer variant must use the general-sibling combinator:\n%s", out)
	}
}

func TestGroupVariantUsesDescendant(t *testing.T) {
	out := gen(t, "group-hover:flex")
	if !strings.Contains(out, ".group:hover ") {
		t.Errorf("group variant must use a descendant combinator:\n%s", out)
	}
}

func TestGroupAriaUsesAttributeSelector(t *testing.T) {
	out := gen(t, "group-aria-checked:block")
	if !strings.Contains(out, "[aria-checked='true']") {
		t.Errorf("group-aria must emit an attribute selector:\n%s", out)
	}
	if strings.Contains(out, ":aria-checked") {
		t.Errorf("group-aria must not emit an invalid :aria-* pseudo-class:\n%s", out)
	}
}

// TestFontFamilyUtilitiesEmit verifies font-sans/serif/mono are no longer
// dropped because their stacks contain quotes.
func TestFontFamilyUtilitiesEmit(t *testing.T) {
	for _, cls := range []string{"font-sans", "font-serif", "font-mono"} {
		out := gen(t, cls)
		if !strings.Contains(out, "font-family:") {
			t.Errorf("%s should emit a font-family declaration:\n%s", cls, out)
		}
	}
}
