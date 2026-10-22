package jsvalue

import (
	"encoding/json"
	"math"
	"strings"

	"github.com/kratejs/krate/packages/compiler/ast"
	"github.com/kratejs/krate/packages/compiler/internal/lexer"
	"github.com/kratejs/krate/packages/compiler/internal/parser"
)

// ParseExprSource parses a single JS expression from source text (nil on
// failure). Object literals are accepted without wrapping parentheses.
func ParseExprSource(src string) ast.Expr {
	src = strings.TrimSpace(src)
	if src == "" {
		return nil
	}
	try := func(s string) ast.Expr {
		p := parser.New(lexer.New(s).Tokenize())
		prog := p.ParseProgram()
		if len(p.Errors()) > 0 || prog == nil || len(prog.Body) != 1 {
			return nil
		}
		if es, ok := prog.Body[0].(*ast.ExprStmt); ok {
			return es.Expression
		}
		return nil
	}
	if src[0] == '{' {
		if e := try("(" + src + ")"); e != nil {
			return e
		}
	}
	return try(src)
}

// ParseLiteralSource parses JS literal source (as produced by JSSource or the
// legacy const folder) into a Value.
func ParseLiteralSource(src string) (Value, bool) {
	expr := ParseExprSource(src)
	if expr == nil {
		return Value{}, false
	}
	return FromLiteralAST(expr)
}

// FromJSON decodes JSON text into a Value, preserving object key order.
func FromJSON(text string) (Value, bool) {
	dec := json.NewDecoder(strings.NewReader(text))
	dec.UseNumber()
	v, ok := decodeJSON(dec)
	if !ok {
		return Value{}, false
	}
	if _, err := dec.Token(); err == nil {
		return Value{}, false // trailing data
	}
	return v, true
}

func decodeJSON(dec *json.Decoder) (Value, bool) {
	tok, err := dec.Token()
	if err != nil {
		return Value{}, false
	}
	switch t := tok.(type) {
	case json.Delim:
		switch t {
		case '[':
			var elems []Value
			for dec.More() {
				v, ok := decodeJSON(dec)
				if !ok {
					return Value{}, false
				}
				elems = append(elems, v)
			}
			if _, err := dec.Token(); err != nil {
				return Value{}, false
			}
			return NewArray(elems...), true
		case '{':
			obj := NewObject()
			for dec.More() {
				kt, err := dec.Token()
				if err != nil {
					return Value{}, false
				}
				key, ok := kt.(string)
				if !ok {
					return Value{}, false
				}
				v, ok := decodeJSON(dec)
				if !ok {
					return Value{}, false
				}
				obj.Set(key, v)
			}
			if _, err := dec.Token(); err != nil {
				return Value{}, false
			}
			return obj, true
		}
	case string:
		return String(t), true
	case json.Number:
		f, err := t.Float64()
		if err != nil && !math.IsInf(f, 0) {
			return Value{}, false
		}
		return Number(f), true
	case bool:
		return Bool(t), true
	case nil:
		return Null(), true
	}
	return Value{}, false
}
