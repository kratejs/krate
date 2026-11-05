package build

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/kratejs/krate/packages/compiler/ast"
	"github.com/kratejs/krate/packages/compiler/internal/astprint"
	"github.com/kratejs/krate/packages/compiler/internal/bundler"
	"github.com/kratejs/krate/packages/compiler/internal/diag"
	"github.com/kratejs/krate/packages/compiler/internal/irtree"
)

// A layout is compiled by Krate, but the islands of a page run in Preact, so a
// context provider a layout wraps around {children} cannot reach them. Krate
// lifts such wrappers out of the layout: the layout renders {children} alone,
// and the provider chain is generated as a module of its own that every island
// of the page renders inside. The chain's props are evaluated by that module,
// once per page, so all islands share one query client, store or theme object.

// layoutProviders is the provider chain of one layout.
type layoutProviders struct {
	Spec   string
	Module islandVirtualModule
	Layout string
	Tags   []string
	// Notes are warnings about wrappers that could not be lifted; they are
	// printed for pages that have islands.
	Notes []diag.Diagnostic
}

// islandProviders is the provider chain around the islands of one page,
// outermost first. A nil value is no chain.
type islandProviders struct {
	chain []*layoutProviders
	// declined are warnings about wrappers of the page's layouts that could
	// not be lifted.
	declined []diag.Diagnostic
}

func (p *islandProviders) empty() bool { return p == nil || len(p.chain) == 0 }

// printProviderNotes prints the warnings about layout providers that could not
// reach the islands of a page (once per build).
func printProviderNotes(p *islandProviders) {
	if p == nil || len(p.declined) == 0 {
		return
	}
	printWarningsOnce(os.Stderr, p.declined)
}

func (p *islandProviders) virtualModules() map[string]islandVirtualModule {
	if p.empty() {
		return nil
	}
	out := make(map[string]islandVirtualModule, len(p.chain))
	for _, c := range p.chain {
		out[c.Spec] = c.Module
	}
	return out
}

func providerIdent(i int) string { return "__KrateProviders" + strconv.Itoa(i) }

func (p *islandProviders) importLines() string {
	if p.empty() {
		return ""
	}
	var sb strings.Builder
	for i, c := range p.chain {
		fmt.Fprintf(&sb, "import %s from %q;\n", providerIdent(i), c.Spec)
	}
	return sb.String()
}

// wrapVNodeJS rewrites the statement that holds an island's vnode so the
// vnode renders inside the provider chain.
func (p *islandProviders) wrapVNodeJS(v string) string {
	if p.empty() {
		return ""
	}
	return "      " + v + "=" + p.wrapExprJS(v) + ";\n"
}

// wrapExprJS wraps a JS expression that evaluates to a vnode.
func (p *islandProviders) wrapExprJS(expr string) string {
	if p.empty() {
		return expr
	}
	out := expr
	for i := len(p.chain) - 1; i >= 0; i-- {
		out = "h(" + providerIdent(i) + ",null," + out + ")"
	}
	return out
}

// layoutProviderCache holds the provider chain of each layout built so far
// (path -> *layoutProviders, nil when the layout has none).
var layoutProviderCache sync.Map

// pageProviders returns the provider chain of the layouts around a page.
func (b *Builder) pageProviders(page string) *islandProviders {
	stack := b.routeStack(page)
	var out islandProviders
	// routeStack lists the innermost boundary first.
	for i := len(stack) - 1; i >= 0; i-- {
		if v, ok := layoutProviderCache.Load(stack[i].Path); ok {
			if lp, _ := v.(*layoutProviders); lp != nil {
				if lp.Spec != "" {
					out.chain = append(out.chain, lp)
				}
				out.declined = append(out.declined, lp.Notes...)
			}
		}
	}
	if len(out.chain) == 0 && len(out.declined) == 0 {
		return nil
	}
	return &out
}

// verifyProviderChain checks that a lifted provider renders nothing of its
// own: Krate renders the page content, so a wrapper that adds markup around
// its children cannot move into the islands. It returns a warning when the
// chain must stay in the layout.
func (b *Builder) verifyProviderChain(lp *layoutProviders) *diag.Diagnostic {
	if _, err := b.ensurePreactShared(); err != nil {
		// Without Preact there are no islands to give a provider to.
		d := diag.Diagnostic{File: lp.Layout, Message: "providers in this layout were left in place: " + err.Error(), Severity: diag.SeverityWarning}.WithCode(diag.CodeLayoutProvider)
		return &d
	}
	prov := &islandProviders{chain: []*layoutProviders{lp}}
	src := "import { h } from \"preact\";\nimport renderToString from \"preact-render-to-string\";\n" + prov.importLines() +
		"globalThis.__krate_island_html = \"\";\nglobalThis.__krate_island_ssr_error = \"\";\n" +
		"try { globalThis.__krate_island_html = renderToString(" + prov.wrapExprJS(`h("krate-slot", null)`) + "); } catch (e) { globalThis.__krate_island_ssr_error = String((e && e.message) || e); }\n"
	html, err := b.buildIslandSSR(src, lp.Module.ResolveDir, &islandCSSSink{root: b.Root}, prov)
	var be *bundleError
	if errors.As(err, &be) {
		d := diag.Diagnostic{
			File: lp.Layout, Message: "the providers of this layout (" + strings.Join(lp.Tags, ", ") + ") could not be bundled for the islands: " + err.Error(),
			Hint: "check the provider's imports", Severity: diag.SeverityWarning,
		}.WithCode(diag.CodeLayoutProvider)
		return &d
	}
	if err != nil {
		// The provider needs the browser to render; islands inside it are
		// client-only, which the island build reports.
		return nil
	}
	if html == "<krate-slot></krate-slot>" {
		return nil
	}
	d := diag.Diagnostic{
		File: lp.Layout, Message: "the provider " + strings.Join(lp.Tags, " > ") + " in this layout renders markup around its children, so it stays in the layout and the islands of its pages do not see its context",
		Hint: "use a provider that renders only its children, or wrap the island's own module in it", Severity: diag.SeverityWarning,
	}.WithCode(diag.CodeProviderDOM)
	return &d
}

// providerPlan is the result of lifting the providers out of a layout.
type providerPlan struct {
	providers *layoutProviders
	// module is the layout without the lifted wrappers, for the Krate side.
	module *bundler.Module
}

// layoutShape is what the planner finds out about a layout component.
type layoutShape struct {
	fn         *ast.FnDecl
	arrow      *ast.ArrowFn
	params     []*ast.Param
	body       []ast.Stmt
	ret        *ast.ReturnStmt
	retIndex   int
	propsName  string          // `props` when the component takes one object
	childNames map[string]bool // local names bound to the children prop
	paramNames map[string]bool // every name the parameters bind
}

var bindingIdentRe = regexp.MustCompile(`[A-Za-z_$][A-Za-z0-9_$]*`)

func paramBindingNames(params []*ast.Param) (names map[string]bool, props string) {
	names = map[string]bool{}
	for _, p := range params {
		if p == nil {
			continue
		}
		switch {
		case p.Target != nil:
			irtree.WalkAST(p.Target, func(n any) bool {
				if id, ok := n.(*ast.Identifier); ok {
					names[id.Name] = true
				}
				return true
			})
		case p.Pattern != "":
			for _, id := range bindingIdentRe.FindAllString(p.Pattern, -1) {
				names[id] = true
			}
		case p.Name != "":
			names[p.Name] = true
			if props == "" {
				props = p.Name
			}
		}
	}
	return names, props
}

func findLayoutShape(prog *ast.Program) *layoutShape {
	name := ""
	var fn *ast.FnDecl
	var arrow *ast.ArrowFn
	for _, st := range prog.Body {
		if exp, ok := st.(*ast.ExportStmt); ok && exp.Default {
			if f, ok := exp.Declaration.(*ast.FnDecl); ok {
				fn = f
			} else if exp.Local != "" {
				name = exp.Local
			}
		}
	}
	if fn == nil && name != "" {
		for _, st := range prog.Body {
			switch s := st.(type) {
			case *ast.FnDecl:
				if s.Name == name {
					fn = s
				}
			case *ast.ExportStmt:
				if f, ok := s.Declaration.(*ast.FnDecl); ok && f.Name == name {
					fn = f
				}
			case *ast.VarStmt:
				for _, d := range s.Decls {
					if a, ok := d.Init.(*ast.ArrowFn); ok && d.Name == name {
						arrow = a
					}
				}
			}
		}
	}
	sh := &layoutShape{fn: fn, arrow: arrow}
	switch {
	case fn != nil:
		sh.params, sh.body = fn.Params, fn.Body
	case arrow != nil:
		sh.params, sh.body = arrow.Params, arrow.Body
	default:
		return nil
	}
	for i, st := range sh.body {
		if r, ok := st.(*ast.ReturnStmt); ok {
			sh.ret, sh.retIndex = r, i
		}
	}
	if sh.ret == nil {
		return nil
	}
	sh.paramNames, sh.propsName = paramBindingNames(sh.params)
	sh.childNames = map[string]bool{}
	if sh.paramNames["children"] && sh.propsName == "" {
		sh.childNames["children"] = true
	}
	return sh
}

func (sh *layoutShape) isChildren(e ast.Expr) bool {
	switch x := e.(type) {
	case *ast.Identifier:
		return sh.childNames[x.Name]
	case *ast.MemberExpr:
		obj, ok1 := x.Object.(*ast.Identifier)
		prop, ok2 := x.Property.(*ast.Identifier)
		return ok1 && ok2 && !x.Computed && sh.propsName != "" && obj.Name == sh.propsName && prop.Name == "children"
	}
	return false
}

// childrenPath finds the elements that enclose the one {children} of the
// layout, outermost first. ok is false unless there is exactly one.
func (sh *layoutShape) childrenPath() ([]*ast.JSXElement, bool) {
	var found [][]*ast.JSXElement
	var walk func(cs []ast.JSXChild, path []*ast.JSXElement)
	walk = func(cs []ast.JSXChild, path []*ast.JSXElement) {
		for _, c := range cs {
			switch v := c.(type) {
			case *ast.JSXExprContainer:
				if sh.isChildren(v.Expression) {
					found = append(found, append([]*ast.JSXElement(nil), path...))
				}
			case *ast.JSXElementChild:
				walk(v.Element.Children, append(path, v.Element))
			case *ast.JSXFragmentChild:
				walk(v.Fragment.Children, path)
			}
		}
	}
	switch v := sh.ret.Value.(type) {
	case *ast.JSXElement:
		walk([]ast.JSXChild{&ast.JSXElementChild{Element: v}}, nil)
	case *ast.JSXFragment:
		walk(v.Children, nil)
	}
	if len(found) != 1 {
		return nil, false
	}
	return found[0], true
}

// tagRoot is the binding a JSX tag name refers to: `Foo` for `Foo.Provider`.
func tagRoot(name string) string {
	if i := strings.IndexByte(name, '.'); i >= 0 {
		return name[:i]
	}
	return name
}

func isComponentTag(name string) bool {
	r := tagRoot(name)
	return r != "" && r[0] >= 'A' && r[0] <= 'Z'
}

// importBinding finds the import statement that binds local.
func importBinding(prog *ast.Program, local string) *ast.ImportStmt {
	for _, st := range prog.Body {
		imp, ok := st.(*ast.ImportStmt)
		if !ok {
			continue
		}
		if imp.Default == local || imp.Namespace == local {
			return imp
		}
		for _, n := range imp.Named {
			l := n.Local
			if l == "" {
				l = n.Remote
			}
			if l == local {
				return imp
			}
		}
	}
	return nil
}

// forwardsChildrenToLibrary reports whether a user module renders a component
// of a React-bound package around its children: a provider component of the
// project (`Providers`) that the layout wraps {children} in.
func forwardsChildrenToLibrary(m *bundler.Module, resolve func(importer, spec string) string) bool {
	if m == nil || m.Program == nil || m.SourceCode == "" {
		return false
	}
	libs, ns := reactLibBindings(m, resolve)
	if len(libs) == 0 && len(ns) == 0 {
		return false
	}
	prog, errs, _ := astprint.Parse(m.SourceCode)
	if prog == nil || len(errs) > 0 {
		return false
	}
	hit := false
	irtree.WalkAST(prog.Body, func(n any) bool {
		el, ok := n.(*ast.JSXElement)
		if !ok || hit || el.Opening == nil {
			return !hit
		}
		root := tagRoot(el.Opening.Name)
		_, inLibs := libs[root]
		_, inNS := ns[root]
		if !inLibs && !inNS {
			return true
		}
		for _, c := range el.Children {
			if ec, ok := c.(*ast.JSXExprContainer); ok {
				switch x := ec.Expression.(type) {
				case *ast.Identifier:
					hit = hit || x.Name == "children"
				case *ast.MemberExpr:
					if p, ok := x.Property.(*ast.Identifier); ok && p.Name == "children" {
						hit = true
					}
				}
			}
		}
		return !hit
	})
	return hit
}

// providerWrapperKind says what a layout wrapper tag renders: "" for a
// component Krate renders itself.
func providerWrapperKind(layout *bundler.Module, tag string, libs, nsLibs map[string]string, mods map[string]*bundler.Module, islands map[string]*reactIslandModule, resolve func(importer, spec string) string) string {
	if !isComponentTag(tag) {
		return ""
	}
	root := tagRoot(tag)
	if pkg, ok := libs[root]; ok {
		return pkg
	}
	if pkg, ok := nsLibs[root]; ok {
		return pkg
	}
	imp := importBinding(layout.Program, root)
	if imp == nil {
		return ""
	}
	src := strings.Trim(imp.Source, "\"'")
	if isBarePackageImport(src) && !strings.HasPrefix(src, "@/") && !strings.HasPrefix(src, "~/") {
		return ""
	}
	target := resolve(layout.Path, src)
	if target == "" {
		return ""
	}
	if im, ok := islands[target]; ok {
		return im.Pkg
	}
	if forwardsChildrenToLibrary(mods[target], resolve) {
		return filepath.Base(target)
	}
	return ""
}

// isKrateStateSource reports whether an import comes from Krate's own runtime,
// whose state the islands cannot read.
func isKrateStateSource(src string) bool {
	return src == "@krate/runtime" || strings.HasPrefix(src, "@krate/") || strings.HasPrefix(src, "krate/")
}

// planLayoutProviders lifts the provider wrappers out of a layout. It returns
// nil when the layout has none or they cannot be lifted (notes then say why).
func (b *Builder) planLayoutProviders(layoutPath string, layout *bundler.Module, mods []*bundler.Module, resolve func(importer, spec string) string) (*providerPlan, []diag.Diagnostic) {
	if layout == nil || layout.Program == nil || layout.SourceCode == "" {
		return nil, nil
	}
	// A layout that wraps nothing in a component cannot have a provider.
	if !strings.Contains(layout.SourceCode, "children") {
		return nil, nil
	}
	libs, nsLibs := reactLibBindings(layout, resolve)
	byPath := make(map[string]*bundler.Module, len(mods))
	for _, m := range mods {
		if m != nil {
			byPath[m.Path] = m
		}
	}
	islands := detectReactIslandModules(mods, resolve)
	if len(libs) == 0 && len(nsLibs) == 0 {
		hasUser := false
		for _, st := range layout.Program.Body {
			if imp, ok := st.(*ast.ImportStmt); ok {
				if t := resolve(layout.Path, strings.Trim(imp.Source, "\"'")); t != "" && (islands[t] != nil || forwardsChildrenToLibrary(byPath[t], resolve)) {
					hasUser = true
				}
			}
		}
		if !hasUser {
			return nil, nil
		}
	}

	// Analysis runs on a fresh parse: the module's own program may already
	// have been rewritten by the React transforms of an earlier build.
	prog, perrs, _ := astprint.Parse(layout.SourceCode)
	if prog == nil || len(perrs) > 0 {
		return nil, nil
	}
	shape := findLayoutShape(prog)
	if shape == nil {
		return nil, nil
	}
	path, ok := shape.childrenPath()
	if !ok {
		return nil, nil
	}
	var wrappers []*ast.JSXElement
	for _, el := range path {
		if el.Opening == nil {
			continue
		}
		if providerWrapperKind(layout, el.Opening.Name, libs, nsLibs, byPath, islands, resolve) != "" {
			wrappers = append(wrappers, el)
		}
	}
	if len(wrappers) == 0 {
		return nil, nil
	}

	note := func(el *ast.JSXElement, msg, hint string) diag.Diagnostic {
		return diag.Diagnostic{
			File: layoutPath, Line: el.Position.Line, Col: el.Position.Col,
			Message: msg, Hint: hint, Severity: diag.SeverityWarning,
		}.WithCode(diag.CodeLayoutProvider)
	}
	first := wrappers[0]
	tags := make([]string, len(wrappers))
	for i, el := range wrappers {
		tags[i] = el.Opening.Name
	}
	decline := func(msg, hint string) (*providerPlan, []diag.Diagnostic) {
		return nil, []diag.Diagnostic{note(first, msg, hint)}
	}

	// Gather what the wrappers' props and tags refer to.
	needed := map[string]bool{}
	var attrsJS []string
	for _, el := range wrappers {
		needed[tagRoot(el.Opening.Name)] = true
		var sb strings.Builder
		for _, a := range el.Opening.Attributes {
			if a == nil {
				continue
			}
			switch {
			case a.Spread:
				sb.WriteString(" {..." + astprint.PrintExpr(a.Value) + "}")
			case a.Value == nil:
				sb.WriteString(" " + a.Name)
			default:
				sb.WriteString(" " + a.Name + "={" + astprint.PrintExpr(a.Value) + "}")
			}
			if a.Value != nil {
				irtree.WalkAST(a.Value, func(n any) bool {
					if id, ok := n.(*ast.Identifier); ok {
						needed[id.Name] = true
					}
					return true
				})
			}
		}
		attrsJS = append(attrsJS, sb.String())
	}

	// Resolve every name against the layout's imports and declarations.
	type localDecl struct {
		stmt ast.Stmt
		text string // replacement text for hoisted hook calls
	}
	moduleDecls := map[string]ast.Stmt{}
	moduleOrder := map[ast.Stmt]int{}
	for i, st := range prog.Body {
		moduleOrder[st] = i
		switch s := st.(type) {
		case *ast.VarStmt:
			for _, d := range s.Decls {
				for _, n := range declNames(d) {
					moduleDecls[n] = s
				}
			}
		case *ast.FnDecl:
			moduleDecls[s.Name] = s
		case *ast.ClassDecl:
			moduleDecls[s.Name] = s
		}
	}
	bodyDecls := map[string]*ast.VarDecl{}
	bodyStmt := map[string]*ast.VarStmt{}
	bodyFns := map[string]*ast.FnDecl{}
	for _, st := range shape.body[:shape.retIndex] {
		switch s := st.(type) {
		case *ast.VarStmt:
			for _, d := range s.Decls {
				for _, n := range declNames(d) {
					bodyDecls[n] = d
					bodyStmt[n] = s
				}
			}
		case *ast.FnDecl:
			bodyFns[s.Name] = s
		}
	}

	usedImports := map[*ast.ImportStmt]map[string]bool{}
	var keepModule []ast.Stmt
	keptModule := map[ast.Stmt]bool{}
	var hoisted []string
	hoistedSeen := map[*ast.VarDecl]bool{}
	var failure string
	done := map[string]bool{}
	var resolveName func(name string)
	collect := func(node any) {
		irtree.WalkAST(node, func(n any) bool {
			switch x := n.(type) {
			case *ast.Identifier:
				resolveName(x.Name)
			case *ast.JSXElement:
				if x.Opening != nil && isComponentTag(x.Opening.Name) {
					resolveName(tagRoot(x.Opening.Name))
				}
			}
			return failure == ""
		})
	}
	resolveName = func(name string) {
		if failure != "" || done[name] || name == "" {
			return
		}
		done[name] = true
		if shape.childNames[name] || name == shape.propsName {
			// `children` itself never appears in a lifted prop.
			failure = "a prop of `" + first.Opening.Name + "` reads the layout's own props (`" + name + "`)"
			return
		}
		if shape.paramNames[name] {
			failure = "a prop of `" + first.Opening.Name + "` depends on the layout's `" + name + "` parameter, which is only known per request"
			return
		}
		if d, ok := bodyDecls[name]; ok {
			if fn := bodyFns[name]; fn != nil {
				return
			}
			if hoistedSeen[d] {
				return
			}
			hoistedSeen[d] = true
			text, why := hoistBodyDecl(d, bodyStmt[name].Kind, prog, shape, needed)
			if why != "" {
				failure = why
				return
			}
			hoisted = append(hoisted, text)
			if d.Init != nil {
				collect(d.Init)
			}
			return
		}
		if fn, ok := bodyFns[name]; ok {
			collect(fn.Body)
			failure = "a prop of `" + first.Opening.Name + "` calls the function `" + name + "`, declared inside the layout component"
			return
		}
		if st, ok := moduleDecls[name]; ok {
			if !keptModule[st] {
				keptModule[st] = true
				keepModule = append(keepModule, st)
				collect(st)
			}
			return
		}
		if imp := importBinding(prog, name); imp != nil {
			src := strings.Trim(imp.Source, "\"'")
			if isKrateStateSource(src) {
				failure = "a prop of `" + first.Opening.Name + "` uses `" + name + "` from \"" + src + "\", which is Krate state the islands cannot read"
				return
			}
			if usedImports[imp] == nil {
				usedImports[imp] = map[string]bool{}
			}
			usedImports[imp][name] = true
		}
	}
	for _, name := range sortedKeys(needed) {
		resolveName(name)
	}
	if failure != "" {
		return decline("the provider `"+first.Opening.Name+"` in this layout cannot be made available to islands: "+failure,
			"build the provider's props from constants and imports (a module-level value, or `useState(() => new Client())` with no setter), or wrap the island's own module in the provider")
	}

	// Generate the module that holds the chain.
	var src strings.Builder
	src.WriteString("// Generated by Krate: the providers " + layoutBase(layoutPath) + " wraps around its children.\n")
	imps := make([]*ast.ImportStmt, 0, len(usedImports))
	for imp := range usedImports {
		imps = append(imps, imp)
	}
	sort.Slice(imps, func(i, j int) bool { return moduleOrder[imps[i]] < moduleOrder[imps[j]] })
	for _, imp := range imps {
		src.WriteString(astprint.Print(&ast.Program{Body: []ast.Stmt{filterImport(imp, usedImports[imp])}}))
	}
	sort.Slice(keepModule, func(i, j int) bool { return moduleOrder[keepModule[i]] < moduleOrder[keepModule[j]] })
	src.WriteString(astprint.Print(&ast.Program{Body: keepModule}))
	for _, h := range hoisted {
		src.WriteString(h + "\n")
	}
	src.WriteString("export default function KrateLayoutProviders(props) {\n  return (\n")
	for i, el := range wrappers {
		src.WriteString(strings.Repeat("  ", i+2) + "<" + el.Opening.Name + attrsJS[i] + ">\n")
	}
	src.WriteString(strings.Repeat("  ", len(wrappers)+2) + "{props.children}\n")
	for i := len(wrappers) - 1; i >= 0; i-- {
		src.WriteString(strings.Repeat("  ", i+2) + "</" + wrappers[i].Opening.Name + ">\n")
	}
	src.WriteString("  );\n}\n")

	body := src.String()
	sum := sha256.Sum256([]byte(layoutPath + "\x00" + body))
	lp := &layoutProviders{
		Spec:   islandVirtualPrefix + "providers-" + hex.EncodeToString(sum[:])[:12],
		Module: islandVirtualModule{Source: body, ResolveDir: filepath.Dir(layoutPath)},
		Layout: layoutPath,
		Tags:   tags,
	}

	// The wrappers are in the lifted module now; the layout keeps their
	// children.
	rewritten, why := liftWrappers(layout, shape, wrappers, usedImports, keptModule, hoistedSeen, bodyDecls, bodyStmt)
	if why != "" {
		return decline("the provider `"+first.Opening.Name+"` in this layout cannot be made available to islands: "+why, "keep the provider in a module the islands import")
	}
	return &providerPlan{providers: lp, module: rewritten}, nil
}

func layoutBase(p string) string { return filepath.Base(p) }

func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// declNames lists the names a variable declaration binds.
func declNames(d *ast.VarDecl) []string {
	if d.IsDestructuring {
		return append([]string(nil), d.Names...)
	}
	if d.Name == "" {
		return nil
	}
	return []string{d.Name}
}

// filterImport keeps the bindings of an import that the lifted module uses.
func filterImport(imp *ast.ImportStmt, used map[string]bool) *ast.ImportStmt {
	out := *imp
	if out.Default != "" && !used[out.Default] {
		out.Default = ""
	}
	if out.Namespace != "" && !used[out.Namespace] {
		out.Namespace = ""
	}
	out.Named = nil
	for _, n := range imp.Named {
		l := n.Local
		if l == "" {
			l = n.Remote
		}
		if used[l] {
			out.Named = append(out.Named, n)
		}
	}
	return &out
}

// hoistBodyDecl turns a declaration inside the layout component into a
// module-level one. Only values that do not change per render can move: the
// `useState(() => new Client())` idiom with no setter in use, `useMemo` with
// no dependencies, and plain expressions.
func hoistBodyDecl(d *ast.VarDecl, kind ast.VarKind, prog *ast.Program, shape *layoutShape, used map[string]bool) (string, string) {
	names := declNames(d)
	if d.Init == nil {
		return "", "`" + strings.Join(names, ", ") + "` is declared without a value"
	}
	if call, ok := d.Init.(*ast.CallExpr); ok {
		if id, ok := call.Callee.(*ast.Identifier); ok && hookNameRe.MatchString(id.Name) {
			imp := importBinding(prog, id.Name)
			from := ""
			if imp != nil {
				from = strings.Trim(imp.Source, "\"'")
			}
			switch {
			case id.Name == "useState" && (from == "react" || from == "preact/hooks" || from == "preact/compat"):
				if !d.IsDestructuring || len(d.Names) == 0 {
					return "", "`useState` in the layout is not destructured"
				}
				if len(d.Names) > 1 && used[d.Names[1]] {
					return "", "the layout changes `" + d.Names[0] + "` with `" + d.Names[1] + "`, so it is not a constant"
				}
				if len(call.Args) == 0 {
					return "const " + d.Names[0] + " = undefined;", ""
				}
				arg := astprint.PrintExpr(call.Args[0])
				if fn, ok := call.Args[0].(*ast.ArrowFn); ok && len(fn.Params) == 0 {
					return "const " + d.Names[0] + " = (" + arg + ")();", ""
				}
				return "const " + d.Names[0] + " = " + arg + ";", ""
			case id.Name == "useMemo" && (from == "react" || from == "preact/hooks" || from == "preact/compat") && !d.IsDestructuring:
				if len(call.Args) == 2 {
					if arr, ok := call.Args[1].(*ast.ArrayExpr); ok && len(arr.Elements) == 0 {
						return "const " + d.Name + " = (" + astprint.PrintExpr(call.Args[0]) + ")();", ""
					}
				}
				return "", "`useMemo` in the layout depends on values that can change"
			default:
				return "", "the layout calls the hook `" + id.Name + "`, whose state the islands cannot share"
			}
		}
	}
	if d.IsDestructuring {
		return "", "`" + strings.Join(names, ", ") + "` is destructured in the layout"
	}
	kw := "const"
	if kind == ast.VarLet {
		kw = "let"
	}
	return kw + " " + d.Name + " = " + astprint.PrintExpr(d.Init) + ";", ""
}

// liftWrappers returns the layout module with the wrapper elements replaced by
// their children, and without the declarations only they used.
func liftWrappers(layout *bundler.Module, shape *layoutShape, wrappers []*ast.JSXElement, usedImports map[*ast.ImportStmt]map[string]bool, keptModule map[ast.Stmt]bool, hoisted map[*ast.VarDecl]bool, bodyDecls map[string]*ast.VarDecl, bodyStmt map[string]*ast.VarStmt) (*bundler.Module, string) {
	// The same elements in the module's own (possibly transformed) program are
	// found by position.
	targets := map[int]string{}
	for _, el := range wrappers {
		targets[el.Position.Offset] = el.Opening.Name
	}
	cur := findLayoutShape(layout.Program)
	if cur == nil {
		return nil, "the layout component has an unexpected shape"
	}
	replaced := 0
	var rewriteChildren func(cs []ast.JSXChild) []ast.JSXChild
	var rewriteElement func(el *ast.JSXElement) ast.JSXChild
	rewriteElement = func(el *ast.JSXElement) ast.JSXChild {
		if name, ok := targets[el.Position.Offset]; ok && el.Opening != nil && el.Opening.Name == name {
			replaced++
			kids := rewriteChildren(el.Children)
			return &ast.JSXFragmentChild{Fragment: &ast.JSXFragment{Position: el.Position, Children: kids}}
		}
		c := *el
		c.Children = rewriteChildren(el.Children)
		return &ast.JSXElementChild{Element: &c}
	}
	rewriteChildren = func(cs []ast.JSXChild) []ast.JSXChild {
		out := make([]ast.JSXChild, len(cs))
		for i, c := range cs {
			switch v := c.(type) {
			case *ast.JSXElementChild:
				out[i] = rewriteElement(v.Element)
			case *ast.JSXFragmentChild:
				f := *v.Fragment
				f.Children = rewriteChildren(v.Fragment.Children)
				out[i] = &ast.JSXFragmentChild{Fragment: &f}
			default:
				out[i] = c
			}
		}
		return out
	}
	var newRet ast.Expr
	switch v := cur.ret.Value.(type) {
	case *ast.JSXElement:
		switch r := rewriteElement(v).(type) {
		case *ast.JSXElementChild:
			newRet = r.Element
		case *ast.JSXFragmentChild:
			newRet = r.Fragment
		}
	case *ast.JSXFragment:
		f := *v
		f.Children = rewriteChildren(v.Children)
		newRet = &f
	default:
		return nil, "the layout does not return JSX"
	}
	if replaced != len(wrappers) {
		return nil, "the layout changed while it was being analyzed"
	}

	// Rebuild the component with the new return and without the hoisted
	// declarations nothing else reads.
	bodyKeep := make([]ast.Stmt, 0, len(cur.body))
	for i, st := range cur.body {
		if i == cur.retIndex {
			bodyKeep = append(bodyKeep, &ast.ReturnStmt{Position: cur.ret.Position, Value: newRet})
			continue
		}
		bodyKeep = append(bodyKeep, st)
	}
	var lifted []string
	for _, d := range bodyDecls {
		if hoisted[d] {
			lifted = append(lifted, declNames(d)...)
		}
	}
	// Names only the lifted code read: drop their declarations when nothing
	// that stays refers to them.
	stay := func(body []ast.Stmt) map[string]bool {
		refs := map[string]bool{}
		note := func(node any) {
			irtree.WalkAST(node, func(n any) bool {
				switch x := n.(type) {
				case *ast.Identifier:
					refs[x.Name] = true
				case *ast.JSXElement:
					if x.Opening != nil {
						refs[tagRoot(x.Opening.Name)] = true
					}
				}
				return true
			})
		}
		for _, st := range body {
			note(st)
		}
		return refs
	}
	liftedSet := map[string]bool{}
	for _, n := range lifted {
		liftedSet[n] = true
	}
	for changed := true; changed; {
		changed = false
		refs := stay(bodyKeep)
		next := bodyKeep[:0:0]
		for _, st := range bodyKeep {
			if vs, ok := st.(*ast.VarStmt); ok && allLifted(vs, liftedSet, refs, hoisted) {
				changed = true
				continue
			}
			next = append(next, st)
		}
		bodyKeep = next
	}

	np := *layout.Program
	np.Body = append([]ast.Stmt(nil), layout.Program.Body...)
	for i, st := range np.Body {
		switch s := st.(type) {
		case *ast.FnDecl:
			if cur.fn == s {
				c := *s
				c.Body = bodyKeep
				np.Body[i] = &c
			}
		case *ast.ExportStmt:
			if f, ok := s.Declaration.(*ast.FnDecl); ok && cur.fn == f {
				ec := *s
				fc := *f
				fc.Body = bodyKeep
				ec.Declaration = &fc
				np.Body[i] = &ec
			}
		case *ast.VarStmt:
			decls := make([]*ast.VarDecl, len(s.Decls))
			copy(decls, s.Decls)
			for j, d := range decls {
				if a, ok := d.Init.(*ast.ArrowFn); ok && cur.arrow == a {
					ac := *a
					ac.Body = bodyKeep
					dc := *d
					dc.Init = &ac
					decls[j] = &dc
				}
			}
			vc := *s
			vc.Decls = decls
			np.Body[i] = &vc
		}
	}
	np.Body = pruneLiftedModuleLevel(np.Body, usedImports, keptModule)

	nm := *layout
	nm.Program = &np
	nm.Imports = nil
	for _, st := range np.Body {
		switch v := st.(type) {
		case *ast.ImportStmt:
			nm.Imports = append(nm.Imports, strings.Trim(v.Source, "\"'"))
		case *ast.ExportStmt:
			if v.ReexportSource != "" {
				nm.Imports = append(nm.Imports, strings.Trim(v.ReexportSource, "\"'"))
			}
		}
	}
	return &nm, ""
}

// allLifted reports whether every name a layout-body declaration binds was
// lifted and nothing that stays reads it.
func allLifted(vs *ast.VarStmt, lifted, refs map[string]bool, hoisted map[*ast.VarDecl]bool) bool {
	if len(vs.Decls) == 0 {
		return false
	}
	for _, d := range vs.Decls {
		if !hoisted[d] {
			return false
		}
		for _, n := range declNames(d) {
			if !lifted[n] || refs[n] {
				return false
			}
		}
	}
	return true
}

// pruneLiftedModuleLevel drops the module-level declarations and import
// bindings that only the lifted provider code used.
func pruneLiftedModuleLevel(body []ast.Stmt, usedImports map[*ast.ImportStmt]map[string]bool, kept map[ast.Stmt]bool) []ast.Stmt {
	candidates := map[ast.Stmt]bool{}
	for st := range kept {
		candidates[st] = true
	}
	importBindings := map[string]*ast.ImportStmt{}
	for imp, names := range usedImports {
		for n := range names {
			importBindings[n] = imp
		}
	}
	removed := map[ast.Stmt]bool{}
	droppedBindings := map[string]bool{}
	for changed := true; changed; {
		changed = false
		refs := map[string]bool{}
		for _, st := range body {
			if removed[st] {
				continue
			}
			if _, ok := st.(*ast.ImportStmt); ok {
				continue
			}
			irtree.WalkAST(st, func(n any) bool {
				switch x := n.(type) {
				case *ast.Identifier:
					refs[x.Name] = true
				case *ast.JSXElement:
					if x.Opening != nil {
						refs[tagRoot(x.Opening.Name)] = true
					}
				}
				return true
			})
		}
		for _, st := range body {
			if !candidates[st] || removed[st] {
				continue
			}
			names := stmtDeclNames(st)
			if len(names) == 0 {
				continue
			}
			live := false
			for _, n := range names {
				if refs[n] {
					live = true
				}
			}
			if !live {
				removed[st] = true
				changed = true
			}
		}
		for n := range importBindings {
			if !refs[n] && !droppedBindings[n] {
				droppedBindings[n] = true
				changed = true
			}
		}
	}
	out := make([]ast.Stmt, 0, len(body))
	for _, st := range body {
		if removed[st] {
			continue
		}
		if imp, ok := st.(*ast.ImportStmt); ok {
			if kept := withoutBindings(imp, droppedBindings); kept != nil {
				out = append(out, kept)
			}
			continue
		}
		out = append(out, st)
	}
	return out
}

func stmtDeclNames(st ast.Stmt) []string {
	switch s := st.(type) {
	case *ast.VarStmt:
		var out []string
		for _, d := range s.Decls {
			out = append(out, declNames(d)...)
		}
		return out
	case *ast.FnDecl:
		return []string{s.Name}
	case *ast.ClassDecl:
		return []string{s.Name}
	}
	return nil
}

// withoutBindings drops the named bindings of an import; the statement stays
// when it had none to begin with (a side-effect import).
func withoutBindings(imp *ast.ImportStmt, drop map[string]bool) *ast.ImportStmt {
	if imp.Default == "" && imp.Namespace == "" && len(imp.Named) == 0 {
		return imp
	}
	out := *imp
	if drop[out.Default] {
		out.Default = ""
	}
	if drop[out.Namespace] {
		out.Namespace = ""
	}
	out.Named = nil
	for _, n := range imp.Named {
		l := n.Local
		if l == "" {
			l = n.Remote
		}
		if !drop[l] {
			out.Named = append(out.Named, n)
		}
	}
	if out.Default == "" && out.Namespace == "" && len(out.Named) == 0 {
		// Nothing left: the layout no longer imports the package.
		return nil
	}
	return &out
}
