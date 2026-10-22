package annotator

import (
	"strings"

	"github.com/kratejs/krate/packages/compiler/ast"
	"github.com/kratejs/krate/packages/compiler/internal/diag"
	irtree "github.com/kratejs/krate/packages/compiler/internal/irtree"
)

// classLoweringIssue reports the first construct that keeps a React class
// component from being lowered, or nil when synthesizeClassComponent can
// lower it. It does not mutate the class.
func classLoweringIssue(class *ast.ClassDecl) *classIssue {
	if class == nil || class.Name == "" || !isReactClassBase(class.SuperClass) {
		return nil
	}
	return analyzeClass(class).issue
}

// collectClassRejections records, for every React class component in body,
// the warning diagnostic positioned at the member that keeps it from being
// lowered (reported where the component is used), whether it is an error
// boundary, and any non-blocking caveats.
func collectClassRejections(body []ast.Stmt, ann *irtree.Annotations, file, raw string) {
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
