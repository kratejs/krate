package renderer

import (
	"strings"
	"testing"
)

func TestEmitExprSlotEscapesInitial(t *testing.T) {
	src := "export default function Page() {\n  const [code, setCode] = createSignal(`return <h1>x</h1>`);\n  return <code>{code()}</code>;\n}"
	result, _ := fullPipeline(t, src)
	if !strings.Contains(result.HTML, "&lt;h1&gt;") {
		t.Errorf("expected escaped expr-slot initial, got:\n%s", result.HTML)
	}
	if strings.Contains(result.HTML, "<h1>x</h1>") {
		t.Errorf("expr slot initial leaked raw markup:\n%s", result.HTML)
	}
}
