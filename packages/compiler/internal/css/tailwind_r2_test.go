package css

import (
	"strings"
	"testing"
)

func TestV4PaletteIsOklch(t *testing.T) {
	wantGen(t, map[string]string{
		"bg-red-500":       "background-color: oklch(63.7% 0.237 25.331);",
		"text-blue-500":    "color: oklch(62.3% 0.214 259.815);",
		"bg-white":         "background-color: #fff;",
		"bg-black":         "background-color: #000;",
		"bg-mauve-500":     "background-color: oklch(",
		"bg-olive-500":     "background-color: oklch(",
		"bg-mist-500":      "background-color: oklch(",
		"bg-taupe-500":     "background-color: oklch(",
		"bg-red-500/50":    "background-color: color-mix(in oklab, oklch(63.7% 0.237 25.331) 50%, transparent);",
		"border-slate-200": "border-color: oklch(92.9% 0.013 255.508);",
	})
	for _, name := range []string{"mauve", "olive", "mist", "taupe"} {
		if len(defaultColors()[name]) != 11 {
			t.Errorf("%s: want 11 shades", name)
		}
	}
	for name, shades := range defaultColors() {
		for shade, v := range shades {
			if !strings.HasPrefix(v, "oklch(") {
				t.Errorf("%s-%s = %q, want oklch", name, shade, v)
			}
		}
	}
}

func TestV4ThemeColorOverrideStillWins(t *testing.T) {
	g := NewTailwindGenerator()
	AtThemeApply(t, g, `@theme { --color-red-500: #123456; }`)
	out, _ := genKF(t, g, "bg-red-500", "bg-red-400/50")
	if !strings.Contains(out, "background-color: #123456;") {
		t.Errorf("override lost: %s", out)
	}
	if !strings.Contains(out, "oklch(70.4% 0.191 22.216) 50%") {
		t.Errorf("untouched shade must keep the v4 value: %s", out)
	}
}

// AtThemeApply applies @theme CSS to the generator's theme.
func AtThemeApply(t *testing.T, g *TailwindGenerator, src string) {
	t.Helper()
	ParseAtTheme(src).Apply(&g.Theme)
}

func TestV4FontSansStack(t *testing.T) {
	wantGen(t, map[string]string{
		"font-sans": "font-family: -apple-system, BlinkMacSystemFont, \"Segoe UI\", Roboto,",
	})
}

func TestJustifySelfMatchesV4(t *testing.T) {
	wantGen(t, map[string]string{
		"justify-self-end-safe": "justify-self: safe flex-end;",
		"justify-self-end":      "justify-self: flex-end;",
		"justify-self-start":    "justify-self: flex-start;",
	})
}

func TestOutlineHiddenForcedColors(t *testing.T) {
	wantGen(t, map[string]string{
		"outline-hidden":       "outline-style: none; @media (forced-colors: active){outline: 2px solid transparent; outline-offset: 2px;}",
		"hover:outline-hidden": "@media (hover: hover){.hover\\:outline-hidden:hover{--tw-outline-style: none; outline-style: none; @media (forced-colors: active){",
		"!outline-hidden":      "outline-style: none !important; @media (forced-colors: active){outline: 2px solid transparent !important;",
	})
}

func TestColorSchemeUtilities(t *testing.T) {
	wantGen(t, map[string]string{
		"scheme-dark":       "color-scheme: dark;",
		"scheme-light-dark": "color-scheme: light dark;",
		"scheme-only-dark":  "color-scheme: only dark;",
		"scheme-normal":     "color-scheme: normal;",
	})
	g := NewTailwindGenerator()
	g.Strict = true
	genKF(t, g, "color-scheme-dark")
	if len(g.Unknown) != 1 {
		t.Errorf("color-scheme-dark is not a v4 utility: %v", g.Unknown)
	}
}

func TestMaskFamily(t *testing.T) {
	wantGen(t, map[string]string{
		"mask-none":                     "mask-image: none;",
		"mask-add":                      "mask-composite: add;",
		"mask-match":                    "mask-mode: match-source;",
		"mask-type-alpha":               "mask-type: alpha;",
		"mask-cover":                    "mask-size: cover;",
		"mask-size-[10px]":              "mask-size: 10px;",
		"mask-clip-fill":                "mask-clip: fill-box;",
		"mask-no-clip":                  "mask-clip: no-clip;",
		"mask-origin-view":              "mask-origin: view-box;",
		"mask-top-left":                 "mask-position: left top;",
		"mask-position-[10px_20px]":     "mask-position: 10px 20px;",
		"mask-repeat-x":                 "mask-repeat: repeat-x;",
		"mask-[url(/a.png)]":            "mask-image: url(/a.png);",
		"mask-(--m)":                    "mask-image: var(--m);",
		"mask-circle":                   "--tw-mask-radial-shape: circle;",
		"mask-radial-at-top-left":       "--tw-mask-radial-position: top left;",
		"mask-radial-farthest-corner":   "--tw-mask-radial-size: farthest-corner;",
		"mask-linear-50":                "--tw-mask-linear-position: 50deg;",
		"-mask-linear-50":               "--tw-mask-linear-position: -50deg;",
		"-mask-conic-30":                "--tw-mask-conic-position: -30deg;",
		"mask-linear-[45deg]":           "--tw-mask-linear-position: 45deg;",
		"mask-radial-[100px_at_center]": "--tw-mask-radial-size: 100px at center;",
		"mask-t-from-50%":               "--tw-mask-top-from-position: 50%;",
		"mask-t-from-4":                 "--tw-mask-top-from-position: 1rem;",
		"mask-t-from-(--x)":             "--tw-mask-top-from-position: var(--x);",
		"mask-b-to-transparent":         "--tw-mask-bottom-to-color: transparent;",
		"mask-linear-from-red-500":      "--tw-mask-linear-from-color: oklch(63.7% 0.237 25.331);",
		"mask-linear-to-30%":            "--tw-mask-linear-to-position: 30%;",
		"mask-conic-from-1":             "--tw-mask-conic-from-position: 0.25rem;",
	})
	out := gen(t, "mask-x-from-10%")
	for _, want := range []string{
		"--tw-mask-right: linear-gradient(to right,",
		"--tw-mask-left: linear-gradient(to left,",
		"--tw-mask-right-from-position: 10%;",
		"--tw-mask-left-from-position: 10%;",
		"mask-composite: intersect;",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("mask-x-from-10%% missing %q: %s", want, out)
		}
	}
	g := NewTailwindGenerator()
	_, props := genKF(t, g, "mask-t-from-50%")
	if !strings.Contains(props, "@property --tw-mask-top-from-color{syntax:\"*\";inherits:false;initial-value:black}") {
		t.Errorf("mask variables must be registered: %s", props)
	}
	g = NewTailwindGenerator()
	g.Strict = true
	genKF(t, g, "mask-t-from-bogus", "mask-origin-nope", "mask-linear-abc", "-mask-add")
	if len(g.Unknown) != 4 {
		t.Errorf("invalid mask utilities must be unknown: %v", g.Unknown)
	}
}

func TestScrollbarAndContainAndTab(t *testing.T) {
	wantGen(t, map[string]string{
		"scrollbar-thin":            "scrollbar-width: thin;",
		"scrollbar-none":            "scrollbar-width: none;",
		"scrollbar-auto":            "scrollbar-width: auto;",
		"scrollbar-gutter-both":     "scrollbar-gutter: stable both-edges;",
		"scrollbar-gutter-stable":   "scrollbar-gutter: stable;",
		"scrollbar-thumb-red-500":   "--tw-scrollbar-thumb: oklch(63.7% 0.237 25.331); scrollbar-color: var(--tw-scrollbar-thumb, #0000) var(--tw-scrollbar-track, #0000);",
		"scrollbar-track-white":     "--tw-scrollbar-track: #fff;",
		"contain-strict":            "contain: strict;",
		"contain-size":              "--tw-contain-size: size; contain: var(--tw-contain-size,)",
		"contain-inline-size":       "--tw-contain-size: inline-size;",
		"contain-paint":             "--tw-contain-paint: paint;",
		"tab-4":                     "tab-size: 4;",
		"tab-[3]":                   "tab-size: 3;",
		"normal-nums":               "font-variant-numeric: normal;",
		"translate-3d":              "translate: var(--tw-translate-x, 0) var(--tw-translate-y, 0) var(--tw-translate-z, 0);",
		"transform-fill":            "transform-box: fill-box;",
		"transform-view":            "transform-box: view-box;",
		"overflow-x-visible":        "overflow-x: visible;",
		"overflow-y-visible":        "overflow-y: visible;",
		"place-content-baseline":    "place-content: baseline;",
		"justify-baseline":          "justify-content: baseline;",
		"content-end-safe":          "align-content: safe flex-end;",
		"place-self-end-safe":       "place-self: safe end;",
		"break-inside-avoid-column": "break-inside: avoid-column;",
		"bg-top-left":               "background-position: left top;",
		"bg-bottom-right":           "background-position: right bottom;",
		"object-top-left":           "object-position: left top;",
		"-underline-offset-2":       "text-underline-offset: calc(2px * -1);",
		"via-none":                  "--tw-gradient-via-stops: initial;",
		"shadow-initial":            "--tw-shadow-color: initial;",
		"inset-shadow-initial":      "--tw-inset-shadow-color: initial;",
		"text-shadow-initial":       "--tw-text-shadow-color: initial;",
		"-bg-linear-90":             "--tw-gradient-position: -90deg in oklab;",
		"-bg-conic-30":              "--tw-gradient-position: from -30deg in oklab;",
	})
}

func TestLogicalUtilities(t *testing.T) {
	wantGen(t, map[string]string{
		"block-4":           "block-size: 1rem;",
		"block-full":        "block-size: 100%;",
		"block-screen":      "block-size: 100vh;",
		"block-lh":          "block-size: 1lh;",
		"inline-1/2":        "inline-size: 50%;",
		"inline-screen":     "inline-size: 100vw;",
		"inline-sm":         "inline-size: 24rem;",
		"min-block-4":       "min-block-size: 1rem;",
		"max-block-none":    "max-block-size: none;",
		"min-inline-fit":    "min-inline-size: fit-content;",
		"max-inline-sm":     "max-inline-size: 24rem;",
		"mbs-4":             "margin-block-start: 1rem;",
		"-mbe-px":           "margin-block-end: -1px;",
		"mbs-auto":          "margin-block-start: auto;",
		"pbs-2":             "padding-block-start: 0.5rem;",
		"pbe-px":            "padding-block-end: 1px;",
		"inset-bs-1/2":      "inset-block-start: 50%;",
		"-inset-be-4":       "inset-block-end: -1rem;",
		"border-bs":         "border-block-start-style: var(--tw-border-style, solid); border-block-start-width: 1px;",
		"border-be-2":       "border-block-end-width: 2px;",
		"border-bs-red-500": "border-block-start-color: oklch(63.7% 0.237 25.331);",
		"scroll-mbs-4":      "scroll-margin-block-start: 1rem;",
		"-scroll-mbe-4":     "scroll-margin-block-end: -1rem;",
		"scroll-pbs-3":      "scroll-padding-block-start: 0.75rem;",
		"-scroll-m-px":      "scroll-margin: -1px;",
		"scroll-px-px":      "scroll-padding-inline: 1px;",
		"w-dvh":             "width: 100dvh;",
		"h-dvw":             "height: 100dvw;",
		"size-dvh":          "width: 100dvh; height: 100dvh;",
		"min-w-svh":         "min-width: 100svh;",
		"max-h-lvw":         "max-height: 100lvw;",
	})
	g := NewTailwindGenerator()
	g.Strict = true
	genKF(t, g, "size-screen", "size-sm", "pbs-auto", "scroll-mt-auto", "w-lh", "basis-dvh")
	if len(g.Unknown) != 6 {
		t.Errorf("v4 has none of these: %v", g.Unknown)
	}
}

func TestBackdropAndFilterFunctions(t *testing.T) {
	wantGen(t, map[string]string{
		"backdrop-grayscale":      "--tw-backdrop-grayscale: grayscale(100%);",
		"backdrop-grayscale-50":   "--tw-backdrop-grayscale: grayscale(50%);",
		"backdrop-invert-25":      "--tw-backdrop-invert: invert(25%);",
		"backdrop-sepia":          "--tw-backdrop-sepia: sepia(100%);",
		"backdrop-opacity-50":     "--tw-backdrop-opacity: opacity(50%);",
		"backdrop-opacity-[.3]":   "--tw-backdrop-opacity: opacity(.3);",
		"backdrop-hue-rotate-15":  "--tw-backdrop-hue-rotate: hue-rotate(15deg);",
		"-backdrop-hue-rotate-15": "--tw-backdrop-hue-rotate: hue-rotate(calc(15deg * -1));",
		"-hue-rotate-15":          "--tw-hue-rotate: hue-rotate(calc(15deg * -1));",
		"backdrop-brightness-0":   "--tw-backdrop-brightness: brightness(0%);",
		"brightness-0":            "--tw-brightness: brightness(0%);",
		"brightness-137":          "--tw-brightness: brightness(137%);",
		"invert-[.5]":             "--tw-invert: invert(.5);",
		"hue-rotate-[1turn]":      "--tw-hue-rotate: hue-rotate(1turn);",
		"grayscale":               "--tw-grayscale: grayscale(100%);",
	})
	g := NewTailwindGenerator()
	g.Strict = true
	genKF(t, g, "-brightness-50", "opacity-filter", "-invert")
	if len(g.Unknown) != 3 {
		t.Errorf("only hue-rotate is negatable: %v", g.Unknown)
	}
}

func TestNestedAtRuleBlocksValidate(t *testing.T) {
	if !validDeclarationBlock("a: b; @media (x){c: d; e: f;}") {
		t.Error("nested at-rule block must be valid")
	}
	if validDeclarationBlock("a: b; @media (x){c: ;}") {
		t.Error("an invalid declaration inside a nested block must be rejected")
	}
	got := addImportant("a: b; @media (x){c: d;}")
	if got != "a: b !important; @media (x){c: d !important;};" {
		t.Errorf("addImportant: %q", got)
	}
}
