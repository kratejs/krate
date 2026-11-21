package scene3d

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/kratejs/krate/packages/compiler/ast"
	"github.com/kratejs/krate/packages/compiler/internal/diag"
)

type propKind int

const (
	pFloat propKind = iota
	pColor4
	pColor3
	pURL
)

type matProp struct {
	param string
	kind  propKind
}

// Three.js-style material props and the uber-material parameters they set.
// The names match MATERIAL_PROPS in packages/3d/src/runtime/tree.ts.
var (
	unlitProps = map[string]matProp{
		"color":   {"baseColor", pColor4},
		"map":     {"baseColorMap", pURL},
		"opacity": {"opacity", pFloat},
	}
	standardProps = map[string]matProp{
		"roughness":         {"roughness", pFloat},
		"metallic":          {"metallic", pFloat},
		"metalness":         {"metallic", pFloat},
		"emissive":          {"emissive", pColor3},
		"emissiveIntensity": {"emissiveIntensity", pFloat},
		"normalMap":         {"normalMap", pURL},
		"aoMap":             {"aoMap", pURL},
		"roughnessMap":      {"roughnessMap", pURL},
		"metalnessMap":      {"metallicMap", pURL},
		"emissiveMap":       {"emissiveMap", pURL},
		"normalScale":       {"normalScale", pFloat},
		"aoMapIntensity":    {"aoMapIntensity", pFloat},
	}
	physicalProps = map[string]matProp{
		"clearcoat":          {"clearcoat", pFloat},
		"clearcoatRoughness": {"clearcoatRoughness", pFloat},
		"sheenColor":         {"sheenColor", pColor3},
		"sheenRoughness":     {"sheenRoughness", pFloat},
		"transmission":       {"transmission", pFloat},
		"ior":                {"ior", pFloat},
	}
)

func propsFor(kind string) map[string]matProp {
	out := map[string]matProp{}
	for k, v := range unlitProps {
		out[k] = v
	}
	if kind != "unlit" {
		for k, v := range standardProps {
			out[k] = v
		}
	}
	if kind == "physical" {
		for k, v := range physicalProps {
			out[k] = v
		}
	}
	return out
}

func (a *analyzer) material(el *ast.JSXElement, cx *ctx) *matDef {
	a.pos = el.Position
	kind := materialTags[el.Opening.Name]
	if kind == "custom" {
		return a.addMaterial(a.shaderMaterial(el, cx))
	}
	known := propsFor(kind)
	m := &matDef{kind: kind}
	transparent := false
	reactiveOpacity := false
	for _, at := range el.Opening.Attributes {
		if at.Spread {
			a.bail(el.Position, "a spread prop on <%s>", el.Opening.Name)
		}
		e := attrValue(at)
		if at.Name == "transparent" {
			x, ok := a.f.fold(e, cx.sc)
			switch {
			case ok && x.k == vBool:
				transparent = x.b
			case ok:
				a.bail(at.Position, "transparent must be a boolean")
			default:
				a.dyn(cx, at.Position, "transparent")
				m.params = append(m.params, kv("transparent", e))
			}
			continue
		}
		mp, ok := known[at.Name]
		if !ok {
			continue // dropped, like the runtime path drops unknown props
		}
		x, folded := a.f.fold(e, cx.sc)
		if !folded {
			a.dyn(cx, at.Position, at.Name)
			conv := ""
			switch mp.kind {
			case pColor4:
				conv = "color4"
			case pColor3:
				conv = "color3"
			}
			m.dyn = append(m.dyn, dynParam{key: mp.param, expr: e, conv: conv})
			if mp.param == "opacity" {
				reactiveOpacity = true
			}
			continue
		}
		m.params = append(m.params, kv(mp.param, a.staticParam(at, mp, x)))
		if mp.param == "opacity" && x.k == vNum && x.n < 1 {
			transparent = true
		}
	}
	if transparent || reactiveOpacity {
		m.params = append(m.params, kv("transparent", boolLit(true)))
	}
	return a.addMaterial(m)
}

func (a *analyzer) staticParam(at *ast.JSXAttr, mp matProp, x value) ast.Expr {
	switch mp.kind {
	case pFloat:
		if x.k != vNum {
			a.bail(at.Position, "%s must be a number", at.Name)
		}
		return numLit(x.n)
	case pURL:
		if x.k != vStr {
			a.bail(at.Position, "%s must be a texture URL string", at.Name)
		}
		return strLit(x.s)
	}
	var rgb [3]float64
	switch x.k {
	case vStr:
		rgb = ColorToLinear(x.s)
	case vNum:
		rgb = NumberColorToLinear(x.n)
	default:
		a.bail(at.Position, "%s must be a CSS color string or a 0xRRGGBB number", at.Name)
	}
	if mp.kind == pColor4 {
		return sigArr(rgb[0], rgb[1], rgb[2], 1)
	}
	return sigArr(rgb[0], rgb[1], rgb[2])
}

// materialCall builds `__scene.material(kind, params)`.
func (m *matDef) call() ast.Expr {
	if m.custom {
		return method(sceneName, "material", strLit("custom"), obj(m.params...))
	}
	return method(sceneName, "material", strLit(m.kind), obj(m.params...))
}

// effects returns the statements that keep reactive parameters in sync.
func (m *matDef) effects() []ast.Stmt {
	var out []ast.Stmt
	for _, d := range m.dyn {
		var v ast.Expr = d.expr
		switch d.conv {
		case "color3":
			v = method(k3dName, "color", d.expr)
		case "color4":
			v = call(member(method(k3dName, "color", d.expr), "concat"), numberText("1", false))
		}
		out = append(out, effect(exprStmt(method(sceneName, "setMaterialParam", ident(m.name), strLit(d.key), v))))
	}
	return out
}

// Custom shaders (Tier 2)
//
// <shaderMaterial> props:
//
//	fragmentShader  string, required: the statements of Filament's
//	                `void material(inout MaterialInputs material)` after
//	                prepareMaterial(material). Write material.baseColor (and
//	                for lit materials roughness, metallic, ...).
//	vertexShader    string, optional: the statements of
//	                `void materialVertex(inout MaterialVertexInputs material)`.
//	uniforms        object, optional: name to constant (number: float,
//	                [x, y, z]: float3, [x, y, z, w]: float4) or
//	                { type: "float" | "float3" | "float4", value: expr } where
//	                value may be reactive. Read them as materialParams.<name>.
//	shadingModel    "unlit" (default) or "lit".
//	transparent     boolean; alpha blending, color premultiplied by alpha for
//	                unlit materials.
//
// The strings must be constants (a literal, a template without expressions, a
// module-level const). The generated .mat is hashed and compiled by matc.

var uniformName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

type uniformDef struct {
	name string
	typ  string
}

func (a *analyzer) shaderMaterial(el *ast.JSXElement, cx *ctx) *matDef {
	p := a.props(el, "vertexShader", "fragmentShader", "uniforms", "shadingModel", "transparent")
	a.dyn(cx, el.Position, "shaderMaterial")
	text := func(name string, required bool) string {
		at := p[name]
		if at == nil {
			if required {
				a.bail(el.Position, "<shaderMaterial> without %s", name)
			}
			return ""
		}
		x, ok := a.f.fold(attrValue(at), cx.sc)
		if !ok || x.k != vStr {
			a.bail(at.Position, "%s is not a static string (inline it or use a module-level const)", name)
		}
		return x.s
	}
	frag := text("fragmentShader", true)
	vert := text("vertexShader", false)
	lit := false
	if s := text("shadingModel", false); s != "" {
		switch s {
		case "lit":
			lit = true
		case "unlit":
		default:
			a.bail(p["shadingModel"].Position, `shadingModel must be "lit" or "unlit"`)
		}
	}
	transparent := false
	if at := p["transparent"]; at != nil {
		x, ok := a.f.fold(attrValue(at), cx.sc)
		if !ok || x.k != vBool {
			a.bail(at.Position, "transparent must be a constant boolean on <shaderMaterial>")
		}
		transparent = x.b
	}

	m := &matDef{kind: "custom", custom: true}
	var defs []uniformDef
	if at := p["uniforms"]; at != nil {
		o, ok := attrValue(at).(*ast.ObjectExpr)
		if !ok {
			a.bail(at.Position, "uniforms must be an object literal")
		}
		for _, prop := range o.Properties {
			if prop.Spread || prop.Computed || prop.Method || prop.Getter || prop.Setter {
				a.bail(at.Position, "uniforms must be a plain object literal")
			}
			name := prop.Key
			if !uniformName.MatchString(name) || name == "true" || name == "false" {
				a.bail(at.Position, "%q is not a valid uniform name", name)
			}
			val := prop.Value
			if prop.Shorthand {
				val = ident(name)
			}
			def, init, expr := a.uniform(at, name, val, cx)
			defs = append(defs, def)
			switch {
			case init != nil:
				m.paramValue(name, init)
			case expr != nil:
				m.dyn = append(m.dyn, dynParam{key: name, expr: expr})
			}
		}
	}
	src := buildMaterialSource(frag, vert, lit, transparent, defs)
	if a.opts.Shader == nil {
		panic(&hardError{err: a.shaderDiag(el, "the shaderMaterial cannot be compiled: no material compiler is configured",
			"build with `krate build` so matc can be located, or set KRATE_FILAMENT_MATC to a matc binary")})
	}
	url, err := a.opts.Shader(src)
	if err != nil {
		panic(&hardError{err: a.wrapShaderErr(el, err)})
	}
	m.params = append([]*ast.ObjectProp{kv("url", strLit(url))}, m.paramList()...)
	if transparent {
		m.params = append(m.params, kv("transparent", boolLit(true)))
	}
	return m
}

// paramValue records the initial value of a custom uniform.
func (m *matDef) paramValue(name string, v ast.Expr) {
	m.customInit = append(m.customInit, kv(name, v))
}

func (m *matDef) paramList() []*ast.ObjectProp {
	if len(m.customInit) == 0 {
		return nil
	}
	return []*ast.ObjectProp{kv("params", obj(m.customInit...))}
}

// uniform reads one uniforms entry: a constant, or {type, value}.
func (a *analyzer) uniform(at *ast.JSXAttr, name string, val ast.Expr, cx *ctx) (uniformDef, ast.Expr, ast.Expr) {
	if x, ok := a.f.fold(val, cx.sc); ok {
		return uniformDef{name: name, typ: a.uniformType(at, name, x)}, valueExpr(x), nil
	}
	o, ok := val.(*ast.ObjectExpr)
	if !ok {
		a.bail(at.Position, "uniform %q is not a constant or a {type, value} object", name)
	}
	var typ string
	var expr ast.Expr
	for _, p := range o.Properties {
		switch p.Key {
		case "type":
			x, ok := a.f.fold(p.Value, cx.sc)
			if !ok || x.k != vStr || (x.s != "float" && x.s != "float3" && x.s != "float4") {
				a.bail(at.Position, `uniform %q type must be "float", "float3" or "float4"`, name)
			}
			typ = x.s
		case "value":
			expr = p.Value
		default:
			a.bail(at.Position, "uniform %q has an unknown field %q", name, p.Key)
		}
	}
	if typ == "" || expr == nil {
		a.bail(at.Position, "uniform %q needs a type and a value", name)
	}
	return uniformDef{name: name, typ: typ}, nil, expr
}

func (a *analyzer) uniformType(at *ast.JSXAttr, name string, x value) string {
	switch {
	case x.k == vNum:
		return "float"
	case x.k == vArr && (len(x.a) == 3 || len(x.a) == 4):
		for _, v := range x.a {
			if v.k != vNum {
				a.bail(at.Position, "uniform %q holds a non-number", name)
			}
		}
		return fmt.Sprintf("float%d", len(x.a))
	}
	a.bail(at.Position, "uniform %q must be a number, [x, y, z] or [x, y, z, w]", name)
	return ""
}

// buildMaterialSource writes the Filament .mat text for a shaderMaterial.
func buildMaterialSource(frag, vert string, lit, transparent bool, uniforms []uniformDef) []byte {
	var b strings.Builder
	b.WriteString("material {\n    name : krate_custom,\n")
	if lit {
		b.WriteString("    shadingModel : lit,\n")
	} else {
		b.WriteString("    shadingModel : unlit,\n")
	}
	if transparent {
		b.WriteString("    blending : transparent,\n")
	} else {
		b.WriteString("    blending : opaque,\n")
	}
	b.WriteString("    requires : [ uv0 ]")
	if len(uniforms) > 0 {
		b.WriteString(",\n    parameters : [\n")
		for i, u := range uniforms {
			fmt.Fprintf(&b, "        { type : %s, name : \"%s\" }", u.typ, u.name)
			if i < len(uniforms)-1 {
				b.WriteString(",")
			}
			b.WriteString("\n")
		}
		b.WriteString("    ]")
	}
	b.WriteString("\n}\n")
	if strings.TrimSpace(vert) != "" {
		b.WriteString("\nvertex {\n    void materialVertex(inout MaterialVertexInputs material) {\n")
		b.WriteString(vert)
		b.WriteString("\n    }\n}\n")
	}
	b.WriteString("\nfragment {\n    void material(inout MaterialInputs material) {\n        prepareMaterial(material);\n")
	b.WriteString(frag)
	b.WriteString("\n")
	if transparent && !lit {
		b.WriteString("        material.baseColor.rgb *= material.baseColor.a;\n")
	}
	b.WriteString("    }\n}\n")
	return []byte(b.String())
}

func (a *analyzer) shaderDiag(el *ast.JSXElement, msg, hint string) diag.Diagnostic {
	return diag.New(a.opts.File, el.Position.Line, el.Position.Col, msg, hint, sourceLine(a.opts.Source, el.Position.Line))
}

// wrapShaderErr positions a matc or download failure at the shaderMaterial.
func (a *analyzer) wrapShaderErr(el *ast.JSXElement, err error) error {
	if d, ok := diag.FromError(err); ok {
		if d.File == "" {
			d.File = a.opts.File
			d.Line = el.Position.Line
			d.Col = el.Position.Col
			d.Source = sourceLine(a.opts.Source, el.Position.Line)
		}
		return d
	}
	return a.shaderDiag(el, "the shaderMaterial could not be compiled: "+err.Error(),
		"set KRATE_FILAMENT_MATC to a matc binary, or unset KRATE_OFFLINE so krate can download it")
}
