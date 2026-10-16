// Package astjson encodes and decodes an *ast.Program to and from a
// kind-tagged JSON document. The document is the editable AST surface handed
// to JavaScript plugins running inside QuickJS: every node becomes an object
// with a "kind" discriminator plus its exported fields, so plugin authors can
// inspect and patch the tree with plain JS and hand it back.
package astjson

import (
	"encoding/json"
	"fmt"
	"reflect"
	"unicode"

	"github.com/kratejs/krate/packages/compiler/ast"
)

// EncodeProgram converts a parsed program into the AST document JSON.
func EncodeProgram(p *ast.Program) ([]byte, error) {
	doc := encodeValue(reflect.ValueOf(p))
	b, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("astjson: encoding program: %w", err)
	}
	return b, nil
}

// DecodeProgram reconstructs a program from an AST document. It accepts the
// output of EncodeProgram or a hand-edited equivalent.
func DecodeProgram(data []byte) (*ast.Program, error) {
	var doc interface{}
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("astjson: parsing program doc: %w", err)
	}
	m, ok := doc.(map[string]interface{})
	if !ok || m["kind"] != "Program" {
		return nil, fmt.Errorf("astjson: document is not a Program")
	}
	prog, err := decodeValue(m, reflect.TypeOf((*ast.Program)(nil)))
	if err != nil {
		return nil, err
	}
	return prog.Interface().(*ast.Program), nil
}

// nodeTypes maps a node kind (the Go type name without the package prefix) to
// the concrete pointer type used to rebuild it.
var nodeTypes = map[string]reflect.Type{
	"Identifier":       reflect.TypeOf((*ast.Identifier)(nil)),
	"Literal":          reflect.TypeOf((*ast.Literal)(nil)),
	"CallExpr":         reflect.TypeOf((*ast.CallExpr)(nil)),
	"MemberExpr":       reflect.TypeOf((*ast.MemberExpr)(nil)),
	"BinaryExpr":       reflect.TypeOf((*ast.BinaryExpr)(nil)),
	"UnaryExpr":        reflect.TypeOf((*ast.UnaryExpr)(nil)),
	"ConditionalExpr":  reflect.TypeOf((*ast.ConditionalExpr)(nil)),
	"TypeAssertion":    reflect.TypeOf((*ast.TypeAssertion)(nil)),
	"ArrowFn":          reflect.TypeOf((*ast.ArrowFn)(nil)),
	"ObjectExpr":       reflect.TypeOf((*ast.ObjectExpr)(nil)),
	"ObjectProp":       reflect.TypeOf((*ast.ObjectProp)(nil)),
	"ArrayExpr":        reflect.TypeOf((*ast.ArrayExpr)(nil)),
	"TemplateExpr":     reflect.TypeOf((*ast.TemplateExpr)(nil)),
	"JSXElement":       reflect.TypeOf((*ast.JSXElement)(nil)),
	"JSXFragment":      reflect.TypeOf((*ast.JSXFragment)(nil)),
	"JSXOpening":       reflect.TypeOf((*ast.JSXOpening)(nil)),
	"JSXClosing":       reflect.TypeOf((*ast.JSXClosing)(nil)),
	"JSXText":          reflect.TypeOf((*ast.JSXText)(nil)),
	"JSXExprContainer": reflect.TypeOf((*ast.JSXExprContainer)(nil)),
	"JSXElementChild":  reflect.TypeOf((*ast.JSXElementChild)(nil)),
	"JSXFragmentChild": reflect.TypeOf((*ast.JSXFragmentChild)(nil)),
	"JSXAttr":          reflect.TypeOf((*ast.JSXAttr)(nil)),
	"Param":            reflect.TypeOf((*ast.Param)(nil)),
	"Program":          reflect.TypeOf((*ast.Program)(nil)),
	"ImportStmt":       reflect.TypeOf((*ast.ImportStmt)(nil)),
	"NamedImport":      reflect.TypeOf((*ast.NamedImport)(nil)),
	"ExportStmt":       reflect.TypeOf((*ast.ExportStmt)(nil)),
	"VarStmt":          reflect.TypeOf((*ast.VarStmt)(nil)),
	"VarDecl":          reflect.TypeOf((*ast.VarDecl)(nil)),
	"FnDecl":           reflect.TypeOf((*ast.FnDecl)(nil)),
	"ReturnStmt":       reflect.TypeOf((*ast.ReturnStmt)(nil)),
	"ExprStmt":         reflect.TypeOf((*ast.ExprStmt)(nil)),
	"IfStmt":           reflect.TypeOf((*ast.IfStmt)(nil)),
	"BlockStmt":        reflect.TypeOf((*ast.BlockStmt)(nil)),
	"ForStmt":          reflect.TypeOf((*ast.ForStmt)(nil)),
	"ForInStmt":        reflect.TypeOf((*ast.ForInStmt)(nil)),
	"WhileStmt":        reflect.TypeOf((*ast.WhileStmt)(nil)),
	"DoWhileStmt":      reflect.TypeOf((*ast.DoWhileStmt)(nil)),
	"SwitchStmt":       reflect.TypeOf((*ast.SwitchStmt)(nil)),
	"CaseClause":       reflect.TypeOf((*ast.CaseClause)(nil)),
	"TryStmt":          reflect.TypeOf((*ast.TryStmt)(nil)),
	"CatchClause":      reflect.TypeOf((*ast.CatchClause)(nil)),
	"ThrowStmt":        reflect.TypeOf((*ast.ThrowStmt)(nil)),
	"BreakStmt":        reflect.TypeOf((*ast.BreakStmt)(nil)),
	"ContinueStmt":     reflect.TypeOf((*ast.ContinueStmt)(nil)),
	"NewExpr":          reflect.TypeOf((*ast.NewExpr)(nil)),
	"ThisExpr":         reflect.TypeOf((*ast.ThisExpr)(nil)),
	"AwaitExpr":        reflect.TypeOf((*ast.AwaitExpr)(nil)),
	"DynamicImport":    reflect.TypeOf((*ast.DynamicImport)(nil)),
	"ImportMetaExpr":   reflect.TypeOf((*ast.ImportMetaExpr)(nil)),
}

// typeIsNode reports whether a reflect.Type names one of the AST node types.
func typeIsNode(t reflect.Type) bool {
	if t.Kind() == reflect.Ptr {
		t = t.Elem()
	}
	_, known := nodeTypes[t.Name()]
	return known
}

// kindOf returns the AST doc kind string for a node value.
func kindOf(v reflect.Value) string {
	if v.Kind() == reflect.Ptr {
		return v.Type().Elem().Name()
	}
	return v.Type().Name()
}

// encodeNode encodes an AST node into a kind-tagged map.
func encodeNode(v reflect.Value) interface{} {
	v = derefPtr(v)
	m := map[string]interface{}{"kind": kindOf(v)}
	t := v.Type()
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if f.PkgPath != "" {
			continue // unexported
		}
		m[fieldKey(t.Name(), f.Name)] = encodeValue(v.Field(i))
	}
	return m
}

// encodeValue encodes an arbitrary reflect.Value into JSON-friendly form.
func encodeValue(v reflect.Value) interface{} {
	if !v.IsValid() {
		return nil
	}
	switch v.Kind() {
	case reflect.Interface, reflect.Ptr:
		if v.IsNil() {
			return nil
		}
		if v.Kind() == reflect.Ptr && typeIsNode(v.Type()) {
			return encodeNode(v)
		}
		if v.Kind() == reflect.Interface && typeIsNode(v.Elem().Type()) {
			return encodeNode(v.Elem())
		}
		return encodeValue(v.Elem())
	case reflect.Struct:
		if v.Type() == reflect.TypeOf(ast.Pos{}) {
			return map[string]interface{}{
				"line": v.FieldByName("Line").Int(),
				"col":  v.FieldByName("Col").Int(),
			}
		}
		if typeIsNode(v.Type()) {
			return encodeNode(v)
		}
		m := make(map[string]interface{})
		t := v.Type()
		for i := 0; i < t.NumField(); i++ {
			f := t.Field(i)
			if f.PkgPath != "" {
				continue
			}
			m[f.Name] = encodeValue(v.Field(i))
		}
		return m
	case reflect.Slice:
		out := make([]interface{}, v.Len())
		for i := 0; i < v.Len(); i++ {
			out[i] = encodeValue(v.Index(i))
		}
		return out
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return v.Convert(reflect.TypeOf(int64(0))).Int()
	case reflect.Bool:
		return v.Bool()
	case reflect.String:
		return v.String()
	case reflect.Float32, reflect.Float64:
		return v.Float()
	default:
		return v.Interface()
	}
}

func derefPtr(v reflect.Value) reflect.Value {
	if v.Kind() == reflect.Ptr {
		if v.IsNil() {
			return reflect.Value{}
		}
		return v.Elem()
	}
	return v
}

// decodeValue rebuilds a reflect.Value of the requested type from a document.
func decodeValue(doc interface{}, typ reflect.Type) (reflect.Value, error) {
	if doc == nil {
		return reflect.Zero(typ), nil
	}

	switch typ.Kind() {
	case reflect.Ptr:
		m, ok := doc.(map[string]interface{})
		if !ok {
			return reflect.Value{}, fmt.Errorf("astjson: expected object for %s", typ)
		}
		return decodeNodeValue(m, typ)
	case reflect.Interface:
		m, ok := doc.(map[string]interface{})
		if !ok {
			return reflect.Value{}, fmt.Errorf("astjson: expected object for interface %s", typ)
		}
		concrete := nodeTypes[kindOfString(m["kind"])]
		if concrete == nil {
			return reflect.Value{}, fmt.Errorf("astjson: unknown node kind %q", m["kind"])
		}
		return decodeFields(m, concrete)
	case reflect.Struct:
		m, ok := doc.(map[string]interface{})
		if !ok {
			return reflect.Value{}, fmt.Errorf("astjson: expected object for %s", typ)
		}
		if typ == reflect.TypeOf(ast.Pos{}) {
			return reflect.ValueOf(ast.Pos{Line: int(toInt(m["line"])), Col: int(toInt(m["col"]))}), nil
		}
		inst := reflect.New(typ)
		if err := decodeFieldsMap(inst.Elem(), m); err != nil {
			return reflect.Value{}, err
		}
		return inst.Elem(), nil
	case reflect.Slice:
		if doc == nil {
			return reflect.Zero(typ), nil
		}
		arr, ok := doc.([]interface{})
		if !ok {
			return reflect.Value{}, fmt.Errorf("astjson: expected array for %s", typ)
		}
		if len(arr) == 0 {
			return reflect.Zero(typ), nil
		}
		out := reflect.MakeSlice(typ, 0, len(arr))
		for _, item := range arr {
			ev, err := decodeValue(item, typ.Elem())
			if err != nil {
				return reflect.Value{}, err
			}
			out = reflect.Append(out, ev)
		}
		return out, nil
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return reflect.ValueOf(toInt(doc)).Convert(typ), nil
	case reflect.Bool:
		b, ok := doc.(bool)
		if !ok {
			return reflect.Value{}, fmt.Errorf("astjson: expected bool")
		}
		return reflect.ValueOf(b), nil
	case reflect.String:
		s, ok := doc.(string)
		if !ok {
			return reflect.Value{}, fmt.Errorf("astjson: expected string")
		}
		return reflect.ValueOf(s), nil
	case reflect.Float32, reflect.Float64:
		f, ok := doc.(float64)
		if !ok {
			return reflect.Value{}, fmt.Errorf("astjson: expected number")
		}
		return reflect.ValueOf(f).Convert(typ), nil
	}
	return reflect.Value{}, fmt.Errorf("astjson: unsupported field type %s", typ)
}

// decodeNodeValue rebuilds a concrete node pointer of the requested type.
func decodeNodeValue(m map[string]interface{}, typ reflect.Type) (reflect.Value, error) {
	concrete := nodeTypes[kindOfString(m["kind"])]
	if concrete == nil {
		return reflect.Value{}, fmt.Errorf("astjson: unknown node kind %q", m["kind"])
	}
	if typ != nil && typ != concrete {
		return reflect.Value{}, fmt.Errorf("astjson: kind %q does not match %s", m["kind"], typ)
	}
	return decodeFields(m, concrete)
}

// decodeFields fills a concrete pointer node from a kind-tagged map.
func decodeFields(m map[string]interface{}, concrete reflect.Type) (reflect.Value, error) {
	inst := reflect.New(concrete.Elem())
	if err := decodeFieldsMap(inst.Elem(), m); err != nil {
		return reflect.Value{}, err
	}
	return inst, nil
}

func decodeFieldsMap(rv reflect.Value, m map[string]interface{}) error {
	t := rv.Type()
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if f.PkgPath != "" {
			continue
		}
		raw, ok := m[fieldKey(t.Name(), f.Name)]
		if !ok {
			continue
		}
		fv, err := decodeValue(raw, f.Type)
		if err != nil {
			return fmt.Errorf("astjson: field %s.%s: %w", t.Name(), f.Name, err)
		}
		rv.Field(i).Set(fv)
	}
	return nil
}

func kindOfString(v interface{}) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

// lowerFirst lowercases the first rune of a field name so the JSON document
// uses idiomatic camelCase keys (Value -> value, RawCSS -> rawCSS).
func lowerFirst(s string) string {
	if s == "" {
		return s
	}
	r := []rune(s)
	r[0] = unicode.ToLower(r[0])
	return string(r)
}

// collidingKindKeys renames struct fields literally named "Kind" so their
// encoded keys never clobber the "kind" discriminator every node carries.
var collidingKindKeys = map[string]string{
	"Literal.Kind": "literalKind",
	"VarStmt.Kind": "variableKind",
}

// fieldKey returns the JSON key for a node struct field. The discriminator
// "kind" is reserved, so any field that would map onto it is renamed.
func fieldKey(typeName, fieldName string) string {
	k := lowerFirst(fieldName)
	if k == "kind" {
		if v, ok := collidingKindKeys[typeName+"."+fieldName]; ok {
			return v
		}
		return "kindValue"
	}
	return k
}

func toInt(v interface{}) int64 {
	switch n := v.(type) {
	case int64:
		return n
	case float64:
		return int64(n)
	case int:
		return int64(n)
	case json.Number:
		i, _ := n.Int64()
		return i
	}
	return 0
}
