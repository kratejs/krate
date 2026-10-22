package irtree

import (
	"sort"
	"strings"

	"github.com/kratejs/krate/packages/compiler/ast"
	"github.com/kratejs/krate/packages/compiler/internal/diag"
	"github.com/kratejs/krate/packages/compiler/internal/jsvalue"
)

// warnUnrenderedComponent reports a component tag that renders nothing
// because it could not be resolved or (for a React class) lowered.
func (b *builder) warnUnrenderedComponent(name string, el *ast.JSXElement) {
	if b.ann != nil && b.ann.ClassComponents[name] {
		if d, ok := b.ann.ClassRejections[name]; ok {
			use := b.diagAt(el.Position, diag.SeverityWarning, "", "")
			d.Message += " (rendered at " + diagLocation(use) + ", where it renders nothing)"
			d.Severity = diag.SeverityWarning
			b.warnings = append(b.warnings, d)
			return
		}
		b.warnAt(el.Position, "class component <"+name+"> is not lowered and will not render",
			"supported: state, this.props, this.setState, componentDidMount/componentDidUpdate/componentWillUnmount, "+
				"helper methods, arrow handlers and instance fields; move other logic into a function component")
		return
	}
	b.warnAt(el.Position, "component <"+name+"> could not be resolved and will not render",
		"import <"+name+"> (or check its spelling); a component must be a function declaration or a const bound to an arrow or function expression")
}

// diagLocation renders a diagnostic's file:line:col.
func diagLocation(d diag.Diagnostic) string {
	loc := itoa(d.Line) + ":" + itoa(d.Col)
	if d.File != "" {
		loc = d.File + ":" + loc
	}
	return loc
}

// findJSXElementNamed returns the first JSX element named name in the tree
// rooted at e, or nil.
func findJSXElementNamed(e ast.Expr, name string) *ast.JSXElement {
	switch x := e.(type) {
	case *ast.JSXElement:
		if x.Opening != nil && x.Opening.Name == name {
			return x
		}
		return findJSXInChildren(x.Children, name)
	case *ast.JSXFragment:
		return findJSXInChildren(x.Children, name)
	}
	return nil
}

func findJSXInChildren(children []ast.JSXChild, name string) *ast.JSXElement {
	for _, c := range children {
		var found *ast.JSXElement
		switch ch := c.(type) {
		case *ast.JSXElementChild:
			found = findJSXElementNamed(ch.Element, name)
		case *ast.JSXFragmentChild:
			found = findJSXElementNamed(ch.Fragment, name)
		case *ast.JSXExprContainer:
			found = findJSXElementNamed(ch.Expression, name)
		}
		if found != nil {
			return found
		}
	}
	return nil
}

// srcFnName returns the name of the function whose AST is being walked, used
// to resolve the source file and text of a diagnostic position.
func (b *builder) srcFnName() string {
	if b.srcFn != "" {
		return b.srcFn
	}
	if b.currentNode != nil {
		return b.currentNode.Name
	}
	if b.ann != nil {
		return b.ann.EntryPoint
	}
	return ""
}

// diagAt builds a diagnostic positioned at pos inside the function currently
// being walked, including the offending source line for caret rendering.
func (b *builder) diagAt(pos ast.Pos, sev diag.Severity, msg, hint string) diag.Diagnostic {
	file, raw := "", ""
	if b.ann != nil {
		name := b.srcFnName()
		file = b.ann.ComponentSources[name]
		raw = b.ann.ComponentRaw[name]
		if file == "" {
			file = b.ann.SourceFile
		}
	}
	if pos.Line == 0 {
		// Some synthesized or parser nodes carry no position; point at the
		// enclosing component instead of 0:0.
		if fn := b.functions[b.srcFnName()]; fn != nil {
			pos = fn.Position
		}
	}
	d := diag.New(file, pos.Line, pos.Col, msg, hint, sourceLine(raw, pos.Line))
	d.Severity = sev
	return d
}

// warnAt records a positioned, non-fatal build warning.
func (b *builder) warnAt(pos ast.Pos, msg, hint string) {
	b.warnings = append(b.warnings, b.diagAt(pos, diag.SeverityWarning, msg, hint))
}

// errorAt records a positioned hard build error.
func (b *builder) errorAt(pos ast.Pos, msg, hint string) {
	b.buildErrs = append(b.buildErrs, b.diagAt(pos, diag.SeverityError, msg, hint))
}

// sourceLine returns the 1-based line of raw, or "" when unavailable.
func sourceLine(raw string, line int) string {
	if raw == "" || line <= 0 {
		return ""
	}
	for i := 1; ; i++ {
		nl := strings.IndexByte(raw, '\n')
		if i == line {
			if nl < 0 {
				return strings.TrimRight(raw, "\r")
			}
			return strings.TrimRight(raw[:nl], "\r")
		}
		if nl < 0 {
			return ""
		}
		raw = raw[nl+1:]
	}
}

// Recursive component expansion.
//
// Components are expanded (inlined into the slot tree) at build time, so a
// component that renders itself, directly or through other components, would
// expand forever. Every component expansion pushes a frame recording the
// component and a signature of its resolved call-site props:
//
//   - re-entering a component with the same prop signature as an enclosing
//     frame can never terminate, and is reported immediately;
//   - re-entering it with different props (a tree walked over build-time
//     data) is allowed, because the recursion terminates when the data runs
//     out, but only up to maxRecursionDepth nested frames of one component and
//     maxRecursiveExpansions re-entries per build.
//
// Recursion over data only known at runtime cannot be expanded at build time;
// the diagnostic points the author at a runtime component instead.
const (
	maxRecursionDepth      = 32
	maxRecursiveExpansions = 2000
)

// expansionFrame is one component expansion on the builder's stack.
type expansionFrame struct {
	name string
	sig  string
}

// enterExpansion pushes an expansion frame for the component rendered by el.
// It returns ok=false, after recording a diagnostic, when the expansion would
// recurse without bound; the caller must then render nothing for el. On
// success the returned function pops the frame.
func (b *builder) enterExpansion(name string, el *ast.JSXElement, attrs map[string]ast.Expr) (leave func(), ok bool) {
	sig := b.expansionSignature(attrs, el)
	depth := 0
	for i, f := range b.expansion {
		if f.name != name {
			continue
		}
		depth++
		if f.sig == sig {
			b.recursionError(el, name, b.expansionCycle(i, name),
				"it renders itself with the same props, so the expansion can never terminate")
			return nil, false
		}
	}
	if depth > 0 {
		b.recursiveExpansions++
		if depth >= maxRecursionDepth || b.recursiveExpansions > maxRecursiveExpansions {
			first := 0
			for i, f := range b.expansion {
				if f.name == name {
					first = i
					break
				}
			}
			b.recursionError(el, name, b.expansionCycle(first, name),
				"the recursion did not terminate within "+itoa(maxRecursionDepth)+" levels at build time")
			return nil, false
		}
	}
	b.expansion = append(b.expansion, expansionFrame{name: name, sig: sig})
	return func() { b.expansion = b.expansion[:len(b.expansion)-1] }, true
}

// expansionCycle renders the component chain from frame index start back to
// name, e.g. "Tree > Folder > Tree".
func (b *builder) expansionCycle(start int, name string) string {
	parts := make([]string, 0, len(b.expansion)-start+1)
	for _, f := range b.expansion[start:] {
		parts = append(parts, f.name)
	}
	parts = append(parts, name)
	return strings.Join(parts, " > ")
}

func (b *builder) recursionError(el *ast.JSXElement, name, cycle, why string) {
	// Only the first report for a component is useful; later re-entries are
	// the same cycle seen from deeper frames.
	if b.recursionReported == nil {
		b.recursionReported = make(map[string]bool)
	}
	if b.recursionReported[name] {
		return
	}
	b.recursionReported[name] = true
	b.errorAt(el.Position, "recursive component <"+name+"> cannot be expanded at build time ("+cycle+"): "+why,
		"Krate expands components while building. Make the recursion stop on a build-time prop "+
			"(for example `{node.children.length > 0 && node.children.map(c => <"+name+" node={c} />)}` over static data), "+
			"or render the recursive part at request time by moving it into a runtime component (*.runtime.tsx).")
}

// expansionSignature summarizes the resolved call-site props of a component
// expansion. Two expansions with equal signatures render identically.
func (b *builder) expansionSignature(attrs map[string]ast.Expr, el *ast.JSXElement) string {
	keys := make([]string, 0, len(attrs))
	for k := range attrs {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var sb strings.Builder
	for _, k := range keys {
		expr := attrs[k]
		v := ""
		if expr != nil && !b.referencesSignal(expr) {
			if src, ok := jsvalue.JSSource(b.resolveBindingValue(expr, b.localProps)); ok {
				v = src
			}
		}
		if v == "" {
			v = "js:" + generateExprJS(expr, b.sigMap())
		}
		sb.WriteString(k)
		sb.WriteByte('=')
		sb.WriteString(v)
		sb.WriteByte(';')
	}
	if el != nil && len(el.Children) > 0 {
		sb.WriteString("children=")
		sb.WriteString(itoa(len(el.Children)))
	}
	return sb.String()
}
