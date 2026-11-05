package bundler

import (
	"strings"

	"github.com/kratejs/krate/packages/compiler/ast"
)

// memberTagSuffix marks an import binding renamed by renameMemberTagImports.
const memberTagSuffix = "$ns"

// renameMemberTagImports gives an import binding a capitalized name when it is
// the root of a member tag: `import * as ui from "./ui"` used as <ui.Button>.
// JSX reads a member tag as a component whatever its case, but the rest of the
// pipeline tells components from HTML elements by a capital first letter, so a
// lowercase namespace would render as the literal element <ui.Button>.
// Renaming the binding and its references (`ui` -> `Ui$ns`) lets every later
// stage treat the tag like <Card.Root>.
func renameMemberTagImports(prog *ast.Program) {
	roots := map[string]bool{}
	visitStmts(prog.Body, false, func(n any, _ bool) bool {
		if el, ok := n.(*ast.JSXElement); ok && el.Opening != nil {
			if head, _, dotted := strings.Cut(el.Opening.Name, "."); dotted && head != "" && head[0] >= 'a' && head[0] <= 'z' {
				roots[head] = true
			}
		}
		return true
	})
	if len(roots) == 0 {
		return
	}
	names := map[string]string{}
	note := func(local string) {
		if roots[local] {
			names[local] = strings.ToUpper(local[:1]) + local[1:] + memberTagSuffix
		}
	}
	for _, stmt := range prog.Body {
		imp, ok := stmt.(*ast.ImportStmt)
		if !ok {
			continue
		}
		note(imp.Default)
		note(imp.Namespace)
		for _, n := range imp.Named {
			if n.Local != "" {
				note(n.Local)
			} else {
				note(n.Remote)
			}
		}
	}
	if len(names) == 0 {
		return
	}
	applyRenames(prog, names)
	for _, stmt := range prog.Body {
		imp, ok := stmt.(*ast.ImportStmt)
		if !ok {
			continue
		}
		if to, ok := names[imp.Default]; ok && imp.Default != "" {
			imp.Default = to
		}
		if to, ok := names[imp.Namespace]; ok && imp.Namespace != "" {
			imp.Namespace = to
		}
		for i, spec := range imp.Named {
			local := spec.Local
			if local == "" {
				local = spec.Remote
			}
			if to, ok := names[local]; ok {
				imp.Named[i] = ast.NamedImport{Local: to, Remote: spec.Remote}
			}
		}
	}
}
