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
