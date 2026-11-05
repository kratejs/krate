package bundler

import (
	"strings"
	"testing"
)

// An attribute named by an expression survives jsx() lowering as a spread of
// that property; it is not renamed to the expression's spelling.
func TestJSXCallComputedPropKeepsItsKey(t *testing.T) {
	out := rewriteSrc(t, `
		import { jsx } from 'react/jsx-runtime';
		const k = 'title';
		export default function Page() {
			return jsx('div', { [k]: 'x', id: 'a' });
		}
	`)
	if !strings.Contains(out, "...{ [k]:") && !strings.Contains(out, "...{[k]:") {
		t.Errorf("computed key not kept as a spread of one property:\n%s", out)
	}
	if strings.Contains(out, " k=") {
		t.Errorf("computed key renamed to its spelling:\n%s", out)
	}
}
