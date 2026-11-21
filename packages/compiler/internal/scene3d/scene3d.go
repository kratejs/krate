// Package scene3d compiles the JSX scene of an @krate/3d <Canvas> into direct
// Filament engine calls at build time.
//
// Hook point: the bundler, right after a TSX module is parsed and before its
// imports are collected (bundler.resolveTSXModule). That is the one place that
// sees a whole module AST with its import bindings and module-level constants
// intact, before the IR builder lowers JSX. The pass replaces
// `<Canvas ...>children</Canvas>` in place with
// `<K3dCanvas setup={(canvas) => { ...engine calls... }} />`, a plain
// component from @krate/3d/runtime/compiled, so the rest of the pipeline (IR,
// SSR, hydration) sees ordinary JSX with one function prop and never needs to
// know about scenes. It also adds the import of that component, which the
// bundler then resolves like any other.
//
// A scene the pass cannot analyse is left untouched: the @renderer Canvas
// component and its runtime reconciler handle it, and a KR8701 warning names
// the element and the reason. Reactivity is decided conservatively: a prop
// that does not fold to a constant is reactive and gets an effect, which is
// correct for signals and merely redundant for other values.
package scene3d

import (
	"fmt"
	"reflect"
	"strings"

	"github.com/kratejs/krate/packages/compiler/ast"
	"github.com/kratejs/krate/packages/compiler/internal/diag"
)

// Names the generated code uses. They are prefixed so they cannot collide
// with user bindings.
const (
	k3dName    = "__k3d"
	sceneName  = "__scene"
	canvasName = "__canvas"
	compName   = "K3dCanvas"

	// CompiledModule exports __k3d; CanvasModule exports the CompiledCanvas
	// component the Canvas element is replaced with.
	CompiledModule = "@krate/3d/runtime/compiled"
	CanvasModule   = "@krate/3d/runtime/compiled-canvas"
)

// Options configures Rewrite.
type Options struct {
	// File is the path used in diagnostics and Source the module text, for
	// the source line shown under a diagnostic.
	File   string
	Source string
	// Shader compiles a generated Filament material source (.mat text) and
	// returns the site URL of the compiled .filamat. A nil Shader makes a
	// <shaderMaterial> a build error.
	Shader func(mat []byte) (url string, err error)
	// Assets registers the engine files the page loads at run time (the
	// filament package's filament.js and filament.wasm, and the packaged
	// uber-materials) as site assets and returns their URLs. An error is
	// reported as the KR8702 warning and the scene is emitted without URLs.
	Assets func() (Assets, error)
}

// Assets are the site URLs of the engine files a scene loads.
type Assets struct {
	// FilamentURL is the URL of filament.js; filament.wasm sits next to it.
	FilamentURL string
	// MaterialBase is the directory URL of the packaged .filamat files.
	MaterialBase string
}

// Mentions reports whether source text can contain an @krate/3d scene, so
// callers can skip the pass for every other module.
func Mentions(src string) bool { return strings.Contains(src, "@krate/3d") }

// Rewrite compiles every <Canvas> imported from @krate/3d in prog. It reports
// whether the AST changed. Fallbacks are returned as warnings; a failing
// custom shader is returned as err.
func Rewrite(prog *ast.Program, opts Options) (changed bool, diags []diag.Diagnostic, err error) {
	syms := findSymbols(prog)
	if len(syms.canvas) == 0 {
		return false, nil, nil
	}
	consts := moduleConsts(prog)
	var assets Assets
	assetsDone := false
	getAssets := func(el *ast.JSXElement) Assets {
		if assetsDone || opts.Assets == nil {
			return assets
		}
		assetsDone = true
		a, aerr := opts.Assets()
		if aerr != nil {
			diags = append(diags, assetsDiag(opts, el.Position, aerr))
			return assets
		}
		assets = a
		return assets
	}
	for _, stmt := range prog.Body {
		var canvases []*ast.JSXElement
		visit(reflect.ValueOf(stmt), map[uintptr]bool{}, func(n any) bool {
			if el, ok := n.(*ast.JSXElement); ok && el.Opening != nil && syms.canvas[el.Opening.Name] {
				canvases = append(canvases, el)
				return false
			}
			return true
		})
		if len(canvases) == 0 {
			continue
		}
		f := newFolder(consts)
		f.shadow = bindings(stmt)
		for _, el := range canvases {
			a := &analyzer{opts: opts, f: f, syms: syms, assets: getAssets(el)}
			setup, props, fb, herr := a.run(el)
			switch {
			case herr != nil:
				return changed, diags, herr
			case fb != nil:
				diags = append(diags, fallbackDiag(opts, fb))
				addAssetAttrs(el, getAssets(el))
			default:
				applyCanvas(el, setup, props)
				changed = true
			}
		}
	}
	if changed {
		finishImports(prog, syms)
	}
	return changed, diags, nil
}

// symbols are the local names bound to @krate/3d exports and to <For>.
type symbols struct {
	canvas map[string]bool   // local tag names that are <Canvas>
	other  map[string]string // local tag name to OrbitControls | Environment
	forTag map[string]bool   // local names of an imported <For>
	// canvasImports are the import statements the Canvas bindings come from.
	canvasImports []*ast.ImportStmt
}

func is3DSource(src string) bool {
	src = strings.Trim(src, "\"'")
	return src == "@krate/3d" || src == "@krate/3d/runtime"
}

func findSymbols(prog *ast.Program) *symbols {
	s := &symbols{canvas: map[string]bool{}, other: map[string]string{}, forTag: map[string]bool{}}
	for _, stmt := range prog.Body {
		imp, ok := stmt.(*ast.ImportStmt)
		if !ok {
			continue
		}
		src := strings.Trim(imp.Source, "\"'")
		switch {
		case is3DSource(imp.Source):
			if imp.Namespace != "" {
				s.canvas[imp.Namespace+".Canvas"] = true
				s.other[imp.Namespace+".OrbitControls"] = "OrbitControls"
				s.other[imp.Namespace+".Environment"] = "Environment"
				s.canvasImports = append(s.canvasImports, imp)
			}
			for _, n := range imp.Named {
				switch n.Remote {
				case "Canvas":
					s.canvas[n.Local] = true
					s.canvasImports = append(s.canvasImports, imp)
				case "OrbitControls", "Environment":
					s.other[n.Local] = n.Remote
				}
			}
		case strings.HasPrefix(src, "@krate/"):
			// Krate lists are written `{LIST.map(...)}`; a `<For>` component
			// is accepted too when a Krate package exports one.
			for _, n := range imp.Named {
				if n.Remote == "For" {
					s.forTag[n.Local] = true
				}
			}
		}
	}
	return s
}

// moduleConsts collects top-level `const NAME = init` declarations.
func moduleConsts(prog *ast.Program) map[string]ast.Expr {
	out := map[string]ast.Expr{}
	add := func(v *ast.VarStmt) {
		if v.Kind != ast.VarConst {
			return
		}
		for _, d := range v.Decls {
			if d != nil && d.Name != "" && !d.IsDestructuring && d.Init != nil {
				out[d.Name] = d.Init
			}
		}
	}
	for _, stmt := range prog.Body {
		switch s := stmt.(type) {
		case *ast.VarStmt:
			add(s)
		case *ast.ExportStmt:
			if v, ok := s.Declaration.(*ast.VarStmt); ok {
				add(v)
			}
		}
	}
	return out
}

// bindings returns every name bound anywhere inside stmt (parameters,
// declarations, function names). It over-approximates on purpose: a module
// constant is only folded when nothing in the enclosing statement rebinds it.
func bindings(stmt ast.Stmt) map[string]bool {
	out := map[string]bool{}
	addTarget := func(e ast.Expr) {
		if e == nil {
			return
		}
		visit(reflect.ValueOf(e), map[uintptr]bool{}, func(n any) bool {
			if id, ok := n.(*ast.Identifier); ok {
				out[id.Name] = true
			}
			return true
		})
	}
	visit(reflect.ValueOf(stmt), map[uintptr]bool{}, func(n any) bool {
		switch v := n.(type) {
		case *ast.VarDecl:
			if v.Name != "" {
				out[v.Name] = true
			}
			for _, nm := range v.Names {
				out[nm] = true
			}
			if v.RestName != "" {
				out[v.RestName] = true
			}
			addTarget(v.Target)
		case *ast.Param:
			if v.Name != "" {
				out[v.Name] = true
			}
			addTarget(v.Target)
		case *ast.FnDecl:
			out[v.Name] = true
		case *ast.ArrowFn:
			if v.Name != "" {
				out[v.Name] = true
			}
		case *ast.ClassDecl:
			out[v.Name] = true
		}
		return true
	})
	return out
}

// visit walks every AST node reachable from v. fn returns false to skip a
// node's children.
func visit(v reflect.Value, seen map[uintptr]bool, fn func(n any) bool) {
	switch v.Kind() {
	case reflect.Interface:
		if !v.IsNil() {
			visit(v.Elem(), seen, fn)
		}
	case reflect.Ptr:
		if v.IsNil() {
			return
		}
		p := v.Pointer()
		if seen[p] {
			return
		}
		seen[p] = true
		if v.CanInterface() && !fn(v.Interface()) {
			return
		}
		visit(v.Elem(), seen, fn)
	case reflect.Struct:
		for i := 0; i < v.NumField(); i++ {
			visit(v.Field(i), seen, fn)
		}
	case reflect.Slice:
		for i := 0; i < v.Len(); i++ {
			visit(v.Index(i), seen, fn)
		}
	}
}

// finishImports adds the import of the runtime entry points and drops the
// Canvas binding when no reference to it is left.
func finishImports(prog *ast.Program, syms *symbols) {
	imps := []ast.Stmt{
		&ast.ImportStmt{Source: `"` + CompiledModule + `"`, Named: []ast.NamedImport{{Local: k3dName, Remote: "__k3d"}}},
		&ast.ImportStmt{Source: `"` + CanvasModule + `"`, Named: []ast.NamedImport{{Local: compName, Remote: "CompiledCanvas"}}},
	}
	prog.Body = append(imps, prog.Body...)

	for local := range syms.canvas {
		if refCount(prog, local) > 0 {
			continue
		}
		for _, ci := range syms.canvasImports {
			kept := ci.Named[:0]
			for _, n := range ci.Named {
				if n.Local != local {
					kept = append(kept, n)
				}
			}
			ci.Named = kept
		}
	}
	body := prog.Body[:0]
	for _, stmt := range prog.Body {
		if ci, ok := stmt.(*ast.ImportStmt); ok && is3DSource(ci.Source) && ci.Default == "" && ci.Namespace == "" && len(ci.Named) == 0 && containsImport(syms.canvasImports, ci) {
			continue
		}
		body = append(body, stmt)
	}
	prog.Body = body
}

func containsImport(list []*ast.ImportStmt, imp *ast.ImportStmt) bool {
	for _, x := range list {
		if x == imp {
			return true
		}
	}
	return false
}

// refCount counts the identifier, JSX tag and export references to local.
func refCount(prog *ast.Program, local string) int {
	n := 0
	visit(reflect.ValueOf(prog), map[uintptr]bool{}, func(node any) bool {
		switch v := node.(type) {
		case *ast.Identifier:
			if v.Name == local || strings.HasPrefix(local, v.Name+".") {
				n++
			}
		case *ast.JSXElement:
			if v.Opening != nil && v.Opening.Name == local {
				n++
			}
		case *ast.ExportStmt:
			for _, e := range v.Named {
				if e.Local == local {
					n++
				}
			}
		}
		return true
	})
	return n
}

// addAssetAttrs gives a Canvas that stays on the runtime path the engine URLs
// the build produced, unless the author set them.
func addAssetAttrs(el *ast.JSXElement, a Assets) {
	set := func(name, value string) {
		if value == "" {
			return
		}
		for _, at := range el.Opening.Attributes {
			if at.Name == name {
				return
			}
		}
		el.Opening.Attributes = append(el.Opening.Attributes, &ast.JSXAttr{Position: el.Position, Name: name, Value: strLit(value)})
	}
	set("filamentUrl", a.FilamentURL)
	set("materialBase", a.MaterialBase)
}

// assetsDiag builds the KR8702 warning for engine files the build could not
// provide.
func assetsDiag(opts Options, pos ast.Pos, err error) diag.Diagnostic {
	d := diag.New(opts.File, pos.Line, pos.Col, err.Error(),
		"install filament in the app (npm install filament) so the build can copy filament.js and filament.wasm into the site",
		sourceLine(opts.Source, pos.Line))
	d.Severity = diag.SeverityWarning
	d.Code = diag.CodeScene3DAssets
	return d
}

// fallbackDiag builds the KR8701 warning for a Canvas left to the runtime.
func fallbackDiag(opts Options, fb *fallback) diag.Diagnostic {
	d := diag.New(opts.File, fb.pos.Line, fb.pos.Col,
		fmt.Sprintf("<Canvas> is not compiled ahead of time: %s", fb.reason),
		"the scene still works: it runs on the @krate/3d runtime path without the compile-time flattening, instancing and direct transform writes; "+
			"remove the construct named above (or hoist it into a module-level constant) to get the compiled path",
		sourceLine(opts.Source, fb.pos.Line))
	d.Severity = diag.SeverityWarning
	d.Code = diag.CodeScene3DFallback
	return d
}

func sourceLine(src string, line int) string {
	if src == "" || line < 1 {
		return ""
	}
	lines := strings.Split(src, "\n")
	if line > len(lines) {
		return ""
	}
	return strings.TrimRight(lines[line-1], "\r")
}
