package annotator

import (
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/config"
)

// TestCollectMemberComponents verifies plain namespaced part assignments
// (`Root.Part = PartFn`) register dotted tags, including presentational
// compounds (Breadcrumb.List, Table.Row, …).
func TestCollectMemberComponents(t *testing.T) {
	src := `export function Breadcrumb(props) { return <nav>{props.children}</nav>; }
export function BreadcrumbList(props) { return <ol>{props.children}</ol>; }
Breadcrumb.List = BreadcrumbList;
export default function Page() {
	return <Breadcrumb><Breadcrumb.List>Home</Breadcrumb.List></Breadcrumb>;
}`
	ann := Annotate(parseProg(t, src), &config.Config{}, "index.tsx", src)
	if ann.Functions["Breadcrumb.List"] == nil {
		t.Fatalf("Breadcrumb.List was not registered")
	}
	if ann.Functions["Breadcrumb.List"] != ann.Functions["BreadcrumbList"] {
		t.Errorf("Breadcrumb.List should alias BreadcrumbList")
	}
}
