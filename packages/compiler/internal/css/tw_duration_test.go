package css

import (
	"strings"
	"testing"
)

// A minimal @utility set shaped like an animation helper package: a duration
// type with a catch-all fallback.
func TestUtilityValueDurationType(t *testing.T) {
	d := NewTailwindDirectives(t.TempDir())
	d.Add(`@utility animation-duration-* {
  --tw-anim-duration: --value(--anim-duration-*, [duration], "initial", [*]);
  animation-duration: --value(--anim-duration-*, [duration], "initial", [*]);
}
@utility wait-* {
  --tw-anim-wait: --value(--anim-wait-*, [duration], "initial", [*]);
  animation-delay: --value(--anim-wait-*, [duration], "initial", [*]);
}
@utility only-dur-* { animation-delay: --value([duration]); }`, "")
	if w := d.Warnings(); len(w) != 0 {
		t.Fatalf("unexpected warnings: %v", w)
	}
	g := NewTailwindGenerator()
	d.ApplyTo(&g.Theme)
	cases := []struct{ class, want string }{
		{"animation-duration-[400ms]", "animation-duration: 400ms"},
		{"animation-duration-[0.3s]", "animation-duration: 0.3s"},
		{"wait-[150ms]", "animation-delay: 150ms"},
		{"wait-[duration:150ms]", "animation-delay: 150ms"},
		{"only-dur-[duration:2s]", "animation-delay: 2s"},
	}
	for _, c := range cases {
		if got := g.Generate(map[string]bool{c.class: true}); !strings.Contains(got, c.want) {
			t.Errorf("%s: want %q in %q", c.class, c.want, got)
		}
	}
	// Tailwind never infers duration from a bare value.
	if got := g.Generate(map[string]bool{"only-dur-[3s]": true}); strings.Contains(got, "animation-delay") {
		t.Errorf("only-dur-[3s] should not match: %q", got)
	}
}
