package scene3d

import (
	"fmt"
	"strconv"

	"github.com/kratejs/krate/packages/compiler/ast"
)

// temitter builds the body of one build function.
type temitter struct {
	geoVar map[string]string
	matVar map[string]string
	decls  []ast.Stmt
	body   []ast.Stmt
	nObj   int
	nWrite int
}

func nullLit() ast.Expr { return &ast.Literal{Kind: ast.NullLit, Value: "null"} }

func root() ast.Expr { return member(ident(threeBuilder), "root") }

// compiledElement builds <K3CompiledScene top build={...}/> for the nodes.
func (c *threeCompiler) compiledElement(nodes []*tnode, top bool) *ast.JSXElement {
	e := &temitter{geoVar: map[string]string{}, matVar: map[string]string{}}
	groups := collectInstances(nodes)
	for _, n := range nodes {
		e.node(n, root())
	}
	for _, g := range groups {
		e.instanced(g)
	}
	body := append(e.decls, e.body...)
	var attrs []*ast.JSXAttr
	if top {
		attrs = append(attrs, &ast.JSXAttr{Name: "top", Value: boolLit(true)})
	}
	attrs = append(attrs, &ast.JSXAttr{Name: "build", Value: arrowBlock([]string{threeBuilder}, body)})
	return &ast.JSXElement{Opening: &ast.JSXOpening{Name: threeCompName, Attributes: attrs, SelfClosing: true}}
}

// instGroup is the static meshes that share a geometry, material and props.
type instGroup struct {
	geo   *tgeom
	mat   *tmat
	props []cprop
	mats  []mat4
}

// collectInstances marks the static meshes that repeat and returns their groups.
func collectInstances(nodes []*tnode) []*instGroup {
	var order []*instGroup
	byKey := map[string]*instGroup{}
	members := map[string][]*tnode{}
	var walk func(list []*tnode)
	walk = func(list []*tnode) {
		for _, n := range list {
			walk(n.kids)
			if !n.instanceable() {
				continue
			}
			key := n.geo.key + "#" + n.mat.key + "#" + fmt.Sprint(n.props)
			g := byKey[key]
			if g == nil {
				g = &instGroup{geo: n.geo, mat: n.mat, props: n.props}
				byKey[key] = g
				order = append(order, g)
			}
			members[key] = append(members[key], n)
			g.mats = append(g.mats, n.world)
		}
	}
	walk(nodes)
	var out []*instGroup
	for _, g := range order {
		key := g.geo.key + "#" + g.mat.key + "#" + fmt.Sprint(g.props)
		if len(g.mats) < 2 {
			continue
		}
		for _, n := range members[key] {
			n.instanced = true
		}
		out = append(out, g)
	}
	return out
}

// instanceable: a static, anonymous, handler-free, opaque mesh whose whole
// ancestor chain is static and carries no props of its own.
func (n *tnode) instanceable() bool {
	if !n.mesh || n.dyn != nil || !n.chainStatic || !n.plain || len(n.events) > 0 || !n.mat.opaque() {
		return false
	}
	for _, p := range n.props {
		if p.key == "name" {
			return false
		}
	}
	return true
}

// vanishes: a plain group left with nothing to hold.
func (n *tnode) vanishes() bool {
	if n.mesh || n.dyn != nil || len(n.props) > 0 {
		return false
	}
	for _, k := range n.kids {
		if !k.instanced && k.dyn == nil && !k.vanishes() {
			return false
		}
	}
	return true
}

// trimWorld drops an `undefined` world matrix that ends the arguments.
func trimWorld(args []ast.Expr, worldIdx int) []ast.Expr {
	if len(args) == worldIdx+1 && isUndefined(args[worldIdx]) {
		return args[:worldIdx]
	}
	return args
}

func (e *temitter) geometry(g *tgeom) ast.Expr {
	name, ok := e.geoVar[g.key]
	if !ok {
		name = fmt.Sprintf("__g%d", len(e.geoVar))
		e.geoVar[g.key] = name
		args := []ast.Expr{strLit(g.kind)}
		if len(g.args) > 0 {
			args = append(args, numArr(g.args...))
		}
		e.decls = append(e.decls, constStmt(name, method(threeBuilder, "geo", args...)))
	}
	return ident(name)
}

func (e *temitter) material(m *tmat) ast.Expr {
	name, ok := e.matVar[m.key]
	if !ok {
		name = fmt.Sprintf("__m%d", len(e.matVar))
		e.matVar[m.key] = name
		args := []ast.Expr{strLit(m.tag)}
		if len(m.props) > 0 {
			args = append(args, propsObj(m.props))
		}
		e.decls = append(e.decls, constStmt(name, method(threeBuilder, "mat", args...)))
	}
	return ident(name)
}

func propsObj(props []cprop) *ast.ObjectExpr {
	kvs := make([]*ast.ObjectProp, len(props))
	for i, p := range props {
		kvs[i] = kv(p.key, valueExpr(p.v))
	}
	return obj(kvs...)
}

// precLit emits a matrix element: junk below 1e-12 is zero and the rest keeps
// twelve significant digits.
func precLit(n float64) ast.Expr {
	if n > -1e-12 && n < 1e-12 {
		return numberText("0", false)
	}
	r, _ := strconv.ParseFloat(strconv.FormatFloat(n, 'g', 12, 64), 64)
	return numLit(r)
}

func matLit(m mat4) ast.Expr {
	if m.isIdentity() {
		return boolLit(true)
	}
	out := make([]ast.Expr, 16)
	for i, v := range m {
		out[i] = precLit(v)
	}
	return arrOf(out...)
}

// trimTail drops trailing null arguments after index keep, which the runtime
// treats as absent.
func trimTail(args []ast.Expr, keep int) []ast.Expr {
	for len(args) > keep {
		if l, ok := args[len(args)-1].(*ast.Literal); ok && l.Kind == ast.NullLit {
			args = args[:len(args)-1]
			continue
		}
		break
	}
	return args
}

func undefinedIdent() ast.Expr { return ident("undefined") }

func isUndefined(e ast.Expr) bool {
	id, ok := e.(*ast.Identifier)
	return ok && id.Name == "undefined"
}

func (e *temitter) node(n *tnode, parent ast.Expr) {
	if n.dyn != nil {
		e.dynamic(n.dyn)
		return
	}
	if n.instanced {
		return
	}
	if n.vanishes() {
		// Reactive instanced meshes below it still live at the root.
		for _, k := range n.kids {
			e.node(k, parent)
		}
		return
	}
	// The constant props, then the constant transform props, applied through
	// applyProps so the object holds the values the author wrote.
	props := append([]cprop(nil), n.props...)
	for _, t := range []struct {
		name string
		v    tvec
	}{{"position", n.pos}, {"rotation", n.rot}, {"scale", n.scl}} {
		if t.v.present && t.v.static {
			props = append(props, cprop{key: t.name, v: value{k: vArr, a: []value{num(t.v.v[0]), num(t.v.v[1]), num(t.v.v[2])}}})
		}
	}
	var propsArg ast.Expr = nullLit()
	if len(props) > 0 {
		propsArg = propsObj(props)
	}
	var local, world ast.Expr = nullLit(), nullLit()
	if n.staticLocal {
		local = matLit(n.local)
	}
	if n.chainStatic {
		world = matLit(n.world)
		if n.staticLocal && n.world == n.local {
			// The runtime reads an omitted world matrix as the local one.
			world = undefinedIdent()
		}
	}
	var call *ast.CallExpr
	if n.mesh {
		var handlers ast.Expr = nullLit()
		if len(n.events) > 0 {
			handlers = obj(n.events...)
		}
		call = method(threeBuilder, "mesh", trimWorld(trimTail([]ast.Expr{parent, e.geometry(n.geo), e.material(n.mat), local, world, propsArg, handlers}, 5), 4)...)
	} else {
		call = method(threeBuilder, "group", trimWorld(trimTail([]ast.Expr{parent, local, world, propsArg}, 3), 2)...)
	}
	reactive := !n.staticLocal
	if len(n.kids) == 0 && !reactive {
		e.body = append(e.body, exprStmt(call))
		return
	}
	name := fmt.Sprintf("__o%d", e.nObj)
	e.nObj++
	e.body = append(e.body, constStmt(name, call))
	if reactive {
		for _, t := range []struct {
			field string
			v     tvec
		}{{"position", n.pos}, {"rotation", n.rot}, {"scale", n.scl}} {
			if t.v.present && !t.v.static {
				e.write(name, t.field, t.v.expr)
			}
		}
	}
	for _, k := range n.kids {
		e.node(k, ident(name))
	}
}

// litElems returns the three elements of a literal [x, y, z].
func litElems(x ast.Expr) ([]ast.Expr, bool) {
	arr, ok := x.(*ast.ArrayExpr)
	if !ok || len(arr.Elements) != 3 {
		return nil, false
	}
	for _, el := range arr.Elements {
		if el == nil {
			return nil, false
		}
		if u, ok := el.(*ast.UnaryExpr); ok && u.Op == "..." {
			return nil, false
		}
	}
	return arr.Elements, true
}

// write emits the effect that sets obj.field from a reactive expression.
func (e *temitter) write(objName, field string, x ast.Expr) {
	target := member(ident(objName), field)
	var stmt ast.Stmt
	if elems, ok := litElems(x); ok {
		stmt = exprStmt(call(member(target, "set"), elems...))
	} else {
		stmt = exprStmt(method(threeBuilder, "vec", target, x))
	}
	e.body = append(e.body, exprStmt(method(threeBuilder, "fx", arrowBlock(nil, []ast.Stmt{stmt}))))
}

func (e *temitter) instanced(g *instGroup) {
	elems := make([]ast.Expr, 0, 16*len(g.mats))
	for _, m := range g.mats {
		for _, v := range m {
			elems = append(elems, f32Lit(v))
		}
	}
	args := []ast.Expr{root(), e.geometry(g.geo), e.material(g.mat),
		&ast.NewExpr{Callee: ident("Float32Array"), Args: []ast.Expr{arrOf(elems...)}}}
	if len(g.props) > 0 {
		args = append(args, propsObj(g.props))
	}
	e.body = append(e.body, exprStmt(method(threeBuilder, "instances", args...)))
}

// dynamic emits a `.map` of reactive meshes: one InstancedMesh and one effect
// per item that writes the item's matrix.
func (e *temitter) dynamic(d *tdyn) {
	w := fmt.Sprintf("__w%d", e.nWrite)
	e.nWrite++
	var parentArg ast.Expr = nullLit()
	if !d.parent.isIdentity() {
		parentArg = arrOf(matElems(d.parent)...)
	}
	args := []ast.Expr{root(), e.geometry(d.geo), e.material(d.mat), numLit(float64(d.count)), parentArg}
	if len(d.props) > 0 {
		args = append(args, propsObj(d.props))
	}
	args = trimTail(args, 4)
	e.body = append(e.body, constStmt(w, method(threeBuilder, "dynamicInstances", args...)))

	names := []string{"__item", "__i"}
	for i, p := range d.params {
		names[i] = p.Name
	}
	idx := ident(names[1])
	var write ast.Expr
	if p, r, s, ok := d.literal(); ok {
		args := append([]ast.Expr{idx}, p...)
		args = append(args, r...)
		args = append(args, s...)
		write = method(w, "setv", args...)
	} else {
		write = method(w, "set", idx, optExpr(d.pos), optExpr(d.rot), optExpr(d.scl))
	}
	fx := method(threeBuilder, "fx", arrowBlock(nil, []ast.Stmt{exprStmt(write)}))
	e.body = append(e.body, exprStmt(call(member(d.list, "forEach"), arrowBlock(names, []ast.Stmt{exprStmt(fx)}))))
}

func optExpr(t tvec) ast.Expr {
	if !t.present {
		return ident("undefined")
	}
	return t.expr
}

// literal returns the nine numbers of setv when every transform prop is
// absent or a literal [x, y, z] (a scalar scale has no literal form here).
func (d *tdyn) literal() (p, r, s []ast.Expr, ok bool) {
	get := func(t tvec, def float64) ([]ast.Expr, bool) {
		if !t.present {
			return []ast.Expr{numLit(def), numLit(def), numLit(def)}, true
		}
		return litElems(t.expr)
	}
	var ok1, ok2, ok3 bool
	p, ok1 = get(d.pos, 0)
	r, ok2 = get(d.rot, 0)
	s, ok3 = get(d.scl, 1)
	return p, r, s, ok1 && ok2 && ok3
}

func matElems(m mat4) []ast.Expr {
	out := make([]ast.Expr, 16)
	for i, v := range m {
		out[i] = precLit(v)
	}
	return out
}
