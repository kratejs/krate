package css

import (
	"os"
	"strings"
	"testing"
)

// --- Value-producing bugs ---

func TestScaleAxisUtilities(t *testing.T) {
	tests := map[string]string{
		"scale-x-50":  "--tw-scale-x: 0.5;",
		"scale-y-110": "--tw-scale-y: 1.1;",
		"scale-95":    "--tw-scale-x: 0.95; --tw-scale-y: 0.95;",
		"scale-[1.7]": "--tw-scale-x: 1.7; --tw-scale-y: 1.7;",
	}
	for cls, want := range tests {
		got := gen(t, cls)
		if !strings.Contains(got, want) {
			t.Errorf("%s: want %q, got: %s", cls, want, got)
		}
		// The axis letter must never leak into the cumulative custom-property
		// value (the selector legitimately contains the class name).
		if strings.Contains(got, ": x-") || strings.Contains(got, ": y-") {
			t.Errorf("%s: axis leaked into value: %s", cls, got)
		}
	}
}

func TestRotateAndSkewArbitrary(t *testing.T) {
	tests := map[string]string{
		"rotate-[17deg]": "--tw-rotate: 17deg;",
		"rotate-45":      "--tw-rotate: 45deg;",
		"skew-x-[12deg]": "--tw-skew-x: 12deg;",
		"skew-y-6":       "--tw-skew-y: 6deg;",
	}
	for cls, want := range tests {
		got := gen(t, cls)
		if !strings.Contains(got, want) {
			t.Errorf("%s: want %q, got: %s", cls, want, got)
		}
		if strings.Contains(got, "deg]deg") || strings.Contains(got, "[12deg]") {
			t.Errorf("%s: malformed unit: %s", cls, got)
		}
	}
}

func TestTransformOriginArbitrary(t *testing.T) {
	got := gen(t, "origin-[33%_75%]")
	if !strings.Contains(got, "transform-origin: 33% 75%;") {
		t.Errorf("origin arbitrary should unwrap and unescape: %s", got)
	}
	if strings.Contains(got, "[33%") {
		t.Errorf("origin kept brackets/underscores: %s", got)
	}
}

func TestTransformNoneResets(t *testing.T) {
	got := gen(t, "transform-none")
	if !strings.Contains(got, "transform: none;") {
		t.Errorf("transform-none should set transform:none: %s", got)
	}
	if strings.Contains(got, "translate(var(") {
		t.Errorf("transform-none should not compose: %s", got)
	}
}

func TestHeightScreenIsViewportHeight(t *testing.T) {
	if got := gen(t, "h-screen"); !strings.Contains(got, "height: 100vh;") {
		t.Errorf("h-screen should be 100vh: %s", got)
	}
	if got := gen(t, "min-h-screen"); !strings.Contains(got, "min-height: 100vh;") {
		t.Errorf("min-h-screen should be 100vh: %s", got)
	}
	if got := gen(t, "w-screen"); !strings.Contains(got, "width: 100vw;") {
		t.Errorf("w-screen should be 100vw: %s", got)
	}
	if got := gen(t, "h-dvh"); !strings.Contains(got, "height: 100dvh;") {
		t.Errorf("h-dvh should be 100dvh: %s", got)
	}
}

func TestArbitraryTextSizeVsColor(t *testing.T) {
	if got := gen(t, "text-[14px]"); !strings.Contains(got, "font-size: 14px;") {
		t.Errorf("text-[14px] should be font-size: %s", got)
	}
	if got := gen(t, "text-[1.5rem]"); !strings.Contains(got, "font-size: 1.5rem;") {
		t.Errorf("text-[1.5rem] should be font-size: %s", got)
	}
	if got := gen(t, "text-[#bada55]"); !strings.Contains(got, "color: #bada55;") {
		t.Errorf("text-[#bada55] should be color: %s", got)
	}
	if got := gen(t, "text-[color:var(--fg)]"); !strings.Contains(got, "color: var(--fg);") {
		t.Errorf("text-[color:...] hint should be color: %s", got)
	}
}

func TestArbitraryBackground(t *testing.T) {
	if got := gen(t, "bg-[url(/img.png)]"); !strings.Contains(got, "background-image: url(/img.png);") {
		t.Errorf("bg-[url(...)] should be background-image: %s", got)
	}
	if got := gen(t, "bg-[length:200px_100px]"); !strings.Contains(got, "background-size: 200px 100px;") {
		t.Errorf("bg-[length:...] should be background-size: %s", got)
	}
	if got := gen(t, "bg-[#ff0000]"); !strings.Contains(got, "background-color: #ff0000;") {
		t.Errorf("bg-[#ff0000] should be background-color: %s", got)
	}
}

func TestArbitraryGridTemplates(t *testing.T) {
	if got := gen(t, "grid-cols-[200px_minmax(900px,_1fr)_100px]"); !strings.Contains(got, "grid-template-columns: 200px minmax(900px, 1fr) 100px;") {
		t.Errorf("grid-cols arbitrary failed: %s", got)
	}
	if got := gen(t, "grid-rows-[1fr_2fr]"); !strings.Contains(got, "grid-template-rows: 1fr 2fr;") {
		t.Errorf("grid-rows arbitrary failed: %s", got)
	}
}

func TestArbitraryTypedUtilities(t *testing.T) {
	tests := map[string]string{
		"leading-[3rem]":                             "line-height: 3rem;",
		"tracking-[0.2em]":                           "letter-spacing: 0.2em;",
		"duration-[2s]":                              "transition-duration: 2s;",
		"ease-[cubic-bezier(0.1,0.2,0.3,0.4)]":       "transition-timing-function: cubic-bezier(0.1,0.2,0.3,0.4);",
		"ring-offset-[3px]":                          "--tw-ring-offset-width: 3px;",
		"shadow-[0_35px_60px_-15px_rgba(0,0,0,0.3)]": "--tw-shadow: 0 35px 60px -15px rgba(0,0,0,0.3);",
	}
	for cls, want := range tests {
		got := gen(t, cls)
		if !strings.Contains(got, want) {
			t.Errorf("%s: want %q, got: %s", cls, want, got)
		}
	}
	// The declaration value must not retain underscores (only the escaped
	// selector legitimately contains them).
	if got := gen(t, "shadow-[0_35px_60px_-15px_rgba(0,0,0,0.3)]"); strings.Contains(got, "box-shadow: 0_35px") {
		t.Errorf("shadow arbitrary kept underscores in value: %s", got)
	}
}

func TestGradientStopsWithAlpha(t *testing.T) {
	tests := map[string]string{
		"from-indigo-400/50": "--tw-gradient-from: rgb(129 140 248 / 0.5);",
		"via-purple-500/50":  "rgb(168 85 247 / 0.5)",
		"to-pink-500/25":     "--tw-gradient-to: rgb(236 72 153 / 0.25);",
		"from-[#bada55]":     "--tw-gradient-from: #bada55;",
	}
	for cls, want := range tests {
		got := gen(t, cls)
		if !strings.Contains(got, want) {
			t.Errorf("%s: want %q, got: %s", cls, want, got)
		}
	}
}

func TestSpacingArbitraryMultiples(t *testing.T) {
	tests := map[string]string{
		"p-13":    "padding: 3.25rem;",
		"p-15":    "padding: 3.75rem;",
		"p-13.5":  "padding: 3.375rem;",
		"px-13":   "padding-left: 3.25rem; padding-right: 3.25rem;",
		"gap-13":  "gap: 3.25rem;",
		"top-1/2": "top: 50%;",
	}
	for cls, want := range tests {
		got := gen(t, cls)
		if !strings.Contains(got, want) {
			t.Errorf("%s: want %q, got: %s", cls, want, got)
		}
		if strings.Contains(got, "13.5px") {
			t.Errorf("%s: decimal fell through to px: %s", cls, got)
		}
	}
}

func TestRingOffsetColor(t *testing.T) {
	if got := gen(t, "ring-offset-blue-500"); !strings.Contains(got, "--tw-ring-offset-color: #3b82f6;") {
		t.Errorf("ring-offset color unsupported: %s", got)
	}
	if got := gen(t, "ring-offset-2"); !strings.Contains(got, "--tw-ring-offset-width: 2px;") {
		t.Errorf("ring-offset numeric unsupported: %s", got)
	}
}

func TestSpaceReverseSelector(t *testing.T) {
	got := gen(t, "space-x-reverse")
	if strings.Contains(got, "> :not([hidden])") {
		t.Errorf("space-x-reverse must not get the sibling suffix: %s", got)
	}
	if !strings.Contains(got, "--tw-space-x-reverse: 1;") {
		t.Errorf("space-x-reverse missing: %s", got)
	}
	// space-x-4 still uses the suffix.
	if got := gen(t, "space-x-4"); !strings.Contains(got, "> :not([hidden]) ~ :not([hidden])") {
		t.Errorf("space-x-4 should still use the suffix: %s", got)
	}
}

func TestDivideBareAndReverse(t *testing.T) {
	tests := map[string]string{
		"divide-x":         "border-left-width",
		"divide-y":         "border-top-width",
		"divide-y-reverse": "--tw-divide-y-reverse: 1;",
	}
	for cls, want := range tests {
		got := gen(t, cls)
		if !strings.Contains(got, want) {
			t.Errorf("%s: want %q, got: %s", cls, want, got)
		}
	}
	if got := gen(t, "divide-y-reverse"); strings.Contains(got, "> :not([hidden])") {
		t.Errorf("divide-y-reverse must not get the sibling suffix: %s", got)
	}
}

func TestShadow2xlAndNone(t *testing.T) {
	if got := gen(t, "shadow-2xl"); !strings.Contains(got, "--tw-shadow: 0 25px 50px -12px rgb(0 0 0 / 0.25);") {
		t.Errorf("shadow-2xl rejected: %s", got)
	}
	if got := gen(t, "shadow-none"); !strings.Contains(got, "box-shadow: none;") {
		t.Errorf("shadow-none failed: %s", got)
	}
}

func TestNotSrOnly(t *testing.T) {
	got := gen(t, "not-sr-only")
	if !strings.Contains(got, "position: static;") {
		t.Errorf("not-sr-only should reset: %s", got)
	}
}

// --- Scanner ---

func TestScannerBareUtilities(t *testing.T) {
	src := `<div class="isolate grayscale invert italic grow shrink fixed static overline"></div>`
	got := candidateTokens(src)
	seen := map[string]bool{}
	for _, c := range got {
		seen[c] = true
	}
	for _, want := range []string{"isolate", "grayscale", "invert", "italic", "grow", "shrink", "fixed", "static", "overline"} {
		if !seen[want] {
			t.Errorf("scanner dropped bare utility %q; got %v", want, got)
		}
	}
}

func TestScannerRejectsProse(t *testing.T) {
	src := `const message = "this is a normal sentence of prose";`
	for _, tok := range candidateTokens(src) {
		if tok == "normal" || tok == "sentence" || tok == "prose" {
			t.Errorf("scanner emitted prose token %q", tok)
		}
	}
}

// --- Variants ---

func TestContainerQueryVariants(t *testing.T) {
	got := gen(t, "@sm:flex")
	if !strings.Contains(got, "@media (min-width: 24rem)") {
		t.Errorf("@sm container query missing: %s", got)
	}
	got = gen(t, "@[400px]:grid")
	if !strings.Contains(got, "@media (min-width: 400px)") {
		t.Errorf("@[400px] container query missing: %s", got)
	}
}

func TestNotVariant(t *testing.T) {
	got := gen(t, "not-hover:underline")
	if !strings.Contains(got, ":not(:hover)") {
		t.Errorf("not-hover should negate :hover: %s", got)
	}
	got = gen(t, "not-supports-[display:grid]:flex")
	if !strings.Contains(got, "@supports not (display:grid)") {
		t.Errorf("not-supports failed: %s", got)
	}
}

func TestChildVariants(t *testing.T) {
	if got := gen(t, "*:p-2"); !strings.Contains(got, "> *") {
		t.Errorf("*: child variant missing: %s", got)
	}
	if got := gen(t, "**:underline"); !strings.Contains(got, " *") {
		t.Errorf("**: descendant variant missing: %s", got)
	}
}

func TestNthAndCapabilityVariants(t *testing.T) {
	tests := map[string]string{
		"nth-3:flex":           ":nth-child(3)",
		"nth-last-2:flex":      ":nth-last-child(2)",
		"nth-of-type-odd:flex": ":nth-of-type(odd)",
		"first-line:underline": "::first-line",
	}
	for cls, want := range tests {
		got := gen(t, cls)
		if !strings.Contains(got, want) {
			t.Errorf("%s: want %q, got: %s", cls, want, got)
		}
	}
	if got := gen(t, "pointer-coarse:p-4"); !strings.Contains(got, "@media (pointer: coarse)") {
		t.Errorf("pointer-coarse variant missing: %s", got)
	}
	if got := gen(t, "noscript:block"); !strings.Contains(got, "scripting: none") {
		t.Errorf("noscript variant missing: %s", got)
	}
}

// --- Missing families ---

func TestFilterFamilies(t *testing.T) {
	tests := map[string]string{
		"blur-sm":        "--tw-blur: blur(4px);",
		"brightness-150": "--tw-brightness: brightness(1.5);",
		"contrast-125":   "--tw-contrast: contrast(1.25);",
		"grayscale":      "--tw-grayscale: grayscale(100%);",
		"hue-rotate-90":  "--tw-hue-rotate: hue-rotate(90deg);",
		"invert":         "--tw-invert: invert(100%);",
		"saturate-200":   "--tw-saturate: saturate(2);",
		"sepia":          "--tw-sepia: sepia(100%);",
		"drop-shadow-md": "--tw-drop-shadow: drop-shadow(",
	}
	for cls, want := range tests {
		got := gen(t, cls)
		if !strings.Contains(got, want) {
			t.Errorf("%s: want %q, got: %s", cls, want, got)
		}
		if !strings.Contains(got, "filter: var(--tw-blur") {
			t.Errorf("%s: missing composed filter: %s", cls, got)
		}
	}
}

func TestBackdropFilters(t *testing.T) {
	got := gen(t, "backdrop-blur-lg")
	if !strings.Contains(got, "--tw-backdrop-blur: blur(16px);") || !strings.Contains(got, "backdrop-filter:") {
		t.Errorf("backdrop-blur-lg failed: %s", got)
	}
}

func TestAnimationFamilies(t *testing.T) {
	tests := map[string]string{
		"animate-spin":   "animation: spin 1s linear infinite;",
		"animate-ping":   "animation: ping",
		"animate-pulse":  "animation: pulse",
		"animate-bounce": "animation: bounce 1s infinite;",
		"animate-none":   "animation: none;",
	}
	for cls, want := range tests {
		got := gen(t, cls)
		if !strings.Contains(got, want) {
			t.Errorf("%s: want %q, got: %s", cls, want, got)
		}
	}
}

func TestBlendModes(t *testing.T) {
	if got := gen(t, "mix-blend-multiply"); !strings.Contains(got, "mix-blend-mode: multiply;") {
		t.Errorf("mix-blend-multiply failed: %s", got)
	}
	if got := gen(t, "bg-blend-overlay"); !strings.Contains(got, "background-blend-mode: overlay;") {
		t.Errorf("bg-blend-overlay failed: %s", got)
	}
}

func TestLayoutFamilies(t *testing.T) {
	tests := map[string]string{
		"isolate":              "isolation: isolate;",
		"float-right":          "float: right;",
		"clear-both":           "clear: both;",
		"overscroll-contain":   "overscroll-behavior: contain;",
		"overscroll-x-none":    "overscroll-behavior-x: none;",
		"columns-3":            "columns: 3;",
		"break-after-page":     "break-after: page;",
		"box-decoration-clone": "box-decoration-break: clone;",
	}
	for cls, want := range tests {
		got := gen(t, cls)
		if !strings.Contains(got, want) {
			t.Errorf("%s: want %q, got: %s", cls, want, got)
		}
	}
}

func TestTypographyFamilies(t *testing.T) {
	tests := map[string]string{
		"italic":                  "font-style: italic;",
		"not-italic":              "font-style: normal;",
		"overline":                "text-decoration-line: overline;",
		"tabular-nums":            "--tw-numeric-spacing: tabular-nums;",
		"decoration-wavy":         "text-decoration-style: wavy;",
		"decoration-2":            "text-decoration-thickness: 2px;",
		"underline-offset-4":      "text-underline-offset: 4px;",
		"indent-8":                "text-indent: 2rem;",
		"whitespace-break-spaces": "white-space: break-spaces;",
		"hyphens-auto":            "hyphens: auto;",
	}
	for cls, want := range tests {
		got := gen(t, cls)
		if !strings.Contains(got, want) {
			t.Errorf("%s: want %q, got: %s", cls, want, got)
		}
	}
}

func TestInteractivityFamilies(t *testing.T) {
	tests := map[string]string{
		"resize-none":           "resize: none;",
		"touch-pan-x":           "touch-action: pan-x;",
		"will-change-transform": "will-change: transform;",
		"accent-pink-500":       "accent-color: #ec4899;",
		"caret-red-500":         "caret-color: #ef4444;",
		"scroll-smooth":         "scroll-behavior: smooth;",
		"field-sizing-content":  "field-sizing: content;",
	}
	for cls, want := range tests {
		got := gen(t, cls)
		if !strings.Contains(got, want) {
			t.Errorf("%s: want %q, got: %s", cls, want, got)
		}
	}
}

func TestTransform3DFamilies(t *testing.T) {
	tests := map[string]string{
		"perspective-normal": "perspective: 500px;",
		"transform-style-3d": "transform-style: preserve-3d;",
		"backface-hidden":    "backface-visibility: hidden;",
		"rotate-x-45":        "--tw-rotate-x: 45deg;",
		"zoom-75":            "zoom: 0.75;",
	}
	for cls, want := range tests {
		got := gen(t, cls)
		if !strings.Contains(got, want) {
			t.Errorf("%s: want %q, got: %s", cls, want, got)
		}
	}
}

func TestBorderExtras(t *testing.T) {
	tests := map[string]string{
		"outline-dashed":   "outline-style: dashed;",
		"outline-offset-4": "outline-offset: 4px;",
		"outline-blue-500": "outline-color: #3b82f6;",
		"border-spacing-2": "--tw-border-spacing-x: 0.5rem; --tw-border-spacing-y: 0.5rem;",
		"border-s-2":       "border-inline-start-width: 2px;",
	}
	for cls, want := range tests {
		got := gen(t, cls)
		if !strings.Contains(got, want) {
			t.Errorf("%s: want %q, got: %s", cls, want, got)
		}
	}
}

func TestBackgroundExtras(t *testing.T) {
	if got := gen(t, "bg-linear-to-r"); !strings.Contains(got, "linear-gradient(to right") {
		t.Errorf("bg-linear-to-r failed: %s", got)
	}
	if got := gen(t, "from-50%"); !strings.Contains(got, "--tw-gradient-from-position: 50%;") {
		t.Errorf("from-50%% position failed: %s", got)
	}
}

// --- Minifier ---

func TestMinifierKeepsVendorPrefixes(t *testing.T) {
	in := `.a{display:-webkit-box;display:flex}`
	out := Minify(in)
	if !strings.Contains(out, "-webkit-box") || !strings.Contains(out, "display:flex") {
		t.Errorf("vendor-prefixed fallback dropped: %s", out)
	}
}

func TestMinifierCustomPropertyCase(t *testing.T) {
	out := Minify(`.a{--Foo:1px;--foo:2px}`)
	if !strings.Contains(out, "--Foo:1px") || !strings.Contains(out, "--foo:2px") {
		t.Errorf("custom-property case folded: %s", out)
	}
}

func TestMinifierSpaceSyntaxRGB(t *testing.T) {
	if out := Minify(`.a{color:rgb(255 0 0)}`); !strings.Contains(out, "#f00") {
		t.Errorf("space-syntax rgb not minified: %s", out)
	}
	if out := Minify(`.a{color:rgb(255 0 0 / 1)}`); !strings.Contains(out, "#f00") {
		t.Errorf("space-syntax rgb with alpha 1 not minified: %s", out)
	}
	// Alpha < 1 must be preserved.
	if out := Minify(`.a{color:rgb(255 0 0 / 0.5)}`); strings.Contains(out, "#f00") {
		t.Errorf("transparent color must not become hex: %s", out)
	}
}

func TestMinifierCalcNoSpaces(t *testing.T) {
	tests := map[string]string{
		"calc(1*2rem)": "2rem",
		"calc(2rem*1)": "2rem",
		"calc(0+2rem)": "2rem",
		"calc(2rem+0)": "2rem",
	}
	for in, want := range tests {
		out := Minify(".a{width:" + in + "}")
		if !strings.Contains(out, "width:"+want) {
			t.Errorf("%s: want width:%s, got: %s", in, want, out)
		}
	}
}

// --- Preflight / config ---

func TestPreflightNoGlobalScrollBehavior(t *testing.T) {
	got := TailwindPreflight(DefaultTailwindTheme())
	if strings.Contains(got, "scroll-behavior:smooth") {
		t.Errorf("preflight must not set global scroll-behavior: %s", got)
	}
	if !strings.Contains(got, "--tw-divide-x-reverse:0") {
		t.Errorf("preflight missing divide reverse defaults: %s", got)
	}
}

func TestBeforeAfterInjectsContent(t *testing.T) {
	if got := gen(t, "before:content-['→']"); !strings.Contains(got, "content: var(--tw-content);") {
		t.Errorf("before: should inject the content hook: %s", got)
	}
	if got := gen(t, "before:block"); !strings.Contains(got, "content: var(--tw-content);") {
		t.Errorf("before:block should inject the content hook: %s", got)
	}
	if got := gen(t, "hover:block"); strings.Contains(got, "content: var(--tw-content)") {
		t.Errorf("non-pseudo variant must not inject content: %s", got)
	}
}

func TestConfigExtendMergesDefaults(t *testing.T) {
	dir := t.TempDir()
	cfgPath := dir + "/tailwind.config.ts"
	content := "export default { theme: { extend: { borderRadius: { xl: '1rem' }, boxShadow: { glow: '0 0 8px red' } } } }"
	if err := writeTestFile(cfgPath, content); err != nil {
		t.Fatal(err)
	}
	cfg, ok := ParseTailwindConfigStatic(cfgPath)
	if !ok {
		t.Fatal("static parse failed")
	}
	// The extended key must be present...
	if cfg.Theme.Radii["xl"] != "1rem" {
		t.Errorf("extend.borderRadius.xl missing: %v", cfg.Theme.Radii)
	}
	if cfg.Theme.Shadows["glow"] != "0 0 8px red" {
		t.Errorf("extend.boxShadow.glow missing: %v", cfg.Theme.Shadows)
	}
	// ...and the built-in scale must survive.
	if cfg.Theme.Radii["full"] != "9999px" {
		t.Errorf("extend.borderRadius wiped the default scale: %v", cfg.Theme.Radii)
	}
	if cfg.Theme.Shadows["md"] == "" {
		t.Errorf("extend.boxShadow wiped the default scale: %v", cfg.Theme.Shadows)
	}
}

func TestConfigNestedColors(t *testing.T) {
	dir := t.TempDir()
	cfgPath := dir + "/tailwind.config.js"
	content := "module.exports = { theme: { extend: { colors: { brand: { DEFAULT: '#f00', light: '#faa' } } } } }"
	if err := writeTestFile(cfgPath, content); err != nil {
		t.Fatal(err)
	}
	cfg, ok := ParseTailwindConfigStatic(cfgPath)
	if !ok {
		t.Fatal("static parse failed")
	}
	brand := cfg.Theme.Colors["brand"]
	if brand == nil || brand["DEFAULT"] != "#f00" || brand["light"] != "#faa" {
		t.Errorf("nested colors mis-parsed: %v", cfg.Theme.Colors["brand"])
	}
}

func TestConfigIdentifierColorReference(t *testing.T) {
	dir := t.TempDir()
	cfgPath := dir + "/tailwind.config.js"
	content := "module.exports = { theme: { extend: { colors: { brand: colors.blue } } } }"
	if err := writeTestFile(cfgPath, content); err != nil {
		t.Fatal(err)
	}
	// Identifier references cannot be resolved statically; the parse must not
	// crash and must not emit a bogus color entry.
	cfg, ok := ParseTailwindConfigStatic(cfgPath)
	if !ok {
		t.Fatal("static parse failed")
	}
	if b, exists := cfg.Theme.Colors["brand"]; exists {
		if b["DEFAULT"] == "" {
			t.Errorf("identifier reference produced an empty color")
		}
	}
}

func TestConfigDarkModeArray(t *testing.T) {
	dir := t.TempDir()
	cfgPath := dir + "/tailwind.config.js"
	content := "module.exports = { darkMode: ['class', '.dark-mode'] }"
	if err := writeTestFile(cfgPath, content); err != nil {
		t.Fatal(err)
	}
	cfg, ok := ParseTailwindConfigStatic(cfgPath)
	if !ok {
		t.Fatal("static parse failed")
	}
	if cfg.Theme.DarkMode != "class" {
		t.Errorf("darkMode array form not parsed: %q", cfg.Theme.DarkMode)
	}
}

func writeTestFile(path, content string) error {
	return os.WriteFile(path, []byte(content), 0644)
}
