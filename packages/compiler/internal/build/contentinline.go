package build

import (
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"sync"

	"github.com/kratejs/krate/packages/compiler/ast"
	"github.com/kratejs/krate/packages/compiler/internal/bundler"
	"github.com/kratejs/krate/packages/compiler/internal/content"
	"github.com/kratejs/krate/packages/compiler/internal/diag"
	"github.com/kratejs/krate/packages/compiler/internal/irtree"
	"github.com/kratejs/krate/packages/compiler/internal/lexer"
	"github.com/kratejs/krate/packages/compiler/internal/parser"
	"github.com/kratejs/krate/packages/native/jsruntime"
)

// contentFoldCache memoizes baked collection-chain evaluation across pages within
// a build. The key is the rendered chain source, which embeds the collection
// entries and any literal bindings, so identical templates on many generated
// routes (e.g. a `[slug]` page calling getCollection("blog").map(...)) share one
// QuickJS evaluation. The decoded value is read-only, so arrayASTFromJSON can
// still build a fresh AST per call site. Cleared per build.
var contentFoldCache = struct {
	mu sync.Mutex
	m  map[string]contentFoldEntry
}{m: make(map[string]contentFoldEntry)}

type contentFoldEntry struct {
	val any
	ok  bool
}

func resetContentFoldCache() {
	contentFoldCache.mu.Lock()
	contentFoldCache.m = make(map[string]contentFoldEntry)
	contentFoldCache.mu.Unlock()
}

// contentFoldCacheEnabled can be disabled with KRATE_NO_CONTENT_FOLD_CACHE (used
// for before/after measurement).
func contentFoldCacheEnabled() bool {
	return os.Getenv("KRATE_NO_CONTENT_FOLD_CACHE") == ""
}

// InlineContent rewrites `getCollection("name")` calls (and the `getCollection`
// import) into a literal array expression parsed from the collection's baked
// data. After inlining, existing build-time folding (`collectLocalVars`,
// `.map()` resolution, member-chain const folding) renders collection content
// into static HTML with no runtime work.
// It also rewrites direct collection imports - `import { blog } from
// "krate/content"` - but those are less common; the primary API is
// getCollection.
func (b *Builder) InlineContent(prog *ast.Program) {
	if prog == nil || len(b.contentCollections) == 0 {
		return
	}
	ic := &inlineCtx{collections: b.contentCollections, use: collectFieldUse(prog), deferred: map[*ast.VarDecl]bool{}}
	if v, ok := b.templateCovered.Load(prog); ok {
		ic.covered = v.(map[string]bool)
	}
	defer func() {
		if len(ic.sites) > 0 {
			b.collectionSites.Store(prog, ic.sites)
		}
	}()
	// Collect simple literal local bindings (e.g. `const slug = "hello"` after
	// route-param substitution) so collection chains that reference them can be
	// evaluated. Without this, `find((p) => p.slug === slug)` can't fold.
	bindings := collectLiteralBindings(prog)
	if v, ok := b.paramBindings.Load(prog); ok {
		for k, js := range v.(map[string]string) {
			bindings[k] = js
		}
	}
	ic.helpers = collectHelpers(prog)
	scope := ic.pushAliasScope(nil, prog.Body)
	for _, stmt := range prog.Body {
		inlineContentStmt(stmt, ic, bindings)
	}
	ic.popAliasScope(scope, prog.Body, bindings)
	ic.settleHelpers(prog)
}

// inlineCtx carries the state of one program's content inlining.
type inlineCtx struct {
	collections map[string][]content.Entry
	// use limits the inlined entry fields to the ones the program mentions.
	use *content.FieldUse
	// sites are the getCollection calls left to run in the browser: chains
	// that could not be folded at build time and bare collection reads.
	sites []ast.Pos
	depth int
	// covered holds the route-param values that have a generated page of their
	// own. A collection read that stays in the browser (depth > 0) in a
	// dynamic-route template only needs the entries without one.
	covered map[string]bool
	// scopes and deferred drive alias folding (see contentalias.go).
	scopes   []aliasScope
	deferred map[*ast.VarDecl]bool
	helpers  map[string]*helperEntry
}

// browserEntries returns the entries of a collection read that cannot be
// folded: all of them, minus those already covered by a generated page when
// this is the fallback template of a generateStaticParams route.
func (ic *inlineCtx) browserEntries(name string) []content.Entry {
	entries := ic.collections[name]
	if ic.depth == 0 || len(ic.covered) == 0 {
		return entries
	}
	kept := make([]content.Entry, 0, len(entries))
	for _, e := range entries {
		if !ic.covered[e.Slug] {
			kept = append(kept, e)
		}
	}
	return kept
}

// collectFieldUse gathers every identifier and string the program mentions, so
// collection entries are inlined with only the fields the code can read
// (slug and path always stay). Code that reads fields dynamically keeps all.
func collectFieldUse(prog *ast.Program) *content.FieldUse {
	use := &content.FieldUse{Names: map[string]bool{}}
	var walk func(v reflect.Value)
	add := func(str string) {
		for _, w := range identWordRe.FindAllString(str, -1) {
			use.Names[w] = true
			if w == "Object" || w == "JSON" || w == "Reflect" {
				use.All = true
			}
		}
	}
	walk = func(v reflect.Value) {
		switch v.Kind() {
		case reflect.Interface:
			if !v.IsNil() {
				walk(v.Elem())
			}
		case reflect.Pointer:
			if v.IsNil() {
				return
			}
			switch x := v.Interface().(type) {
			case *ast.MemberExpr:
				if x.Computed {
					if _, lit := x.Property.(*ast.Literal); !lit {
						use.All = true
					}
				}
			case *ast.ObjectProp:
				if x.Spread || x.Computed {
					use.All = true
				}
			case *ast.JSXAttr:
				if x.Spread {
					use.All = true
				}
			case *ast.UnaryExpr:
				if x.Op == "..." {
					use.All = true
				}
			}
			walk(v.Elem())
		case reflect.Slice, reflect.Array:
			for i := 0; i < v.Len(); i++ {
				walk(v.Index(i))
			}
		case reflect.Struct:
			for i := 0; i < v.NumField(); i++ {
				if v.Type().Field(i).IsExported() {
					walk(v.Field(i))
				}
			}
		case reflect.String:
			add(v.String())
		}
	}
	walk(reflect.ValueOf(prog.Body))
	return use
}

var identWordRe = regexp.MustCompile(`[A-Za-z_$][A-Za-z0-9_$]*`)

// collectLiteralBindings maps local/module identifiers bound to a literal
// expression to their JS source, so collection chain evaluation can reference
// them. Only simple literals (string/number/bool/null) and literal-only
// templates are collected.
func collectLiteralBindings(prog *ast.Program) map[string]string {
	out := map[string]string{}
	var walkStmts func(stmts []ast.Stmt)
	var walkStmt func(stmt ast.Stmt)
	walkStmt = func(stmt ast.Stmt) {
		switch s := stmt.(type) {
		case *ast.VarStmt:
			for _, d := range s.Decls {
				if d.Name == "" || d.Init == nil {
					continue
				}
				if js, ok := literalJS(d.Init); ok {
					out[d.Name] = js
				}
			}
		case *ast.FnDecl:
			walkStmts(s.Body)
		case *ast.ExportStmt:
			if s.Declaration != nil {
				walkStmt(s.Declaration)
			}
		case *ast.ReturnStmt:
			// An expression-bodied arrow is `Body: [ReturnStmt]`; descend so a
			// nested block's consts are still found (best-effort, flat scope).
		case *ast.IfStmt:
			walkStmts(s.Consequent)
			walkStmts(s.Alternate)
		case *ast.BlockStmt:
			walkStmts(s.Body)
		}
	}
	walkStmts = func(stmts []ast.Stmt) {
		for _, s := range stmts {
			walkStmt(s)
		}
	}
	walkStmts(prog.Body)
	return out
}

// literalJS returns the JS source for a simple literal expression.
func literalJS(expr ast.Expr) (string, bool) {
	switch e := expr.(type) {
	case *ast.Literal:
		// GenerateExprJS renders literals to valid JS source (escapes intact).
		return irtree.GenerateExprJS(e, nil), true
	case *ast.TemplateExpr:
		// Only literal-only templates (no interpolated identifiers).
		for _, p := range e.Parts {
			if _, ok := p.(*ast.Literal); !ok {
				return "", false
			}
		}
		js := irtree.GenerateExprJS(e, nil)
		return js, js != ""
	}
	return "", false
}

// inlineContentStmt walks a statement, replacing getCollection call chains with
// literal arrays.
func inlineContentStmt(stmt ast.Stmt, ic *inlineCtx, bindings map[string]string) {
	switch s := stmt.(type) {
	case *ast.ExportStmt:
		if s.Declaration != nil {
			inlineContentStmt(s.Declaration, ic, bindings)
		}
	case *ast.FnDecl:
		scope := ic.pushAliasScope(s.Params, s.Body)
		for _, inner := range s.Body {
			inlineContentStmt(inner, ic, bindings)
		}
		ic.popAliasScope(scope, s.Body, bindings)
	case *ast.VarStmt:
		for _, d := range s.Decls {
			if d.Init != nil && !ic.deferred[d] {
				d.Init = inlineContentExpr(d.Init, ic, bindings)
			}
		}
	case *ast.ReturnStmt:
		if s.Value != nil {
			s.Value = inlineContentExpr(s.Value, ic, bindings)
		}
	case *ast.ExprStmt:
		if s.Expression != nil {
			s.Expression = inlineContentExpr(s.Expression, ic, bindings)
		}
	case *ast.IfStmt:
		if s.Test != nil {
			s.Test = inlineContentExpr(s.Test, ic, bindings)
		}
		for _, inner := range s.Consequent {
			inlineContentStmt(inner, ic, bindings)
		}
		for _, inner := range s.Alternate {
			inlineContentStmt(inner, ic, bindings)
		}
	case *ast.BlockStmt:
		for _, inner := range s.Body {
			inlineContentStmt(inner, ic, bindings)
		}
	case *ast.ForStmt:
		if s.Test != nil {
			s.Test = inlineContentExpr(s.Test, ic, bindings)
		}
		for _, inner := range s.Body {
			inlineContentStmt(inner, ic, bindings)
		}
	}
}

// inlineContentExpr rewrites getCollection(...) chains within an expression.
func inlineContentExpr(expr ast.Expr, ic *inlineCtx, bindings map[string]string) ast.Expr {
	if expr == nil {
		return nil
	}
	switch e := expr.(type) {
	case *ast.CallExpr:
		if rep := ic.foldAliased(e, bindings); rep != nil {
			return rep
		}
		if rep := ic.foldHelperCall(e, bindings); rep != nil {
			return rep
		}
		// getCollection("name")
		if lit := getCollectionName(e); lit != "" {
			if ic.depth == 0 {
				ic.sites = append(ic.sites, e.Position)
			}
			return literalArrayExpr(ic.browserEntries(lit), ic.use)
		}
		// `...map(fn)` rooted at getCollection: fold the receiver chain
		// (filter/sort/etc.) to a literal array so the existing `.map()` list
		// resolution sees real items. The `.map` itself is left intact.
		if arr := foldContentMapReceiver(e, ic.collections, bindings); arr != nil {
			if mem, ok := e.Callee.(*ast.MemberExpr); ok {
				mem.Object = arr
			}
			return e
		}
		// Any other chain rooted at getCollection (e.g. `.filter().sort()`)
		// folds wholesale to a literal array.
		if arr := foldCollectionChain(e, ic.collections, bindings); arr != nil {
			return arr
		}
		if rootCollectionName(e) != "" {
			// Unfoldable chain: the collection stays in the page script.
			if ic.depth == 0 {
				ic.sites = append(ic.sites, e.Position)
			}
			ic.depth++
			defer func() { ic.depth-- }()
		}
		e.Callee = inlineContentExpr(e.Callee, ic, bindings)
		for i, arg := range e.Args {
			e.Args[i] = inlineContentExpr(arg, ic, bindings)
		}
		return e
	case *ast.MemberExpr:
		if rep := ic.foldAliased(e, bindings); rep != nil {
			return rep
		}
		if arr := foldCollectionChain(e, ic.collections, bindings); arr != nil {
			return arr
		}
		if rootCollectionName(e) != "" {
			if ic.depth == 0 {
				ic.sites = append(ic.sites, e.Position)
			}
			ic.depth++
			defer func() { ic.depth-- }()
		}
		e.Object = inlineContentExpr(e.Object, ic, bindings)
		return e
	case *ast.ArrayExpr:
		for i, el := range e.Elements {
			e.Elements[i] = inlineContentExpr(el, ic, bindings)
		}
		return e
	case *ast.ObjectExpr:
		for _, prop := range e.Properties {
			if prop.Value != nil {
				prop.Value = inlineContentExpr(prop.Value, ic, bindings)
			}
		}
		return e
	case *ast.BinaryExpr:
		e.Left = inlineContentExpr(e.Left, ic, bindings)
		e.Right = inlineContentExpr(e.Right, ic, bindings)
		return e
	case *ast.ConditionalExpr:
		e.Test = inlineContentExpr(e.Test, ic, bindings)
		e.Consequent = inlineContentExpr(e.Consequent, ic, bindings)
		e.Alternate = inlineContentExpr(e.Alternate, ic, bindings)
		return e
	case *ast.UnaryExpr:
		e.Arg = inlineContentExpr(e.Arg, ic, bindings)
		return e
	case *ast.TemplateExpr:
		for i, part := range e.Parts {
			e.Parts[i] = inlineContentExpr(part, ic, bindings)
		}
		return e
	case *ast.ArrowFn:
		// A function body is its own alias scope; a collection read inside it
		// (a component written as an arrow, a helper, a handler) is inlined
		// like one in a declared function.
		scope := ic.pushAliasScope(e.Params, e.Body)
		for _, inner := range e.Body {
			inlineContentStmt(inner, ic, bindings)
		}
		ic.popAliasScope(scope, e.Body, bindings)
		return e
	case *ast.JSXElement:
		inlineContentJSX(e, ic, bindings)
		return e
	case *ast.JSXFragment:
		inlineContentJSXChildren(e.Children, ic, bindings)
		return e
	}
	return expr
}

func inlineContentJSX(el *ast.JSXElement, ic *inlineCtx, bindings map[string]string) {
	for _, attr := range el.Opening.Attributes {
		if attr.Spread || attr.Value == nil {
			continue
		}
		attr.Value = inlineContentExpr(attr.Value, ic, bindings)
	}
	inlineContentJSXChildren(el.Children, ic, bindings)
}

func inlineContentJSXChildren(children []ast.JSXChild, ic *inlineCtx, bindings map[string]string) {
	for _, child := range children {
		switch c := child.(type) {
		case *ast.JSXExprContainer:
			c.Expression = inlineContentExpr(c.Expression, ic, bindings)
		case *ast.JSXElementChild:
			inlineContentJSX(c.Element, ic, bindings)
		case *ast.JSXFragmentChild:
			inlineContentJSXChildren(c.Fragment.Children, ic, bindings)
		}
	}
}

// getCollectionName returns the literal collection name when expr is a
// `getCollection("name")` call, else "".
func getCollectionName(call *ast.CallExpr) string {
	id, ok := call.Callee.(*ast.Identifier)
	if !ok || id.Name != "getCollection" || len(call.Args) != 1 {
		return ""
	}
	if lit, ok := call.Args[0].(*ast.Literal); ok && lit.Kind == ast.StringLit {
		return lit.Value
	}
	return ""
}

// foldContentMapReceiver detects a `.map(...)` call whose receiver is a chain
// rooted at `getCollection("name")`, evaluates the receiver chain (everything
// before `.map`) in QuickJS, and returns the resulting array as an AST
// ArrayExpr. Returns nil when the chain isn't rooted at getCollection or the
// evaluation fails.
func foldContentMapReceiver(call *ast.CallExpr, collections map[string][]content.Entry, bindings map[string]string) ast.Expr {
	mem, ok := call.Callee.(*ast.MemberExpr)
	if !ok {
		return nil
	}
	if prop, ok := mem.Property.(*ast.Identifier); !ok || prop.Name != "map" {
		return nil
	}
	return foldCollectionChain(mem.Object, collections, bindings)
}

// foldCollectionChain evaluates a chain rooted at `getCollection("name")`
// (including the getCollection call itself) in QuickJS and returns the result
// as a literal AST expression. Returns nil when the expression isn't rooted at
// getCollection, contains unsupported constructs, or evaluation fails.
func foldCollectionChain(expr ast.Expr, collections map[string][]content.Entry, bindings map[string]string) ast.Expr {
	base := rootCollectionName(expr)
	if base == "" {
		return nil
	}
	entries, ok := collections[base]
	if !ok {
		return nil
	}
	chainJS, ok := renderContentChain(expr, base, entries, bindings)
	if !ok {
		return nil
	}
	decoded, ok := foldChainJSON(chainJS)
	if !ok {
		return nil
	}
	return arrayASTFromJSON(decoded)
}

// foldChainJSON evaluates a baked collection chain and returns its decoded JSON
// value, memoized by the chain source. The QuickJS evaluation is the expensive
// step (and repeats for every generated route that shares a template); the
// decoded value is immutable, so callers build fresh AST nodes from it.
func foldChainJSON(chainJS string) (any, bool) {
	if !contentFoldCacheEnabled() {
		return evaluateChainJSON(chainJS)
	}

	contentFoldCache.mu.Lock()
	if e, ok := contentFoldCache.m[chainJS]; ok {
		contentFoldCache.mu.Unlock()
		return e.val, e.ok
	}
	contentFoldCache.mu.Unlock()

	entry := contentFoldEntry{}
	entry.val, entry.ok = evaluateChainJSON(chainJS)

	contentFoldCache.mu.Lock()
	contentFoldCache.m[chainJS] = entry
	contentFoldCache.mu.Unlock()
	return entry.val, entry.ok
}

// evaluateChainJSON runs the (uncached) QuickJS evaluation of a baked chain.
func evaluateChainJSON(chainJS string) (any, bool) {
	out, err := jsruntime.EvaluateExpr("JSON.stringify(" + chainJS + ")")
	if err != nil || out == "" || out == "undefined" {
		return nil, false
	}
	var decoded any
	if err := json.Unmarshal([]byte(out), &decoded); err != nil {
		return nil, false
	}
	return decoded, true
}

// rootCollectionName returns the collection name if expr is (or is chained off)
// a direct `getCollection("name")` call.
func rootCollectionName(expr ast.Expr) string {
	switch e := expr.(type) {
	case *ast.CallExpr:
		if name := getCollectionName(e); name != "" {
			return name
		}
		if mem, ok := e.Callee.(*ast.MemberExpr); ok {
			return rootCollectionName(mem.Object)
		}
	case *ast.MemberExpr:
		return rootCollectionName(e.Object)
	}
	return ""
}

// renderContentChain renders an expression chain rooted at `getCollection(base)`
// as JavaScript, substituting the baked entries array for the root call.
// Returns ok=false on any construct it can't faithfully render.
func renderContentChain(expr ast.Expr, base string, entries []content.Entry, bindings map[string]string) (string, bool) {
	switch e := expr.(type) {
	case *ast.CallExpr:
		if getCollectionName(e) == base {
			return content.CollectionJS(entries), true
		}
		mem, ok := e.Callee.(*ast.MemberExpr)
		if !ok {
			return "", false
		}
		prop, ok := mem.Property.(*ast.Identifier)
		if !ok {
			return "", false
		}
		recv, ok := renderContentChain(mem.Object, base, entries, bindings)
		if !ok {
			return "", false
		}
		var args []string
		for _, a := range e.Args {
			js := renderArgWithBindings(a, bindings)
			if js == "" {
				return "", false
			}
			args = append(args, js)
		}
		return recv + "." + prop.Name + "(" + strings.Join(args, ", ") + ")", true
	case *ast.MemberExpr:
		recv, ok := renderContentChain(e.Object, base, entries, bindings)
		if !ok {
			return "", false
		}
		prop, ok := e.Property.(*ast.Identifier)
		if !ok {
			return "", false
		}
		return recv + "." + prop.Name, true
	}
	return "", false
}

// renderArgWithBindings renders a callback argument (e.g. a filter/find
// predicate) to JS, substituting known literal local bindings first so chains
// like `.find((p) => p.slug === slug)` can be evaluated. If the argument
// contains any unknown identifier, returns "" so the caller bails rather than
// emitting a reference that would throw.
func renderArgWithBindings(arg ast.Expr, bindings map[string]string) string {
	substituted := substituteBindings(arg, bindings)
	if hasUnknownIdentifier(substituted) {
		return ""
	}
	return irtree.GenerateExprJS(substituted, nil)
}

// substituteBindings replaces bare identifier references with their literal
// source. It skips identifiers that are declared as parameters inside an arrow
// (so `p` in `(p) => p.slug` is not replaced).
func substituteBindings(expr ast.Expr, bindings map[string]string) ast.Expr {
	if len(bindings) == 0 {
		return expr
	}
	switch e := expr.(type) {
	case *ast.Identifier:
		if js, ok := bindings[e.Name]; ok {
			if lit := literalFromJS(js); lit != nil {
				return lit
			}
		}
		return e
	case *ast.ArrowFn:
		shadow := map[string]bool{}
		for _, p := range e.Params {
			if p != nil {
				shadow[p.Name] = true
			}
		}
		local := make(map[string]string, len(bindings))
		for k, v := range bindings {
			if !shadow[k] {
				local[k] = v
			}
		}
		for _, stmt := range e.Body {
			substituteBindingsStmt(stmt, local)
		}
		return e
	case *ast.MemberExpr:
		e.Object = substituteBindings(e.Object, bindings)
		return e
	case *ast.CallExpr:
		e.Callee = substituteBindings(e.Callee, bindings)
		for i, a := range e.Args {
			e.Args[i] = substituteBindings(a, bindings)
		}
		return e
	case *ast.BinaryExpr:
		e.Left = substituteBindings(e.Left, bindings)
		e.Right = substituteBindings(e.Right, bindings)
		return e
	case *ast.UnaryExpr:
		e.Arg = substituteBindings(e.Arg, bindings)
		return e
	case *ast.ConditionalExpr:
		e.Test = substituteBindings(e.Test, bindings)
		e.Consequent = substituteBindings(e.Consequent, bindings)
		e.Alternate = substituteBindings(e.Alternate, bindings)
		return e
	case *ast.TemplateExpr:
		for i, part := range e.Parts {
			e.Parts[i] = substituteBindings(part, bindings)
		}
		return e
	case *ast.ArrayExpr:
		for i, el := range e.Elements {
			e.Elements[i] = substituteBindings(el, bindings)
		}
		return e
	case *ast.ObjectExpr:
		for _, p := range e.Properties {
			if p.Value != nil {
				p.Value = substituteBindings(p.Value, bindings)
			}
		}
		return e
	}
	return expr
}

func substituteBindingsStmt(stmt ast.Stmt, bindings map[string]string) {
	if rs, ok := stmt.(*ast.ReturnStmt); ok && rs.Value != nil {
		rs.Value = substituteBindings(rs.Value, bindings)
	}
}

// literalFromJS parses a JS literal source string back into an AST literal.
func literalFromJS(js string) ast.Expr {
	prog := parseInlineSource("(" + js + ")")
	if prog == nil {
		return nil
	}
	for _, stmt := range prog.Body {
		if es, ok := stmt.(*ast.ExprStmt); ok {
			switch es.Expression.(type) {
			case *ast.Literal, *ast.TemplateExpr, *ast.ArrayExpr, *ast.ObjectExpr:
				return es.Expression
			}
		}
	}
	return nil
}

// hasUnknownIdentifier reports whether expr references a bare identifier that
// is neither a known JS global nor bound by an enclosing arrow parameter.
func hasUnknownIdentifier(expr ast.Expr) bool {
	found := false
	var walk func(e ast.Expr, bound map[string]bool)
	walk = func(e ast.Expr, bound map[string]bool) {
		if e == nil || found {
			return
		}
		switch x := e.(type) {
		case *ast.Identifier:
			if !bound[x.Name] && !knownGlobalIdent(x.Name) {
				found = true
			}
		case *ast.MemberExpr:
			walk(x.Object, bound)
		case *ast.CallExpr:
			walk(x.Callee, bound)
			for _, a := range x.Args {
				walk(a, bound)
			}
		case *ast.BinaryExpr:
			walk(x.Left, bound)
			walk(x.Right, bound)
		case *ast.UnaryExpr:
			walk(x.Arg, bound)
		case *ast.ConditionalExpr:
			walk(x.Test, bound)
			walk(x.Consequent, bound)
			walk(x.Alternate, bound)
		case *ast.TemplateExpr:
			for _, p := range x.Parts {
				walk(p, bound)
			}
		case *ast.ArrayExpr:
			for _, el := range x.Elements {
				walk(el, bound)
			}
		case *ast.ObjectExpr:
			for _, p := range x.Properties {
				walk(p.Value, bound)
			}
		case *ast.ArrowFn:
			inner := map[string]bool{}
			for k := range bound {
				inner[k] = true
			}
			for _, p := range x.Params {
				if p != nil {
					inner[p.Name] = true
				}
			}
			for _, stmt := range x.Body {
				if rs, ok := stmt.(*ast.ReturnStmt); ok {
					walk(rs.Value, inner)
				}
			}
		}
	}
	walk(expr, nil)
	return found
}

// knownGlobalIdent reports whether name is a JS global we allow in evaluated
// predicates/literals.
func knownGlobalIdent(name string) bool {
	switch name {
	case "undefined", "null", "true", "false", "NaN", "Infinity",
		"JSON", "Math", "String", "Number", "Boolean", "Object", "Array", "Date":
		return true
	}
	return false
}

// arrayASTFromJSON converts a JSON-decoded array into an AST ArrayExpr of
// object/array/scalar literals.
func arrayASTFromJSON(v any) ast.Expr {
	switch t := v.(type) {
	case []any:
		els := make([]ast.Expr, len(t))
		for i, item := range t {
			els[i] = arrayASTFromJSON(item)
		}
		return &ast.ArrayExpr{Elements: els}
	case map[string]any:
		props := make([]*ast.ObjectProp, 0, len(t))
		for k, val := range t {
			props = append(props, &ast.ObjectProp{Key: k, Value: arrayASTFromJSON(val)})
		}
		// Stable key order for deterministic output.
		sortObjectProps(props)
		return &ast.ObjectExpr{Properties: props}
	case string:
		return &ast.Literal{Kind: ast.StringLit, Value: t}
	case bool:
		if t {
			return &ast.Literal{Kind: ast.BoolLit, Value: "true"}
		}
		return &ast.Literal{Kind: ast.BoolLit, Value: "false"}
	case float64:
		if t == float64(int64(t)) {
			return &ast.Literal{Kind: ast.NumberLit, Value: fmt.Sprintf("%d", int64(t))}
		}
		return &ast.Literal{Kind: ast.NumberLit, Value: fmt.Sprintf("%g", t)}
	case nil:
		return &ast.Literal{Kind: ast.NullLit, Value: "null"}
	default:
		return &ast.Literal{Kind: ast.StringLit, Value: fmt.Sprintf("%v", t)}
	}
}

func sortObjectProps(props []*ast.ObjectProp) {
	for i := 1; i < len(props); i++ {
		for j := i; j > 0 && props[j-1].Key > props[j].Key; j-- {
			props[j-1], props[j] = props[j], props[j-1]
		}
	}
}

// literalArrayExpr parses the collection's JS array literal (produced by
// content.CollectionJS) back into an AST ArrayExpr. Parsing the generated
// source reuses the real lexer/parser, so nested objects, arrays, and escaping
// are handled uniformly.
func literalArrayExpr(entries []content.Entry, use *content.FieldUse) ast.Expr {
	src := content.CollectionJSFields(entries, use)
	prog := parseInlineSource(src)
	if prog == nil {
		return &ast.ArrayExpr{}
	}
	for _, stmt := range prog.Body {
		if es, ok := stmt.(*ast.ExprStmt); ok {
			if arr, ok := es.Expression.(*ast.ArrayExpr); ok {
				return arr
			}
		}
	}
	return &ast.ArrayExpr{}
}

// parseInlineSource parses a snippet of JS source into a program.
func parseInlineSource(src string) *ast.Program {
	toks := lexer.New(src).Tokenize()
	return parser.New(toks).ParseProgram()
}

// warnShippedCollections reports a content collection whose entries ended up
// in the page's hydration script because a getCollection chain could not be
// folded at build time. A collection counts once at least minEntries of its
// entries are in the script: a generated page legitimately carries its own
// entry (handed to a client component), a lookup that failed carries them all.
func (b *Builder) warnShippedCollections(page string, prog *ast.Program, js string, minEntries int) {
	if len(b.contentCollections) == 0 || js == "" {
		return
	}
	// An inlined entry is an object literal starting `slug: ..., path: ...`;
	// a path that only appears on its own (an edit link) is not the collection.
	compact := stripSpace(js)
	use := collectFieldUse(prog)
	var shipped []string
	drafts, total, bytes := 0, 0, 0
	for name, entries := range b.contentCollections {
		var hits []content.Entry
		for _, e := range entries {
			if e.Path == "" {
				continue
			}
			for _, q := range []string{`"`, `'`, "`"} {
				if strings.Contains(compact, stripSpace("slug:"+q+e.Slug+q+",path:"+q+e.Path+q)) {
					hits = append(hits, e)
					break
				}
			}
		}
		if len(hits) == 0 || len(hits) < minEntries {
			continue
		}
		shipped = append(shipped, name)
		total += len(hits)
		bytes += len(content.CollectionJSFields(hits, use))
		for _, e := range hits {
			if d, _ := e.Data["draft"].(bool); d {
				drafts++
			}
		}
	}
	if len(shipped) == 0 {
		return
	}
	sort.Strings(shipped)
	pos := ast.Pos{Line: 1, Col: 1}
	if v, ok := b.collectionSites.Load(prog); ok {
		if sites := v.([]ast.Pos); len(sites) > 0 {
			pos = sites[0]
		}
	}
	msg := fmt.Sprintf("collection %s is evaluated in the browser: %d %s (about %s) copied into the page script",
		strings.Join(shipped, ", "), total, plural(total, "entry", "entries"), humanBytes(bytes))
	if drafts > 0 {
		msg += fmt.Sprintf(" (draft entries included: %d)", drafts)
	}
	hint := "fold the chain at build time (use literal or const values in filter/sort/find), or compute the list in generateStaticParams or a server-rendered page; only the fields this page mentions are shipped"
	if minEntries == 1 && isDynamicRoute(page, b.Cfg.PagesDir) {
		hint = "this is the fallback page of a dynamic route, built for params generateStaticParams did not list: add `export const dynamicParams = false` to close the route so it is not built, or " + hint
	}
	d := diag.New(b.relPath(page), pos.Line, pos.Col, msg, hint, "")
	d.Code = diag.CodeCollectionShipped
	d.Severity = diag.SeverityWarning
	if _, dup := b.shippedWarned.LoadOrStore(d.Error(), true); dup {
		return
	}
	fmt.Fprintf(os.Stderr, "  %sWarning:%s %s\n      hint: %s\n", cYellow, cReset, d.Error(), d.Hint)
}

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}

// humanBytes formats a size as B or KB.
func humanBytes(n int) string {
	if n < 1024 {
		return fmt.Sprintf("%d B", n)
	}
	return fmt.Sprintf("%.1f KB", float64(n)/1024)
}

// stripSpace removes ASCII whitespace.
func stripSpace(s string) string {
	return strings.Map(func(r rune) rune {
		switch r {
		case ' ', '\t', '\n', '\r':
			return -1
		}
		return r
	}, s)
}

// markTemplateCoverage records, for the fallback template of a dynamic route
// that has generateStaticParams, which param values already have a generated
// page. The template only answers the other URLs, so a lookup it must run in
// the browser (the route param is unknown at build time) needs only the
// collection entries without a page. Without this, every post body of a blog
// would ship in the [slug] template's script.
func (b *Builder) markTemplateCoverage(page string, mods []*bundler.Module) {
	if len(b.contentCollections) == 0 || !isDynamicRoute(page, b.Cfg.PagesDir) {
		return
	}
	if cachedInterceptForPage(page, b.Cfg.PagesDir) != nil {
		return
	}
	res := b.resolveStaticParamsPageOnce(page)
	if !res.note || len(res.errs) > 0 || len(res.expanded) == 0 {
		return
	}
	covered := map[string]bool{}
	for _, p := range res.expanded {
		for _, v := range p.Params {
			covered[v] = true
		}
	}
	for _, mod := range mods {
		if mod != nil && mod.Path == page && mod.Program != nil {
			b.templateCovered.Store(mod.Program, covered)
		}
	}
}
