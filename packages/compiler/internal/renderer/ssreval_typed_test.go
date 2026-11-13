package renderer

import (
	"strings"
	"testing"
)

// TestPropStringIsNeverMarkup is the stored-XSS regression: a string prop that
// looks like markup must be escaped, because only JSX values are markup.
func TestPropStringIsNeverMarkup(t *testing.T) {
	cases := []string{
		`function Card(props) { return <h1>{props.title}</h1>; }
export default function Page() { return <Card title={"<img src=x onerror=alert(1)>"} />; }`,
		`function Card({ title }) { return <h1>{title}</h1>; }
export default function Page() { return <Card title="<img src=x onerror=alert(1)>" />; }`,
		`function Inner(props) { return <h1>{props.t}</h1>; }
function Card(props) { return <div><Inner t={props.title} /></div>; }
export default function Page() { return <Card title={"<img src=x onerror=alert(1)>"} />; }`,
	}
	for _, src := range cases {
		result, _ := fullPipeline(t, src)
		if strings.Contains(result.HTML, "<img") {
			t.Errorf("prop string injected as raw markup:\n%s", result.HTML)
		}
		if !strings.Contains(result.HTML, "&lt;img src=x onerror=alert(1)&gt;") {
			t.Errorf("expected escaped text, got:\n%s", result.HTML)
		}
	}
}
