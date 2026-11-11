// Package bake holds the parts of build functions (`@build` in TypeScript,
// `//krate:build` in Go) that do not depend on how a function is evaluated:
// finding the calls in UI code, turning literal arguments into values, the
// canonical key that stubs look baked values up by, and the checks a result
// must pass before it is embedded in the app.
package bake

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"

	"github.com/kratejs/krate/packages/compiler/ast"
	"github.com/kratejs/krate/packages/compiler/internal/bundler"
)

// Limits on a baked value.
const (
	MaxBytes    = 8 << 20
	MaxDepth    = 512
	MaxArrayLen = 1_000_000
)

// Site is one use of an imported name in a UI module.
type Site struct {
	// Name is the exported (remote) name; Local the binding the module uses.
	Name, Local string
	// Line and Col locate the use, 1-based; Offset is its byte offset.
	Line, Col, Offset int
	// Call is true for a direct call; Args then holds the literal arguments
	// when Literal is true.
	Call    bool
	Literal bool
	Args    []any
	// BadArg is the 1-based index of the first non-literal argument.
	BadArg int
}

// FindUses finds the calls and other uses of the local names in locals (local
// binding to exported name) in a module. A name that is shadowed locally is
// not detected.
func FindUses(prog *ast.Program, locals map[string]string) []Site {
	if len(locals) == 0 {
		return nil
	}
	var sites []Site
	callee := map[*ast.Identifier]bool{}
	bundler.WalkStmts(prog.Body, func(n any, _ bool) bool {
		c, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		id, ok := unparen(c.Callee).(*ast.Identifier)
		if !ok {
			return true
		}
		remote, ok := locals[id.Name]
		if !ok {
			return true
		}
		callee[id] = true
		s := Site{Name: remote, Local: id.Name, Line: id.Position.Line, Col: id.Position.Col, Offset: id.Position.Offset, Call: true, Literal: true}
		for i, a := range c.Args {
			v, ok := Literal(a)
			if !ok {
				s.Literal, s.BadArg = false, i+1
				s.Args = nil
				break
			}
			s.Args = append(s.Args, v)
		}
		if s.Literal && s.Args == nil {
			s.Args = []any{}
		}
		sites = append(sites, s)
		return true
	})
	bundler.WalkStmts(prog.Body, func(n any, _ bool) bool {
		if id, ok := n.(*ast.Identifier); ok && !callee[id] {
			if remote, ok := locals[id.Name]; ok {
				sites = append(sites, Site{Name: remote, Local: id.Name, Line: id.Position.Line, Col: id.Position.Col, Offset: id.Position.Offset})
			}
		}
		return true
	})
	sort.SliceStable(sites, func(i, j int) bool {
		if sites[i].Line != sites[j].Line {
			return sites[i].Line < sites[j].Line
		}
		return sites[i].Col < sites[j].Col
	})
	return sites
}

func unparen(e ast.Expr) ast.Expr {
	for {
		p, ok := e.(*ast.ParenExpr)
		if !ok {
			return e
		}
		e = p.Expr
	}
}

// Literal converts a literal expression to a JSON-compatible value: nil, bool,
// float64, string, []any or map[string]any. It reports false for anything
// else.
func Literal(e ast.Expr) (any, bool) {
	switch x := unparen(e).(type) {
	case *ast.Literal:
		switch x.Kind {
		case ast.StringLit:
			return x.Value, true
		case ast.NumberLit:
			f, err := strconv.ParseFloat(strings.ReplaceAll(x.Value, "_", ""), 64)
			if err != nil || math.IsInf(f, 0) || math.IsNaN(f) {
				return nil, false
			}
			return f, true
		case ast.BoolLit:
			return x.Value == "true", true
		case ast.NullLit:
			return nil, true
		}
	case *ast.TemplateExpr:
		if len(x.Parts) == 0 && len(x.Raw) == 1 && !strings.Contains(x.Raw[0], `\`) {
			return x.Raw[0], true
		}
	case *ast.UnaryExpr:
		if x.Postfix || (x.Op != "-" && x.Op != "+") {
			return nil, false
		}
		if lit, ok := unparen(x.Arg).(*ast.Literal); ok && lit.Kind == ast.NumberLit {
			v, ok := Literal(lit)
			if f, isF := v.(float64); ok && isF && x.Op == "-" {
				return -f, true
			}
			return v, ok
		}
	case *ast.ArrayExpr:
		out := []any{}
		for _, el := range x.Elements {
			if el == nil {
				return nil, false
			}
			v, ok := Literal(el)
			if !ok {
				return nil, false
			}
			out = append(out, v)
		}
		return out, true
	case *ast.ObjectExpr:
		out := map[string]any{}
		for _, p := range x.Properties {
			if p == nil || p.Spread || p.Method || p.Computed || p.Shorthand || p.Getter || p.Setter || p.KeyNumeric || p.Value == nil {
				return nil, false
			}
			v, ok := Literal(p.Value)
			if !ok {
				return nil, false
			}
			out[p.Key] = v
		}
		return out, true
	}
	return nil, false
}

// Key is the canonical form of an argument list that a stub looks a baked
// value up by. The stub computes the same string in JavaScript: arrays and
// primitives as JSON.stringify writes them, object keys sorted.
func Key(args []any) string {
	var sb strings.Builder
	writeKey(&sb, args)
	return sb.String()
}

func writeKey(sb *strings.Builder, v any) {
	switch x := v.(type) {
	case nil:
		sb.WriteString("null")
	case bool:
		sb.WriteString(strconv.FormatBool(x))
	case float64:
		if x == 0 {
			sb.WriteString("0")
			return
		}
		b, _ := json.Marshal(x)
		sb.Write(b)
	case string:
		sb.WriteString(jsString(x))
	case []any:
		sb.WriteByte('[')
		for i, e := range x {
			if i > 0 {
				sb.WriteByte(',')
			}
			writeKey(sb, e)
		}
		sb.WriteByte(']')
	case map[string]any:
		keys := make([]string, 0, len(x))
		for k := range x {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		sb.WriteByte('{')
		for i, k := range keys {
			if i > 0 {
				sb.WriteByte(',')
			}
			sb.WriteString(jsString(k))
			sb.WriteByte(':')
			writeKey(sb, x[k])
		}
		sb.WriteByte('}')
	default:
		sb.WriteString(fmt.Sprint(x))
	}
}

// jsString quotes s the way JSON.stringify does.
func jsString(s string) string {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(s)
	out := strings.TrimSuffix(buf.String(), "\n")
	out = strings.ReplaceAll(out, `\u2028`, string(rune(0x2028)))
	return strings.ReplaceAll(out, `\u2029`, string(rune(0x2029)))
}

// ArgsJSON renders an argument list as a JSON array.
func ArgsJSON(args []any) string {
	b, _ := json.Marshal(args)
	return string(b)
}

// Table maps a function id to its baked values by Key.
type Table map[string]map[string]json.RawMessage

// Set stores a value.
func (t Table) Set(id, key string, v json.RawMessage) {
	if t[id] == nil {
		t[id] = map[string]json.RawMessage{}
	}
	t[id][key] = v
}

// Merge copies o into t.
func (t Table) Merge(o Table) {
	for id, m := range o {
		for k, v := range m {
			t.Set(id, k, v)
		}
	}
}

// Validate checks that raw is JSON a build function may bake: at most MaxBytes,
// nested at most MaxDepth levels, arrays of at most MaxArrayLen elements, and
// none of the wire protocol's tagged types (bytes, dates, maps, sets), which
// the UI stubs would not decode.
func Validate(raw []byte) error {
	if len(raw) > MaxBytes {
		return fmt.Errorf("the value is %d bytes of JSON, over the %d byte limit", len(raw), MaxBytes)
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return fmt.Errorf("the value is not valid JSON: %v", err)
	}
	return check(v, 1, "")
}

var wireTags = map[string]string{"$b": "binary data", "$d": "a Date", "$m": "a Map", "$s": "a Set"}

func check(v any, depth int, path string) error {
	if depth > MaxDepth {
		return fmt.Errorf("the value is nested deeper than %d levels at %s", MaxDepth, orRoot(path))
	}
	switch x := v.(type) {
	case []any:
		if len(x) > MaxArrayLen {
			return fmt.Errorf("an array at %s has %d elements, over the %d limit", orRoot(path), len(x), MaxArrayLen)
		}
		for i, e := range x {
			if err := check(e, depth+1, path+"["+strconv.Itoa(i)+"]"); err != nil {
				return err
			}
		}
	case map[string]any:
		if len(x) == 1 {
			for k := range x {
				if what, ok := wireTags[k]; ok {
					return fmt.Errorf("%s is %s, which cannot be baked; use plain JSON data", orRoot(path), what)
				}
			}
		}
		for k, e := range x {
			if err := check(e, depth+1, path+"."+k); err != nil {
				return err
			}
		}
	}
	return nil
}

func orRoot(p string) string {
	if p == "" {
		return "the result"
	}
	return p
}

// ErrUnsettled is the message for a promise that never resolved.
var ErrUnsettled = errors.New("the promise never settled (timers, fetch and other I/O are not available at build time)")

// KeyJS is the JavaScript twin of Key.
const KeyJS = `function __krateKey(v) {
  if (v === undefined) return 'null';
  if (v === null || typeof v !== 'object') return JSON.stringify(v);
  if (Array.isArray(v)) return '[' + v.map(__krateKey).join(',') + ']';
  return '{' + Object.keys(v).sort().map(function (k) { return JSON.stringify(k) + ':' + __krateKey(v[k]); }).join(',') + '}';
}
`

// AppModule returns the JavaScript appended to the app module: the baked
// table and the __krateHostBaked export that the stubs of build functions
// call. The values are looked up by id and the canonical key of the arguments.
func AppModule(t Table) string {
	ids := make([]string, 0, len(t))
	for id := range t {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	var sb strings.Builder
	sb.WriteString("const __krateBakedTable = {")
	for i, id := range ids {
		if i > 0 {
			sb.WriteByte(',')
		}
		sb.WriteString("\n  " + jsString(id) + ": {")
		keys := make([]string, 0, len(t[id]))
		for k := range t[id] {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for j, k := range keys {
			if j > 0 {
				sb.WriteByte(',')
			}
			sb.WriteString("\n    " + jsString(k) + ": " + string(t[id][k]))
		}
		sb.WriteString("\n  }")
	}
	sb.WriteString("\n};\n")
	sb.WriteString(KeyJS)
	sb.WriteString(`export function __krateHostBaked(id, args) {
  var t = __krateBakedTable[id], k = __krateKey(args);
  if (!t || !Object.prototype.hasOwnProperty.call(t, k)) {
    var e = new Error('no baked value for ' + id + '(' + k.slice(1, -1) + '): a build function can only be called with the literal arguments it has in the source');
    e.code = 'build_args';
    throw e;
  }
  var r = t[k];
  return r !== null && typeof r === 'object' ? JSON.parse(JSON.stringify(r)) : r;
}
`)
	return sb.String()
}

// Edit replaces the call that starts at Offset (an identifier of Len bytes
// followed by its argument list) with a baked value.
type Edit struct {
	Offset, Len int
	Value       json.RawMessage
}

// Rewrite replaces the calls of edits in src with their values, so the
// prerendered page sees constants. The newlines of a replaced call are kept
// after the value, so later lines keep their numbers. A call whose extent
// cannot be found is left alone and reported in the returned list.
func Rewrite(src string, edits []Edit) (string, []Edit) {
	sort.Slice(edits, func(i, j int) bool { return edits[i].Offset > edits[j].Offset })
	var skipped []Edit
	for _, e := range edits {
		i := e.Offset + e.Len
		if e.Offset < 0 || i > len(src) {
			skipped = append(skipped, e)
			continue
		}
		for i < len(src) && (src[i] == ' ' || src[i] == '\t' || src[i] == '\n' || src[i] == '\r') {
			i++
		}
		if i >= len(src) || src[i] != '(' {
			skipped = append(skipped, e)
			continue
		}
		end := matchParen(src, i)
		if end < 0 {
			skipped = append(skipped, e)
			continue
		}
		val := string(e.Value)
		if len(val) > 0 && (val[0] == '{' || val[0] == '[') {
			val = "(" + val + ")"
		}
		src = src[:e.Offset] + val + strings.Repeat("\n", strings.Count(src[e.Offset:end], "\n")) + src[end:]
	}
	return src, skipped
}

// matchParen returns the offset just past the ")" that closes the "(" at open,
// skipping strings, templates and comments, or -1.
func matchParen(src string, open int) int {
	depth := 0
	for i := open; i < len(src); i++ {
		switch c := src[i]; c {
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				return i + 1
			}
		case '"', '\'':
			for i++; i < len(src) && src[i] != c; i++ {
				if src[i] == '\\' {
					i++
				}
			}
		case '`':
			i = skipTemplate(src, i)
		case '/':
			if i+1 < len(src) {
				switch src[i+1] {
				case '/':
					j := strings.IndexByte(src[i:], '\n')
					if j < 0 {
						return -1
					}
					i += j
				case '*':
					j := strings.Index(src[i+2:], "*/")
					if j < 0 {
						return -1
					}
					i += j + 3
				}
			}
		}
	}
	return -1
}

func skipTemplate(src string, i int) int {
	for i++; i < len(src); i++ {
		switch src[i] {
		case '\\':
			i++
		case '`':
			return i
		case '$':
			if i+1 < len(src) && src[i+1] == '{' {
				depth := 0
				for j := i + 1; j < len(src); j++ {
					switch src[j] {
					case '{':
						depth++
					case '}':
						depth--
					case '`':
						j = skipTemplate(src, j)
					}
					if depth == 0 {
						i = j
						break
					}
				}
			}
		}
	}
	return len(src)
}
