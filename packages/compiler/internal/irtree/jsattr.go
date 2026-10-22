package irtree

import (
	"github.com/kratejs/krate/packages/compiler/internal/htmlspec"
	"github.com/kratejs/krate/packages/compiler/internal/jsvalue"
)

// JSAttrValue maps an evaluated build-time JS value to the serializer's attribute
// value. jsvalue owns evaluation and JS types; Value is only the narrow view
// the attribute/style rules need (plus Bare and Loose, which have no JS
// counterpart). Unknown values map to Loose with the given fallback text.
func JSAttrValue(v jsvalue.Value, unknownText string) htmlspec.Value {
	switch v.Kind() {
	case jsvalue.KindUndefined, jsvalue.KindNull, jsvalue.KindFunction:
		return htmlspec.Value{Kind: htmlspec.Null}
	case jsvalue.KindBool:
		return htmlspec.BoolVal(v.BoolValue())
	case jsvalue.KindNumber:
		return htmlspec.Value{Kind: htmlspec.Number, Str: jsvalue.NumberToString(v.NumberValue())}
	case jsvalue.KindBigInt:
		return htmlspec.Value{Kind: htmlspec.Number, Str: v.ToString()}
	case jsvalue.KindString:
		return htmlspec.StrVal(v.StringValue())
	case jsvalue.KindArray:
		items := make([]string, 0, len(v.Elems()))
		for _, el := range v.Elems() {
			items = append(items, el.ToString())
		}
		return htmlspec.Value{Kind: htmlspec.String, Str: v.ToString(), Items: items}
	case jsvalue.KindUnknown:
		return htmlspec.LooseVal(unknownText)
	}
	// Plain objects and JSX elements stringify like any JS object.
	return htmlspec.StrVal(v.ToString())
}
