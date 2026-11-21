package scene3d

import (
	"fmt"
	"sort"
	"strings"

	"github.com/kratejs/krate/packages/compiler/ast"
)

// fallback is the reason a Canvas stays on the runtime path.
type fallback struct {
	pos    ast.Pos
	reason string
}

// hardError is a failure that must stop the build (a shader that does not compile).
type hardError struct{ err error }

// maxLoopItems bounds how many items a constant <For> is unrolled into.
const maxLoopItems = 4096

type ctx struct {
	parent mat4
	sc     *scope
	// loop is true inside a <For> body, which is unrolled at compile time and
	// so can only hold constant values.
	loop bool
}

type geomDef struct {
	name string
	kind string
	args []float64
}

type matDef struct {
	name       string
	kind       string
	params     []*ast.ObjectProp
	dyn        []dynParam
	custom     bool
	customInit []*ast.ObjectProp
}

type dynParam struct {
	key  string
	expr ast.Expr
	conv string // "", "color4", "color3"
}

type meshFlags struct {
	props  []*ast.ObjectProp
	key    string
	static bool
}

type event struct {
	name    string
	handler ast.Expr
}

type staticMesh struct {
	g      *geomDef
	m      *matDef
	matrix mat4
	flags  meshFlags
	events []event
}

type analyzer struct {
	opts   Options
	f      *folder
	syms   *symbols
	assets Assets

	pos     ast.Pos
	shadows bool

	geoms    map[string]*geomDef
	geomList []*geomDef
	mats     map[string]*matDef
	matList  []*matDef

	statics    []*staticMesh
	dynStmts   []ast.Stmt
	matEffects []ast.Stmt
	lightStmts []ast.Stmt
	extraStmts []ast.Stmt
	slots      int
	nEntity    int
	nLight     int
}

func (a *analyzer) bail(pos ast.Pos, format string, args ...any) {
	if pos.Line == 0 {
		pos = a.pos
	}
	panic(&fallback{pos: pos, reason: fmt.Sprintf(format, args...)})
}

// run analyses one <Canvas>. On success it returns the setup function and the
// attributes to keep on the replacement element.
func (a *analyzer) run(el *ast.JSXElement) (setup *ast.ArrowFn, keep []*ast.JSXAttr, fb *fallback, err error) {
	defer func() {
		switch r := recover().(type) {
		case nil:
		case *fallback:
			fb = r
		case *hardError:
			err = r.err
		default:
			panic(r)
		}
	}()
	a.pos = el.Position
	a.geoms = map[string]*geomDef{}
	a.mats = map[string]*matDef{}

	view, camera, sceneOpts, onCreated, keep := a.canvasProps(el)
	a.children(el.Children, &ctx{parent: identity})
	return a.assemble(view, camera, sceneOpts, onCreated), keep, nil, nil
}

// canvasProps splits the <Canvas> attributes into view options, camera
// options, engine options, the onCreated callback and attributes forwarded to
// the DOM canvas.
func (a *analyzer) canvasProps(el *ast.JSXElement) (view, camera, opts []*ast.ObjectProp, onCreated ast.Expr, keep []*ast.JSXAttr) {
	var camExpr, exposure ast.Expr
	for _, at := range el.Opening.Attributes {
		if at.Spread {
			a.bail(el.Position, "a spread prop on <Canvas>")
		}
		v := attrValue(at)
		switch at.Name {
		case "class", "style", "id", "role", "aria-label", "aria-hidden":
			keep = append(keep, at)
		case "key":
		case "shadows":
			if x, ok := a.f.fold(v, nil); ok && x.k == vBool {
				a.shadows = x.b
			} else {
				a.shadows = true
			}
			view = append(view, kv("shadows", v))
		case "bloom", "ssao", "antialias", "toneMapping":
			view = append(view, kv(at.Name, v))
		case "camera":
			camExpr = v
		case "exposure":
			exposure = v
		case "filament", "filamentUrl", "materialBase", "pixelRatio", "fetchBytes":
			opts = append(opts, kv(at.Name, v))
		case "onCreated":
			onCreated = v
		default:
			a.bail(at.Position, "the <Canvas> prop %q is not known to the compiler", at.Name)
		}
	}
	camera = []*ast.ObjectProp{
		kv("aperture", numLit(DefaultAperture)),
		kv("shutter", numLit(DefaultShutter)),
		kv("iso", numLit(DefaultISO)),
	}
	switch c := camExpr.(type) {
	case nil:
	case *ast.ObjectExpr:
		camera = append(camera, c.Properties...)
	default:
		camera = append(camera, &ast.ObjectProp{Spread: true, Value: c})
	}
	if exposure != nil {
		camera = append(camera, kv("exposure", exposure))
	}
	return view, camera, opts, onCreated, keep
}

func attrValue(at *ast.JSXAttr) ast.Expr {
	if at.Value == nil {
		return boolLit(true)
	}
	return at.Value
}

// attrs reads the props of a scene element. Unknown props (other than key and
// name) send the Canvas to the runtime path.
type attrs map[string]*ast.JSXAttr

func (a *analyzer) props(el *ast.JSXElement, allowed ...string) attrs {
	ok := map[string]bool{"key": true, "name": true}
	for _, n := range allowed {
		ok[n] = true
	}
	out := attrs{}
	for _, at := range el.Opening.Attributes {
		if at.Spread {
			a.bail(el.Position, "a spread prop on <%s>", el.Opening.Name)
		}
		if !ok[at.Name] {
			a.bail(at.Position, "the prop %q on <%s> is not supported by the compiler", at.Name, el.Opening.Name)
		}
		out[at.Name] = at
	}
	return out
}

func (a *analyzer) children(children []ast.JSXChild, cx *ctx) {
	for _, c := range children {
		switch v := c.(type) {
		case *ast.JSXText:
			if strings.TrimSpace(v.Value) != "" {
				a.bail(a.pos, "text inside a <Canvas>")
			}
		case *ast.JSXExprContainer:
			if v.Expression == nil {
				continue
			}
			if x, ok := a.f.fold(v.Expression, cx.sc); ok && (x.k == vNull || x.k == vBool) {
				continue
			}
			if a.mapChild(v.Expression, cx) {
				continue
			}
			a.bail(v.Expression.Pos(), "an expression child inside a <Canvas> (conditionals and lists over signals are not statically known; map over a module-level constant array instead)")
		case *ast.JSXElementChild:
			a.element(v.Element, cx)
		case *ast.JSXFragmentChild:
			a.children(v.Fragment.Children, cx)
		}
	}
}

func (a *analyzer) element(el *ast.JSXElement, cx *ctx) {
	name := el.Opening.Name
	a.pos = el.Position
	if a.syms.forTag[name] {
		a.forEach(el, cx)
		return
	}
	if remote, ok := a.syms.other[name]; ok {
		a.control(remote, name, el, cx)
		return
	}
	switch name {
	case "group":
		a.group(el, cx)
	case "mesh":
		a.mesh(el, cx)
	case "directionalLight":
		a.light(LightDirectional, el, cx)
	case "pointLight":
		a.light(LightPoint, el, cx)
	case "spotLight":
		a.light(LightSpot, el, cx)
	case "ambientLight":
		a.light(LightAmbient, el, cx)
	default:
		if name != "" && name[0] >= 'A' && name[0] <= 'Z' || strings.Contains(name, ".") {
			a.bail(el.Position, "the component <%s> may return scene nodes the compiler cannot see through", name)
		}
		a.bail(el.Position, "the element <%s> is not a known scene element", name)
	}
}

// node analyses an expression that returns scene JSX.
func (a *analyzer) node(e ast.Expr, cx *ctx) {
	switch v := e.(type) {
	case *ast.ParenExpr:
		a.node(v.Expr, cx)
	case *ast.JSXElement:
		a.element(v, cx)
	case *ast.JSXFragment:
		a.children(v.Children, cx)
	default:
		a.bail(e.Pos(), "a list callback that does not return a scene element directly")
	}
}

func (a *analyzer) forEach(el *ast.JSXElement, cx *ctx) {
	p := a.props(el, "each")
	at := p["each"]
	if at == nil {
		a.bail(el.Position, "<For> without an `each` prop")
	}
	var fn *ast.ArrowFn
	for _, c := range el.Children {
		switch v := c.(type) {
		case *ast.JSXText:
			if strings.TrimSpace(v.Value) != "" {
				a.bail(el.Position, "text inside <For>")
			}
		case *ast.JSXExprContainer:
			f, ok := v.Expression.(*ast.ArrowFn)
			if !ok || fn != nil {
				a.bail(el.Position, "<For> needs one callback child")
			}
			fn = f
		default:
			a.bail(el.Position, "<For> needs one callback child")
		}
	}
	a.unroll(attrValue(at), fn, at.Position, "<For>", cx)
}

// mapChild compiles `{LIST.map((item, i) => <mesh />)}` over a constant array.
// It reports false for any other expression.
func (a *analyzer) mapChild(e ast.Expr, cx *ctx) bool {
	call, ok := e.(*ast.CallExpr)
	if !ok || call.Optional || len(call.Args) != 1 {
		return false
	}
	m, ok := call.Callee.(*ast.MemberExpr)
	if !ok || m.Computed || m.Optional {
		return false
	}
	if id, ok := m.Property.(*ast.Identifier); !ok || id.Name != "map" {
		return false
	}
	fn, ok := call.Args[0].(*ast.ArrowFn)
	if !ok {
		return false
	}
	a.unroll(m.Object, fn, e.Pos(), ".map()", cx)
	return true
}

// unroll expands a callback over a constant array at compile time.
func (a *analyzer) unroll(listExpr ast.Expr, fn *ast.ArrowFn, pos ast.Pos, what string, cx *ctx) {
	list, ok := a.f.fold(listExpr, cx.sc)
	if !ok || list.k != vArr {
		a.bail(pos, "%s over a list that is not a constant array (a signal or computed value is not statically known)", what)
	}
	if len(list.a) > maxLoopItems {
		a.bail(pos, "%s over %d items (the limit is %d)", what, len(list.a), maxLoopItems)
	}
	if fn == nil || len(fn.Params) > 2 || len(fn.Body) != 1 || fn.Async || fn.Generator {
		a.bail(pos, "%s needs a callback that returns one scene element", what)
	}
	ret, ok := fn.Body[0].(*ast.ReturnStmt)
	if !ok {
		a.bail(pos, "%s needs a callback that returns one scene element", what)
	}
	names := make([]string, len(fn.Params))
	for i, pr := range fn.Params {
		if pr.Name == "" || pr.Pattern != "" || pr.Target != nil || pr.IsRest || pr.Default != nil {
			a.bail(pos, "%s callback parameters must be plain names", what)
		}
		names[i] = pr.Name
	}
	for i, item := range list.a {
		vars := map[string]value{}
		if len(names) > 0 {
			vars[names[0]] = item
		}
		if len(names) > 1 {
			vars[names[1]] = num(float64(i))
		}
		a.node(ret.Value, &ctx{parent: cx.parent, sc: &scope{parent: cx.sc, vars: vars}, loop: true})
	}
}

// dyn guards non-constant values: inside a <For> body they would have to be
// duplicated per item, so the scene stays on the runtime path.
func (a *analyzer) dyn(cx *ctx, pos ast.Pos, what string) {
	if cx.loop {
		a.bail(pos, "a non-constant %s inside a list callback", what)
	}
}

// tprop is one transform prop of a group or mesh.
type tprop struct {
	present bool
	static  bool
	v       [3]float64
	expr    ast.Expr
	attr    *ast.JSXAttr
}

func (a *analyzer) transformProp(p attrs, name string, cx *ctx) tprop {
	at := p[name]
	if at == nil {
		return tprop{}
	}
	e := attrValue(at)
	x, ok := a.f.fold(e, cx.sc)
	if !ok {
		a.dyn(cx, at.Position, name)
		return tprop{present: true, expr: e, attr: at}
	}
	if x.k == vNum && name == "scale" {
		return tprop{present: true, static: true, v: [3]float64{x.n, x.n, x.n}, attr: at}
	}
	if x.k == vArr && len(x.a) == 3 && x.a[0].k == vNum && x.a[1].k == vNum && x.a[2].k == vNum {
		return tprop{present: true, static: true, v: [3]float64{x.a[0].n, x.a[1].n, x.a[2].n}, attr: at}
	}
	a.bail(at.Position, "%s is not a [x, y, z] array", name)
	return tprop{}
}

func (t tprop) or(def [3]float64) [3]float64 {
	if t.present && t.static {
		return t.v
	}
	return def
}

func localMatrix(pos, rot, scl tprop) mat4 {
	r := rot.or([3]float64{})
	return composeTRS(pos.or([3]float64{}), eulerQuat(r[0], r[1], r[2]), scl.or([3]float64{1, 1, 1}))
}

func (a *analyzer) group(el *ast.JSXElement, cx *ctx) {
	p := a.props(el, "position", "rotation", "scale")
	pos := a.transformProp(p, "position", cx)
	rot := a.transformProp(p, "rotation", cx)
	scl := a.transformProp(p, "scale", cx)
	for _, t := range []tprop{pos, rot, scl} {
		if t.present && !t.static {
			a.bail(t.attr.Position, "a reactive <group> transform (the runtime composes meshes against a static parent matrix only)")
		}
	}
	a.children(el.Children, &ctx{parent: mul(cx.parent, localMatrix(pos, rot, scl)), sc: cx.sc, loop: cx.loop})
}

var eventNames = map[string]string{
	"onClick":       "click",
	"onPointerOver": "pointerover",
	"onPointerOut":  "pointerout",
	"onPointerMove": "pointermove",
	"onPointerDown": "pointerdown",
	"onPointerUp":   "pointerup",
}

var eventOrder = []string{"onClick", "onPointerOver", "onPointerOut", "onPointerMove", "onPointerDown", "onPointerUp"}

var geometryKinds = map[string]string{"boxGeometry": "box", "sphereGeometry": "sphere", "planeGeometry": "plane"}

var materialTags = map[string]string{
	"meshStandardMaterial": "standard",
	"meshPhysicalMaterial": "physical",
	"meshBasicMaterial":    "unlit",
	"shaderMaterial":       "custom",
}

func (a *analyzer) mesh(el *ast.JSXElement, cx *ctx) {
	allowed := []string{"position", "rotation", "scale", "castShadow", "receiveShadow"}
	allowed = append(allowed, eventOrder...)
	p := a.props(el, allowed...)

	var geoEl, matEl *ast.JSXElement
	for _, c := range el.Children {
		switch v := c.(type) {
		case *ast.JSXText:
			if strings.TrimSpace(v.Value) != "" {
				a.bail(el.Position, "text inside <mesh>")
			}
		case *ast.JSXElementChild:
			n := v.Element.Opening.Name
			switch {
			case geometryKinds[n] != "":
				if geoEl != nil {
					a.bail(v.Element.Position, "<mesh> with two geometries")
				}
				geoEl = v.Element
			case materialTags[n] != "":
				if matEl != nil {
					a.bail(v.Element.Position, "<mesh> with two materials")
				}
				matEl = v.Element
			default:
				a.bail(v.Element.Position, "<%s> inside <mesh> (only a geometry and a material are supported)", n)
			}
		case *ast.JSXExprContainer:
			if v.Expression != nil {
				a.bail(v.Expression.Pos(), "an expression child inside <mesh>")
			}
		default:
			a.bail(el.Position, "an unsupported child inside <mesh>")
		}
	}
	if geoEl == nil || matEl == nil {
		a.bail(el.Position, "<mesh> without both a geometry and a material")
	}
	g := a.geometry(geoEl, cx)
	m := a.material(matEl, cx)
	pos := a.transformProp(p, "position", cx)
	rot := a.transformProp(p, "rotation", cx)
	scl := a.transformProp(p, "scale", cx)
	flags := a.flags(p, cx)
	var events []event
	for _, name := range eventOrder {
		if at := p[name]; at != nil {
			a.dyn(cx, at.Position, "event handler")
			events = append(events, event{name: eventNames[name], handler: attrValue(at)})
		}
	}

	if (!pos.present || pos.static) && (!rot.present || rot.static) && (!scl.present || scl.static) {
		a.statics = append(a.statics, &staticMesh{
			g: g, m: m, matrix: mul(cx.parent, localMatrix(pos, rot, scl)), flags: flags, events: events,
		})
		return
	}
	a.dynamicMesh(g, m, cx, pos, rot, scl, flags, events)
}

func (a *analyzer) flags(p attrs, cx *ctx) meshFlags {
	f := meshFlags{static: true}
	if !a.shadows {
		return f
	}
	for _, n := range []string{"castShadow", "receiveShadow"} {
		at := p[n]
		if at == nil {
			continue
		}
		e := attrValue(at)
		x, ok := a.f.fold(e, cx.sc)
		switch {
		case ok && x.k == vBool:
			if x.b {
				f.props = append(f.props, kv(n, boolLit(true)))
				f.key += n[:1]
			}
		case ok:
			a.bail(at.Position, "%s must be a boolean", n)
		default:
			a.dyn(cx, at.Position, n)
			f.props = append(f.props, kv(n, e))
			f.static = false
		}
	}
	return f
}

func (a *analyzer) entityName() string {
	n := fmt.Sprintf("__e%d", a.nEntity)
	a.nEntity++
	return n
}

func (a *analyzer) dynamicMesh(g *geomDef, m *matDef, cx *ctx, pos, rot, scl tprop, flags meshFlags, events []event) {
	slot := a.slots
	a.slots++
	var initial []*ast.ObjectProp
	if pos.present && pos.static {
		initial = append(initial, kv("pos", numArr(pos.v[:]...)))
	}
	if rot.present && rot.static {
		q := eulerQuat(rot.v[0], rot.v[1], rot.v[2])
		initial = append(initial, kv("quat", sigArr(q[:]...)))
	}
	if scl.present && scl.static {
		initial = append(initial, kv("scale", numArr(scl.v[:]...)))
	}
	args := []ast.Expr{numLit(float64(slot)), ident(g.name), ident(m.name), matArr(cx.parent), obj(initial...)}
	if len(flags.props) > 0 {
		args = append(args, obj(flags.props...))
	}
	h := a.entityName()
	a.dynStmts = append(a.dynStmts, constStmt(h, method(sceneName, "dynamicMesh", args...)))
	for _, t := range []struct {
		name string
		p    tprop
	}{{"pos", pos}, {"rot", rot}, {"scale", scl}} {
		if !t.p.present || t.p.static {
			continue
		}
		a.dynStmts = append(a.dynStmts, effect(exprStmt(method(sceneName, t.name, append([]ast.Expr{numLit(float64(slot))}, vecArgs(t.p.expr)...)...))))
	}
	a.addEvents(h, events, &a.dynStmts)
}

// vecArgs spreads a literal [x, y, z] into three arguments; any other
// expression is passed whole (the runtime accepts an array or a number).
func vecArgs(e ast.Expr) []ast.Expr {
	if arr, ok := e.(*ast.ArrayExpr); ok && len(arr.Elements) == 3 {
		plain := true
		for _, el := range arr.Elements {
			if el == nil {
				plain = false
			} else if u, ok := el.(*ast.UnaryExpr); ok && u.Op == "..." {
				plain = false
			}
		}
		if plain {
			return arr.Elements
		}
	}
	return []ast.Expr{e}
}

func (a *analyzer) addEvents(handle string, events []event, into *[]ast.Stmt) {
	for _, ev := range events {
		*into = append(*into, exprStmt(method(sceneName, "on", ident(handle), strLit(ev.name), ev.handler)))
	}
}

func (a *analyzer) geometry(el *ast.JSXElement, cx *ctx) *geomDef {
	a.pos = el.Position
	p := a.props(el, "args")
	var args []float64
	if at := p["args"]; at != nil {
		x, ok := a.f.fold(attrValue(at), cx.sc)
		if !ok {
			a.bail(at.Position, "reactive geometry args (a geometry is built once; its args must be constants)")
		}
		if x.k != vArr {
			a.bail(at.Position, "geometry args is not an array")
		}
		for _, v := range x.a {
			if v.k != vNum {
				a.bail(at.Position, "geometry args holds a non-number")
			}
			args = append(args, v.n)
		}
	}
	kind := geometryKinds[el.Opening.Name]
	key := kind + ":" + fmt.Sprint(args)
	if g, ok := a.geoms[key]; ok {
		return g
	}
	g := &geomDef{name: fmt.Sprintf("__g%d", len(a.geomList)), kind: kind, args: args}
	a.geoms[key] = g
	a.geomList = append(a.geomList, g)
	return g
}

func (a *analyzer) addMaterial(m *matDef) *matDef {
	key := ""
	if len(m.dyn) == 0 {
		var b strings.Builder
		b.WriteString(m.kind)
		keys := make([]string, 0, len(m.params))
		byKey := map[string]ast.Expr{}
		for _, p := range m.params {
			keys = append(keys, p.Key)
			byKey[p.Key] = p.Value
		}
		sort.Strings(keys)
		for _, k := range keys {
			b.WriteString("|" + k + "=" + printConst(byKey[k]))
		}
		key = b.String()
		if prev, ok := a.mats[key]; ok {
			return prev
		}
	}
	m.name = fmt.Sprintf("__m%d", len(a.matList))
	if key != "" {
		a.mats[key] = m
	}
	a.matList = append(a.matList, m)
	return m
}

// printConst renders a constant AST value (numbers, strings, booleans and
// arrays of them) as a dedupe key.
func printConst(e ast.Expr) string {
	switch v := e.(type) {
	case *ast.Literal:
		return v.Value
	case *ast.UnaryExpr:
		return v.Op + printConst(v.Arg)
	case *ast.ArrayExpr:
		parts := make([]string, len(v.Elements))
		for i, el := range v.Elements {
			parts[i] = printConst(el)
		}
		return "[" + strings.Join(parts, ",") + "]"
	case *ast.ObjectExpr:
		parts := make([]string, len(v.Properties))
		for i, p := range v.Properties {
			parts[i] = p.Key + ":" + printConst(p.Value)
		}
		return "{" + strings.Join(parts, ",") + "}"
	}
	return fmt.Sprintf("%p", e)
}

// valueExpr turns a folded constant back into an expression.
func valueExpr(v value) ast.Expr {
	switch v.k {
	case vNum:
		return numLit(v.n)
	case vStr:
		return strLit(v.s)
	case vBool:
		return boolLit(v.b)
	case vArr:
		out := make([]ast.Expr, len(v.a))
		for i, x := range v.a {
			out[i] = valueExpr(x)
		}
		return arrOf(out...)
	case vObj:
		keys := make([]string, 0, len(v.o))
		for k := range v.o {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		props := make([]*ast.ObjectProp, len(keys))
		for i, k := range keys {
			props[i] = kv(k, valueExpr(v.o[k]))
		}
		return obj(props...)
	}
	return &ast.Literal{Kind: ast.NullLit, Value: "null"}
}
