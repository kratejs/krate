package css

import (
	"os"
	"path/filepath"
	"testing"
)

// liveSheet builds a throwaway project with the given CSS and page source and
// returns the generated stylesheet.
func liveSheet(t *testing.T, cssText, page string) string {
	t.Helper()
	root := t.TempDir()
	src := filepath.Join(root, "src")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "app.css"), []byte(cssText), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "page.tsx"), []byte(page), 0o644); err != nil {
		t.Fatal(err)
	}
	opts := TailwindOptions{Preflight: true}
	res, err := GenerateTailwindDirectives(root, nil, opts, ScanTailwindDirectives(root, opts))
	if err != nil {
		t.Fatal(err)
	}
	return res.CSS
}

func TestThemeVariablesStayLiveInUtilities(t *testing.T) {
	sheet := liveSheet(t, `@theme {
  --color-primary: #123456;
  --radius-card: 12px;
  --spacing-gutter: 3rem;
  --text-huge: 3rem;
  --text-huge--line-height: 1.1;
  --leading-loose2: 3;
  --tracking-mega: 1em;
  --font-display: Georgia;
  --font-weight-mega: 850;
  --container-page: 50rem;
  --animate-wiggle: wiggle 1s infinite;
  @keyframes wiggle { to { opacity: 0 } }
}`, `<i class="bg-primary bg-primary/50 fill-primary rounded-card p-gutter -m-gutter text-huge leading-loose2 -tracking-mega font-display font-mega max-w-page @page:p-1 animate-wiggle"/>`)
	wantAll(t, "sheet", sheet,
		"--color-primary:#123456",
		"background-color: var(--color-primary);",
		"background-color: color-mix(in oklab, var(--color-primary) 50%, transparent);",
		"fill: var(--color-primary);",
		"border-radius: var(--radius-card);",
		"padding: var(--spacing-gutter);",
		"margin: calc(var(--spacing-gutter) * -1);",
		"font-size: var(--text-huge); line-height: var(--tw-leading, var(--text-huge--line-height));",
		"line-height: var(--leading-loose2);",
		"letter-spacing: calc(var(--tracking-mega) * -1);",
		"font-family: var(--font-display);",
		"font-weight: var(--font-weight-mega);",
		"max-width: var(--container-page);",
		"animation: var(--animate-wiggle);",
		"@keyframes wiggle",
		// Container queries cannot read var(), so the width stays inlined.
		"50rem",
	)
	wantNone(t, "sheet", sheet, "@container (min-width: var(")
}

func TestThemeSpacingUnitStaysLive(t *testing.T) {
	sheet := liveSheet(t, `@theme { --spacing: 0.3rem; }`, `<i class="p-4 -mt-2 w-8 p-0 p-px"/>`)
	wantAll(t, "sheet", sheet,
		"--spacing:0.3rem",
		"padding: calc(var(--spacing) * 4);",
		"margin-top: calc(var(--spacing) * -2);",
		"width: calc(var(--spacing) * 8);",
		"padding: 1px;",
	)
	wantNone(t, "sheet", sheet, "1.2rem", "-calc(")
}

func TestThemeInlineAndReferenceStayInlined(t *testing.T) {
	sheet := liveSheet(t, `@theme inline { --color-a: #111111; }
@theme reference { --color-b: #222222; }
.dark { --color-a: #333333; --color-b: #444444; }`, `<i class="bg-a bg-b"/>`)
	wantAll(t, "sheet", sheet, "background-color: #111111;", "background-color: #222222;")
	wantNone(t, "sheet", sheet, "var(--color-a)", "var(--color-b)")
}

// Defaults stay inlined until the project sets one, so unthemed sites pay
// nothing for the runtime path.
func TestDefaultThemeVariablesGoLiveOnlyWhenSet(t *testing.T) {
	const page = `<i class="bg-blue-600 bg-red-500 p-4 text-lg rounded-lg font-sans tracking-tight max-w-md shadow-md"/>`
	sheet := liveSheet(t, `.x { color: red; }`, page)
	wantNone(t, "sheet", sheet, "var(--color-blue-600)", "var(--spacing)", "var(--text-lg)", "@layer theme{")

	sheet = liveSheet(t, `.rebrand { --color-blue-600: red; --spacing: 2px; --text-lg: 2rem; --radius-lg: 1px; --font-sans: serif; --tracking-tight: 1px; --container-md: 9px; --shadow-md: none; }`, page)
	wantAll(t, "sheet", sheet,
		"--color-blue-600:oklch(",
		"--spacing:0.25rem",
		"--text-lg:1.125rem",
		"--tracking-tight:",
		"--container-md:28rem",
		"background-color: var(--color-blue-600);",
		"padding: calc(var(--spacing) * 4);",
		"font-size: var(--text-lg);",
		"border-radius: var(--radius-lg);",
		"font-family: var(--font-sans);",
		"html,:host{line-height:1.5;-webkit-text-size-adjust:100%;tab-size:4;font-family:var(--font-sans)",
		"letter-spacing: var(--tracking-tight);",
		"max-width: var(--container-md);",
	)
	// v4 inlines shadows, and red-500 was not set.
	wantNone(t, "sheet", sheet, "var(--shadow-md)", "var(--color-red-500)")
}

func TestDefaultThemeVariablesSetFromSource(t *testing.T) {
	for name, page := range map[string]string{
		"style object": `<i class="bg-blue-600" style={{ "--color-blue-600": "red" }}/>`,
		"setProperty":  `el.setProperty('--color-blue-600', 'red'); const c = "bg-blue-600";`,
		"style string": `<i class="bg-blue-600" style="--color-blue-600: red"/>`,
		"class":        `<i class="bg-blue-600 [--color-blue-600:red]"/>`,
	} {
		t.Run(name, func(t *testing.T) {
			wantAll(t, name, liveSheet(t, `.x{}`, page), "background-color: var(--color-blue-600);", "--color-blue-600:oklch(")
		})
	}
}

func TestParenShorthandDefinesThemeVariable(t *testing.T) {
	sheet := liveSheet(t, `.x{}`, `<i class="bg-(--color-blue-500) text-(--text-lg)"/>`)
	wantAll(t, "sheet", sheet, "--color-blue-500:oklch(", "--text-lg:1.125rem")
}

func TestSvgAndPlaceholderColorOpacity(t *testing.T) {
	g := NewTailwindGenerator()
	for cls, want := range map[string]string{
		"fill-amber-300/50":        "fill: color-mix(in oklab, oklch(87.9% 0.169 91.605) 50%, transparent);",
		"stroke-indigo-500/20":     "stroke: color-mix(in oklab, oklch(58.5% 0.233 277.117) 20%, transparent);",
		"placeholder-amber-300/50": "color: color-mix(in oklab, oklch(87.9% 0.169 91.605) 50%, transparent);",
		"fill-amber-300":           "fill: oklch(87.9% 0.169 91.605);",
		"stroke-2":                 "stroke-width: 2;",
	} {
		if got := generateCSS(cls, g.Theme); got != want {
			t.Errorf("%s = %q, want %q", cls, got, want)
		}
	}
}
