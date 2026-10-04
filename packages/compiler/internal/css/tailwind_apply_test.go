package css

import (
	"strings"
	"testing"
)

func TestExpandApplyPlainUtilities(t *testing.T) {
	theme := NewTailwindGenerator().Theme
	out, unresolved := ExpandApply(".card {\n  @apply p-4 font-bold;\n}", theme)
	if len(unresolved) != 0 {
		t.Fatalf("unexpected unresolved: %v", unresolved)
	}
	if strings.Contains(out, "@apply") {
		t.Fatalf("@apply was not removed:\n%s", out)
	}
	if !strings.Contains(out, "padding") {
		t.Errorf("p-4 declaration missing:\n%s", out)
	}
	if !strings.Contains(out, "font-weight") {
		t.Errorf("font-bold declaration missing:\n%s", out)
	}
}

func TestExpandApplyVariantUnresolved(t *testing.T) {
	theme := NewTailwindGenerator().Theme
	out, unresolved := ExpandApply(".btn {\n  @apply hover:bg-red-500;\n}", theme)
	if len(unresolved) != 1 || unresolved[0] != "hover:bg-red-500" {
		t.Fatalf("expected hover:bg-red-500 unresolved, got %v", unresolved)
	}
	if strings.Contains(out, "@apply") {
		t.Errorf("@apply line should be dropped even when unresolved:\n%s", out)
	}
}

func TestExpandApplyUnknownClass(t *testing.T) {
	theme := NewTailwindGenerator().Theme
	_, unresolved := ExpandApply(".x {\n  @apply zzz-nonexistent-utility;\n}", theme)
	if len(unresolved) != 1 {
		t.Fatalf("expected unknown class unresolved, got %v", unresolved)
	}
}
