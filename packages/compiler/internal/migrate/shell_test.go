package migrate

import (
	"strings"
	"testing"
)

func TestStripDocumentShellCarriesAttributes(t *testing.T) {
	src := `export default function RootLayout({ children }: { children: any }) {
  return (
    <html lang="en" className="dark" suppressHydrationWarning>
      <body className={cn("font-sans", dark ? "a>b" : 'c')} data-x="1">
        {children}
      </body>
    </html>
  );
}`
	out, ok := stripDocumentShell(src)
	if !ok {
		t.Fatal("shell not rewritten")
	}
	for _, want := range []string{
		`<Head>`,
		`<html lang="en" className="dark" />`,
		`<body className={cn("font-sans", dark ? "a>b" : 'c')} data-x="1" />`,
		`{children}`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "suppressHydrationWarning") || strings.Contains(out, "</body>") || strings.Contains(out, "</html>") {
		t.Errorf("leftovers of the shell:\n%s", out)
	}
}

func TestStripDocumentShellWithoutAttributesAddsNoHead(t *testing.T) {
	out, ok := stripDocumentShell(`export default function L({ children }) { return <html><body>{children}</body></html>; }`)
	if !ok || strings.Contains(out, "<Head>") || strings.Contains(out, "<html") {
		t.Fatalf("ok=%v:\n%s", ok, out)
	}
}
