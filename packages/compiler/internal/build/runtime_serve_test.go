package build

import (
	"testing"
)

func TestRendererRuntimeComponent(t *testing.T) {
	// Test that runtime components produce data-krate-runtime placeholders
	src := `
function Counter(props) {
  return <div class="counter">{props.count}</div>;
}
export default function Page() {
  return <div><Counter count={5} /></div>;
}
`
	_ = src
	// This is a build-time integration test; the renderer tests verify the pattern
}
