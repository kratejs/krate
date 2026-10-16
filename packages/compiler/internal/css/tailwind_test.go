package css

import (
	"strings"
	"testing"
)

// gen is a helper that generates CSS for a single class (or space-separated
// class list) and returns the raw output.
func gen(t *testing.T, classes ...string) string {
	t.Helper()
	set := map[string]bool{}
	for _, c := range classes {
		for _, f := range strings.Fields(c) {
			set[f] = true
		}
	}
	return NewTailwindGenerator().Generate(set)
}

func TestVariantsEmitSelectors(t *testing.T) {
	tests := []struct {
		cls  string
		want string
	}{
		{"hover:bg-red-500", ".hover\\:bg-red-500:hover{background-color: #ef4444;}"},
		{"focus:ring-2", ".focus\\:ring-2:focus"},
		{"active:opacity-50", ".active\\:opacity-50:active"},
		{"disabled:opacity-50", ".disabled\\:opacity-50:disabled"},
		{"before:block", ".before\\:block::before"},
		{"after:content-none", ".after\\:content-none::after"},
		{"group-hover:underline", ".group:hover .group-hover\\:underline"},
		{"odd:bg-white", ".odd\\:bg-white:nth-child(odd)"},
		{"first:mt-0", ".first\\:mt-0:first-child"},
	}
	for _, tt := range tests {
		got := gen(t, tt.cls)
		if !strings.Contains(got, tt.want) {
			t.Errorf("%s: want %q in output, got:\n%s", tt.cls, tt.want, got)
		}
		// The bug was emitting the bare class unconditionally.
		if strings.Contains(got, "{"+strings.TrimPrefix(tt.want, ".")+"}"+":") {
			t.Errorf("%s: emitted unconditionally:\n%s", tt.cls, got)
		}
	}
}

func TestVariantStacking(t *testing.T) {
	got := gen(t, "sm:hover:bg-blue-500")
	// Must be a media query wrapping a :hover selector.
	if !strings.Contains(got, "@media (min-width: 640px)") {
		t.Errorf("missing sm media query:\n%s", got)
	}
	if !strings.Contains(got, ".sm\\:hover\\:bg-blue-500:hover") {
		t.Errorf("missing stacked selector:\n%s", got)
	}
}

func TestDarkModeVariants(t *testing.T) {
	// Default: media strategy.
	got := gen(t, "dark:bg-zinc-900")
	if !strings.Contains(got, "@media (prefers-color-scheme: dark)") {
		t.Errorf("dark: should use prefers-color-scheme by default:\n%s", got)
	}
	// Class strategy.
	g := NewTailwindGenerator()
	g.Theme.DarkMode = "class"
	out := g.Generate(map[string]bool{"dark:bg-zinc-900": true})
	if !strings.Contains(out, ".dark .dark\\:bg-zinc-900") {
		t.Errorf("dark:class should use ancestor selector:\n%s", out)
	}
}

func TestEscapeSelector(t *testing.T) {
	tests := map[string]string{
		"w-1/2":         "w-1\\/2",
		"w-[100px]":     "w-\\[100px\\]",
		"bg-red-500/50": "bg-red-500\\/50",
		"text-[#fff]":   "text-\\[\\#fff\\]",
		"p-1.5":         "p-1\\.5",
		"hover:bg":      "hover\\:bg",
		"2xl:flex":      "\\32 xl\\:flex",
		"-mt-4":         "-mt-4",
	}
	for in, want := range tests {
		if got := EscapeClass(in); got != want {
			t.Errorf("EscapeClass(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestOpacityScale(t *testing.T) {
	tests := map[string]string{
		"opacity-100": "opacity: 1;",
		"opacity-0":   "opacity: 0;",
		"opacity-50":  "opacity: 0.5;",
		"opacity-75":  "opacity: 0.75;",
	}
	for cls, want := range tests {
		if got := gen(t, cls); !strings.Contains(got, want) {
			t.Errorf("%s: want %q, got: %s", cls, want, got)
		}
	}
}

func TestRoundedSidesUseRadii(t *testing.T) {
	tests := map[string]string{
		"rounded-tl-lg": "border-top-left-radius: 0.5rem;",
		"rounded-t-xl":  "border-top-left-radius: 0.75rem; border-top-right-radius: 0.75rem;",
		"rounded-full":  "border-radius: 9999px;",
		"rounded":       "border-radius: 0.25rem;",
	}
	for cls, want := range tests {
		got := gen(t, cls)
		if !strings.Contains(got, want) {
			t.Errorf("%s: want %q, got: %s", cls, want, got)
		}
	}
}

func TestMaxWidthScale(t *testing.T) {
	tests := map[string]string{
		"max-w-lg":    "max-width: 32rem;",
		"max-w-xl":    "max-width: 36rem;",
		"max-w-prose": "max-width: 65ch;",
		"max-w-full":  "max-width: 100%;",
		"max-w-none":  "max-width: none;",
	}
	for cls, want := range tests {
		got := gen(t, cls)
		if !strings.Contains(got, want) {
			t.Errorf("%s: want %q, got: %s", cls, want, got)
		}
	}
}

func TestLeadingScale(t *testing.T) {
	if got := gen(t, "leading-6"); !strings.Contains(got, "line-height: 1.5rem;") {
		t.Errorf("leading-6 should map through spacing: %s", got)
	}
	if got := gen(t, "leading-tight"); !strings.Contains(got, "line-height: 1.25;") {
		t.Errorf("leading-tight: %s", got)
	}
}

func TestColorAlpha(t *testing.T) {
	tests := map[string]string{
		"text-white/70":   "color: rgb(255 255 255 / 0.7);",
		"bg-blue-500/50":  "background-color: rgb(59 130 246 / 0.5);",
		"border-black/10": "border-color: rgb(0 0 0 / 0.1);",
		"bg-red-500/25":   "background-color: rgb(239 68 68 / 0.25);",
	}
	for cls, want := range tests {
		got := gen(t, cls)
		if !strings.Contains(got, want) {
			t.Errorf("%s: want %q, got: %s", cls, want, got)
		}
	}
}

func TestNegatives(t *testing.T) {
	tests := map[string]string{
		"-mt-4":            "margin-top: -1rem;",
		"-m-2":             "margin: -0.5rem;",
		"-translate-x-1/2": "--tw-translate-x: -50%;",
		"-top-2":           "top: -0.5rem;",
	}
	for cls, want := range tests {
		got := gen(t, cls)
		if !strings.Contains(got, want) {
			t.Errorf("%s: want %q, got: %s", cls, want, got)
		}
	}
}

func TestBorderConsistency(t *testing.T) {
	// Every width form must produce a visible (styled) border.
	for _, cls := range []string{"border", "border-2", "border-t", "border-t-2", "border-x", "border-y-4"} {
		got := gen(t, cls)
		if !strings.Contains(got, "border-style: solid;") {
			t.Errorf("%s: should emit border-style: solid: %s", cls, got)
		}
	}
	// Colors must not be mistaken for widths.
	got := gen(t, "border-red-500")
	if !strings.Contains(got, "border-color: #ef4444;") || strings.Contains(got, "border-width") {
		t.Errorf("border-red-500 should be a color: %s", got)
	}
}

func TestSpaceSiblingSelector(t *testing.T) {
	got := gen(t, "space-x-4")
	if !strings.Contains(got, "> :not([hidden]) ~ :not([hidden])") {
		t.Errorf("space-x-4 should use the sibling selector: %s", got)
	}
	if !strings.Contains(got, "--tw-space-x-reverse") {
		t.Errorf("space-x-4 should use the reverse variable: %s", got)
	}
}

func TestTransformCompose(t *testing.T) {
	got := gen(t, "translate-x-2 scale-95 rotate-45")
	// All three utilities must reference the shared variables, not clobber.
	for _, want := range []string{"--tw-translate-x:", "--tw-scale-x:", "--tw-rotate:", "translate(var(--tw-translate-x"} {
		if !strings.Contains(got, want) {
			t.Errorf("transform composition missing %q:\n%s", want, got)
		}
	}
}

func TestDeterministicOutput(t *testing.T) {
	classes := []string{"p-4", "m-2", "text-red-500", "hover:bg-blue-500", "sm:flex", "w-1/2"}
	// Build a shuffled map twice; map iteration order differs run to run.
	a := map[string]bool{}
	b := map[string]bool{}
	for _, c := range classes {
		a[c] = true
	}
	for i := len(classes) - 1; i >= 0; i-- {
		b[classes[i]] = true
	}
	g1 := NewTailwindGenerator().Generate(a)
	g2 := NewTailwindGenerator().Generate(b)
	if g1 != g2 {
		t.Errorf("output not deterministic:\n--- g1 ---\n%s\n--- g2 ---\n%s", g1, g2)
	}
	if g1 == "" {
		t.Fatal("expected non-empty output")
	}
}

func TestArbitraryValues(t *testing.T) {
	tests := map[string]string{
		"w-[100px]":       "width: 100px;",
		"bg-[#ff0000]":    "background-color: #ff0000;",
		"text-[14px]":     "font-size: 14px;",
		"text-[#fff]":     "color: #fff;",
		"p-[1.5rem]":      "padding: 1.5rem;",
		"bg-[url(/a)]":    "background-image: url(/a);",
		"bg-[length:8px]": "background-size: 8px;",
	}
	for cls, want := range tests {
		got := gen(t, cls)
		if !strings.Contains(got, want) {
			t.Errorf("%s: want %q, got: %s", cls, want, got)
		}
	}
}

func TestUnknownClassSkipped(t *testing.T) {
	got := gen(t, "this-is-not-a-utility")
	if got != "" {
		t.Errorf("unknown class should produce no CSS, got: %s", got)
	}
}

// TestInvalidValueRejected verifies a utility whose value resolver fails does not
// emit invalid CSS like `margin: my-project` (the root cause of a real bug).
func TestInvalidValueRejected(t *testing.T) {
	for _, cls := range []string{"my-project", "p-foo", "w-bar", "gap-baz"} {
		got := gen(t, cls)
		if strings.Contains(got, ": "+strings.TrimPrefix(cls, "p-")) && got != "" {
			t.Errorf("%s should not emit invalid CSS: %s", cls, got)
		}
	}
	// Specifically, no rule may contain a bare word value.
	if got := gen(t, "my-project"); got != "" {
		t.Errorf("my-project should be dropped, got: %s", got)
	}
}

func TestPreflight(t *testing.T) {
	got := TailwindPreflight(DefaultTailwindTheme())
	for _, want := range []string{"box-sizing:border-box", "margin:0", "::before", "border-style:solid"} {
		if !strings.Contains(got, want) {
			t.Errorf("preflight missing %q", want)
		}
	}
}

func TestScalesFromConfig(t *testing.T) {
	// A custom breakpoint must produce its own media query.
	g := NewTailwindGenerator()
	g.Theme.Screens["3xl"] = "1920px"
	out := g.Generate(map[string]bool{"3xl:flex": true})
	if !strings.Contains(out, "@media (min-width: 1920px)") {
		t.Errorf("custom screen not honored: %s", out)
	}
}

func TestStrictMode(t *testing.T) {
	g := NewTailwindGenerator()
	g.Strict = true
	g.Generate(map[string]bool{"nope-123": true, "flex": true})
	if len(g.Unknown) != 1 || g.Unknown[0] != "nope-123" {
		t.Errorf("strict mode should report unknown classes, got: %v", g.Unknown)
	}
}
