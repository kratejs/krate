package scene3d

import (
	"fmt"
	"strings"

	"github.com/kratejs/krate/packages/compiler/ast"
)

var (
	orbitProps = map[string]bool{"target": true, "enableZoom": true, "enablePan": true, "enableRotate": true,
		"autoRotate": true, "autoRotateSpeed": true, "minDistance": true, "maxDistance": true}
	envProps = map[string]bool{"preset": true, "files": true, "background": true, "intensity": true}
)

// control compiles <OrbitControls> and <Environment>, which attach to the
// scene through calls to the runtime component functions.
func (a *analyzer) control(remote, tag string, el *ast.JSXElement, cx *ctx) {
	a.dyn(cx, el.Position, "<"+remote+">")
	allowed := orbitProps
	if remote == "Environment" {
		allowed = envProps
	}
	var props []*ast.ObjectProp
	for _, at := range el.Opening.Attributes {
		if at.Spread {
			a.bail(el.Position, "a spread prop on <%s>", remote)
		}
		if at.Name == "key" {
			continue
		}
		if !allowed[at.Name] {
			a.bail(at.Position, "the prop %q on <%s> is not supported by the compiler", at.Name, remote)
		}
		e := attrValue(at)
		if x, ok := a.f.fold(e, cx.sc); ok {
			props = append(props, kv(at.Name, valueExpr(x)))
			continue
		}
		if remote == "OrbitControls" {
			// The controller re-reads its props every frame: keep them live.
			props = append(props, &ast.ObjectProp{
				Key: at.Name, Method: true, Getter: true,
				Value: &ast.ArrowFn{Function: true, Name: at.Name, Body: []ast.Stmt{&ast.ReturnStmt{Value: e}}},
			})
		} else {
			props = append(props, kv(at.Name, e))
		}
	}
	a.extraStmts = append(a.extraStmts, exprStmt(call(tagExpr(tag), obj(props...))))
}

// tagExpr turns a JSX tag name such as `K.OrbitControls` into an expression.
func tagExpr(name string) ast.Expr {
	parts := strings.Split(name, ".")
	var e ast.Expr = ident(parts[0])
	for _, p := range parts[1:] {
		e = member(e, p)
	}
	return e
}

// assemble builds the setup function from everything the walk collected.
func (a *analyzer) assemble(view, camera, opts []*ast.ObjectProp, onCreated ast.Expr) *ast.ArrowFn {
	var body []ast.Stmt

	sceneOpts := []*ast.ObjectProp{}
	if a.slots > 0 {
		sceneOpts = append(sceneOpts, kv("slots", numLit(float64(a.slots))))
	}
	if len(view) > 0 {
		sceneOpts = append(sceneOpts, kv("view", obj(view...)))
	}
	sceneOpts = append(sceneOpts, kv("camera", obj(camera...)))
	if a.assets.FilamentURL != "" {
		sceneOpts = append(sceneOpts, kv("filamentUrl", strLit(a.assets.FilamentURL)))
	}
	if a.assets.MaterialBase != "" {
		sceneOpts = append(sceneOpts, kv("materialBase", strLit(a.assets.MaterialBase)))
	}
	sceneOpts = append(sceneOpts, opts...)
	body = append(body, constStmt(sceneName, method(k3dName, "scene", ident(canvasName), obj(sceneOpts...))))

	for _, g := range a.geomList {
		args := make([]float64, len(g.args))
		copy(args, g.args)
		body = append(body, constStmt(g.name, method(sceneName, "geometry", strLit(g.kind), numArr(args...))))
	}
	for _, m := range a.matList {
		body = append(body, constStmt(m.name, m.call()))
		body = append(body, m.effects()...)
	}
	body = append(body, a.staticStmts()...)
	body = append(body, a.dynStmts...)
	body = append(body, a.lightStmts...)
	body = append(body, a.extraStmts...)
	if onCreated != nil {
		body = append(body, exprStmt(call(onCreated, ident(sceneName))))
	}
	return arrowBlock([]string{canvasName}, body)
}

// staticStmts emits the fully static meshes. Two or more meshes with the same
// geometry, material and flags and no event handlers become one instanced call.
func (a *analyzer) staticStmts() []ast.Stmt {
	type group struct {
		g     *geomDef
		mat   *matDef
		flags meshFlags
		list  []*staticMesh
	}
	var order []string
	groups := map[string]*group{}
	var singles []*staticMesh
	for _, s := range a.statics {
		if len(s.events) > 0 || !s.flags.static {
			singles = append(singles, s)
			continue
		}
		key := fmt.Sprintf("%s|%s|%s", s.g.name, s.m.name, s.flags.key)
		gr := groups[key]
		if gr == nil {
			gr = &group{g: s.g, mat: s.m, flags: s.flags}
			groups[key] = gr
			order = append(order, key)
		}
		gr.list = append(gr.list, s)
	}
	var out []ast.Stmt
	emitSingle := func(s *staticMesh) {
		args := []ast.Expr{ident(s.g.name), ident(s.m.name), matArr(s.matrix)}
		if len(s.flags.props) > 0 {
			args = append(args, obj(s.flags.props...))
		}
		call := method(sceneName, "staticMesh", args...)
		if len(s.events) == 0 {
			out = append(out, exprStmt(call))
			return
		}
		h := a.entityName()
		out = append(out, constStmt(h, call))
		a.addEvents(h, s.events, &out)
	}
	for _, key := range order {
		gr := groups[key]
		if len(gr.list) < 2 {
			emitSingle(gr.list[0])
			continue
		}
		var elems []ast.Expr
		for _, s := range gr.list {
			for _, v := range s.matrix {
				elems = append(elems, f32Lit(v))
			}
		}
		args := []ast.Expr{ident(gr.g.name), ident(gr.mat.name), numLit(float64(len(gr.list))),
			&ast.NewExpr{Callee: ident("Float32Array"), Args: []ast.Expr{arrOf(elems...)}}}
		if len(gr.flags.props) > 0 {
			args = append(args, obj(gr.flags.props...))
		}
		out = append(out, exprStmt(method(sceneName, "instanced", args...)))
	}
	for _, s := range singles {
		emitSingle(s)
	}
	return out
}

// applyCanvas rewrites the <Canvas> element in place into the compiled form.
func applyCanvas(el *ast.JSXElement, setup *ast.ArrowFn, keep []*ast.JSXAttr) {
	attrs := append([]*ast.JSXAttr{}, keep...)
	attrs = append(attrs, &ast.JSXAttr{Position: el.Position, Name: "setup", Value: setup})
	el.Opening = &ast.JSXOpening{Name: compName, Attributes: attrs, SelfClosing: true}
	el.Children = nil
	el.Closing = nil
}
