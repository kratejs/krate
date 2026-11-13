package gotsgen

import (
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
