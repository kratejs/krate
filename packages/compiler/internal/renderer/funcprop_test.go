package renderer

import (
	"fmt"
	"strings"
	"testing"
)

// TestFunctionPropCopiedToLocal reproduces the docs sidebar pattern: a client
// parent defines a local function and passes it as a prop to a client child
// that copies it into a local and calls it from onMount.
func TestFunctionPropCopiedToLocal(t *testing.T) {
	src := `
import { createSignal, onMount } from "@krate/runtime";
function Child(props: { onGo?: () => void }) {
  var onGo = props.onGo;
  onMount(function () {
    var el = document.querySelector(".child");
    if (el) el.addEventListener("click", function () { if (onGo) onGo(); });
  });
  return <div class="child">child</div>;
}
function Parent(props: any) {
  const [x, setX] = createSignal(0);
  function closeNav() { setX(1); }
  return (
    <div>
      <Child onGo={closeNav} />
      <span id="val">{x()}</span>
    </div>
  );
}
export default function Page() {
  return <Parent />;
}
`
	result, hjs := fullPipeline(t, src)
	fmt.Printf("HTML:\n%s\nHYD:\n%s\n", result.HTML, hjs)
	// Assert the child's local is a runtime prop read, not a stringified name.
	if strings.Contains(hjs, "onGo=\"closeNav\"") || strings.Contains(hjs, "var onGo='closeNav'") || strings.Contains(hjs, "var onGo=\"closeNav\"") {
		t.Errorf("function prop was stringified to its name; child must read props.onGo at runtime:\n%s", hjs)
	}
}
