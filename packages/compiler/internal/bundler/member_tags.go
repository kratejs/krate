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

// rewriteRadixSlotImport maps `import { Slot } from "radix-ui"` (the umbrella
// package that current shadcn components import for `asChild`) onto Krate's
// native Slot, as `@radix-ui/react-slot` already is: the binding becomes a
// namespace of @krate/runtime/slot, so `Slot.Root` and `Slot.Slottable` keep
// working. The rest of the specifiers stay on the umbrella import. The binding
// is renamed to Slot, the name the build folds.
func rewriteRadixSlotImport(prog *ast.Program) {
	for i, stmt := range prog.Body {
		imp, ok := stmt.(*ast.ImportStmt)
		if !ok || strings.Trim(imp.Source, "\"'") != "radix-ui" {
			continue
		}
		var kept []ast.NamedImport
		local := ""
		for _, n := range imp.Named {
			if n.Remote == "Slot" {
				local = n.Local
				if local == "" {
					local = n.Remote
				}
				continue
			}
			kept = append(kept, n)
		}
		if local == "" {
			continue
		}
		// The build folds `Slot.Root` by name, so the binding is called Slot.
		if local != "Slot" {
			applyRenames(prog, map[string]string{local: "Slot"})
			local = "Slot"
		}
		slot := &ast.ImportStmt{Position: imp.Position, Namespace: local, Source: `"@krate/runtime/slot"`}
		body := make([]ast.Stmt, 0, len(prog.Body)+1)
		body = append(body, prog.Body[:i]...)
		if len(kept) > 0 || imp.Default != "" || imp.Namespace != "" {
			rest := *imp
			rest.Named = kept
			body = append(body, &rest)
		}
		body = append(body, slot)
		body = append(body, prog.Body[i+1:]...)
		prog.Body = body
		return
	}
}
