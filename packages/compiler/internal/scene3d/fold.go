package scene3d

import (
	"math"
	"strconv"
	"strings"

	"github.com/kratejs/krate/packages/compiler/ast"
)

type vkind int

const (
	vNum vkind = iota + 1
	vStr
	vBool
	vNull
	vArr
	vObj
)

// value is a compile-time constant.
type value struct {
	k vkind
	n float64
	s string
	b bool
	a []value
	o map[string]value
}

func num(n float64) value { return value{k: vNum, n: n} }

// scope holds the constants bound by enclosing <For> callbacks.
type scope struct {
	parent *scope
	vars   map[string]value
}

func (s *scope) lookup(name string) (value, bool) {
	for ; s != nil; s = s.parent {
		if v, ok := s.vars[name]; ok {
			return v, true
		}
	}
	return value{}, false
}

// folder folds expressions to constants: literals, arithmetic, Math calls,
// module-level `const` declarations and the loop variables of a scope.
type folder struct {
	consts map[string]ast.Expr
	// shadow lists names rebound inside the code being analysed; a module
	// const of the same name is not the binding the code sees.
	shadow map[string]bool
	memo   map[string]*value
	depth  int
}

func newFolder(consts map[string]ast.Expr) *folder {
	return &folder{consts: consts, memo: map[string]*value{}}
}

const maxFoldDepth = 64

func (f *folder) fold(e ast.Expr, sc *scope) (value, bool) {
	f.depth++
	defer func() { f.depth-- }()
	if f.depth > maxFoldDepth {
		return value{}, false
	}
	switch v := e.(type) {
	case nil:
		return value{}, false
	case *ast.Literal:
		return foldLiteral(v)
	case *ast.ParenExpr:
		return f.fold(v.Expr, sc)
	case *ast.TypeAssertion:
		return f.fold(v.Expr, sc)
	case *ast.Identifier:
		return f.ident(v.Name, sc)
	case *ast.UnaryExpr:
		return f.unary(v, sc)
	case *ast.BinaryExpr:
		return f.binary(v, sc)
	case *ast.ConditionalExpr:
		t, ok := f.fold(v.Test, sc)
		if !ok {
			return value{}, false
		}
		if truthy(t) {
			return f.fold(v.Consequent, sc)
		}
		return f.fold(v.Alternate, sc)
	case *ast.ArrayExpr:
		out := value{k: vArr}
		for _, el := range v.Elements {
			if el == nil {
				return value{}, false
			}
			if u, ok := el.(*ast.UnaryExpr); ok && u.Op == "..." {
				return value{}, false
			}
			x, ok := f.fold(el, sc)
			if !ok {
				return value{}, false
			}
			out.a = append(out.a, x)
		}
		return out, true
	case *ast.ObjectExpr:
		out := value{k: vObj, o: map[string]value{}}
		for _, p := range v.Properties {
			if p.Spread || p.Method || p.Getter || p.Setter || p.Computed {
				return value{}, false
			}
			var x value
			var ok bool
			if p.Shorthand {
				x, ok = f.ident(p.Key, sc)
			} else {
				x, ok = f.fold(p.Value, sc)
			}
			if !ok {
				return value{}, false
			}
			out.o[p.Key] = x
		}
		return out, true
	case *ast.TemplateExpr:
		return f.template(v, sc)
	case *ast.MemberExpr:
		return f.member(v, sc)
	case *ast.CallExpr:
		return f.call(v, sc)
	}
	return value{}, false
}

func (f *folder) ident(name string, sc *scope) (value, bool) {
	if v, ok := sc.lookup(name); ok {
		return v, true
	}
	switch name {
	case "Infinity":
		return num(math.Inf(1)), true
	}
	if f.shadow[name] {
		return value{}, false
	}
	if m, ok := f.memo[name]; ok {
		if m == nil {
			return value{}, false
		}
		return *m, true
	}
	init, ok := f.consts[name]
	if !ok {
		return value{}, false
	}
	f.memo[name] = nil // cycle guard
	v, ok := f.fold(init, nil)
	if !ok {
		return value{}, false
	}
	f.memo[name] = &v
	return v, true
}

func foldLiteral(l *ast.Literal) (value, bool) {
	switch l.Kind {
	case ast.NumberLit:
		n, ok := parseNumber(l.Value)
		return num(n), ok
	case ast.StringLit:
		return value{k: vStr, s: l.Value}, true
	case ast.BoolLit:
		return value{k: vBool, b: l.Value == "true"}, true
	case ast.NullLit:
		return value{k: vNull}, true
	}
	return value{}, false
}

func parseNumber(s string) (float64, bool) {
	s = strings.ReplaceAll(s, "_", "")
	if len(s) > 2 && s[0] == '0' {
		base := 0
		switch s[1] {
		case 'x', 'X':
			base = 16
		case 'o', 'O':
			base = 8
		case 'b', 'B':
			base = 2
		}
		if base != 0 {
			n, err := strconv.ParseUint(s[2:], base, 64)
			return float64(n), err == nil
		}
	}
	if strings.HasSuffix(s, "n") {
		return 0, false
	}
	n, err := strconv.ParseFloat(s, 64)
	return n, err == nil
}

func truthy(v value) bool {
	switch v.k {
	case vNum:
		return v.n != 0 && !math.IsNaN(v.n)
	case vStr:
		return v.s != ""
	case vBool:
		return v.b
	case vNull:
		return false
	}
	return true
}

func (f *folder) unary(u *ast.UnaryExpr, sc *scope) (value, bool) {
	if u.Postfix {
		return value{}, false
	}
	x, ok := f.fold(u.Arg, sc)
	if !ok {
		return value{}, false
	}
	switch u.Op {
	case "!":
		return value{k: vBool, b: !truthy(x)}, true
	case "-", "+":
		if x.k != vNum {
			return value{}, false
		}
		if u.Op == "-" {
			return num(-x.n), true
		}
		return x, true
	}
	return value{}, false
}

func (f *folder) binary(b *ast.BinaryExpr, sc *scope) (value, bool) {
	l, ok := f.fold(b.Left, sc)
	if !ok {
		return value{}, false
	}
	switch b.Op {
	case "&&":
		if !truthy(l) {
			return l, true
		}
		return f.fold(b.Right, sc)
	case "||":
		if truthy(l) {
			return l, true
		}
		return f.fold(b.Right, sc)
	}
	r, ok := f.fold(b.Right, sc)
	if !ok {
		return value{}, false
	}
	if b.Op == "+" && l.k == vStr && (r.k == vStr || r.k == vNum) {
		return value{k: vStr, s: l.s + strOf(r)}, true
	}
	if l.k != vNum || r.k != vNum {
		if b.Op == "===" || b.Op == "==" {
			return value{k: vBool, b: l.k == r.k && l.n == r.n && l.s == r.s && l.b == r.b}, true
		}
		return value{}, false
	}
	x, y := l.n, r.n
	switch b.Op {
	case "+":
		return num(x + y), true
	case "-":
		return num(x - y), true
	case "*":
		return num(x * y), true
	case "/":
		return num(x / y), true
	case "%":
		return num(math.Mod(x, y)), true
	case "**":
		return num(math.Pow(x, y)), true
	case "<":
		return value{k: vBool, b: x < y}, true
	case "<=":
		return value{k: vBool, b: x <= y}, true
	case ">":
		return value{k: vBool, b: x > y}, true
	case ">=":
		return value{k: vBool, b: x >= y}, true
	case "===", "==":
		return value{k: vBool, b: x == y}, true
	case "!==", "!=":
		return value{k: vBool, b: x != y}, true
	}
	return value{}, false
}

func strOf(v value) string {
	if v.k == vStr {
		return v.s
	}
	return formatNumber(v.n)
}

func (f *folder) template(t *ast.TemplateExpr, sc *scope) (value, bool) {
	var b strings.Builder
	for i, raw := range t.Raw {
		cooked, ok := cookTemplate(raw)
		if !ok {
			return value{}, false
		}
		b.WriteString(cooked)
		if i < len(t.Parts) {
			x, ok := f.fold(t.Parts[i], sc)
			if !ok || (x.k != vStr && x.k != vNum) {
				return value{}, false
			}
			b.WriteString(strOf(x))
		}
	}
	return value{k: vStr, s: b.String()}, true
}

// cookTemplate decodes the escapes of a template literal chunk. Chunks with
// escapes it does not know are not folded.
func cookTemplate(raw string) (string, bool) {
	if !strings.Contains(raw, `\`) {
		return strings.ReplaceAll(raw, "\r\n", "\n"), true
	}
	switch {
	case strings.ContainsAny(raw, "\r"):
		return "", false
	}
	var b strings.Builder
	for i := 0; i < len(raw); i++ {
		c := raw[i]
		if c != '\\' {
			b.WriteByte(c)
			continue
		}
		i++
		if i >= len(raw) {
			return "", false
		}
		switch raw[i] {
		case 'n':
			b.WriteByte('\n')
		case 't':
			b.WriteByte('\t')
		case '\\', '`', '$', '"', '\'':
			b.WriteByte(raw[i])
		default:
			return "", false
		}
	}
	return b.String(), true
}

var mathConsts = map[string]float64{
	"PI": math.Pi, "E": math.E, "SQRT2": math.Sqrt2, "SQRT1_2": math.Sqrt2 / 2,
	"LN2": math.Ln2, "LN10": math.Ln10, "LOG2E": math.Log2E, "LOG10E": math.Log10E,
}

func (f *folder) member(m *ast.MemberExpr, sc *scope) (value, bool) {
	if m.Optional {
		return value{}, false
	}
	if obj, ok := m.Object.(*ast.Identifier); ok && obj.Name == "Math" && !m.Computed && !f.shadow["Math"] {
		if id, ok := m.Property.(*ast.Identifier); ok {
			if c, ok := mathConsts[id.Name]; ok {
				return num(c), true
			}
		}
		return value{}, false
	}
	base, ok := f.fold(m.Object, sc)
	if !ok {
		return value{}, false
	}
	if m.Computed {
		idx, ok := f.fold(m.Property, sc)
		if !ok {
			return value{}, false
		}
		switch {
		case base.k == vArr && idx.k == vNum:
			i := int(idx.n)
			if float64(i) != idx.n || i < 0 || i >= len(base.a) {
				return value{}, false
			}
			return base.a[i], true
		case base.k == vObj && idx.k == vStr:
			v, ok := base.o[idx.s]
			return v, ok
		}
		return value{}, false
	}
	pid, ok := m.Property.(*ast.Identifier)
	if !ok {
		return value{}, false
	}
	name := pid.Name
	switch {
	case base.k == vArr && name == "length":
		return num(float64(len(base.a))), true
	case base.k == vStr && name == "length":
		return num(float64(len(base.s))), true
	case base.k == vObj:
		v, ok := base.o[name]
		return v, ok
	}
	return value{}, false
}

var mathFns1 = map[string]func(float64) float64{
	"sin": math.Sin, "cos": math.Cos, "tan": math.Tan, "asin": math.Asin, "acos": math.Acos,
	"atan": math.Atan, "sqrt": math.Sqrt, "abs": math.Abs, "floor": math.Floor, "ceil": math.Ceil,
	"trunc": math.Trunc, "exp": math.Exp, "log": math.Log, "sign": func(x float64) float64 {
		switch {
		case x > 0:
			return 1
		case x < 0:
			return -1
		}
		return x
	},
	"round": func(x float64) float64 { return math.Floor(x + 0.5) },
}

func (f *folder) call(c *ast.CallExpr, sc *scope) (value, bool) {
	if c.Optional {
		return value{}, false
	}
	m, ok := c.Callee.(*ast.MemberExpr)
	if !ok || m.Computed || m.Optional {
		return value{}, false
	}
	obj, ok := m.Object.(*ast.Identifier)
	if !ok || obj.Name != "Math" || f.shadow["Math"] {
		return value{}, false
	}
	prop, ok := m.Property.(*ast.Identifier)
	if !ok {
		return value{}, false
	}
	args := make([]float64, len(c.Args))
	for i, a := range c.Args {
		x, ok := f.fold(a, sc)
		if !ok || x.k != vNum {
			return value{}, false
		}
		args[i] = x.n
	}
	if fn, ok := mathFns1[prop.Name]; ok && len(args) == 1 {
		return num(fn(args[0])), true
	}
	switch prop.Name {
	case "pow", "atan2":
		if len(args) != 2 {
			return value{}, false
		}
		if prop.Name == "pow" {
			return num(math.Pow(args[0], args[1])), true
		}
		return num(math.Atan2(args[0], args[1])), true
	case "min", "max":
		if len(args) == 0 {
			return value{}, false
		}
		r := args[0]
		for _, a := range args[1:] {
			if prop.Name == "min" {
				r = math.Min(r, a)
			} else {
				r = math.Max(r, a)
			}
		}
		return num(r), true
	}
	return value{}, false
}

// formatNumber prints a float64 the way JavaScript does for the values the
// pass emits: shortest round-trip form, no negative zero.
func formatNumber(n float64) string {
	if n == 0 {
		return "0"
	}
	return strconv.FormatFloat(n, 'g', -1, 64)
}
