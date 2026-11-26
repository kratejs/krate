package migrate

import (
	"strings"
	"testing"
)

const googleLayout = `import { Noto_Sans_Display, Geist_Mono as Mono, Inter } from "next/font/google";
const noto = Noto_Sans_Display({ variable: "--font-body", weight: ["600", "400"], style: ["normal", "italic"], subsets: ["latin"] });
const mono = Mono({ variable: "--font-code", subsets: ["latin"], display: "optional" });
const inter = Inter({ subsets: ["latin"] });
export default function RootLayout({ children }: { children: any }) {
  return <html><body className={noto.variable + " " + mono.variable + " " + inter.className}>{children}</body></html>;
}
`

func TestNextFontGoogleGoldenCSS(t *testing.T) {
	root := gapsProject(t, map[string]string{
		"app/page.tsx":   `export default function Page() { return <p>hi</p>; }`,
		"app/layout.tsx": googleLayout,
	})
	rep, err := ScanNext(root)
	if err != nil {
		t.Fatal(err)
	}
	want := `@import url("https://fonts.googleapis.com/css2?family=Noto+Sans+Display:ital,wght@0,400;0,600;1,400;1,600&display=swap");
@import url("https://fonts.googleapis.com/css2?family=Geist+Mono&display=optional");
@import url("https://fonts.googleapis.com/css2?family=Inter&display=swap");

/* Google fonts loaded by the migration helper. */
.krate-font-body { font-family: "Noto Sans Display", ui-sans-serif, system-ui, sans-serif; }
.krate-font-body-variable { --font-body: "Noto Sans Display", ui-sans-serif, system-ui, sans-serif; }
.krate-font-code { font-family: "Geist Mono", ui-monospace, SFMono-Regular, Menlo, monospace; }
.krate-font-code-variable { --font-code: "Geist Mono", ui-monospace, SFMono-Regular, Menlo, monospace; }
.krate-font-inter { font-family: "Inter", ui-sans-serif, system-ui, sans-serif; }
`
	if got := fontCSS(t, rep); got != want {
		t.Errorf("golden css mismatch:\n got:\n%s\nwant:\n%s", got, want)
	}
	got := fontCSS(t, rep)
	if strings.Contains(got, "family=Noto_Sans") || strings.Contains(got, `"Noto_Sans`) {
		t.Errorf("underscores leaked into css:\n%s", got)
	}
	layout := string(findPlan(rep, "src/pages/_layout.tsx").Content)
	for _, bad := range []string{"Noto_Sans_Display(", "Mono(", "Inter("} {
		if strings.Contains(layout, bad) {
			t.Errorf("call %s left in layout:\n%s", bad, layout)
		}
	}
	for _, w := range []string{`variable: "krate-font-body-variable"`, `fontFamily: "\"Noto Sans Display\"`} {
		if !strings.Contains(layout, w) {
			t.Errorf("layout missing %q:\n%s", w, layout)
		}
	}
}

func TestNextFontGoogleImportsFirstEvenWithLocalFonts(t *testing.T) {
	root := gapsProject(t, map[string]string{
		"app/page.tsx": `export default function Page() { return <p>hi</p>; }`,
		"app/layout.tsx": `import localFont from "next/font/local";
import { Roboto } from "next/font/google";
const brand = localFont({ src: "./fonts/Brand.woff2", variable: "--font-brand" });
const roboto = Roboto({ weight: "700", subsets: ["latin"] });
export default function RootLayout({ children }: { children: any }) {
  return <html><body className={brand.variable + " " + roboto.className}>{children}</body></html>;
}
`,
		"app/fonts/Brand.woff2": "brand",
	})
	rep, err := ScanNext(root)
	if err != nil {
		t.Fatal(err)
	}
	css := fontCSS(t, rep)
	if !strings.HasPrefix(css, "@import url(") {
		t.Errorf("css must start with @import:\n%s", css)
	}
	if i, j := strings.LastIndex(css, "@import"), strings.Index(css, "@font-face"); j >= 0 && i > j {
		t.Errorf("@import after @font-face:\n%s", css)
	}
	if !strings.Contains(css, "family=Roboto:wght@700&display=swap") {
		t.Errorf("weight axis missing:\n%s", css)
	}
}

func TestNextFontGoogleNonLiteralOptionsKeepHelper(t *testing.T) {
	root := gapsProject(t, map[string]string{
		"app/page.tsx": `export default function Page() { return <p>hi</p>; }`,
		"app/layout.tsx": `import { Open_Sans } from "next/font/google";
const opts = { subsets: ["latin"] };
const f = Open_Sans(opts);
export default function RootLayout({ children }: { children: any }) {
  return <html><body className={f.className}>{children}</body></html>;
}
`,
	})
	rep, err := ScanNext(root)
	if err != nil {
		t.Fatal(err)
	}
	css := fontCSS(t, rep)
	for _, w := range []string{`family=Open+Sans&display=swap`, `.krate-font-open-sans { font-family: "Open Sans"`, `.krate-font-open-sans-variable { --font-open-sans:`} {
		if !strings.Contains(css, w) {
			t.Errorf("css missing %q:\n%s", w, css)
		}
	}
	found := false
	for _, m := range rep.Manual {
		if strings.Contains(m.Reason, "next/font/google") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected a manual item for non-literal options: %+v", rep.Manual)
	}
	helper := string(findPlan(rep, nextFontHelper).Content)
	if !strings.Contains(helper, `__krateFont("Open Sans")`) {
		t.Errorf("helper family not spaced:\n%s", helper)
	}
}

func TestNextFontGoogleWarningSaysRuntime(t *testing.T) {
	root := gapsProject(t, map[string]string{
		"app/page.tsx":   `export default function Page() { return <p>hi</p>; }`,
		"app/layout.tsx": googleLayout,
	})
	rep, err := ScanNext(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, w := range rep.Warnings {
		if strings.Contains(w, "fonts.googleapis.com at runtime") && strings.Contains(w, "self-host") {
			return
		}
	}
	t.Errorf("no runtime-fetch warning: %v", rep.Warnings)
}

func TestGoogleCSSURL(t *testing.T) {
	cases := []struct {
		spec googleFontSpec
		want string
	}{
		{googleFontSpec{family: "Inter"}, "https://fonts.googleapis.com/css2?family=Inter&display=swap"},
		{googleFontSpec{family: "Roboto Mono", weights: []string{"700", "400"}}, "https://fonts.googleapis.com/css2?family=Roboto+Mono:wght@400;700&display=swap"},
		{googleFontSpec{family: "Lora", styles: []string{"italic"}}, "https://fonts.googleapis.com/css2?family=Lora:ital@1&display=swap"},
		{googleFontSpec{family: "Lora", styles: []string{"normal", "italic"}}, "https://fonts.googleapis.com/css2?family=Lora:ital@0;1&display=swap"},
		{googleFontSpec{family: "Lora", weights: []string{"400"}, styles: []string{"italic"}, display: "block"}, "https://fonts.googleapis.com/css2?family=Lora:ital,wght@1,400&display=block"},
	}
	for _, c := range cases {
		if got := googleCSSURL(c.spec); got != c.want {
			t.Errorf("%+v:\n got %s\nwant %s", c.spec, got, c.want)
		}
	}
}
