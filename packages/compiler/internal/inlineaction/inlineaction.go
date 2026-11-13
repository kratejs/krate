// Package inlineaction hoists inline server actions, functions that start with
// a "use server" directive inside a component, out of their module.
//
// Each inline action becomes an exported function of a generated server-action
// module. The values it closes over become its leading parameters; at the
// original site the function is replaced by a call that builds the action
// reference, sealing those values into an opaque token the browser can only
// send back (see build/inline_actions.go for the transport and encryption).
package inlineaction

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/kratejs/krate/packages/compiler/ast"
	"github.com/kratejs/krate/packages/compiler/internal/diag"
	"github.com/kratejs/krate/packages/compiler/internal/lexer"
	"github.com/kratejs/krate/packages/compiler/internal/tsstrip"
)

// ID is the public id of an action export of the module at rel (a slash path
// relative to the project root).
func ID(rel, export string) string {
	sum := sha256.Sum256([]byte("krate:action:v1:" + filepath.ToSlash(rel) + "\x00" + export))
	return "a" + hex.EncodeToString(sum[:9])
}

// Action is one hoisted inline server action.
type Action struct {
	Index int
	// Name is the declared name of a function declaration ("" for a function
	// expression such as an arrow passed straight to an attribute).
	Name string
	// Export is the name of the generated module's export for this action.
	Export   string
	ID       string
	Captures []string
	// EnclosingAsync reports that the nearest enclosing function is async. Only
	// there are captured values sealed per request.
	EnclosingAsync bool
	Pos            ast.Pos
}

// Options configures Hoist.
type Options struct {
	// Path is the file path used in diagnostics; Rel is the slash path relative
	// to the project root that action ids derive from; Dir is the absolute
	// directory of the file, used to resolve relative imports in the generated
	// module.
	Path, Rel, Dir string
	// Client marks a module that starts with "use client".
	Client bool
	TS     tsstrip.Options
	// Call returns the expression that replaces an action in the source.
	Call func(a *Action) string
}

// Result is the outcome of hoisting the inline actions of one file.
type Result struct {
	Actions []*Action
	// Source is the original source with each action replaced by its call.
	// Line numbers are preserved.
	Source string
	// Module is the generated server-action module for all actions.
	Module string
}

// Call is the expression a capturing action is replaced with:
// __krateInlineAction(id, [captured values], [names], label). The helper seals
// the values for the browser (see build/inline_actions.go).
func Call(a *Action) string {
	names, _ := json.Marshal(append([]string{}, a.Captures...))
	label := a.Name
	if label == "" {
		label = "an inline server action"
	}
	labelJSON, _ := json.Marshal(label)
	return fmt.Sprintf("__krateInlineAction(%q, [%s], %s, %s)", a.ID, strings.Join(a.Captures, ", "), names, labelJSON)
}

// FirstPos reports the position of the first inline "use server" function of
// a parsed program.
func FirstPos(prog *ast.Program) (ast.Pos, bool) {
	if prog == nil {
		return ast.Pos{}, false
	}
	if _, ok := isUseServer(prog.Body); ok {
		return ast.Pos{}, false
	}
	h := &hoister{prog: prog}
	cands := h.find()
	if len(cands) == 0 {
		return ast.Pos{}, false
	}
	return cands[0].pos, true
}

// Has reports whether src can contain an inline server action.
func Has(src string) bool { return strings.Contains(src, "use server") }

func isUseServer(stmts []ast.Stmt) (ast.Pos, bool) {
	if len(stmts) == 0 {
		return ast.Pos{}, false
	}
	es, ok := stmts[0].(*ast.ExprStmt)
	if !ok {
		return ast.Pos{}, false
	}
	lit, ok := es.Expression.(*ast.Literal)
	if !ok || lit.Kind != ast.StringLit || strings.Trim(strings.TrimSpace(lit.Value), `"'`) != "use server" {
		return ast.Pos{}, false
	}
	pos := es.Position
	if pos.Line == 0 {
		pos = lit.Position
	}
	return pos, true
}

type candidate struct {
	node  any
	pos   ast.Pos
	scope []map[string]bool
	async bool
	class bool
}

type hoister struct {
	o      Options
	src    string
	prog   *ast.Program
	toks   []lexer.Token
	errAt  func(pos ast.Pos, msg, hint string) error
	bounds []stmtBound
}

type stmtBound struct {
	stmt       ast.Stmt
	start, end int
}

// Hoist finds the inline server actions of a source file. It returns nil when
// there are none (including when the file is itself a "use server" module or
// does not parse; the regular parse reports that).
func Hoist(src string, o Options) (*Result, error) {
	if !Has(src) {
		return nil, nil
	}
	prog, errs := tsstrip.Parse(o.Path, src, o.TS)
	if prog == nil || len(errs) > 0 {
		return nil, nil
	}
	if _, ok := isUseServer(prog.Body); ok {
		return nil, nil
	}
	h := &hoister{o: o, src: src, prog: prog}
	h.errAt = func(pos ast.Pos, msg, hint string) error {
		return diag.New(o.Path, pos.Line, pos.Col, msg, hint, sourceLine(src, pos.Line)).WithCode(diag.CodeInlineUseServer)
	}
	cands := h.find()
	if len(cands) == 0 {
		return nil, nil
	}
	if o.Client {
		return nil, h.errAt(cands[0].pos, `inline "use server" functions are not allowed in a "use client" module`,
			`move the function into a server component, or into a module whose first statement is "use server", and import it`)
	}
	for _, c := range cands {
		if c.class {
			return nil, h.errAt(c.pos, `a "use server" directive inside a class method is not supported`,
				`move the function into a module whose first statement is "use server", export it, and import it here`)
		}
	}
	h.toks = lexer.New(src).Tokenize()
	h.bounds = h.stmtBounds()
	return h.build(cands)
}

func (h *hoister) find() []candidate {
	var cands []candidate
	var stack []map[string]bool
	var asyncs []bool
	var fnStack []any
	walkNode(h.prog.Body, func(n any) bool {
		params, body, async, name, _, ok := fnParts(n)
		if !ok {
			return true
		}
		if pos, has := isUseServer(body); has {
			_, isMember := n.(*ast.ClassMember)
			c := candidate{node: n, pos: pos, class: isMember}
			c.scope = append(c.scope, stack...)
			if len(asyncs) > 0 {
				c.async = asyncs[len(asyncs)-1]
			}
			cands = append(cands, c)
			return false
		}
		stack = append(stack, declaredNames(params, body, name))
		asyncs = append(asyncs, async)
		fnStack = append(fnStack, n)
		return true
	}, func(n any) {
		if len(fnStack) > 0 && fnStack[len(fnStack)-1] == n {
			fnStack = fnStack[:len(fnStack)-1]
			stack = stack[:len(stack)-1]
			asyncs = asyncs[:len(asyncs)-1]
		}
	})
	return cands
}

// stmtBounds computes the source range of each top-level statement as
// [its start, the next statement's start).
func (h *hoister) stmtBounds() []stmtBound {
	out := make([]stmtBound, len(h.prog.Body))
	for i, s := range h.prog.Body {
		out[i] = stmtBound{stmt: s, start: s.Pos().Offset, end: len(h.src)}
		if i > 0 {
			out[i-1].end = out[i].start
		}
	}
	return out
}

// moduleBinding is a top-level binding and the statement that declares it.
func (h *hoister) moduleBindings() map[string]int {
	out := map[string]int{}
	for i, s := range h.prog.Body {
		for _, name := range topLevelNames(s) {
			out[name] = i
		}
	}
	return out
}

func topLevelNames(s ast.Stmt) []string {
	set := map[string]bool{}
	switch x := s.(type) {
	case *ast.ExportStmt:
		if x.Declaration != nil {
			return topLevelNames(x.Declaration)
		}
		return nil
	case *ast.ImportStmt:
		if x.Default != "" {
			set[x.Default] = true
		}
		if x.Namespace != "" {
			set[x.Namespace] = true
		}
		for _, n := range x.Named {
			local := n.Local
			if local == "" {
				local = n.Remote
			}
			set[local] = true
		}
	case *ast.FnDecl:
		if x.Name != "" {
			set[x.Name] = true
		}
	case *ast.ClassDecl:
		if x.Name != "" {
			set[x.Name] = true
		}
	case *ast.VarStmt:
		for _, d := range x.Decls {
			if d == nil {
				continue
			}
			if d.Name != "" && !d.IsDestructuring {
				set[d.Name] = true
			}
			for _, n := range d.Names {
				set[n] = true
			}
			patternNames(d.Target, set)
		}
	}
	out := make([]string, 0, len(set))
	for n := range set {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

type extent struct {
	start, end int // source range of the function
	open       int // token index of the "(" of the parameter list, or -1
	bare       int // token index of a bare arrow parameter, or -1
	hasParams  bool
	prefix     string // "export", "export default" or ""
}

func (h *hoister) tokenAt(offset int) int {
	i := sort.Search(len(h.toks), func(i int) bool { return h.toks[i].Offset >= offset })
	if i < len(h.toks) && h.toks[i].Offset == offset {
		return i
	}
	return -1
}

func (h *hoister) extentOf(c candidate) (extent, error) {
	var ex extent
	pos := c.pos
	startTok := h.tokenAt(startOffset(c.node))
	if startTok < 0 {
		return ex, h.errAt(pos, `cannot locate this inline "use server" function`,
			`write it as a plain function declaration or arrow function`)
	}
	// The body is the first "{" that is followed by the directive string.
	open := -1
	for i := startTok; i+1 < len(h.toks); i++ {
		if h.toks[i].Kind == lexer.LBRACE && h.toks[i+1].Kind == lexer.String &&
			strings.Trim(strings.TrimSpace(h.toks[i+1].Value), `"'`) == "use server" {
			open = i
			break
		}
	}
	if open < 0 {
		return ex, h.errAt(pos, `cannot locate the body of this inline "use server" function`, ``)
	}
	depth := 0
	closeTok := -1
	for i := open; i < len(h.toks); i++ {
		switch h.toks[i].Kind {
		case lexer.LBRACE:
			depth++
		case lexer.RBRACE:
			depth--
			if depth == 0 {
				closeTok = i
			}
		}
		if closeTok >= 0 {
			break
		}
	}
	if closeTok < 0 {
		return ex, h.errAt(pos, `cannot find the end of this inline "use server" function`, ``)
	}
	ex.start = h.toks[startTok].Offset
	ex.end = h.toks[closeTok].End
	ex.open, ex.bare = -1, -1
	for i := startTok; i < open; i++ {
		k := h.toks[i].Kind
		if k == lexer.LPAREN {
			ex.open = i
			ex.hasParams = h.toks[i+1].Kind != lexer.RPAREN
			break
		}
		if k == lexer.ARROW {
			break
		}
		if k == lexer.Identifier && i+1 < len(h.toks) && h.toks[i+1].Kind == lexer.ARROW {
			ex.bare = i
			ex.hasParams = true
			break
		}
	}
	if ex.open < 0 && ex.bare < 0 {
		return ex, h.errAt(pos, `cannot find the parameter list of this inline "use server" function`, ``)
	}
	// A function declaration keeps its "export" / "export default" prefix out
	// of the replaced range.
	if _, isDecl := c.node.(*ast.FnDecl); isDecl {
		if startTok >= 2 && h.toks[startTok-1].Kind == lexer.Default_ && h.toks[startTok-2].Kind == lexer.Export {
			ex.prefix = "export default"
		} else if startTok >= 1 && h.toks[startTok-1].Kind == lexer.Export {
			ex.prefix = "export"
		}
	}
	return ex, nil
}

func startOffset(n any) int {
	switch f := n.(type) {
	case *ast.FnDecl:
		return f.Position.Offset
	case *ast.ArrowFn:
		return f.Position.Offset
	}
	return -1
}

func fnName(n any) string {
	if f, ok := n.(*ast.FnDecl); ok {
		return f.Name
	}
	return ""
}

var identPart = regexp.MustCompile(`[^A-Za-z0-9_]`)

func (h *hoister) build(cands []candidate) (*Result, error) {
	bindings := h.moduleBindings()
	res := &Result{}
	type placed struct {
		a  *Action
		ex extent
	}
	var items []placed
	needed := map[int]bool{}
	for i, c := range cands {
		ex, err := h.extentOf(c)
		if err != nil {
			return nil, err
		}
		name := fnName(c.node)
		free, usesThis := freeNames(c.node)
		if usesThis {
			return nil, h.errAt(c.pos, `an inline "use server" function cannot use "this"`,
				`pass what you need through a local variable the function closes over`)
		}
		enclosing := map[string]bool{}
		for _, s := range c.scope {
			for n := range s {
				enclosing[n] = true
			}
		}
		if name != "" {
			delete(enclosing, name)
		}
		a := &Action{
			Index:          i,
			Name:           name,
			EnclosingAsync: c.async,
			Pos:            c.pos,
		}
		suffix := identPart.ReplaceAllString(name, "_")
		if suffix != "" {
			suffix = "_" + suffix
		}
		a.Export = "inline_" + strconv.Itoa(i) + suffix
		a.ID = ID(h.o.Rel, a.Export)
		var queue []string
		for _, n := range free {
			if enclosing[n] {
				a.Captures = append(a.Captures, n)
				continue
			}
			queue = append(queue, n)
		}
		// Module-level declarations the action uses move into the generated
		// module with it, transitively.
		for len(queue) > 0 {
			n := queue[0]
			queue = queue[1:]
			idx, ok := bindings[n]
			if !ok || needed[idx] {
				continue
			}
			b := h.bounds[idx]
			if h.containsCandidate(b, cands) {
				return nil, h.errAt(c.pos, fmt.Sprintf("an inline \"use server\" function uses %q, which contains the function itself", n),
					`move the shared value out of the component into its own declaration`)
			}
			needed[idx] = true
			target := b.stmt
			if es, ok := target.(*ast.ExportStmt); ok && es.Declaration != nil {
				target = es.Declaration
			}
			more, _ := freeNames(target)
			for _, m := range more {
				if _, isOwn := bindings[m]; isOwn {
					queue = append(queue, m)
				}
			}
		}
		res.Actions = append(res.Actions, a)
		items = append(items, placed{a: a, ex: ex})
	}

	// Generated module.
	var mod strings.Builder
	mod.WriteString("\"use server\";\n")
	idxs := make([]int, 0, len(needed))
	for i := range needed {
		idxs = append(idxs, i)
	}
	sort.Ints(idxs)
	for _, i := range idxs {
		mod.WriteString(h.moduleStmtText(h.bounds[i]))
		mod.WriteString("\n")
	}
	for k := range cands {
		it := items[k]
		a, ex := it.a, it.ex
		text := h.src[ex.start:ex.end]
		text = h.withCaptureParams(text, ex, a.Captures)
		mod.WriteString("export const " + a.Export + " = " + text + ";\n")
	}
	res.Module = mod.String()

	// Rewritten source: replace from the last action backwards.
	order := make([]int, len(items))
	for i := range order {
		order[i] = i
	}
	sort.Slice(order, func(i, j int) bool { return items[order[i]].ex.start > items[order[j]].ex.start })
	out := h.src
	for _, k := range order {
		it := items[k]
		a, ex := it.a, it.ex
		call := h.o.Call(a)
		start := ex.start
		var repl string
		switch {
		case ex.prefix == "export default":
			repl = call
		case a.Name != "":
			repl = "const " + a.Name + " = " + call + ";"
		default:
			repl = call
		}
		replaced := out[start:ex.end]
		repl += strings.Repeat("\n", strings.Count(replaced, "\n"))
		out = out[:start] + repl + out[ex.end:]
	}
	res.Source = out
	return res, nil
}

func (h *hoister) containsCandidate(b stmtBound, cands []candidate) bool {
	for _, c := range cands {
		off := startOffset(c.node)
		if off >= b.start && off < b.end {
			return true
		}
	}
	return false
}

var exportPrefix = regexp.MustCompile(`^export\s+(default\s+)?`)

// moduleStmtText copies a top-level statement for the generated module: its
// "export" keyword is dropped, and relative import specifiers become absolute
// so they resolve from the generated file's location.
func (h *hoister) moduleStmtText(b stmtBound) string {
	text := strings.TrimSpace(h.src[b.start:b.end])
	switch s := b.stmt.(type) {
	case *ast.ImportStmt:
		spec := trimQuotes(s.Source)
		if strings.HasPrefix(spec, "./") || strings.HasPrefix(spec, "../") {
			abs := strconv.Quote(filepath.ToSlash(filepath.Join(h.o.Dir, spec)))
			// The AST holds the specifier as esbuild reprinted it; the source
			// may use either quote.
			for _, q := range []string{`"`, `'`} {
				if strings.Contains(text, q+spec+q) {
					text = strings.Replace(text, q+spec+q, abs, 1)
					break
				}
			}
		}
	case *ast.ExportStmt:
		text = exportPrefix.ReplaceAllString(text, "")
	}
	return text
}

func trimQuotes(s string) string {
	if len(s) >= 2 && (s[0] == '"' || s[0] == '\'') && s[len(s)-1] == s[0] {
		return s[1 : len(s)-1]
	}
	return s
}

// withCaptureParams prepends the captured names to the parameter list of the
// function text.
func (h *hoister) withCaptureParams(text string, ex extent, caps []string) string {
	if len(caps) == 0 {
		return text
	}
	list := strings.Join(caps, ", ")
	if ex.bare >= 0 {
		t := h.toks[ex.bare]
		return text[:t.Offset-ex.start] + "(" + list + ", " + t.Value + ")" + text[t.End-ex.start:]
	}
	t := h.toks[ex.open]
	at := t.End - ex.start
	if ex.hasParams {
		list += ", "
	}
	return text[:at] + list + text[at:]
}

func sourceLine(src string, line int) string {
	lines := strings.Split(src, "\n")
	if line < 1 || line > len(lines) {
		return ""
	}
	return strings.TrimRight(lines[line-1], "\r")
}
