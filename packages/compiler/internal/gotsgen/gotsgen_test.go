package gotsgen

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const cardGo = `package components

type CardProps struct {
	Title string   ` + "`json:\"title\"`" + `
	Count int      ` + "`json:\"count\"`" + `
	Items []string ` + "`json:\"items,omitempty\"`" + `
	Kids  HTML     ` + "`json:\"kids\"`" + `
}

// krate:component server
func Card(p CardProps) HTML { return "" }
`

func TestGenerate(t *testing.T) {
	ts, err := Generate("card.go", []byte(cardGo))
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	for _, want := range []string{
		"export interface CardProps {",
		"title: string;",
		"count: number;",
		"items?: string[];",
		"kids: JSX.Element;",
		"export declare const Card: (props: CardProps) => JSX.Element;",
	} {
		if !strings.Contains(ts, want) {
			t.Errorf("generated TS missing %q:\n%s", want, ts)
		}
	}
}

func TestGenerateAmbientModule(t *testing.T) {
	ts, err := GenerateAmbientModule("*card.go", "card.go", []byte(cardGo), nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`declare module "*card.go" {`,
		"export interface CardProps {",
		"export declare const Card: (props: CardProps) => JSX.Element;",
		"}",
	} {
		if !strings.Contains(ts, want) {
			t.Errorf("ambient module missing %q:\n%s", want, ts)
		}
	}
}

// TestCollectStructs verifies a props type declared in a sibling file resolves
// (instead of degrading to `unknown`).
func TestCollectStructs(t *testing.T) {
	dir := t.TempDir()
	props := "package c\n\ntype Shared struct {\n\tA string `json:\"a\"`\n}\n"
	use := "package c\n\ntype P struct {\n\tS Shared `json:\"s\"`\n}\n"
	if err := os.WriteFile(filepath.Join(dir, "props.go"), []byte(props), 0644); err != nil {
		t.Fatal(err)
	}
	usePath := filepath.Join(dir, "use.go")
	if err := os.WriteFile(usePath, []byte(use), 0644); err != nil {
		t.Fatal(err)
	}
	structs := CollectStructs(dir)
	if _, ok := structs["Shared"]; !ok {
		t.Fatalf("Shared not collected from sibling file")
	}
	ts, err := GenerateAmbientModule("*use.go", usePath, []byte(use), structs)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(ts, "s: Shared;") || !strings.Contains(ts, "export interface Shared") {
		t.Errorf("cross-file type not resolved:\n%s", ts)
	}
}

func TestParseFileTiers(t *testing.T) {
	comps, err := ParseFile("card.go", []byte(cardGo))
	if err != nil {
		t.Fatal(err)
	}
	if len(comps) != 1 || comps[0].Name != "Card" || comps[0].Tier != TierServer {
		t.Fatalf("components = %+v", comps)
	}
	if comps[0].PropsType != "CardProps" {
		t.Errorf("PropsType = %q", comps[0].PropsType)
	}

	// Suffix tier applies when there is no directive.
	runtimeSrc := `package c
type P struct{}
func Widget(p P) string { return "" }
func Echo(p P, r Request) string { return "" }
`
	comps, err = ParseFile("widget.runtime.go", []byte(runtimeSrc))
	if err != nil {
		t.Fatal(err)
	}
	if len(comps) != 2 {
		t.Fatalf(".runtime.go components = %+v", comps)
	}
	byName := map[string]Component{}
	for _, c := range comps {
		byName[c.Name] = c
	}
	if byName["Widget"].Tier != TierRuntime {
		t.Errorf("Widget tier = %v", byName["Widget"].Tier)
	}
	if byName["Echo"].HasRequest != true {
		t.Errorf("Echo should report HasRequest")
	}
	if byName["Widget"].HasRequest {
		t.Errorf("Widget should not report HasRequest")
	}
}

func TestNonComponentFileIgnored(t *testing.T) {
	src := `package c
func helper() {}
func Exported(p int) string { return "" }
`
	comps, err := ParseFile("util.go", []byte(src))
	if err != nil {
		t.Fatal(err)
	}
	if len(comps) != 0 {
		t.Errorf("expected no components, got %+v", comps)
	}
}

// TestNodeReturnAndField verifies a kr.Node return is treated as markup and a
// kr.Node props field maps to JSX.Element.
func TestNodeReturnAndField(t *testing.T) {
	src := "package c\n\n" +
		"import kr \"example.com/kr\"\n\n" +
		"type P struct {\n\tInner kr.Node `json:\"inner\"`\n}\n\n" +
		"// krate:component server\n" +
		"func W(p P) kr.Node { return nil }\n"
	comps, err := ParseFile("w.go", []byte(src))
	if err != nil {
		t.Fatal(err)
	}
	if len(comps) != 1 || !comps[0].ResultHTML {
		t.Fatalf("Node return not treated as markup: %+v", comps)
	}
	ts, err := Generate("w.go", []byte(src))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(ts, "inner: JSX.Element;") {
		t.Errorf("kr.Node field not mapped:\n%s", ts)
	}
}
