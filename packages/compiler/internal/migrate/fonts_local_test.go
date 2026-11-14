package migrate

import (
	"strings"
	"testing"
)

func fontCSS(t *testing.T, rep *Report) string {
	t.Helper()
	f := findPlan(rep, nextFontCSS)
	if f == nil {
		t.Fatalf("no font CSS planned")
	}
	return string(f.Content)
}

func TestNextFontLocalBecomesFontFace(t *testing.T) {
	root := gapsProject(t, map[string]string{
		"app/page.tsx": `export default function Page() { return <p>hi</p>; }`,
		"app/layout.tsx": `import localFont from "next/font/local";
const brand = localFont({
  src: [
    { path: "./fonts/Brand-Regular.woff2", weight: "400", style: "normal" },
    { path: "./fonts/Brand-Italic.woff2", weight: "400", style: "italic" },
  ],
  variable: "--font-brand",
  display: "optional",
  adjustFontFallback: false,
  declarations: [{ prop: "ascent-override", value: "90%" }],
});
const mono = localFont({ src: "./fonts/Mono.woff2", weight: "100 900", fallback: ["Courier New", "monospace"] });
export default function RootLayout({ children }: { children: any }) {
  return <html><body className={brand.className + " " + brand.variable + " " + mono.className}>{children}</body></html>;
}
`,
		"app/fonts/Brand-Regular.woff2": "regular",
		"app/fonts/Brand-Italic.woff2":  "italic",
		"app/fonts/Mono.woff2":          "mono",
	})
	rep, err := ScanNext(root)
	if err != nil {
		t.Fatal(err)
	}
	layout := string(findPlan(rep, "src/pages/_layout.tsx").Content)
	if strings.Contains(layout, "next/font/local") || strings.Contains(layout, "localFont(") {
		t.Errorf("localFont call left:\n%s", layout)
	}
	for _, want := range []string{`className: "krate-local-font-brand"`, `variable: "krate-local-font-brand-variable"`, `fontFamily: "\"Brand Regular\", ui-sans-serif`, "next-fonts.css"} {
		if !strings.Contains(layout, want) {
			t.Errorf("layout missing %q:\n%s", want, layout)
		}
	}
	css := fontCSS(t, rep)
	for _, want := range []string{
		`src: url("/migrated-fonts/Brand-Regular.woff2") format("woff2");`,
		"font-style: italic;",
		"font-display: optional;",
		"ascent-override: 90%;",
		`.krate-local-font-brand-variable { --font-brand: "Brand Regular"`,
		`font-weight: 100 900;`,
		`"Courier New", monospace`,
	} {
		if !strings.Contains(css, want) {
			t.Errorf("css missing %q:\n%s", want, css)
		}
	}
	if f := findPlan(rep, "public/migrated-fonts/Brand-Regular.woff2"); f == nil || string(f.Content) != "regular" {
		t.Errorf("font file not copied: %+v", f)
	}
	for _, m := range rep.Manual {
		if strings.Contains(m.Reason, "font") {
			t.Errorf("unexpected manual item: %+v", m)
		}
	}
}

func TestNextFontLocalNonLiteralIsFlagged(t *testing.T) {
	root := gapsProject(t, map[string]string{
		"app/page.tsx": `import localFont from "next/font/local";
import file from "./x.woff2";
const f = localFont({ src: file });
export default function Page() { return <p className={f.className}>hi</p>; }`,
		"app/x.woff2": "x",
	})
	rep, err := ScanNext(root)
	if err != nil {
		t.Fatal(err)
	}
	if !hasManualReason(rep, "app/page.tsx", "next/font/local call could not be migrated") {
		t.Errorf("want a manual item: %+v", rep.Manual)
	}
}

func TestGeistFontsMapToShippedFiles(t *testing.T) {
	root := gapsProject(t, map[string]string{
		"app/page.tsx": `export default function Page() { return <p>hi</p>; }`,
		"app/layout.tsx": `import { GeistSans } from "geist/font/sans";
import { GeistMono as Mono } from "geist/font/mono";
export default function RootLayout({ children }: { children: any }) {
  return <html><body className={GeistSans.variable + " " + Mono.variable}>{children}</body></html>;
}
`,
		"node_modules/geist/dist/fonts/geist-sans/Geist-Variable.woff2":     "sans",
		"node_modules/geist/dist/fonts/geist-mono/GeistMono-Variable.woff2": "mono",
	})
	rep, err := ScanNext(root)
	if err != nil {
		t.Fatal(err)
	}
	layout := string(findPlan(rep, "src/pages/_layout.tsx").Content)
	for _, want := range []string{"const GeistSans = {", "const Mono = {", `variable: "krate-local-font-geist-sans-variable"`} {
		if !strings.Contains(layout, want) {
			t.Errorf("layout missing %q:\n%s", want, layout)
		}
	}
	if strings.Contains(layout, "geist/font") {
		t.Errorf("geist import left:\n%s", layout)
	}
	css := fontCSS(t, rep)
	for _, want := range []string{`font-family: "Geist";`, `font-family: "Geist Mono";`, "--font-geist-sans:", "--font-geist-mono:", "ui-monospace", `"Roboto Mono"`} {
		if !strings.Contains(css, want) {
			t.Errorf("css missing %q:\n%s", want, css)
		}
	}
	if findPlan(rep, "public/migrated-fonts/Geist-Variable.woff2") == nil || findPlan(rep, "public/migrated-fonts/GeistMono-Variable.woff2") == nil {
		t.Errorf("geist font files not planned")
	}
}

func TestFontParserRejectsComputedOptions(t *testing.T) {
	if _, err := parseLocalFontCall(`{ src: "./a.woff2", ...rest }`); err == nil {
		t.Error("spread must be rejected")
	}
	spec, err := parseLocalFontCall(`{ src: "./a.woff2", weight: 400, // note
 variable: '--font-a', }`)
	if err != nil || spec.weight != "400" || spec.variable != "--font-a" {
		t.Errorf("literal parse: %+v %v", spec, err)
	}
}
