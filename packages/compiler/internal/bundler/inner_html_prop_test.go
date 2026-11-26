package bundler

import (
	"strings"
	"testing"
)

func TestInnerHTMLPropLowersToDangerouslySetInnerHTML(t *testing.T) {
	out, diags := rewriteDiag(t, `export default function Page() { const html = "<b>x</b>"; return <div innerHTML={html} />; }`)
	if len(diags) != 0 {
		t.Fatalf("diags: %v", diags)
	}
	mustContain(t, out, `dangerouslySetInnerHTML={{ __html: html }}`)
	if strings.Contains(out, "innerHTML=") {
		t.Fatalf("innerHTML attribute kept:\n%s", out)
	}
}

func TestInnerHTMLPropOnComponentIsLeftAlone(t *testing.T) {
	out, _ := rewriteDiag(t, `export default function Page() { return <Widget innerHTML={x} textContent={y} />; }`)
	mustContain(t, out, `innerHTML={x}`, `textContent={y}`)
}

func TestTextContentPropLowersToChild(t *testing.T) {
	out, diags := rewriteDiag(t, `export default function Page() { return <p textContent={msg} />; }`)
	if len(diags) != 0 {
		t.Fatalf("diags: %v", diags)
	}
	mustContain(t, out, `<p>{msg}</p>`)
}

func TestInnerHTMLWithChildrenIsAnError(t *testing.T) {
	for _, src := range []string{
		`export default function Page() { return <div innerHTML={h}>x</div>; }`,
		`export default function Page() { return <div textContent={h}><i /></div>; }`,
	} {
		_, diags := rewriteDiag(t, src)
		if len(diags) != 1 || diags[0].IsWarning() || !strings.Contains(diags[0].Message, "both") {
			t.Fatalf("want one error for %q, got %v", src, diags)
		}
	}
}
