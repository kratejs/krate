package scene3d

import (
	"fmt"

	"github.com/kratejs/krate/packages/compiler/ast"
)

// vec3Prop reads a constant [x, y, z] prop. ok is false when the prop is
// absent or not constant (expr then holds the expression, if any).
func (a *analyzer) vec3Prop(p attrs, name string, cx *ctx) (v [3]float64, expr ast.Expr, ok bool) {
	at := p[name]
	if at == nil {
		return v, nil, false
	}
	e := attrValue(at)
	x, folded := a.f.fold(e, cx.sc)
	if !folded {
		a.dyn(cx, at.Position, name)
		return v, e, false
	}
	if x.k != vArr || len(x.a) != 3 || x.a[0].k != vNum || x.a[1].k != vNum || x.a[2].k != vNum {
		a.bail(at.Position, "%s is not a [x, y, z] array", name)
	}
	return [3]float64{x.a[0].n, x.a[1].n, x.a[2].n}, nil, true
}

func (a *analyzer) numProp(p attrs, name string, cx *ctx) (n float64, expr ast.Expr, present, ok bool) {
	at := p[name]
	if at == nil {
		return 0, nil, false, false
	}
	e := attrValue(at)
	x, folded := a.f.fold(e, cx.sc)
	if !folded {
		a.dyn(cx, at.Position, name)
		return 0, e, true, false
	}
	if x.k != vNum {
		a.bail(at.Position, "%s must be a number", name)
	}
	return x.n, nil, true, true
}

func (a *analyzer) lightName() string {
	n := fmt.Sprintf("__l%d", a.nLight)
	a.nLight++
	return n
}

func (a *analyzer) light(kind LightKind, el *ast.JSXElement, cx *ctx) {
	allowed := []string{"color", "intensity"}
	switch kind {
	case LightDirectional:
		allowed = append(allowed, "position", "target", "castShadow")
	case LightPoint:
		allowed = append(allowed, "position", "distance", "castShadow")
	case LightSpot:
		allowed = append(allowed, "position", "target", "distance", "angle", "penumbra", "castShadow")
	}
	p := a.props(el, allowed...)
	h := a.lightName()
	var props []*ast.ObjectProp
	var effects []ast.Stmt

	inten, intenExpr, intenPresent, intenStatic := a.numProp(p, "intensity", cx)
	switch {
	case !intenPresent:
		props = append(props, kv("intensity", numLit(LightIntensity(kind, 1))))
	case intenStatic:
		props = append(props, kv("intensity", numLit(LightIntensity(kind, inten))))
	default:
		effects = append(effects, effect(exprStmt(method(sceneName, "setLight", ident(h), strLit("intensity"),
			method(k3dName, "intensity", strLit(string(kind)), intenExpr)))))
	}

	if at := p["color"]; at != nil {
		e := attrValue(at)
		if x, ok := a.f.fold(e, cx.sc); ok {
			var rgb [3]float64
			switch x.k {
			case vStr:
				rgb = ColorToLinear(x.s)
			case vNum:
				rgb = NumberColorToLinear(x.n)
			default:
				a.bail(at.Position, "color must be a CSS color string or a 0xRRGGBB number")
			}
			props = append(props, kv("color", sigArr(rgb[0], rgb[1], rgb[2])))
		} else {
			a.dyn(cx, at.Position, "color")
			effects = append(effects, effect(exprStmt(method(sceneName, "setLight", ident(h), strLit("color"), method(k3dName, "color", e)))))
		}
	}

	if at := p["castShadow"]; at != nil && a.shadows {
		x, ok := a.f.fold(attrValue(at), cx.sc)
		switch {
		case ok && x.k == vBool:
			if x.b {
				props = append(props, kv("castShadows", boolLit(true)))
			}
		case ok:
			a.bail(at.Position, "castShadow must be a boolean")
		default:
			a.dyn(cx, at.Position, "castShadow")
			props = append(props, kv("castShadows", attrValue(at)))
		}
	}

	if kind != LightAmbient {
		props, effects = a.lightGeometry(kind, h, p, cx, props, effects)
	}

	a.lightStmts = append(a.lightStmts, constStmt(h, method(sceneName, "light", strLit(string(kind)), obj(props...))))
	a.lightStmts = append(a.lightStmts, effects...)
}

// lightGeometry adds position, direction, falloff and cone to a point,
// spot or directional light. With constant inputs they are computed here;
// otherwise __k3d.aimLight recomputes them in an effect.
func (a *analyzer) lightGeometry(kind LightKind, h string, p attrs, cx *ctx, props []*ast.ObjectProp, effects []ast.Stmt) ([]*ast.ObjectProp, []ast.Stmt) {
	pos, posExpr, posOK := a.vec3Prop(p, "position", cx)
	tgt, tgtExpr, tgtOK := a.vec3Prop(p, "target", cx)
	hasTgt := p["target"] != nil
	if p["position"] == nil {
		pos, posOK = [3]float64{}, true
	}
	if !(posOK && (tgtOK || !hasTgt)) {
		// Some input is reactive: aim in an effect, with static parts inlined.
		var posArg ast.Expr
		if posOK {
			posArg = numArr(pos[:]...)
		} else {
			posArg = posExpr
		}
		var tgtArg ast.Expr = &ast.Literal{Kind: ast.NullLit, Value: "null"}
		switch {
		case tgtExpr != nil:
			tgtArg = tgtExpr
		case tgtOK:
			tgtArg = numArr(tgt[:]...)
		}
		var parent ast.Expr = &ast.Literal{Kind: ast.NullLit, Value: "null"}
		if !cx.parent.isIdentity() {
			parent = matArr(cx.parent)
		}
		effects = append(effects, effect(exprStmt(method(k3dName, "aimLight", ident(sceneName), ident(h), strLit(string(kind)), posArg, tgtArg, parent))))
	} else {
		wp := cx.parent.transformPoint(pos)
		props = append(props, kv("position", numArr(wp[:]...)))
		switch kind {
		case LightDirectional:
			// The runtime path aims a directional light from its world position at the origin.
			aim := [3]float64{}
			if hasTgt {
				aim = cx.parent.transformPoint(tgt)
			}
			props = append(props, kv("direction", sigArr(directionTo(wp, aim)...)))
		case LightSpot:
			aim := cx.parent.transformPoint(tgt)
			props = append(props, kv("direction", sigArr(directionTo(wp, aim)...)))
		}
	}
	if kind == LightDirectional {
		return props, effects
	}
	if d, dExpr, present, ok := a.numProp(p, "distance", cx); present {
		switch {
		case ok:
			if d != 0 {
				props = append(props, kv("falloff", numLit(d)))
			}
		default:
			a.bail(p["distance"].Position, "a reactive %s distance (%s)", kind, exprNote(dExpr))
		}
	}
	if kind == LightSpot {
		angle, penumbra := 3.141592653589793/3, 0.0
		if n, e, present, ok := a.numProp(p, "angle", cx); present {
			if !ok {
				a.bail(p["angle"].Position, "a reactive spotLight angle (%s)", exprNote(e))
			}
			angle = n
		}
		if n, e, present, ok := a.numProp(p, "penumbra", cx); present {
			if !ok {
				a.bail(p["penumbra"].Position, "a reactive spotLight penumbra (%s)", exprNote(e))
			}
			penumbra = n
		}
		props = append(props, kv("outer", numLit(angle)), kv("inner", numLit(angle*(1-penumbra))))
	}
	return props, effects
}

func exprNote(ast.Expr) string { return "the cone and falloff are fixed at creation" }

func directionTo(from, to [3]float64) []float64 {
	d := normalize3([3]float64{to[0] - from[0], to[1] - from[1], to[2] - from[2]})
	return d[:]
}
