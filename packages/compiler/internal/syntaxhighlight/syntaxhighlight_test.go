package syntaxhighlight

import (
	"fmt"
	"strings"
	"testing"

	"github.com/alecthomas/chroma/v2/formatters/html"
	"github.com/alecthomas/chroma/v2/lexers"
	"github.com/alecthomas/chroma/v2/styles"
)

func TestIndentationPreserved(t *testing.T) {
	code := "function hello() {\n    console.log(\"world\");\n        console.log(\"deep indent\");\n}"
	fmt.Printf("INPUT:\n%s\n\n", code)

	l := lexers.Get("javascript")
	iter, err := l.Tokenise(nil, code)
	if err != nil {
		t.Fatal(err)
	}

	f := html.New(html.WithClasses(true))
	var buf strings.Builder
	f.Format(&buf, styles.Get("monokai"), iter)
	fmt.Printf("RAW CHROMA OUTPUT:\n%s\n", buf.String())

	// Also test our Highlight function
	hl := Highlight(code, "javascript")
	fmt.Printf("\nOUR Highlight() OUTPUT:\n%s\n", hl)

	// Check if spaces are preserved
	if !strings.Contains(hl, "    console") {
		t.Error("4-space indent NOT preserved")
	}
	if !strings.Contains(hl, "        console") {
		t.Error("8-space indent NOT preserved")
	}
}
