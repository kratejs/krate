package css

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestExpandApplyAnywhereInRule(t *testing.T) {
	theme := DefaultTailwindTheme()
	out, un := ExpandApply(".btn { @apply px-4 py-2; }\n.b2{@apply font-bold;color:red}\n.c{\n  color: blue; @apply p-2 }", theme)
	if len(un) != 0 {
		t.Fatalf("unresolved: %v", un)
	}
	if strings.Contains(out, "@apply") {
		t.Fatalf("@apply left in output:\n%s", out)
	}
	for _, want := range []string{"padding-inline", "font-weight", "color:red", "color: blue", "padding"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
}

func TestExpandApplyAnimationAndOrder(t *testing.T) {
	g := NewTailwindGenerator()
	out, un := g.ExpandApply(".s { @apply animate-spin; }")
	if len(un) != 0 || strings.Contains(out, "@apply") || !strings.Contains(out, "animation") {
		t.Fatalf("animate-spin not expanded: %q %v", out, un)
	}
	out, _ = g.ExpandApply(".a { @apply duration-300 transition; }")
	if strings.Contains(out, "@apply") {
		t.Fatalf("not expanded: %s", out)
	}
}

func TestExpandApplyUnresolvedReported(t *testing.T) {
	out, un := ExpandApply(".a { @apply hover:underline flex nope-nope-zz; }", DefaultTailwindTheme())
	if len(un) != 1 || un[0] != "nope-nope-zz" || !strings.Contains(out, "display") || !strings.Contains(out, "&:hover") {
		t.Fatalf("out=%q un=%v", out, un)
	}
}

func TestParseAtThemeNestedBraces(t *testing.T) {
	src := `@theme{ --animate-fadein: fadein 1s; @keyframes fadein{from{opacity:0}to{opacity:1}} --color-brand-500:#f00; }
.x{color:red}`
	at := ParseAtTheme(src)
	theme := DefaultTailwindTheme()
	at.Apply(&theme)
	if theme.Animation["fadein"] != "fadein 1s" {
		t.Errorf("animation = %v", theme.Animation)
	}
	if kf := theme.Keyframes["fadein"]; !strings.Contains(kf, "from{opacity:0}") || !strings.Contains(kf, "to{opacity:1}") {
		t.Errorf("keyframes = %q", kf)
	}
	if v, ok := colorValue("brand-500", "", theme); !ok || v != "#f00" {
		t.Errorf("brand-500 = %q %v (parse stopped at first })", v, ok)
	}
	if out := StripTailwindDirectives(src); strings.Contains(out, "@theme") || !strings.Contains(out, ".x{color:red}") {
		t.Errorf("strip: %q", out)
	}
}

func TestAtThemeNamespaces(t *testing.T) {
	src := `@theme {
  --spacing: 0.3rem;
  --text-xl: 1.4rem;
  --text-xl--line-height: 2;
  --text-huge: 5rem;
  --tracking-wide2: 0.1em;
  --container-8xl: 90rem;
  --font-display: "Inter", sans-serif;
  --font-weight-heavy: 950;
  --leading-loose2: 2.2;
  --radius-4xl: 2rem;
  --shadow-glow: 0 0 4px red;
  --breakpoint-3xl: 120rem;
  --ease-snappy: cubic-bezier(0.2,0,0,1);
}`
	at := ParseAtTheme(src)
	th := DefaultTailwindTheme()
	at.Apply(&th)
	checks := map[string]string{
		"spacing":    th.SpacingUnit,
		"text-xl":    th.TextSizes["xl"],
		"huge":       th.TextSizes["huge"],
		"tracking":   th.Tracking["wide2"],
		"container":  th.Containers["8xl"],
		"font":       th.FontFamily["display"],
		"weight":     th.FontWeights["heavy"],
		"leading":    th.LineHeight["loose2"],
		"radius":     th.Radii["4xl"],
		"shadow":     th.Shadows["glow"],
		"breakpoint": th.Screens["3xl"],
	}
	want := map[string]string{
		"spacing": "0.3rem", "text-xl": "font-size: 1.4rem; line-height: 2;", "huge": "font-size: 5rem;",
		"tracking": "0.1em", "container": "90rem", "font": `"Inter", sans-serif`, "weight": "950",
		"leading": "2.2", "radius": "2rem", "shadow": "0 0 4px red", "breakpoint": "120rem",
	}
	for k, w := range want {
		if checks[k] != w {
			t.Errorf("%s = %q, want %q", k, checks[k], w)
		}
	}
	found := false
	for _, w := range at.Warnings {
		if strings.Contains(w, "ease") {
			found = true
		}
	}
	if !found {
		t.Errorf("no warning for unsupported --ease-* namespace: %v", at.Warnings)
	}
	if v, ok := themeVarValue(&th, "text-xl--line-height"); !ok || v != "2" {
		t.Errorf("text-xl--line-height var = %q %v", v, ok)
	}
}

func TestAtThemeReset(t *testing.T) {
	th := DefaultTailwindTheme()
	ParseAtTheme(`@theme { --color-*: initial; --color-brand: #123; }`).Apply(&th)
	if _, ok := colorValue("red-500", "", th); ok {
		t.Error("red-500 survived --color-*: initial")
	}
	if v, ok := colorValue("brand", "", th); !ok || v != "#123" {
		t.Errorf("brand = %q %v", v, ok)
	}
	th = DefaultTailwindTheme()
	ParseAtTheme(`@theme { --*: initial; --color-x: #fff; }`).Apply(&th)
	if len(th.Screens) != 0 || len(th.Radii) != 0 {
		t.Errorf("--*: initial did not reset namespaces: %v %v", th.Screens, th.Radii)
	}
}

func TestAtThemeReferenceNotEmitted(t *testing.T) {
	at := ParseAtTheme(`@theme reference { --color-ghost: #abc; } @theme static { --color-real: #def; }`)
	th := DefaultTailwindTheme()
	at.Apply(&th)
	vars := themeLayerVars(&th, at, nil)
	if len(vars) != 1 || vars[0].Name != "color-real" {
		t.Errorf("vars = %v", vars)
	}
	if v, ok := colorValue("ghost", "", th); !ok || v != "#abc" {
		t.Errorf("reference var not usable by utilities")
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestGenerateLayeredAndThemeVars(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "src/p.tsx"), `<div class="bg-brand-500 p-4 flex">Don't</div>`)
	writeFile(t, filepath.Join(root, "src/g.css"), `@import "tailwindcss";
@theme { --color-brand-500: #f00; }
.x { background: var(--color-red-500); }`)
	opts := TailwindOptions{Preflight: true}
	d := ScanTailwindDirectives(root, opts)
	res, err := GenerateTailwindDirectives(root, LoadTailwindConfigWithOptions(root, opts), opts, d)
	if err != nil {
		t.Fatal(err)
	}
	out := res.CSS
	order := []string{"@layer theme, base, components, utilities;", "@layer theme{:root,:host{", "--color-brand-500:#f00", "--color-red-500:", "@layer base{", "@layer utilities{"}
	last := -1
	for _, o := range order {
		i := strings.Index(out, o)
		if i < 0 || i < last {
			t.Fatalf("%q missing or out of order in:\n%s", o, out)
		}
		last = i
	}
	if !strings.Contains(out, "background-color: var(--color-brand-500)") {
		t.Errorf("bg-brand-500 not generated from @theme:\n%s", out)
	}
	if strings.Index(out, "box-sizing") > strings.Index(out, "@layer utilities{") {
		t.Errorf("preflight must sit in base layer before utilities")
	}
	// Determinism.
	res2, _ := GenerateTailwindDirectives(root, LoadTailwindConfigWithOptions(root, opts), opts, ScanTailwindDirectives(root, opts))
	if res2.CSS != out {
		t.Error("output not deterministic")
	}
}

func TestDirectivesImportPrefixSourceUtilityVariant(t *testing.T) {
	root := t.TempDir()
	css := `@import "tailwindcss" prefix(tw) important;
@import "tailwindcss/preflight";
@source "../other";
@source not "../skip";
@source inline("p-{1,2} underline");
@config "./x.js";
@plugin "@tailwindcss/typography";
@utility tab-4 { tab-size: 4; }
@utility tab-* { tab-size: --value(integer); }
@custom-variant theme-mid (&:where([data-theme=mid] *));
@custom-variant print2 (@media print);
@custom-variant hov { &:hover { @slot; } }
.keep{color:red}`
	d := NewTailwindDirectives(root)
	d.Add(css, filepath.Join(root, "src"))
	if d.Prefix != "tw" || !d.Important || !d.Imported {
		t.Errorf("import modifiers: %+v", d)
	}
	if len(d.Sources) != 1 || d.Sources[0] != filepath.Join(root, "other") || len(d.Excludes) != 1 {
		t.Errorf("sources=%v excludes=%v", d.Sources, d.Excludes)
	}
	if strings.Join(d.Inline, " ") != "p-1 p-2 underline" {
		t.Errorf("inline = %v", d.Inline)
	}
	if d.Utilities["tab-4"] != "tab-size: 4;" || len(d.Utilities) != 1 {
		t.Errorf("utilities = %v", d.Utilities)
	}
	if d.Variants["theme-mid"] != "&:where([data-theme=mid] *)" || d.Variants["print2"] != "@media print" || d.Variants["hov"] != "&:hover" {
		t.Errorf("variants = %v", d.Variants)
	}
	w := strings.Join(d.Warnings(), "\n")
	if d.Functional["tab"] != "tab-size: --value(integer);" {
		t.Errorf("functional utilities = %v", d.Functional)
	}
	for _, want := range []string{"@config", "@plugin"} {
		if !strings.Contains(w, want) {
			t.Errorf("no warning mentioning %q:\n%s", want, w)
		}
	}
	out := StripTailwindDirectives(css)
	if strings.TrimSpace(out) != ".keep{color:red}" {
		t.Errorf("strip left: %q", out)
	}
	// Source outside the root is refused with a warning.
	d2 := NewTailwindDirectives(root)
	d2.Add(`@source "../../etc";`, root)
	if len(d2.Sources) != 0 || len(d2.Warnings()) == 0 {
		t.Errorf("outside-root source accepted: %v", d2.Sources)
	}
}

func TestCustomUtilityAndVariantGenerate(t *testing.T) {
	d := NewTailwindDirectives(t.TempDir())
	d.Add(`@utility tab-4 { tab-size: 4; }
@custom-variant theme-mid (&:where([data-theme=mid] *));
@custom-variant print2 (@media print);`, "")
	g := NewTailwindGenerator()
	d.ApplyTo(&g.Theme)
	out := g.Generate(map[string]bool{"tab-4": true, "hover:tab-4": true, "theme-mid:flex": true, "print2:hidden": true})
	for _, want := range []string{".tab-4{tab-size: 4;}", "tab-size", ":hover", ":where([data-theme=mid] *)", "@media print"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
}

func TestSourceNotExcludesAndInlineGenerate(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "src/a.tsx"), `<i class="underline">`)
	writeFile(t, filepath.Join(root, "ext/b.tsx"), `<i class="italic">`)
	writeFile(t, filepath.Join(root, "ext/skip/c.tsx"), `<i class="truncate">`)
	writeFile(t, filepath.Join(root, "src/g.css"), `@source "../ext"; @source not "../ext/skip"; @source inline("p-{1,2}");`)
	opts := TailwindOptions{}
	res, _ := GenerateTailwindDirectives(root, LoadTailwindConfigWithOptions(root, opts), opts, ScanTailwindDirectives(root, opts))
	for _, want := range []string{".underline", ".italic", ".p-1", ".p-2"} {
		if !strings.Contains(res.CSS, want) {
			t.Errorf("missing %s:\n%s", want, res.CSS)
		}
	}
	if strings.Contains(res.CSS, ".truncate") {
		t.Error("@source not did not exclude")
	}
}

func TestStrictReportsUnknown(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "src/a.tsx"), `<i class="flex totally-not-a-utility-xyz">`)
	opts := TailwindOptions{Strict: true}
	res, _ := GenerateTailwindDirectives(root, nil, opts, nil)
	if len(res.Unknown) != 1 || res.Unknown[0] != "totally-not-a-utility-xyz" {
		t.Errorf("Unknown = %v", res.Unknown)
	}
	opts.Strict = false
	res, _ = GenerateTailwindDirectives(root, nil, opts, nil)
	if len(res.Unknown) != 0 {
		t.Errorf("non-strict reported %v", res.Unknown)
	}
}

func TestConfigStaticAnimationKeyframesFontsDark(t *testing.T) {
	dir := t.TempDir()
	src := `export default {
  darkMode: ['class', '[data-mode="dark"]'],
  prefix: 'tw-',
  important: '#app',
  plugins: [require('x')],
  theme: {
    fontFamily: { display: ['Playfair Display', 'serif'], body: 'Inter, sans-serif' },
    screens: { md: { min: '768px', max: '1023px' }, lg: '1024px' },
    extend: {
      animation: { wiggle: 'wiggle 1s ease-in-out infinite' },
      keyframes: { wiggle: { '0%, 100%': { transform: 'rotate(-3deg)' }, '50%': { transform: 'rotate(3deg)' } } },
      letterSpacing: { tightest: '-0.075em' },
      fontSize: { tiny: ['0.5rem', { lineHeight: '1rem' }], big: ['3rem', '1'] },
    },
  },
}`
	path := filepath.Join(dir, "tailwind.config.ts")
	writeFile(t, path, src)
	cfg, ok := ParseTailwindConfigStatic(path)
	if !ok {
		t.Fatal("parse failed")
	}
	th := cfg.Theme
	if th.Animation["wiggle"] != "wiggle 1s ease-in-out infinite" {
		t.Errorf("animation = %v", th.Animation)
	}
	if kf := th.Keyframes["wiggle"]; kf != "0%, 100%{transform:rotate(-3deg);}50%{transform:rotate(3deg);}" {
		t.Errorf("keyframes = %q", kf)
	}
	if th.FontFamily["display"] != `"Playfair Display", serif` || th.FontFamily["body"] != "Inter, sans-serif" {
		t.Errorf("fontFamily = %v", th.FontFamily)
	}
	if th.Tracking["tightest"] != "-0.075em" {
		t.Errorf("tracking = %v", th.Tracking)
	}
	if th.TextSizes["tiny"] != "font-size: 0.5rem; line-height: 1rem;" || th.TextSizes["big"] != "font-size: 3rem; line-height: 1;" {
		t.Errorf("fontSize = %v %v", th.TextSizes["tiny"], th.TextSizes["big"])
	}
	if th.DarkMode != "class" || th.DarkSelector != `[data-mode="dark"]` {
		t.Errorf("dark = %q %q", th.DarkMode, th.DarkSelector)
	}
	if th.Prefix != "tw" || th.Important != "#app" {
		t.Errorf("prefix=%q important=%q", th.Prefix, th.Important)
	}
	if _, bad := th.Screens["min"]; bad || th.Screens["lg"] != "1024px" {
		t.Errorf("screens = %v", th.Screens)
	}
	w := strings.Join(cfg.Warnings, "\n")
	for _, want := range []string{"plugins", "screens.md"} {
		if !strings.Contains(w, want) {
			t.Errorf("no warning for %q:\n%s", want, w)
		}
	}
}

func TestConfigExecuteAnimationKeyframesDark(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "tailwind.config.ts"), `export default {
  darkMode: ['selector', '.x-dark'],
  prefix: 'tw',
  important: true,
  plugins: [() => {}],
  theme: {
    fontFamily: { display: ['Playfair Display', 'serif'] },
    extend: {
      colors: { brand: { 500: '#123456' } },
      animation: { wiggle: 'wiggle 1s ease-in-out infinite' },
      keyframes: { wiggle: { '0%, 100%': { transform: 'rotate(-3deg)' }, '50%': { transform: 'rotate(3deg)' } } },
      letterSpacing: { tightest: '-0.075em' },
      borderRadius: { blob: '40%' },
    },
  },
}`)
	cfg := LoadTailwindConfigWithOptions(dir, TailwindOptions{ExecuteConfig: true})
	th := cfg.Theme
	if th.Animation["wiggle"] == "" || !strings.Contains(th.Keyframes["wiggle"], "rotate(-3deg)") {
		t.Errorf("animation=%v keyframes=%v", th.Animation, th.Keyframes)
	}
	if th.Tracking["tightest"] != "-0.075em" || th.Radii["blob"] != "40%" || th.Radii["lg"] == "" {
		t.Errorf("tracking=%v radii=%v", th.Tracking, th.Radii)
	}
	if th.Colors["brand"]["500"] != "#123456" || th.Colors["red"]["500"] == "" {
		t.Errorf("colors extend not merged")
	}
	if th.FontFamily["display"] != `"Playfair Display", serif` {
		t.Errorf("fontFamily = %v", th.FontFamily)
	}
	if th.DarkMode != "selector" || th.DarkSelector != ".x-dark" || th.Prefix != "tw" || th.Important != "true" {
		t.Errorf("dark=%q/%q prefix=%q important=%q", th.DarkMode, th.DarkSelector, th.Prefix, th.Important)
	}
	if !strings.Contains(strings.Join(cfg.Warnings, "\n"), "plugins") {
		t.Errorf("no plugins warning: %v", cfg.Warnings)
	}
}
