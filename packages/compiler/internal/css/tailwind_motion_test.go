package css

import (
	"strings"
	"testing"
)

// genKF runs the generator and returns the utilities plus KeyframesCSS.
func genKF(t *testing.T, g *TailwindGenerator, classes ...string) (string, string) {
	t.Helper()
	set := map[string]bool{}
	for _, c := range classes {
		for _, f := range strings.Fields(c) {
			set[f] = true
		}
	}
	out := g.Generate(set)
	return out, g.KeyframesCSS()
}

func TestBuiltinAnimationsEmitKeyframes(t *testing.T) {
	want := map[string]string{
		"animate-spin":   "@keyframes spin{to{transform:rotate(360deg)}}",
		"animate-ping":   "@keyframes ping{75%,100%{transform:scale(2);opacity:0}}",
		"animate-pulse":  "@keyframes pulse{50%{opacity:0.5}}",
		"animate-bounce": "@keyframes bounce{0%,100%{transform:translateY(-25%);",
	}
	for cls, kf := range want {
		for _, variant := range []string{cls, "motion-safe:" + cls, "hover:" + cls} {
			css, got := genKF(t, NewTailwindGenerator(), variant)
			if css == "" {
				t.Fatalf("%s: no utility emitted", variant)
			}
			if !strings.Contains(got, kf) {
				t.Errorf("%s: KeyframesCSS missing %q, got %q", variant, kf, got)
			}
		}
	}
}

func TestKeyframesOnlyForUsedAnimations(t *testing.T) {
	_, kf := genKF(t, NewTailwindGenerator(), "animate-spin flex p-4")
	if !strings.Contains(kf, "@keyframes spin") {
		t.Fatalf("missing spin: %q", kf)
	}
	for _, other := range []string{"ping", "pulse", "bounce"} {
		if strings.Contains(kf, "@keyframes "+other) {
			t.Errorf("unused keyframes %s emitted: %q", other, kf)
		}
	}
	_, none := genKF(t, NewTailwindGenerator(), "flex p-4 animate-none")
	if strings.Contains(none, "@keyframes") {
		t.Errorf("animate-none must not emit keyframes: %q", none)
	}
}

func TestKeyframesDeterministicAndOnce(t *testing.T) {
	_, a := genKF(t, NewTailwindGenerator(), "animate-spin animate-ping animate-pulse md:animate-spin hover:animate-spin")
	_, b := genKF(t, NewTailwindGenerator(), "hover:animate-spin md:animate-spin animate-pulse animate-ping animate-spin")
	if a != b {
		t.Errorf("keyframes not deterministic:\n%s\n---\n%s", a, b)
	}
	if n := strings.Count(a, "@keyframes spin"); n != 1 {
		t.Errorf("spin keyframes emitted %d times:\n%s", n, a)
	}
	if strings.Index(a, "@keyframes ping") > strings.Index(a, "@keyframes pulse") {
		t.Errorf("keyframes not sorted by name: %s", a)
	}
}

func TestArbitraryAnimationKeyframes(t *testing.T) {
	g := NewTailwindGenerator()
	g.Theme.Keyframes = map[string]string{"wiggle": "0%,100%{transform:rotate(-3deg)}50%{transform:rotate(3deg)}"}
	css, kf := genKF(t, g, "animate-[wiggle_1s_ease-in-out_infinite]", "animate-[spin_2s_linear_infinite]")
	if !strings.Contains(css, "animation: wiggle 1s ease-in-out infinite;") {
		t.Errorf("arbitrary animation value: %s", css)
	}
	if !strings.Contains(kf, "@keyframes wiggle{0%,100%{transform:rotate(-3deg)}") {
		t.Errorf("theme keyframes not emitted: %q", kf)
	}
	if !strings.Contains(kf, "@keyframes spin{") {
		t.Errorf("builtin keyframes referenced by an arbitrary value not emitted: %q", kf)
	}
}

func TestThemeAnimationUtilities(t *testing.T) {
	g := NewTailwindGenerator()
	g.Theme.Animation = map[string]string{"wiggle": "wiggle 1s ease-in-out infinite", "spin": "spin 3s linear infinite"}
	g.Theme.Keyframes = map[string]string{"wiggle": "50%{opacity:0.5}"}
	css, kf := genKF(t, g, "animate-wiggle animate-spin")
	for _, want := range []string{"animation: wiggle 1s ease-in-out infinite;", "animation: spin 3s linear infinite;"} {
		if !strings.Contains(css, want) {
			t.Errorf("missing %q in %s", want, css)
		}
	}
	if !strings.Contains(kf, "@keyframes wiggle{50%{opacity:0.5}}") || !strings.Contains(kf, "@keyframes spin{") {
		t.Errorf("keyframes: %q", kf)
	}
	css, _ = genKF(t, NewTailwindGenerator(), "animate-(--foo) animate-nope")
	if !strings.Contains(css, "animation: var(--foo);") {
		t.Errorf("animate-(--foo): %s", css)
	}
	if strings.Contains(css, "nope") {
		t.Errorf("unknown animation must not emit a rule: %s", css)
	}
}

func TestStrictReportsUnknownAnimation(t *testing.T) {
	g := NewTailwindGenerator()
	g.Strict = true
	genKF(t, g, "animate-nope")
	if len(g.Unknown) != 1 || g.Unknown[0] != "animate-nope" {
		t.Errorf("Unknown = %v", g.Unknown)
	}
}

func TestTransitionComposesWithDurationAndEase(t *testing.T) {
	css := gen(t, "transition-colors duration-700 transition-opacity ease-linear transition")
	for _, want := range []string{
		"transition-duration: var(--tw-duration, 150ms);",
		"transition-timing-function: var(--tw-ease, cubic-bezier(0.4, 0, 0.2, 1));",
		"--tw-duration: 700ms; transition-duration: 700ms;",
		"--tw-ease: linear; transition-timing-function: linear;",
	} {
		if !strings.Contains(css, want) {
			t.Errorf("missing %q in:\n%s", want, css)
		}
	}
	if strings.Contains(css, "transition-duration: 150ms;") {
		t.Errorf("transition must not hard-code 150ms:\n%s", css)
	}
	if strings.Contains(css, "transition-property: all") {
		t.Errorf("bare transition must use the v4 property list:\n%s", css)
	}
	if !strings.Contains(css, ".transition{transition-property: color, background-color, border-color, outline-color,") {
		t.Errorf("bare `transition` rule missing:\n%s", css)
	}
}

func TestTransitionVariants(t *testing.T) {
	tests := map[string]string{
		"transition-colors":              "outline-color, text-decoration-color, fill, stroke, --tw-gradient-from, --tw-gradient-via, --tw-gradient-to;",
		"transition-transform":           "transition-property: transform, translate, scale, rotate;",
		"transition-[height]":            "transition-property: height;",
		"transition-(--p)":               "transition-property: var(--p);",
		"duration-initial":               "--tw-duration: initial;",
		"ease-initial":                   "--tw-ease: initial;",
		"duration-(--d)":                 "--tw-duration: var(--d); transition-duration: var(--d);",
		"ease-[cubic-bezier(0.3,0,0,1)]": "transition-timing-function: cubic-bezier(0.3,0,0,1);",
		"delay-(--x)":                    "transition-delay: var(--x);",
		"delay-700":                      "transition-delay: 700ms;",
		"transition-discrete":            "transition-behavior: allow-discrete;",
		"transition-none":                "transition-property: none;",
	}
	for cls, want := range tests {
		if got := gen(t, cls); !strings.Contains(got, want) {
			t.Errorf("%s: want %q, got %s", cls, want, got)
		}
	}
}

func TestTransformsCompose(t *testing.T) {
	css, kf := genKF(t, NewTailwindGenerator(), "animate-spin rotate-45 -translate-x-1/2 scale-110 -rotate-12 translate-y-2")
	for _, want := range []string{
		".rotate-45{rotate: 45deg;}",
		".-rotate-12{rotate: -12deg;}",
		"--tw-translate-x: -50%; translate: var(--tw-translate-x) var(--tw-translate-y);",
		"--tw-translate-y: 0.5rem;",
		"--tw-scale-x: 110%; --tw-scale-y: 110%; --tw-scale-z: 110%; scale: var(--tw-scale-x) var(--tw-scale-y);",
	} {
		if !strings.Contains(css, want) {
			t.Errorf("missing %q in:\n%s", want, css)
		}
	}
	// The individual properties must be registered so they do not inherit.
	for _, want := range []string{
		"@property --tw-translate-x{syntax:\"*\";inherits:false;initial-value:0}",
		"@property --tw-scale-x{syntax:\"*\";inherits:false;initial-value:1}",
	} {
		if !strings.Contains(kf, want) {
			t.Errorf("missing %q in %q", want, kf)
		}
	}
	if strings.Contains(css, "transform:") {
		t.Errorf("translate/rotate/scale must not set `transform`:\n%s", css)
	}
}

func TestTransformEdgeUtilities(t *testing.T) {
	tests := map[string]string{
		"translate-none":     "translate: none;",
		"rotate-none":        "rotate: none;",
		"scale-none":         "scale: none;",
		"rotate-(--r)":       "rotate: var(--r);",
		"scale-(--s)":        "scale: var(--s);",
		"translate-x-(--t)":  "--tw-translate-x: var(--t);",
		"-translate-x-(--t)": "--tw-translate-x: calc(var(--t) * -1);",
		"translate-x-full":   "--tw-translate-x: 100%;",
		"-translate-y-full":  "--tw-translate-y: -100%;",
		"translate-x-px":     "--tw-translate-x: 1px;",
		"translate-z-4":      "translate: var(--tw-translate-x) var(--tw-translate-y) var(--tw-translate-z);",
		"translate-4":        "--tw-translate-x: 1rem; --tw-translate-y: 1rem;",
		"rotate-x-12":        "--tw-rotate-x: rotateX(12deg); transform: var(--tw-rotate-x,)",
		"skew-6":             "--tw-skew-x: skewX(6deg); --tw-skew-y: skewY(6deg);",
		"transform-gpu":      "transform: translateZ(0) var(--tw-rotate-x,)",
		"transform-none":     "transform: none;",
		"origin-top-left":    "transform-origin: 0 0;",
		"origin-[33%_75%]":   "transform-origin: 33% 75%;",
		"scale-x-[1.5]":      "--tw-scale-x: 1.5;",
		"-scale-100":         "--tw-scale-x: -100%;",
	}
	for cls, want := range tests {
		if got := gen(t, cls); !strings.Contains(got, want) {
			t.Errorf("%s: want %q, got %s", cls, want, got)
		}
	}
}

func TestNegativeOfNonNegatableIsRejected(t *testing.T) {
	g := NewTailwindGenerator()
	g.Strict = true
	genKF(t, g, "-flex -animate-spin -p-4")
	if len(g.Unknown) != 3 {
		t.Errorf("negative forms of non-negatable utilities must be unknown, Unknown=%v", g.Unknown)
	}
}
