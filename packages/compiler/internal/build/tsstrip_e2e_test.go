package build

import (
	"strings"
	"testing"
)

// TestBuildTypeScriptEnumAndNestedGenerics is the regression for pages whose
// TypeScript Krate's own parser could not skip: an enum plus a nested generic
// annotation used to build "successfully" into an empty root.
func TestBuildTypeScriptEnumAndNestedGenerics(t *testing.T) {
	src := `enum Color { Red = "red" }
const lookup: Map<string, Set<string>> = new Map();

interface Props { title?: string }

export default function Page(props: Props) {
  const label = "Typed page" as const;
  return (
    <main class="typed">
      <h1>{label}</h1>
      <p>Enums and generics are stripped: {Color.Red}</p>
    </main>
  );
}
`
	html, _, err := buildPageSrc(t, src)
	if err != nil {
		t.Fatalf("BuildAll: %v", err)
	}
	for _, want := range []string{"Typed page", "Enums and generics are stripped: red", "typed"} {
		if !strings.Contains(html, want) {
			t.Errorf("rendered page lacks %q:\n%.1500s", want, html)
		}
	}
}

// TestBuildTypeScriptSyntaxErrorLocation checks that a TypeScript syntax error
// fails the build and points at the original file:line:col.
func TestBuildTypeScriptSyntaxErrorLocation(t *testing.T) {
	src := "interface A { a: number }\n" +
		"\n" +
		"const broken: = 1;\n" +
		"export default function Page() { return <p>x</p>; }\n"
	_, _, err := buildPageSrc(t, src)
	if err == nil {
		t.Fatal("expected the build to fail on a TypeScript syntax error")
	}
	if !strings.Contains(err.Error(), "index.tsx:3:15") {
		t.Errorf("error does not point at the original location index.tsx:3:15:\n%v", err)
	}
	var found bool
	for _, d := range DiagnosticsFromError(err) {
		if d.Line == 3 && d.Col == 15 && strings.HasSuffix(d.File, "index.tsx") {
			found = true
			if d.Source != "const broken: = 1;" {
				t.Errorf("diagnostic source = %q", d.Source)
			}
		}
	}
	if !found {
		t.Errorf("no structured diagnostic at 3:15 in %+v", DiagnosticsFromError(err))
	}
}
