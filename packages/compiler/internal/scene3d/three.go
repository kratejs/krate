package scene3d

// The compile-time optimizer for @krate/three. It finds the provably static
// parts of a <Canvas> imported from "@krate/three" and replaces them with one
// <K3CompiledScene build={(__t) => {...}}/> element per parent. The build function
// makes the three.js objects directly (see packages/three/src/engine/compiled.ts):
//
//   - identical static geometries and materials become one shared instance;
//   - static transforms become matrices computed here, with matrixAutoUpdate
//     (and, when nothing above can move, matrixWorldAutoUpdate) turned off;
//   - two or more static meshes with the same geometry and material become one
//     InstancedMesh filled from a Float32Array;
//   - a reactive position, rotation or scale becomes an effect that writes the
//     object (or one instance matrix) directly;
//   - the runtime compiles the scene's shaders before the first frame.
//
// An element is only compiled when every prop and child is understood (the
// allowed sets are in three_analyze.go); everything else, and anything that
// sits beside or around a compiled part, stays JSX on the reconciler path. There is
// no diagnostic for a partial or empty result: the runtime path is the
// reference behavior and always works. <Canvas compile={false}> skips the pass.

import (
	"reflect"
	"strings"

	"github.com/kratejs/krate/packages/compiler/ast"
)

const (
	// ThreeModule is the package whose <Canvas> the pass compiles.
	ThreeModule = "@krate/three"
	// ThreeCompiledModule exports the CompiledScene component the pass emits.
	ThreeCompiledModule = "@krate/three/engine/compiled"

	threeCompName = "K3CompiledScene"
	threeBuilder  = "__t"
)

// MentionsThree reports whether source text can contain a @krate/three Canvas.
func MentionsThree(src string) bool { return strings.Contains(src, ThreeModule) }

// RewriteThree compiles the static parts of every <Canvas> imported from
// @krate/three in prog. It reports whether the AST changed.
func RewriteThree(prog *ast.Program) bool {
	canvas := findThreeCanvas(prog)
	if len(canvas) == 0 {
		return false
	}
	consts := moduleConsts(prog)
	changed := false
	for _, stmt := range prog.Body {
		var canvases []*ast.JSXElement
		visit(reflect.ValueOf(stmt), map[uintptr]bool{}, func(n any) bool {
			if el, ok := n.(*ast.JSXElement); ok && el.Opening != nil && canvas[el.Opening.Name] {
				canvases = append(canvases, el)
			}
			return true
		})
		if len(canvases) == 0 {
			continue
		}
		f := newFolder(consts)
		f.shadow = bindings(stmt)
		for _, el := range canvases {
			if !threeCompileEnabled(el, f) {
				continue
			}
			tc := &threeCompiler{f: f}
			before := tc.count
			el.Children = tc.children(el.Children, true)
			if tc.count != before {
				changed = true
			}
		}
	}
	if changed {
		prog.Body = append([]ast.Stmt{&ast.ImportStmt{
			Source: `"` + ThreeCompiledModule + `"`,
			Named:  []ast.NamedImport{{Local: threeCompName, Remote: "CompiledScene"}},
		}}, prog.Body...)
	}
	return changed
}

// findThreeCanvas returns the local tag names bound to <Canvas> of
// "@krate/three".
func findThreeCanvas(prog *ast.Program) map[string]bool {
	names := map[string]bool{}
	for _, stmt := range prog.Body {
		imp, ok := stmt.(*ast.ImportStmt)
		if !ok || strings.Trim(imp.Source, "\"'") != ThreeModule {
			continue
		}
		if imp.Namespace != "" {
			names[imp.Namespace+".Canvas"] = true
		}
		for _, n := range imp.Named {
			if n.Remote == "Canvas" {
				names[n.Local] = true
			}
		}
	}
	return names
}

// threeCompileEnabled reports whether the Canvas takes part: `compile` absent
// or the constant true. Any other value (false, or an expression the compiler
// cannot read) keeps the whole Canvas on the runtime path.
func threeCompileEnabled(el *ast.JSXElement, f *folder) bool {
	for _, at := range el.Opening.Attributes {
		if at.Spread {
			continue
		}
		if at.Name != "compile" {
			continue
		}
		v, ok := f.fold(attrValue(at), nil)
		return ok && v.k == vBool && v.b
	}
	return true
}

// threeCompiler rewrites the children lists under one Canvas.
type threeCompiler struct {
	f     *folder
	count int
}

// bail aborts the analysis of one element; the element stays JSX.
type threeBail struct{}

func tbail() { panic(threeBail{}) }

// try runs analysis and reports whether the element was understood.
func (c *threeCompiler) try(fn func() []*tnode) (nodes []*tnode, ok bool) {
	defer func() {
		switch r := recover().(type) {
		case nil:
		case threeBail:
			nodes, ok = nil, false
		default:
			panic(r)
		}
	}()
	return fn(), true
}

// children rewrites a children list. top is true when the list is the Canvas's
// own, so a compiled subtree sits directly in the scene.
func (c *threeCompiler) children(list []ast.JSXChild, top bool) []ast.JSXChild {
	out := make([]ast.JSXChild, 0, len(list))
	var compiled []*tnode
	first := -1
	add := func(nodes []*tnode) {
		compiled = append(compiled, nodes...)
		if first < 0 {
			first = len(out)
			out = append(out, nil)
		}
	}
	root := &tctx{parent: identity, chainStatic: true, plain: true}
	for _, ch := range list {
		switch v := ch.(type) {
		case *ast.JSXElementChild:
			name := v.Element.Opening.Name
			if name == "mesh" || name == "group" {
				if nodes, ok := c.try(func() []*tnode { return []*tnode{c.element(v.Element, root)} }); ok {
					add(nodes)
					continue
				}
			}
			if isIntrinsicTag(name) {
				v.Element.Children = c.children(v.Element.Children, false)
			}
		case *ast.JSXExprContainer:
			if nodes, ok := c.try(func() []*tnode { return c.mapExpr(v.Expression, root) }); ok {
				if len(nodes) > 0 {
					add(nodes)
				}
				continue
			}
		case *ast.JSXFragmentChild:
			v.Fragment.Children = c.children(v.Fragment.Children, top)
		}
		out = append(out, ch)
	}
	if first >= 0 {
		c.count++
		el := c.compiledElement(compiled, top)
		out[first] = &ast.JSXElementChild{Element: el}
	}
	return out
}

// isIntrinsicTag reports a lowercase scene tag (not a component).
func isIntrinsicTag(name string) bool {
	return name != "" && name[0] >= 'a' && name[0] <= 'z' && !strings.Contains(name, ".")
}
