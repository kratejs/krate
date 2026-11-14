package annotator

import (
	"strings"

	"github.com/kratejs/krate/packages/compiler/ast"
	"github.com/kratejs/krate/packages/compiler/internal/diag"
	irtree "github.com/kratejs/krate/packages/compiler/internal/irtree"
)

// collectClassRejections records, for every React class component in body,
// the warning diagnostic positioned at the member that keeps it from being
// lowered (reported where the component is used), whether it is an error
// boundary, and any non-blocking caveats.
func collectClassRejections(body []ast.Stmt, ann *irtree.Annotations, file, raw string) {
	collectDecoratedClasses(body, ann, file, raw)
	for _, stmt := range body {
		var class *ast.ClassDecl
		switch s := stmt.(type) {
		case *ast.ClassDecl:
			class = s
		case *ast.ExportStmt:
			if c, ok := s.Declaration.(*ast.ClassDecl); ok {
				class = c
			}
		}
		if class == nil || class.Name == "" || !isReactClassBase(class.SuperClass) {
			continue
		}
		model := analyzeClass(class)
		if model.issue != nil {
			if ann.ClassRejections == nil {
				ann.ClassRejections = make(map[string]diag.Diagnostic)
			}
			if _, exists := ann.ClassRejections[class.Name]; !exists {
				ann.ClassRejections[class.Name] = classDiagnostic(class, model.issue, file, raw,
					"class component <"+class.Name+"> is not lowered: ")
			}
			continue
		}
		if model.isBoundary() {
			if ann.ClassBoundaries == nil {
				ann.ClassBoundaries = make(map[string]bool)
			}
			ann.ClassBoundaries[class.Name] = true
		}
		if len(model.notes) > 0 {
			if ann.ClassNotes == nil {
				ann.ClassNotes = make(map[string][]diag.Diagnostic)
			}
			if _, exists := ann.ClassNotes[class.Name]; !exists {
				for i := range model.notes {
					ann.ClassNotes[class.Name] = append(ann.ClassNotes[class.Name],
						classDiagnostic(class, &model.notes[i], file, raw, "class component <"+class.Name+">: "))
				}
			}
		}
		noteDroppedDecorators(class, ann)
	}
}

// classDiagnostic builds the coded warning for issue.
func classDiagnostic(class *ast.ClassDecl, issue *classIssue, file, raw, prefix string) diag.Diagnostic {
	pos := issue.pos
	if pos.Line == 0 {
		pos = class.Position
	}
	d := diag.New(file, pos.Line, pos.Col, prefix+issue.reason, issue.hint, lineOf(raw, pos.Line)).WithCode(issue.code)
	d.Severity = diag.SeverityWarning
	return d
}

// lineOf returns the 1-based line of raw, or "".
func lineOf(raw string, line int) string {
	if raw == "" || line <= 0 {
		return ""
	}
	lines := strings.SplitN(raw, "\n", line+1)
	if len(lines) < line {
		return ""
	}
	return strings.TrimRight(lines[line-1], "\r")
}

// noteDroppedDecorators warns about a class that lowers despite its decorators:
// legacy member decorators leave the class intact, but it becomes a function
// component, so they never run.
func noteDroppedDecorators(class *ast.ClassDecl, ann *irtree.Annotations) {
	d, ok := ann.DecoratedClasses[class.Name]
	if !ok {
		return
	}
	d.Message = "decorated class <" + class.Name + "> is rendered without running its decorators"
	d.Hint = "a class component is compiled to a function component, so its member decorators do not apply; move their logic into the methods"
	for _, n := range ann.ClassNotes[class.Name] {
		if n.Message == d.Message {
			return
		}
	}
	if ann.ClassNotes == nil {
		ann.ClassNotes = make(map[string][]diag.Diagnostic)
	}
	ann.ClassNotes[class.Name] = append(ann.ClassNotes[class.Name], d)
}

// collectDecoratedClasses records top-level classes whose decorators esbuild
// lowered to `__decorateElement(...)` / `__decorateClass(...)` calls, for the
// class itself or for a member. The decorators run at load time and may replace
// the class or its methods, so Krate cannot compile the class as a component and
// says so instead of a generic "could not be resolved".
func collectDecoratedClasses(body []ast.Stmt, ann *irtree.Annotations, file, raw string) {
	classes := map[string]ast.Pos{}
	for _, stmt := range body {
		if exp, ok := stmt.(*ast.ExportStmt); ok && exp.Declaration != nil {
			stmt = exp.Declaration
		}
		switch s := stmt.(type) {
		case *ast.ClassDecl:
			classes[s.Name] = s.Position
		case *ast.VarStmt:
			// A legacy class decorator lowers to `let Box = class extends ... {}`.
			for _, d := range s.Decls {
				if d == nil {
					continue
				}
				if c, ok := d.Init.(*ast.ClassExpr); ok {
					classes[d.Name] = c.Position
				}
			}
		}
	}
	record := func(name string) {
		pos, ok := classes[name]
		if !ok {
			return
		}
		if ann.DecoratedClasses == nil {
			ann.DecoratedClasses = make(map[string]diag.Diagnostic)
		}
		if _, exists := ann.DecoratedClasses[name]; exists {
			return
		}
		d := diag.New(file, pos.Line, pos.Col,
			"decorated class <"+name+"> is not rendered as a component",
			"decorators run code at load time and can replace the class or its methods, which a build cannot evaluate; remove the decorator, or convert <"+name+"> to a function component and apply the decorator's logic in ordinary code. Tier decorators such as @server are fine",
			lineOf(raw, pos.Line)).WithCode(diag.CodeClassNotLowered)
		d.Severity = diag.SeverityWarning
		ann.DecoratedClasses[name] = d
	}
	var visit func(e ast.Expr)
	visit = func(e ast.Expr) {
		switch x := e.(type) {
		case *ast.BinaryExpr:
			if x.Op == "," || x.Op == "=" {
				visit(x.Left)
				visit(x.Right)
			}
		case *ast.CallExpr:
			fn, ok := x.Callee.(*ast.Identifier)
			if !ok || (fn.Name != "__decorateElement" && fn.Name != "__decorateClass") {
				return
			}
			for _, arg := range x.Args {
				if m, ok := arg.(*ast.MemberExpr); ok {
					arg = m.Object
				}
				if id, ok := arg.(*ast.Identifier); ok {
					record(id.Name)
				}
			}
		}
	}
	for _, stmt := range body {
		if es, ok := stmt.(*ast.ExprStmt); ok {
			visit(es.Expression)
		}
	}
}
