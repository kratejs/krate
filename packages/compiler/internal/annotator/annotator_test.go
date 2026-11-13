package annotator

import (
	"testing"

	"github.com/kratejs/krate/packages/compiler/ast"
	"github.com/kratejs/krate/packages/compiler/internal/config"
	"github.com/kratejs/krate/packages/compiler/internal/irtree"
	"github.com/kratejs/krate/packages/compiler/internal/lexer"
	"github.com/kratejs/krate/packages/compiler/internal/parser"
)

func parseProg(t *testing.T, src string) *ast.Program {
	t.Helper()
	l := lexer.New(src)
	tokens := l.Tokenize()
	p := parser.New(tokens)
	prog := p.ParseProgram()
	if errs := p.Errors(); len(errs) > 0 {
		t.Fatalf("parse errors: %v", errs)
	}
	return prog
}

func entryTier(t *testing.T, ann *irtree.Annotations) irtree.ComponentTier {
	t.Helper()
	return ann.ComponentTiers[ann.EntryPoint]
}

// Tier classification priority

func TestClassifyDirectivePriority(t *testing.T) {
	src := `// @server
export default function App() { return <div>hi</div>; }`
	ann := Annotate(parseProg(t, src), &config.Config{}, "whatever.runtime.tsx", src)
	if got := entryTier(t, ann); got != irtree.TierServer {
		t.Errorf("directive must outrank file convention; got %v", got)
	}
}

func TestImportedRuntimeComponentUsesOwnFileConvention(t *testing.T) {
	// The page is @server, but the imported component lives in a *.runtime.tsx
	// file. Its tier must come from ITS OWN file, not the page directive.
	pageSrc := `// @server
import RuntimeWidget from '../ui/RuntimeWidget.runtime';
export default function Page() {
  return <div><RuntimeWidget label="x" /></div>;
}`
	pageProg := parseProg(t, pageSrc)
	ann := Annotate(pageProg, &config.Config{}, "src/pages/demo.tsx", pageSrc)

	runtimeSrc := `export default function RuntimeWidget(props: { label: string }) {
  return <div>{props.label}</div>;
}`
	MergeModuleFunctions(ann, []ModuleSource{{
		Program:   parseProg(t, runtimeSrc),
		Path:      "src/components/ui/RuntimeWidget.runtime.tsx",
		RawSource: runtimeSrc,
	}})
	ReclassifyTiers(ann, &config.Config{})

	if got := ann.ComponentTiers["Page"]; got != irtree.TierServer {
		t.Errorf("page should be server (own directive), got %v", got)
	}
	if got := ann.ComponentTiers["RuntimeWidget"]; got != irtree.TierRuntime {
		t.Errorf("imported component should be runtime from its own file, got %v", got)
	}
}
