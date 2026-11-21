package scene3d

import (
	"fmt"
	"sort"
	"strings"

	"github.com/kratejs/krate/packages/compiler/ast"
)

// tvec is one transform prop (position, rotation or scale) of a group or mesh.
type tvec struct {
	present bool
	static  bool
	v       [3]float64
	expr    ast.Expr
}

type cprop struct {
	key string
	v   value
}

// tnode is a compiled group or mesh.
type tnode struct {
	mesh          bool
	pos, rot, scl tvec
	// props are the constant props (name, visible, ...); events the handlers.
	props  []cprop
	events []*ast.ObjectProp
	geo    *tgeom
	mat    *tmat
	kids   []*tnode

	// staticLocal: all own transform props are constant. chainStatic: the
	// ancestors' too, so world is the matrix relative to the compiled root.
	staticLocal bool
	chainStatic bool
	plain       bool
	local       mat4
	world       mat4

	// dyn is set on a node standing for a `.map` of meshes with reactive transforms.
	dyn *tdyn
	// instanced is set once the node was folded into an InstancedMesh.
	instanced bool
}

type tgeom struct {
	kind string
	args []float64
	key  string
}

type tmat struct {
	tag   string
	props []cprop
	key   string
}

// tdyn is a `.map` over a constant array whose items are meshes of one
// geometry and material with reactive transforms: one InstancedMesh, written
// by one effect per item.
type tdyn struct {
	geo    *tgeom
	mat    *tmat
	props  []cprop
	count  int
	parent mat4
	list   ast.Expr
	params []*ast.Param
	pos    tvec
	rot    tvec
	scl    tvec
}

type tctx struct {
	parent      mat4
	chainStatic bool
	plain       bool
	sc          *scope
	loop        bool
}

var threeGeometries = map[string]string{
	"boxGeometry": "box", "sphereGeometry": "sphere", "planeGeometry": "plane",
	"cylinderGeometry": "cylinder", "torusGeometry": "torus", "circleGeometry": "circle",
}

var threeMaterialTags = map[string]bool{
	"meshStandardMaterial": true, "meshPhysicalMaterial": true, "meshBasicMaterial": true,
	"meshLambertMaterial": true, "meshPhongMaterial": true,
}

// Constant material props the compiler shares an instance for. Anything else
// (maps, a function, a reactive value) keeps the mesh on the reconciler.
var threeMaterialProps = map[string]bool{
	"color": true, "roughness": true, "metalness": true, "opacity": true, "transparent": true,
	"wireframe": true, "flatShading": true, "emissive": true, "emissiveIntensity": true,
	"envMapIntensity": true, "side": true, "depthWrite": true, "depthTest": true, "visible": true,
	"alphaTest": true, "toneMapped": true, "fog": true, "clearcoat": true, "clearcoatRoughness": true,
	"transmission": true, "ior": true, "thickness": true, "reflectivity": true, "shininess": true,
	"specular": true, "name": true,
}

// Constant object props applied through applyProps.
var threeObjectProps = map[string]bool{
	"name": true, "visible": true, "castShadow": true, "receiveShadow": true,
	"renderOrder": true, "frustumCulled": true,
}

var threeEvents = map[string]bool{
	"onClick": true, "onDoubleClick": true, "onContextMenu": true, "onWheel": true,
	"onPointerOver": true, "onPointerOut": true, "onPointerEnter": true, "onPointerLeave": true,
	"onPointerMove": true, "onPointerDown": true, "onPointerUp": true, "onPointerCancel": true,
}

// element analyses a <group> or <mesh>; any construct it does not understand
// aborts with tbail.
func (c *threeCompiler) element(el *ast.JSXElement, cx *tctx) *tnode {
	switch el.Opening.Name {
	case "group":
		return c.group(el, cx)
	case "mesh":
		return c.meshNode(el, cx)
	}
	tbail()
	return nil
}

type tattrs map[string]*ast.JSXAttr

// attrs reads the props of an element: spreads and `ref` (and every prop that
// is not in the element's allowed sets) abort.
func threeAttrs(el *ast.JSXElement, allowed func(name string) bool) tattrs {
	out := tattrs{}
	for _, at := range el.Opening.Attributes {
		if at.Spread {
			tbail()
		}
		if at.Name == "key" {
			continue
		}
		if !allowed(at.Name) {
			tbail()
		}
		out[at.Name] = at
	}
	return out
}

func isTransform(name string) bool {
	return name == "position" || name == "rotation" || name == "scale"
}

func (c *threeCompiler) vec(p tattrs, name string, cx *tctx) tvec {
	at := p[name]
	if at == nil {
		return tvec{}
	}
	e := attrValue(at)
	x, ok := c.f.fold(e, cx.sc)
	if !ok {
		if cx.loop {
			tbail()
		}
		return tvec{present: true, expr: e}
	}
	if x.k == vNum && name == "scale" {
		return tvec{present: true, static: true, v: [3]float64{x.n, x.n, x.n}}
	}
	if x.k == vArr && len(x.a) == 3 && x.a[0].k == vNum && x.a[1].k == vNum && x.a[2].k == vNum {
		return tvec{present: true, static: true, v: [3]float64{x.a[0].n, x.a[1].n, x.a[2].n}}
	}
	// A number position, a four-element euler, ... keep r3f's own handling.
	tbail()
	return tvec{}
}

func (t tvec) or(def [3]float64) [3]float64 {
	if t.present && t.static {
		return t.v
	}
	return def
}

func (n *tnode) localMatrix() mat4 {
	r := n.rot.or([3]float64{})
	return composeTRS(n.pos.or([3]float64{}), eulerQuat(r[0], r[1], r[2]), n.scl.or([3]float64{1, 1, 1}))
}

// constProps folds the allowed constant props of an element into a sorted list.
func (c *threeCompiler) constProps(p tattrs, allowed map[string]bool, cx *tctx) []cprop {
	var out []cprop
	for name, at := range p {
		if !allowed[name] {
			continue
		}
		x, ok := c.f.fold(attrValue(at), cx.sc)
		if !ok || (x.k != vNum && x.k != vStr && x.k != vBool) {
			tbail()
		}
		out = append(out, cprop{key: name, v: x})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].key < out[j].key })
	return out
}

func (c *threeCompiler) transforms(n *tnode, p tattrs, cx *tctx) {
	n.pos = c.vec(p, "position", cx)
	n.rot = c.vec(p, "rotation", cx)
	n.scl = c.vec(p, "scale", cx)
	n.staticLocal = (!n.pos.present || n.pos.static) && (!n.rot.present || n.rot.static) && (!n.scl.present || n.scl.static)
	n.chainStatic = cx.chainStatic && n.staticLocal
	if n.staticLocal {
		n.local = n.localMatrix()
		n.world = mul(cx.parent, n.local)
	}
}

func (c *threeCompiler) group(el *ast.JSXElement, cx *tctx) *tnode {
	p := threeAttrs(el, func(name string) bool { return isTransform(name) || threeObjectProps[name] })
	n := &tnode{}
	n.props = c.constProps(p, threeObjectProps, cx)
	c.transforms(n, p, cx)
	n.plain = cx.plain && len(n.props) == 0
	kid := &tctx{parent: n.world, chainStatic: n.chainStatic, plain: n.plain, sc: cx.sc, loop: cx.loop}
	n.kids = c.childNodes(el.Children, kid)
	return n
}

func (c *threeCompiler) childNodes(list []ast.JSXChild, cx *tctx) []*tnode {
	var out []*tnode
	for _, ch := range list {
		switch v := ch.(type) {
		case *ast.JSXText:
			if strings.TrimSpace(v.Value) != "" {
				tbail()
			}
		case *ast.JSXExprContainer:
			if v.Expression == nil {
				continue
			}
			if x, ok := c.f.fold(v.Expression, cx.sc); ok && (x.k == vNull || x.k == vBool) {
				continue
			}
			out = append(out, c.mapExpr(v.Expression, cx)...)
		case *ast.JSXElementChild:
			out = append(out, c.element(v.Element, cx))
		case *ast.JSXFragmentChild:
			out = append(out, c.childNodes(v.Fragment.Children, cx)...)
		default:
			tbail()
		}
	}
	return out
}

func (c *threeCompiler) meshNode(el *ast.JSXElement, cx *tctx) *tnode {
	p := threeAttrs(el, func(name string) bool {
		return isTransform(name) || threeObjectProps[name] || threeEvents[name]
	})
	n := &tnode{mesh: true}
	n.props = c.constProps(p, threeObjectProps, cx)
	for name, at := range p {
		if threeEvents[name] {
			if cx.loop {
				tbail()
			}
			n.events = append(n.events, kv(name, attrValue(at)))
		}
	}
	sort.Slice(n.events, func(i, j int) bool { return n.events[i].Key < n.events[j].Key })
	c.transforms(n, p, cx)
	n.plain = cx.plain
	n.geo, n.mat = c.meshParts(el, cx)
	return n
}

// meshParts reads the one geometry and one material a mesh is made of.
func (c *threeCompiler) meshParts(el *ast.JSXElement, cx *tctx) (*tgeom, *tmat) {
	var geoEl, matEl *ast.JSXElement
	for _, ch := range el.Children {
		switch v := ch.(type) {
		case *ast.JSXText:
			if strings.TrimSpace(v.Value) != "" {
				tbail()
			}
		case *ast.JSXElementChild:
			name := v.Element.Opening.Name
			switch {
			case threeGeometries[name] != "" && geoEl == nil:
				geoEl = v.Element
			case threeMaterialTags[name] && matEl == nil:
				matEl = v.Element
			default:
				tbail()
			}
		default:
			tbail()
		}
	}
	if geoEl == nil || matEl == nil {
		tbail()
	}
	return c.geometry(geoEl, cx), c.material(matEl, cx)
}

func (c *threeCompiler) geometry(el *ast.JSXElement, cx *tctx) *tgeom {
	p := threeAttrs(el, func(name string) bool { return name == "args" })
	g := &tgeom{kind: threeGeometries[el.Opening.Name]}
	if at := p["args"]; at != nil {
		x, ok := c.f.fold(attrValue(at), cx.sc)
		if !ok || x.k != vArr {
			tbail()
		}
		for _, v := range x.a {
			if v.k != vNum {
				tbail()
			}
			g.args = append(g.args, v.n)
		}
	}
	parts := make([]string, len(g.args))
	for i, a := range g.args {
		parts[i] = formatNumber(a)
	}
	g.key = g.kind + ":" + strings.Join(parts, ",")
	return g
}

func (c *threeCompiler) material(el *ast.JSXElement, cx *tctx) *tmat {
	p := threeAttrs(el, func(name string) bool { return threeMaterialProps[name] })
	m := &tmat{tag: el.Opening.Name, props: c.constProps(p, threeMaterialProps, cx)}
	var b strings.Builder
	b.WriteString(m.tag)
	for _, pr := range m.props {
		b.WriteString("|" + pr.key + "=" + valueKey(pr.v))
	}
	m.key = b.String()
	return m
}

func valueKey(v value) string {
	switch v.k {
	case vNum:
		return formatNumber(v.n)
	case vStr:
		return fmt.Sprintf("%q", v.s)
	case vBool:
		return fmt.Sprint(v.b)
	}
	return "?"
}

// opaque reports a material that can share draw order with its copies: a
// transparent one is sorted per object, so it is never instanced.
func (m *tmat) opaque() bool {
	for _, p := range m.props {
		switch {
		case p.key == "transparent" && p.v.k == vBool && p.v.b:
			return false
		case p.key == "opacity" && p.v.k == vNum && p.v.n < 1:
			return false
		}
	}
	return true
}

// mapExpr compiles `{LIST.map((item, i) => <mesh/>)}` over a module-level
// constant array: the callback is unrolled at compile time, or, when it
// returns a mesh with reactive transforms, turned into one dynamic InstancedMesh.
func (c *threeCompiler) mapExpr(e ast.Expr, cx *tctx) []*tnode {
	call, ok := e.(*ast.CallExpr)
	if !ok || call.Optional || len(call.Args) != 1 {
		tbail()
	}
	m, ok := call.Callee.(*ast.MemberExpr)
	if !ok || m.Computed || m.Optional {
		tbail()
	}
	if id, ok := m.Property.(*ast.Identifier); !ok || id.Name != "map" {
		tbail()
	}
	fn, ok := call.Args[0].(*ast.ArrowFn)
	if !ok {
		tbail()
	}
	list, ok := c.f.fold(m.Object, cx.sc)
	if !ok || list.k != vArr || len(list.a) > maxLoopItems {
		tbail()
	}
	if len(fn.Params) > 2 || len(fn.Body) != 1 || fn.Async || fn.Generator {
		tbail()
	}
	ret, ok := fn.Body[0].(*ast.ReturnStmt)
	if !ok {
		tbail()
	}
	names := make([]string, len(fn.Params))
	for i, pr := range fn.Params {
		if pr.Name == "" || pr.Pattern != "" || pr.Target != nil || pr.IsRest || pr.Default != nil {
			tbail()
		}
		names[i] = pr.Name
	}
	scopeFor := func(i int) *scope {
		vars := map[string]value{}
		if len(names) > 0 {
			vars[names[0]] = list.a[i]
		}
		if len(names) > 1 {
			vars[names[1]] = num(float64(i))
		}
		return &scope{parent: cx.sc, vars: vars}
	}
	if mesh := meshReturn(ret.Value); mesh != nil && len(list.a) >= 2 && c.reactiveItem(mesh, scopeFor(0), cx) {
		return []*tnode{c.dynamic(mesh, fn, m.Object, list, scopeFor, cx)}
	}
	var out []*tnode
	for i := range list.a {
		item := &tctx{parent: cx.parent, chainStatic: cx.chainStatic, plain: cx.plain, sc: scopeFor(i), loop: true}
		out = append(out, c.returned(ret.Value, item)...)
	}
	return out
}

// returned analyses the expression a list callback returns.
func (c *threeCompiler) returned(e ast.Expr, cx *tctx) []*tnode {
	switch v := e.(type) {
	case *ast.ParenExpr:
		return c.returned(v.Expr, cx)
	case *ast.JSXElement:
		return []*tnode{c.element(v, cx)}
	case *ast.JSXFragment:
		return c.childNodes(v.Children, cx)
	}
	tbail()
	return nil
}

// meshReturn returns the <mesh> a callback returns directly, or nil.
func meshReturn(e ast.Expr) *ast.JSXElement {
	for {
		p, ok := e.(*ast.ParenExpr)
		if !ok {
			break
		}
		e = p.Expr
	}
	if el, ok := e.(*ast.JSXElement); ok && el.Opening != nil && el.Opening.Name == "mesh" {
		return el
	}
	return nil
}

// reactiveItem reports whether a transform prop of the mesh does not fold for
// an item of the list.
func (c *threeCompiler) reactiveItem(mesh *ast.JSXElement, sc *scope, cx *tctx) bool {
	for _, at := range mesh.Opening.Attributes {
		if at.Spread || !isTransform(at.Name) {
			continue
		}
		if _, ok := c.f.fold(attrValue(at), sc); !ok {
			return true
		}
	}
	return false
}

// dynamic builds the node of a `.map` of reactive meshes. Every item must
// resolve to the same geometry, material and constant props, and the ancestors
// must be static plain groups: the instances are composed against one matrix.
func (c *threeCompiler) dynamic(mesh *ast.JSXElement, fn *ast.ArrowFn, listExpr ast.Expr, list value, scopeFor func(int) *scope, cx *tctx) *tnode {
	if !cx.chainStatic || !cx.plain {
		tbail()
	}
	p := threeAttrs(mesh, func(name string) bool { return isTransform(name) || threeObjectProps[name] })
	if p["name"] != nil {
		tbail()
	}
	d := &tdyn{count: len(list.a), parent: cx.parent, list: listExpr, params: fn.Params}
	var geoKey, matKey, propKey string
	for i := range list.a {
		item := &tctx{parent: cx.parent, chainStatic: true, plain: true, sc: scopeFor(i), loop: true}
		props := c.constProps(p, threeObjectProps, item)
		g, m := c.meshParts(mesh, item)
		pk := fmt.Sprint(props)
		if i == 0 {
			geoKey, matKey, propKey = g.key, m.key, pk
			d.geo, d.mat, d.props = g, m, props
		} else if g.key != geoKey || m.key != matKey || pk != propKey {
			tbail()
		}
		// A prop is reactive for every item or for none.
		for _, name := range []string{"position", "rotation", "scale"} {
			if p[name] == nil {
				continue
			}
			_, ok := c.f.fold(attrValue(p[name]), item.sc)
			_, ok0 := c.f.fold(attrValue(p[name]), scopeFor(0))
			if ok != ok0 {
				tbail()
			}
		}
	}
	if !d.mat.opaque() {
		tbail()
	}
	for name, dst := range map[string]*tvec{"position": &d.pos, "rotation": &d.rot, "scale": &d.scl} {
		if at := p[name]; at != nil {
			*dst = tvec{present: true, expr: attrValue(at)}
		}
	}
	return &tnode{dyn: d}
}
