package renderer

import (
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/annotator"
	"github.com/kratejs/krate/packages/compiler/internal/config"
	"github.com/kratejs/krate/packages/compiler/internal/irtree"
)

// TestIDPrefixNamespacesSlotIDs verifies BuildOptions.IDPrefix prefixes the
// compact slot IDs emitted into data-k markers. Layouts are built with a prefix
// so that, when their hydration is merged into a page's, the two ID spaces can
// never collide.
func TestIDPrefixNamespacesSlotIDs(t *testing.T) {
	src := `function Toggle() {
  const [on, setOn] = createSignal(false);
  return <button onClick={() => setOn(!on())}>{on() ? "On" : "Off"}</button>;
}
export default function Page() {
  return <Toggle />;
}`
	prog := parseProg(t, src)
	ann := annotator.Annotate(prog, &config.Config{}, "test.tsx", src)
	tree := irtree.BuildWithOptions(prog, ann, irtree.BuildOptions{IDPrefix: "_z9_"})
	result := NewEmitter().Emit(tree)
	if !strings.Contains(result.HTML, `data-k="k:_z9_`) {
		t.Fatalf("expected prefixed data-k markers, got:\n%s", result.HTML)
	}
}
