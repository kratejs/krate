package irtree

import (
	"math"
	"sort"
	"strconv"
	"strings"

	"github.com/kratejs/krate/packages/compiler/ast"
	"github.com/kratejs/krate/packages/compiler/internal/jsvalue"
)

// evalPureWithJS is the constrained fallback for pure expressions that the Go
// interpreter cannot model. The expression must be completely known, contain no
// signals or JSX, and only call deterministic built-ins on known values. This
// keeps Date, random, I/O, and user code out of build-time execution.
func (b *builder) evalPureWithJS(expr ast.Expr) (jsvalue.Value, bool) {
	if b.evalJS == nil || expr == nil || b.referencesSignal(expr) {
		return jsvalue.Value{}, false
	}
	if !b.pureJSExpr(expr) {
		return jsvalue.Value{}, false
	}
	var prelude strings.Builder
	prelude.WriteString("(function(){")
	names := make([]string, 0, len(b.localProps))
	for name := range b.localProps {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if name == "props" || !validJSBindingName(name) {
			continue
		}
		v := b.localProps[name]
		if !jsBindingValue(v) {
			continue
		}
		src, ok := jsvalue.JSSource(v)
		if !ok {
			continue
		}
		prelude.WriteString("var ")
		prelude.WriteString(name)
		prelude.WriteByte('=')
		prelude.WriteString(src)
		prelude.WriteByte(';')
	}
	prelude.WriteString("var props={")
	first := true
	for _, name := range names {
		v := b.localProps[name]
		if !jsBindingValue(v) {
			continue
		}
		src, ok := jsvalue.JSSource(v)
		if !ok {
			continue
		}
		if !first {
			prelude.WriteByte(',')
		}
		first = false
		prelude.WriteString(strconv.Quote(name))
		prelude.WriteByte(':')
		prelude.WriteString(src)
	}
	prelude.WriteString("};return JSON.stringify(")
	prelude.WriteString(generateExprJS(expr, nil))
	prelude.WriteString(");})()")

	out, err := b.evalJS(prelude.String())
	if err != nil {
		return jsvalue.Value{}, false
	}
	switch out {
	case "", "undefined":
		return jsvalue.Undefined(), true
	case "NaN":
		return jsvalue.Number(math.NaN()), true
	case "Infinity":
		return jsvalue.Number(math.Inf(1)), true
	case "-Infinity":
		return jsvalue.Number(math.Inf(-1)), true
	}
	v, ok := jsvalue.FromJSON(out)
	if !ok {
		return jsvalue.Value{}, false
	}
	return v, true
}

func jsBindingValue(v jsvalue.Value) bool {
	return !v.ContainsUnknown() && !v.IsFunction() && !v.IsJSX()
}

// pureJSProperty accepts a property only when its name is statically known and
// cannot reach a constructor, prototype, random source, or locale-dependent
// method. Computed keys with non-literal expressions are rejected outright so
// string concatenation cannot smuggle one of those names past the check.
func (b *builder) pureJSProperty(obj, prop ast.Expr, computed bool) bool {
	key, ok := staticJSPropertyName(prop, computed)
	if !ok || jsForbiddenProps[key] {
		return false
	}
	if id, ok := obj.(*ast.Identifier); ok && id.Name == "props" {
		if key == "children" {
			return false
		}
		if v, ok := b.localProps[key]; ok && !jsBindingValue(v) {
			return false
		}
	}
	return true
}

func staticJSPropertyName(prop ast.Expr, computed bool) (string, bool) {
	if !computed {
		if id, ok := prop.(*ast.Identifier); ok {
			return id.Name, true
		}
		return "", false
	}
	lit, ok := prop.(*ast.Literal)
	if !ok || (lit.Kind != ast.StringLit && lit.Kind != ast.NumberLit) {
		return "", false
	}
	return lit.Value, true
}

func validJSBindingName(name string) bool {
	if name == "" || name == "props" {
		return false
	}
	for i, r := range name {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || r == '_' || r == '$' {
			continue
		}
		if i > 0 && r >= '0' && r <= '9' {
			continue
		}
		return false
	}
	return true
}

var jsPureGlobals = map[string]bool{
	"undefined": true, "NaN": true, "Infinity": true,
	"String": true, "Number": true, "Boolean": true,
	"Object": true, "Array": true, "JSON": true, "Math": true,
	"parseInt": true, "parseFloat": true, "isNaN": true, "isFinite": true,
	"encodeURIComponent": true, "decodeURIComponent": true,
}

var jsForbiddenRoots = map[string]bool{
	"Date": true, "Intl": true, "process": true, "globalThis": true,
	"window": true, "document": true, "fetch": true, "require": true,
	"eval": true, "Function": true, "WebAssembly": true, "console": true,
}

var jsForbiddenProps = map[string]bool{
	"constructor": true, "__proto__": true, "prototype": true, "random": true,
	"toLocaleUpperCase": true, "toLocaleLowerCase": true, "toLocaleString": true,
	"localeCompare": true,
}

func (b *builder) pureJSExpr(expr ast.Expr) bool {
	switch e := expr.(type) {
	case *ast.Literal:
		return true
	case *ast.Identifier:
		if jsPureGlobals[e.Name] || e.Name == "props" {
			return true
		}
		return b.pureJSBinding(e.Name)
	case *ast.MemberExpr:
		if !b.pureJSExpr(e.Object) || !b.pureJSProperty(e.Object, e.Property, e.Computed) {
			return false
		}
		return true
	case *ast.CallExpr:
		switch callee := e.Callee.(type) {
		case *ast.Identifier:
			if jsForbiddenRoots[callee.Name] || !(jsPureGlobals[callee.Name] || b.pureJSBinding(callee.Name)) {
				return false
			}
			// Functions are not serialized into the fallback prelude.
			if _, ok := b.localProps[callee.Name]; ok {
				if v := b.localProps[callee.Name]; v.IsFunction() {
					return false
				}
			}
		case *ast.MemberExpr:
			if !b.pureJSExpr(callee.Object) || !b.pureJSProperty(callee.Object, callee.Property, callee.Computed) {
				return false
			}
		default:
			return false
		}
		for _, arg := range e.Args {
			if !b.pureJSExpr(arg) {
				return false
			}
		}
		return true
	case *ast.NewExpr:
		id, ok := e.Callee.(*ast.Identifier)
		if !ok || (id.Name != "Array" && id.Name != "Object") {
			return false
		}
		for _, arg := range e.Args {
			if !b.pureJSExpr(arg) {
				return false
			}
		}
		return true
	case *ast.BinaryExpr:
		return b.pureJSExpr(e.Left) && b.pureJSExpr(e.Right)
	case *ast.UnaryExpr:
		return e.Op != "await" && b.pureJSExpr(e.Arg)
	case *ast.ConditionalExpr:
		return b.pureJSExpr(e.Test) && b.pureJSExpr(e.Consequent) && b.pureJSExpr(e.Alternate)
	case *ast.ArrayExpr:
		for _, el := range e.Elements {
			if el != nil && !b.pureJSExpr(el) {
				return false
			}
		}
		return true
	case *ast.ObjectExpr:
		for _, p := range e.Properties {
			if p == nil || p.Spread {
				return false
			}
			if p.KeyExpr != nil && !b.pureJSExpr(p.KeyExpr) {
				return false
			}
			if p.Value != nil && !b.pureJSExpr(p.Value) {
				return false
			}
		}
		return true
	case *ast.TemplateExpr:
		for _, p := range e.Parts {
			if !b.pureJSExpr(p) {
				return false
			}
		}
		return true
	case *ast.ParenExpr:
		return b.pureJSExpr(e.Expr)
	case *ast.TypeAssertion:
		return b.pureJSExpr(e.Expr)
	}
	return false
}

func (b *builder) pureJSBinding(name string) bool {
	if name == "" || name == "props" {
		return false
	}
	v, ok := b.localProps[name]
	if !ok || !jsBindingValue(v) {
		return false
	}
	_, ok = jsvalue.JSSource(v)
	return ok
}
