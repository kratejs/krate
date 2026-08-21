// Package reactive builds a compile-time signal dependency graph from emitted
// component signatures. It is used to validate hydration code before it ships:
// dead signals, write-only effects, effects with no reads, and circular
// signal/effect dependencies are all detectable without executing anything.
package reactive

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"krate-compiler/internal/ast"
	"krate-compiler/internal/irtree"
	"krate-compiler/internal/lexer"
	"krate-compiler/internal/parser"
)

// SignalNode tracks where a signal is read from and written to.
type SignalNode struct {
	Name   string
	Setter string
	// Reads / Writes key by effect name.
	Reads  map[string]bool
	Writes map[string]bool
	// Used is true if the signal is read or written by an effect, handler,
	// slot binding, attribute binding, or extra variable.
	Used bool
}

// EffectNode is a single createEffect (or createMemo) call site.
type EffectNode struct {
	Name   string
	Kind   string          // "effect" or "memo"
	Reads  map[string]bool // signal reads anywhere in the body (sync + deferred callbacks)
	Writes map[string]bool // signal writes anywhere in the body (sync + deferred callbacks)
	// SyncReads / SyncWrites are reads/writes at the effect's top-level scope
	// (excluding nested function/arrow bodies). Only synchronous edges can
	// produce a feedback loop, and only synchronous writes make an effect
	// "write-only".
	SyncReads  map[string]bool
	SyncWrites map[string]bool
	// HasNested is true when the body contains nested function/arrow bodies
	// (deferred callbacks like event handlers, intervals, promise chains).
	HasNested bool
	// HasOtherRefs is true when the synchronous body references anything other
	// than setter calls (props, local variables, conditionals, etc.). Such an
	// effect is doing more than a bare `setX(...)` and isn't a write-only bug.
	HasOtherRefs bool
}

// Graph is the whole-page dependency graph.
type Graph struct {
	Signals map[string]*SignalNode
	Effects []*EffectNode

	// Lookup maps for fast getter/setter resolution during effect scanning.
	byName   map[string]*SignalNode
	bySetter map[string]*SignalNode

	// Compiled regex caches (per-signal patterns) so Build() compiles each
	// read/write pattern exactly once instead of per scan call.
	readCache  map[string]*regexp.Regexp
	writeCache map[string]*regexp.Regexp
}

// Diagnostic is a single validation finding.
type Diagnostic struct {
	Severity string // "warning" or "error"
	Message  string
}

// readRe returns the cached read pattern for a signal name, compiling it once.
func (g *Graph) readRe(name string) *regexp.Regexp {
	if g.readCache == nil {
		g.readCache = make(map[string]*regexp.Regexp)
	}
	re, ok := g.readCache[name]
	if !ok {
		re = regexp.MustCompile(`\b` + regexp.QuoteMeta(name) + `\s*\(`)
		g.readCache[name] = re
	}
	return re
}

// writeRe returns the cached write pattern for a setter, compiling it once.
func (g *Graph) writeRe(setter string) *regexp.Regexp {
	if g.writeCache == nil {
		g.writeCache = make(map[string]*regexp.Regexp)
	}
	re, ok := g.writeCache[setter]
	if !ok {
		re = regexp.MustCompile(`\b` + regexp.QuoteMeta(setter) + `\s*\(`)
		g.writeCache[setter] = re
	}
	return re
}

// Build assembles a Graph from the per-component signatures the emitter
// produces for a page.
func Build(sigs []irtree.ComponentSignature) *Graph {
	g := &Graph{Signals: make(map[string]*SignalNode), byName: make(map[string]*SignalNode), bySetter: make(map[string]*SignalNode)}

	for si, sig := range sigs {
		comp := string(sig.ComponentID)
		if comp == "" {
			comp = fmt.Sprintf("<component %d>", si)
		}

		for _, s := range sig.Signals {
			if _, ok := g.Signals[s.Name]; !ok {
				n := &SignalNode{
					Name:   s.Name,
					Setter: s.SetterName,
					Reads:  make(map[string]bool),
					Writes: make(map[string]bool),
				}
				g.Signals[s.Name] = n
				g.byName[s.Name] = n
				if s.SetterName != "" {
					g.bySetter[s.SetterName] = n
				}
			}
		}

		// Handlers read the signals they reference (explicit list from IR).
		for _, h := range sig.Handlers {
			for _, name := range h.Signals {
				if n := g.Signals[name]; n != nil {
					n.Used = true
					n.Reads["handler:"+comp] = true
				}
			}
		}

		for _, sb := range sig.SlotBindings {
			for _, name := range sb.Signals {
				if n := g.Signals[name]; n != nil {
					n.Used = true
					n.Reads["binding:"+comp] = true
				}
			}
			scanExprForReads(g, comp, sb.ExprJS, "binding")
		}

		for _, a := range sig.AttrBindings {
			if a.SignalName != "" {
				if n := g.Signals[a.SignalName]; n != nil {
					n.Used = true
					n.Reads["attr:"+comp] = true
				}
			}
			scanExprForReads(g, comp, a.ExprSource, "attr")
		}

		// Effects: extract reads/writes from the emitted JS.
		for ei, eff := range sig.Effects {
			e := &EffectNode{
				Name:       fmt.Sprintf("%s:effect%d", comp, ei),
				Kind:       "effect",
				Reads:      make(map[string]bool),
				Writes:     make(map[string]bool),
				SyncReads:  make(map[string]bool),
				SyncWrites: make(map[string]bool),
			}
			scanEffectJS(g, e, eff)
			g.Effects = append(g.Effects, e)
		}

		// Memos behave like effects for dependency analysis.
		for mi, memo := range sig.Memos {
			e := &EffectNode{
				Name:       fmt.Sprintf("%s:memo%d", comp, mi),
				Kind:       "memo",
				Reads:      make(map[string]bool),
				Writes:     make(map[string]bool),
				SyncReads:  make(map[string]bool),
				SyncWrites: make(map[string]bool),
			}
			scanEffectJS(g, e, memo)
			g.Effects = append(g.Effects, e)
		}

		// Extra variables may read signals (e.g. `var x = count() + 1`).
		for _, ev := range sig.ExtraVars {
			scanExprForReads(g, comp, ev, "extra")
		}

		// Signals used anywhere in the component body (named helpers, control
		// flow, early returns) are used even if no binding/handler/effect edge
		// was emitted for them.
		for _, name := range sig.BodyUses {
			if n := g.Signals[name]; n != nil {
				n.Used = true
			}
		}
	}

	return g
}

// scanExprForReads marks signals referenced as `name()` in a JS expression.
func scanExprForReads(g *Graph, comp, js, kind string) {
	if js == "" {
		return
	}
	for _, n := range g.Signals {
		if g.readRe(n.Name).MatchString(js) {
			n.Used = true
			n.Reads[kind+":"+comp] = true
		}
	}
}

// scanEffectJS populates an EffectNode's Reads/Writes from its JS body. It
// parses the effect body and walks the AST so reads/writes inside nested
// function/arrow bodies (deferred callbacks: event handlers, intervals, promise
// chains) are distinguished from synchronous top-level reads/writes. Only the
// synchronous edges can create a feedback loop or a write-only effect; deferred
// writes are the normal "imperative/one-shot" effect pattern. If the body can't
// be parsed, a conservative regex scan is used as a fallback.
func scanEffectJS(g *Graph, e *EffectNode, js string) {
	if js == "" {
		return
	}
	toks := lexer.New(js).Tokenize()
	p := parser.New(toks)
	p.SetSource(js)
	prog := p.ParseProgram()
	if len(p.Errors()) > 0 || prog == nil {
		scanEffectJSRegex(g, e, js)
		return
	}
	for _, stmt := range prog.Body {
		if exprStmt, ok := stmt.(*ast.ExprStmt); ok {
			if call, ok := exprStmt.Expression.(*ast.CallExpr); ok {
				for i, arg := range call.Args {
					if i == 0 {
						// The effect's own function body is the synchronous scope.
						scanEffectArg(g, e, arg)
					} else {
						scanExprForSignals(g, e, arg, false)
					}
				}
				continue
			}
		}
		scanStmtForSignals(g, e, []ast.Stmt{stmt}, false)
	}
}

// scanEffectArg scans the createEffect/createMemo callback. The callback body
// is the synchronous scope; functions nested inside it are deferred.
func scanEffectArg(g *Graph, e *EffectNode, arg ast.Expr) {
	switch a := arg.(type) {
	case *ast.ArrowFn:
		scanStmtForSignals(g, e, a.Body, false)
	default:
		scanExprForSignals(g, e, arg, false)
	}
}

// scanEffectJSRegex is the fallback for effect bodies that fail to parse. It
// conservatively treats every read/write as synchronous.
func scanEffectJSRegex(g *Graph, e *EffectNode, js string) {
	if strings.Contains(js, "=>") || strings.Contains(js, "function ") {
		e.HasNested = true
	}
	for name, n := range g.Signals {
		if g.readRe(n.Name).MatchString(js) {
			e.Reads[name] = true
			e.SyncReads[name] = true
			n.Reads[e.Name] = true
			n.Used = true
		}
		if n.Setter != "" && g.writeRe(n.Setter).MatchString(js) {
			e.Writes[name] = true
			e.SyncWrites[name] = true
			n.Writes[e.Name] = true
			n.Used = true
		}
	}
}

// scanStmtForSignals walks statements looking for signal reads/writes. When
// deferred is true the node is inside a nested function body.
func scanStmtForSignals(g *Graph, e *EffectNode, stmts []ast.Stmt, deferred bool) {
	for _, stmt := range stmts {
		switch s := stmt.(type) {
		case *ast.ExprStmt:
			scanExprForSignals(g, e, s.Expression, deferred)
		case *ast.VarStmt:
			for _, decl := range s.Decls {
				if decl.Init != nil {
					scanExprForSignals(g, e, decl.Init, deferred)
				}
			}
		case *ast.ReturnStmt:
			if s.Value != nil {
				scanExprForSignals(g, e, s.Value, deferred)
			}
		case *ast.BlockStmt:
			scanStmtForSignals(g, e, s.Body, deferred)
		case *ast.IfStmt:
			scanExprForSignals(g, e, s.Test, deferred)
			scanStmtForSignals(g, e, s.Consequent, deferred)
			scanStmtForSignals(g, e, s.Alternate, deferred)
		case *ast.ForStmt:
			if s.Init != nil {
				scanStmtForSignals(g, e, []ast.Stmt{s.Init}, deferred)
			}
			if s.Test != nil {
				scanExprForSignals(g, e, s.Test, deferred)
			}
			scanStmtForSignals(g, e, s.Body, deferred)
		case *ast.WhileStmt:
			scanExprForSignals(g, e, s.Test, deferred)
			scanStmtForSignals(g, e, s.Body, deferred)
		case *ast.DoWhileStmt:
			scanStmtForSignals(g, e, s.Body, deferred)
			scanExprForSignals(g, e, s.Test, deferred)
		case *ast.SwitchStmt:
			scanExprForSignals(g, e, s.Discriminant, deferred)
			for _, c := range s.Cases {
				if c.Test != nil {
					scanExprForSignals(g, e, c.Test, deferred)
				}
				scanStmtForSignals(g, e, c.Body, deferred)
			}
		case *ast.TryStmt:
			scanStmtForSignals(g, e, s.Body, deferred)
			if s.Catch != nil {
				scanStmtForSignals(g, e, s.Catch.Body, deferred)
			}
			scanStmtForSignals(g, e, s.Finally, deferred)
		case *ast.ThrowStmt:
			scanExprForSignals(g, e, s.Value, deferred)
		}
	}
}

// scanExprForSignals walks expressions looking for signal reads/writes.
func scanExprForSignals(g *Graph, e *EffectNode, expr ast.Expr, deferred bool) {
	if expr == nil {
		return
	}
	switch x := expr.(type) {
	case *ast.CallExpr:
		if id, ok := x.Callee.(*ast.Identifier); ok {
			if sig := g.byName[id.Name]; sig != nil {
				// Read: signalName()
				e.Reads[sig.Name] = true
				sig.Reads[e.Name] = true
				sig.Used = true
				if !deferred {
					e.SyncReads[sig.Name] = true
				}
			} else if sig := g.bySetter[id.Name]; sig != nil {
				// Write: setSignalName(...)
				e.Writes[sig.Name] = true
				sig.Writes[e.Name] = true
				sig.Used = true
				if !deferred {
					e.SyncWrites[sig.Name] = true
				}
			} else if !deferred {
				// Referencing something that isn't a signal/setter at the sync
				// scope means the effect does more than a bare setter call.
				e.HasOtherRefs = true
			}
		}
		// Recurse into the callee (method chains like fetch().then().catch()
		// nest the whole chain as the callee of the outer call) and the args.
		scanExprForSignals(g, e, x.Callee, deferred)
		for _, arg := range x.Args {
			scanExprForSignals(g, e, arg, deferred)
		}
	case *ast.MemberExpr:
		if id, ok := x.Object.(*ast.Identifier); ok && id.Name == "props" && !deferred {
			e.HasOtherRefs = true
		}
		scanExprForSignals(g, e, x.Object, deferred)
		scanExprForSignals(g, e, x.Property, deferred)
	case *ast.BinaryExpr:
		scanExprForSignals(g, e, x.Left, deferred)
		scanExprForSignals(g, e, x.Right, deferred)
	case *ast.UnaryExpr:
		scanExprForSignals(g, e, x.Arg, deferred)
	case *ast.ConditionalExpr:
		if !deferred {
			e.HasOtherRefs = true
		}
		scanExprForSignals(g, e, x.Test, deferred)
		scanExprForSignals(g, e, x.Consequent, deferred)
		scanExprForSignals(g, e, x.Alternate, deferred)
	case *ast.TemplateExpr:
		for _, part := range x.Parts {
			scanExprForSignals(g, e, part, deferred)
		}
	case *ast.ArrowFn:
		// Nested function body: everything inside is a deferred callback.
		e.HasNested = true
		scanStmtForSignals(g, e, x.Body, true)
	case *ast.AwaitExpr:
		scanExprForSignals(g, e, x.Arg, deferred)
	case *ast.NewExpr:
		scanExprForSignals(g, e, x.Callee, deferred)
		for _, arg := range x.Args {
			scanExprForSignals(g, e, arg, deferred)
		}
	case *ast.Identifier:
		if !deferred && g.byName[x.Name] == nil && g.bySetter[x.Name] == nil {
			e.HasOtherRefs = true
		}
	}
}

// Validate runs all compile-time checks and returns findings.
func (g *Graph) Validate() []Diagnostic {
	var diags []Diagnostic

	// 1. Unused signals (declared but never read or written anywhere).
	names := make([]string, 0, len(g.Signals))
	for name := range g.Signals {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		n := g.Signals[name]
		if !n.Used {
			diags = append(diags, Diagnostic{
				Severity: "warning",
				Message:  fmt.Sprintf("reactive: signal %q is declared but never read or written", name),
			})
		}
	}

	// 2/3. Write-only and no-read effects.
	// A "write-only, probably a bug" is only reported for a SYNCHRONOUS write
	// (top-level setter call) with no reads, no nested callbacks, and no other
	// references (props / conditionals / locals). Effects that write inside
	// deferred callbacks (timers, listeners, promise chains), sync controlled
	// props, or initialize state from props are all legitimate patterns.
	for _, e := range g.Effects {
		if e.Kind != "effect" {
			continue
		}
		if len(e.SyncWrites) > 0 && len(e.Reads) == 0 && !e.HasNested && !e.HasOtherRefs {
			diags = append(diags, Diagnostic{
				Severity: "warning",
				Message:  fmt.Sprintf("reactive: effect %q calls setters but reads no signals — probably a bug", e.Name),
			})
		} else if len(e.Reads) == 0 && len(e.Writes) == 0 && !e.HasNested {
			diags = append(diags, Diagnostic{
				Severity: "warning",
				Message:  fmt.Sprintf("reactive: effect %q reads no signals and writes none (runs exactly once, may be intended)", e.Name),
			})
		}
	}

	// 4. Circular signal/effect dependencies.
	diags = append(diags, g.findCycles()...)

	return diags
}

// findCycles reports circular dependencies in the bipartite
// effect → (sync reads) signal → (sync writes) effect graph. Only synchronous
// edges are used: a write inside a deferred callback (event handler, timer,
// promise chain) cannot create a synchronous feedback loop.
func (g *Graph) findCycles() []Diagnostic {
	// Vertex ids: signals are "sig:<name>", effects are "eff:<name>".
	adj := make(map[string][]string)
	for name := range g.Signals {
		adj["sig:"+name] = nil
	}
	for _, e := range g.Effects {
		id := "eff:" + e.Name
		for read := range e.SyncReads {
			adj[id] = append(adj[id], "sig:"+read)
		}
		for write := range e.SyncWrites {
			adj["sig:"+write] = append(adj["sig:"+write], id)
		}
	}

	const (
		white = 0
		gray  = 1
		black = 2
	)
	color := make(map[string]int)
	stack := []string{}
	seen := make(map[string]bool)
	var diags []Diagnostic

	var visit func(v string)
	visit = func(v string) {
		color[v] = gray
		stack = append(stack, v)
		for _, w := range adj[v] {
			switch color[w] {
			case white:
				visit(w)
			case gray:
				// Back edge: cycle is from w..end of stack.
				start := 0
				for i, s := range stack {
					if s == w {
						start = i
						break
					}
				}
				cycle := append([]string{}, stack[start:]...)
				cycle = append(cycle, w)
				key := cycleKey(cycle)
				if !seen[key] {
					seen[key] = true
					diags = append(diags, Diagnostic{
						Severity: "warning",
						Message:  fmt.Sprintf("reactive: circular dependency: %s", strings.Join(cycle, " → ")),
					})
				}
			}
		}
		stack = stack[:len(stack)-1]
		color[v] = black
	}

	for v := range adj {
		if color[v] == white {
			visit(v)
		}
	}
	return diags
}

// cycleKey canonicalizes a cycle path so the same cycle is only reported once.
func cycleKey(cycle []string) string {
	sorted := append([]string{}, cycle...)
	sort.Strings(sorted)
	return strings.Join(sorted, ",")
}
