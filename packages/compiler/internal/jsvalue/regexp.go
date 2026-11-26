package jsvalue

import (
	"encoding/json"
	"fmt"
	"math"
	"runtime"
	"strings"
	"sync"

	"github.com/kratejs/krate/packages/native/jsruntime"
)

// RegExpValue is the state of a JavaScript RegExp object. Matching runs in a
// pooled QuickJS VM so semantics match ECMAScript (lookbehind, backreferences,
// sticky, unicode, named groups, lastIndex).
type RegExpValue struct {
	source    string
	flags     string
	lastIndex Value
}

// String returns the canonical `/source/flags` form.
func (r *RegExpValue) String() string {
	return "/" + r.source + "/" + r.flags
}

func (r *RegExpValue) hasFlag(ch byte) bool {
	return strings.IndexByte(r.flags, ch) >= 0
}

// IsRegExp reports whether the value is a RegExp object.
func (v Value) IsRegExp() bool { return v.kind == KindRegExp && v.re != nil }

// NewRegExp compiles a RegExp with JS semantics, returning KindRegExp or
// Unsupported on invalid syntax or flags.
func NewRegExp(pattern, flags string) Value {
	src, fl, err := compileRegExp(pattern, flags)
	if err != nil {
		return Unsupported(err.Error())
	}
	return Value{kind: KindRegExp, re: &RegExpValue{source: src, flags: fl, lastIndex: Number(0)}}
}

// parseRegExpLiteral parses a `/pattern/flags` literal token value.
func parseRegExpLiteral(raw string) Value {
	last := strings.LastIndexByte(raw, '/')
	if len(raw) < 2 || raw[0] != '/' || last <= 0 {
		return Unsupported("SyntaxError: invalid regular expression literal " + raw)
	}
	return NewRegExp(raw[1:last], raw[last+1:])
}

// regexpCtor builds the global RegExp constructor.
func regexpCtor() Value {
	f := Func("RegExp", func(args []Value) Value { return buildRegExp(args, false) })
	f.fn.Construct = func(args []Value) Value { return buildRegExp(args, true) }
	return f
}

func buildRegExp(args []Value, isNew bool) Value {
	for _, a := range args {
		if a.IsUnknown() {
			return a
		}
	}
	patArg := arg(args, 0)
	flagsArg := arg(args, 1)
	if patArg.kind == KindRegExp && patArg.re != nil {
		if !isNew && flagsArg.kind == KindUndefined {
			return patArg
		}
		flags := patArg.re.flags
		if flagsArg.kind != KindUndefined {
			p := flagsArg.ToPrimitive()
			if p.IsUnknown() {
				return p
			}
			flags = p.ToString()
		}
		return NewRegExp(patArg.re.source, flags)
	}
	pattern := ""
	if patArg.kind != KindUndefined {
		p := patArg.ToPrimitive()
		if p.IsUnknown() {
			return p
		}
		pattern = p.ToString()
	}
	flags := ""
	if flagsArg.kind != KindUndefined {
		p := flagsArg.ToPrimitive()
		if p.IsUnknown() {
			return p
		}
		flags = p.ToString()
	}
	return NewRegExp(pattern, flags)
}

var (
	regexpMethods        = memberSet("exec test toString valueOf")
	regexpBuiltinMembers = memberSet("compile dotAll exec flags global hasIndices ignoreCase lastIndex multiline source sticky test toString unicode unicodeSets valueOf constructor hasOwnProperty isPrototypeOf propertyIsEnumerable toLocaleString __proto__")
)

func regexpProperty(v Value, key string) (Value, bool) {
	r := v.re
	if r == nil {
		return Value{}, false
	}
	switch key {
	case "lastIndex":
		return r.lastIndex, true
	case "source":
		return String(r.source), true
	case "flags":
		return String(r.flags), true
	case "global":
		return Bool(r.hasFlag('g')), true
	case "ignoreCase":
		return Bool(r.hasFlag('i')), true
	case "multiline":
		return Bool(r.hasFlag('m')), true
	case "dotAll":
		return Bool(r.hasFlag('s')), true
	case "unicode":
		return Bool(r.hasFlag('u')), true
	case "unicodeSets":
		return Bool(r.hasFlag('v')), true
	case "sticky":
		return Bool(r.hasFlag('y')), true
	case "hasIndices":
		return Bool(r.hasFlag('d')), true
	}
	if regexpMethods[key] {
		return boundMethod(v, key), true
	}
	return Value{}, false
}

func regexpMethod(recv Value, name string, args []Value) Value {
	r := recv.re
	switch name {
	case "toString":
		return String(r.String())
	case "valueOf":
		return recv
	case "exec":
		p := arg(args, 0).ToPrimitive()
		if p.IsUnknown() {
			return p
		}
		return r.exec(p.ToString())
	case "test":
		p := arg(args, 0).ToPrimitive()
		if p.IsUnknown() {
			return p
		}
		return r.test(p.ToString())
	}
	return Unsupported("RegExp.prototype." + name)
}

func (r *RegExpValue) lastIndexLength() float64 {
	f := ToIntegerOrInfinity(r.lastIndex.ToNumber())
	if f <= 0 {
		return 0
	}
	const maxSafe = (1 << 53) - 1
	if f > maxSafe {
		return maxSafe
	}
	return f
}

func (r *RegExpValue) updateLastIndex(next float64) {
	if r.hasFlag('g') || r.hasFlag('y') {
		r.lastIndex = Number(next)
	}
}

func (r *RegExpValue) exec(input string) Value {
	raw, err := callRegexVM("exec", r.source, r.flags, r.lastIndexLength(), input, "", -1)
	if err != nil {
		return Unsupported(err.Error())
	}
	var res struct {
		M *encodedMatch `json:"m"`
		L float64       `json:"l"`
	}
	if err := json.Unmarshal([]byte(raw), &res); err != nil {
		return Unsupported("RegExp.prototype.exec: unexpected engine response")
	}
	r.updateLastIndex(res.L)
	if res.M == nil {
		return Null()
	}
	return decodeMatch(res.M)
}

func (r *RegExpValue) test(input string) Value {
	raw, err := callRegexVM("test", r.source, r.flags, r.lastIndexLength(), input, "", -1)
	if err != nil {
		return Unsupported(err.Error())
	}
	var res struct {
		B bool    `json:"b"`
		L float64 `json:"l"`
	}
	if err := json.Unmarshal([]byte(raw), &res); err != nil {
		return Unsupported("RegExp.prototype.test: unexpected engine response")
	}
	r.updateLastIndex(res.L)
	return Bool(res.B)
}

func coerceToRegExp(v Value, defaultFlags string) Value {
	if v.kind == KindRegExp && v.re != nil {
		return v
	}
	if v.kind == KindUndefined {
		return NewRegExp("", defaultFlags)
	}
	p := v.ToPrimitive()
	if p.IsUnknown() {
		return p
	}
	return NewRegExp(p.ToString(), defaultFlags)
}

func stringMatch(s string, arg0 Value) Value {
	reVal := coerceToRegExp(arg0, "")
	if reVal.IsUnknown() {
		return reVal
	}
	r := reVal.re
	raw, err := callRegexVM("match", r.source, r.flags, r.lastIndexLength(), s, "", -1)
	if err != nil {
		return Unsupported(err.Error())
	}
	if r.hasFlag('g') {
		var res struct {
			A []string `json:"a"`
			L float64  `json:"l"`
		}
		if err := json.Unmarshal([]byte(raw), &res); err != nil {
			return Unsupported("String.prototype.match: unexpected engine response")
		}
		r.updateLastIndex(res.L)
		if res.A == nil {
			return Null()
		}
		elems := make([]Value, len(res.A))
		for i, item := range res.A {
			elems[i] = String(item)
		}
		return NewArray(elems...)
	}
	var res struct {
		M *encodedMatch `json:"m"`
		L float64       `json:"l"`
	}
	if err := json.Unmarshal([]byte(raw), &res); err != nil {
		return Unsupported("String.prototype.match: unexpected engine response")
	}
	r.updateLastIndex(res.L)
	if res.M == nil {
		return Null()
	}
	return decodeMatch(res.M)
}

func stringMatchAll(s string, arg0 Value) Value {
	var r *RegExpValue
	if arg0.kind == KindRegExp && arg0.re != nil {
		if !arg0.re.hasFlag('g') {
			return Unsupported("TypeError: String.prototype.matchAll called with a non-global RegExp argument")
		}
		r = arg0.re
	} else {
		reVal := coerceToRegExp(arg0, "g")
		if reVal.IsUnknown() {
			return reVal
		}
		r = reVal.re
	}
	raw, err := callRegexVM("matchAll", r.source, r.flags, r.lastIndexLength(), s, "", -1)
	if err != nil {
		return Unsupported(err.Error())
	}
	var res struct {
		A []*encodedMatch `json:"a"`
	}
	if err := json.Unmarshal([]byte(raw), &res); err != nil {
		return Unsupported("String.prototype.matchAll: unexpected engine response")
	}
	matches := make([]Value, len(res.A))
	for i, m := range res.A {
		matches[i] = decodeMatch(m)
	}
	i := 0
	return NewIterator("RegExp String Iterator", func() (Value, bool) {
		if i >= len(matches) {
			return Value{}, false
		}
		v := matches[i]
		i++
		return v, true
	})
}

func stringSearch(s string, arg0 Value) Value {
	reVal := coerceToRegExp(arg0, "")
	if reVal.IsUnknown() {
		return reVal
	}
	r := reVal.re
	raw, err := callRegexVM("search", r.source, r.flags, 0, s, "", -1)
	if err != nil {
		return Unsupported(err.Error())
	}
	var res struct {
		I int `json:"i"`
	}
	if err := json.Unmarshal([]byte(raw), &res); err != nil {
		return Unsupported("String.prototype.search: unexpected engine response")
	}
	return Number(float64(res.I))
}

func stringSplitRegExp(s string, sepV Value, limitV Value) Value {
	r := sepV.re
	limit := int64(-1)
	if limitV.kind != KindUndefined {
		limit = int64(ToUint32(limitV.ToNumber()))
	}
	raw, err := callRegexVM("split", r.source, r.flags, 0, s, "", limit)
	if err != nil {
		return Unsupported(err.Error())
	}
	var res struct {
		A []*string `json:"a"`
	}
	if err := json.Unmarshal([]byte(raw), &res); err != nil {
		return Unsupported("String.prototype.split: unexpected engine response")
	}
	parts := make([]Value, len(res.A))
	for i, p := range res.A {
		if p == nil {
			parts[i] = Undefined()
		} else {
			parts[i] = String(*p)
		}
	}
	return NewArray(parts...)
}

func stringReplaceRegExp(s string, all bool, patV Value, replV Value) Value {
	r := patV.re
	if all && !r.hasFlag('g') {
		return Unsupported("TypeError: String.prototype.replaceAll called with a non-global RegExp argument")
	}
	if !replV.IsFunction() {
		p := replV.ToPrimitive()
		if p.IsUnknown() {
			return p
		}
		raw, err := callRegexVM("replace", r.source, r.flags, r.lastIndexLength(), s, p.ToString(), -1)
		if err != nil {
			return Unsupported(err.Error())
		}
		var res struct {
			R string  `json:"r"`
			L float64 `json:"l"`
		}
		if err := json.Unmarshal([]byte(raw), &res); err != nil {
			return Unsupported("String.prototype.replace: unexpected engine response")
		}
		r.updateLastIndex(res.L)
		return String(res.R)
	}
	raw, err := callRegexVM("replaceMatches", r.source, r.flags, r.lastIndexLength(), s, "", -1)
	if err != nil {
		return Unsupported(err.Error())
	}
	var res struct {
		A []struct {
			C []*string            `json:"c"`
			I int                  `json:"i"`
			G [][2]json.RawMessage `json:"g"`
		} `json:"a"`
		L float64 `json:"l"`
	}
	if err := json.Unmarshal([]byte(raw), &res); err != nil {
		return Unsupported("String.prototype.replace: unexpected engine response")
	}
	r.updateLastIndex(res.L)
	if len(res.A) == 0 {
		return String(s)
	}
	u := units(s)
	var out strings.Builder
	nextPos := 0
	for _, m := range res.A {
		if m.I < nextPos || m.I > len(u) {
			continue
		}
		out.WriteString(fromUnits(u[nextPos:m.I]))
		callArgs := make([]Value, 0, len(m.C)+3)
		matchText := ""
		for j, cp := range m.C {
			if cp == nil {
				callArgs = append(callArgs, Undefined())
			} else {
				if j == 0 {
					matchText = *cp
				}
				callArgs = append(callArgs, String(*cp))
			}
		}
		callArgs = append(callArgs, Number(float64(m.I)), String(s))
		if m.G != nil {
			callArgs = append(callArgs, decodeGroupPairs(m.G))
		}
		rv := replV.Call(callArgs...)
		if rv.IsUnknown() {
			return rv
		}
		rp := rv.ToPrimitive()
		if rp.IsUnknown() {
			return rp
		}
		out.WriteString(rp.ToString())
		nextPos = m.I + len(units(matchText))
		if nextPos > len(u) {
			nextPos = len(u)
		}
	}
	out.WriteString(fromUnits(u[nextPos:]))
	return String(out.String())
}

// Match JSON decoding

type encodedIndices struct {
	A []*[2]int            `json:"a"`
	G [][2]json.RawMessage `json:"g"`
}

type encodedMatch struct {
	C []*string            `json:"c"`
	I int                  `json:"i"`
	S string               `json:"s"`
	G [][2]json.RawMessage `json:"g"`
	D *encodedIndices      `json:"d"`
}

func decodeGroupPairs(pairs [][2]json.RawMessage) Value {
	obj := NewObject()
	for _, pair := range pairs {
		var key string
		if err := json.Unmarshal(pair[0], &key); err != nil {
			continue
		}
		var val *string
		if err := json.Unmarshal(pair[1], &val); err != nil || val == nil {
			obj.Set(key, Undefined())
		} else {
			obj.Set(key, String(*val))
		}
	}
	return obj
}

func decodeMatch(m *encodedMatch) Value {
	elems := make([]Value, len(m.C))
	for i, cp := range m.C {
		if cp == nil {
			elems[i] = Undefined()
		} else {
			elems[i] = String(*cp)
		}
	}
	arr := NewArray(elems...)
	props := &Object{props: map[string]Value{}}
	pv := Value{kind: KindObject, obj: props}
	pv.Set("index", Number(float64(m.I)))
	pv.Set("input", String(m.S))
	if m.G != nil {
		pv.Set("groups", decodeGroupPairs(m.G))
	} else {
		pv.Set("groups", Undefined())
	}
	if m.D != nil {
		ielems := make([]Value, len(m.D.A))
		for i, pair := range m.D.A {
			if pair == nil {
				ielems[i] = Undefined()
			} else {
				ielems[i] = NewArray(Number(float64(pair[0])), Number(float64(pair[1])))
			}
		}
		iarr := NewArray(ielems...)
		iprops := &Object{props: map[string]Value{}}
		ipv := Value{kind: KindObject, obj: iprops}
		if m.D.G != nil {
			ig := NewObject()
			for _, pair := range m.D.G {
				var key string
				if err := json.Unmarshal(pair[0], &key); err != nil {
					continue
				}
				var span *[2]int
				if err := json.Unmarshal(pair[1], &span); err != nil || span == nil {
					ig.Set(key, Undefined())
				} else {
					ig.Set(key, NewArray(Number(float64(span[0])), Number(float64(span[1]))))
				}
			}
			ipv.Set("groups", ig)
		} else {
			ipv.Set("groups", Undefined())
		}
		iarr.arr.Props = iprops
		pv.Set("indices", iarr)
	}
	arr.arr.Props = props
	return arr
}

// Compile cache and pooled QuickJS VM

type compiledRegExp struct {
	source string
	flags  string
	err    string
}

var (
	regexCompileMu    sync.RWMutex
	regexCompileCache = map[string]compiledRegExp{}
)

const maxRegexCompileCache = 1024

func compileRegExp(pattern, flags string) (string, string, error) {
	key := flags + "\x00" + pattern
	regexCompileMu.RLock()
	if c, ok := regexCompileCache[key]; ok {
		regexCompileMu.RUnlock()
		if c.err != "" {
			return "", "", fmt.Errorf("%s", c.err)
		}
		return c.source, c.flags, nil
	}
	regexCompileMu.RUnlock()

	raw, err := callRegexVM("compile", pattern, flags, 0, "", "", -1)
	if err != nil {
		msg := cleanRegexErr(err)
		storeCompiledRegExp(key, compiledRegExp{err: msg})
		return "", "", fmt.Errorf("%s", msg)
	}
	var res struct {
		S string `json:"s"`
		F string `json:"f"`
	}
	if err := json.Unmarshal([]byte(raw), &res); err != nil {
		return "", "", fmt.Errorf("invalid regular expression: %s", err.Error())
	}
	storeCompiledRegExp(key, compiledRegExp{source: res.S, flags: res.F})
	return res.S, res.F, nil
}

func storeCompiledRegExp(key string, c compiledRegExp) {
	regexCompileMu.Lock()
	if len(regexCompileCache) >= maxRegexCompileCache {
		clear(regexCompileCache)
	}
	regexCompileCache[key] = c
	regexCompileMu.Unlock()
}

var regexVMPool = make(chan *jsruntime.Runtime, max(1, runtime.GOMAXPROCS(0)))

func acquireRegexVM() (*jsruntime.Runtime, error) {
	select {
	case rt := <-regexVMPool:
		return rt, nil
	default:
		rt, err := jsruntime.NewBare()
		if err != nil {
			return nil, err
		}
		if _, err := rt.Execute(regexHelperJS); err != nil {
			_ = rt.Close()
			return nil, err
		}
		return rt, nil
	}
}

func releaseRegexVM(rt *jsruntime.Runtime, healthy bool) {
	if !healthy {
		_ = rt.Close()
		return
	}
	select {
	case regexVMPool <- rt:
	default:
		_ = rt.Close()
	}
}

func callRegexVM(op, source, flags string, lastIndex float64, input, extra string, limit int64) (string, error) {
	rt, err := acquireRegexVM()
	if err != nil {
		return "", fmt.Errorf("regex JS engine unavailable: %w", err)
	}
	if math.IsNaN(lastIndex) || lastIndex < 0 {
		lastIndex = 0
	}
	res, err := rt.Call("__krateRegex", op, source, flags, lastIndex, input, extra, limit)
	if err != nil {
		msg := cleanRegexErr(err)
		healthy := strings.HasPrefix(msg, "SyntaxError:") || strings.HasPrefix(msg, "TypeError:") || strings.HasPrefix(msg, "RangeError:")
		releaseRegexVM(rt, healthy)
		return "", fmt.Errorf("%s", msg)
	}
	releaseRegexVM(rt, true)
	s, ok := res.(string)
	if !ok {
		return "", fmt.Errorf("regex JS engine returned non-string result")
	}
	return s, nil
}

func cleanRegexErr(err error) string {
	msg := strings.TrimSpace(err.Error())
	if i := strings.IndexByte(msg, '\n'); i >= 0 {
		msg = strings.TrimSpace(msg[:i])
	}
	return msg
}

const regexHelperJS = `(function() {
  "use strict";
  const cache = new Map();
  const MAX_CACHE = 512;

  function getRE(source, flags) {
    const key = flags + "\0" + source;
    let re = cache.get(key);
    if (!re) {
      re = new RegExp(source, flags);
      if (cache.size >= MAX_CACHE) {
        cache.clear();
      }
      cache.set(key, re);
    }
    return re;
  }

  function encodeMatch(m) {
    if (m === null) return null;
    const caps = new Array(m.length);
    for (let i = 0; i < m.length; i++) {
      caps[i] = m[i] === undefined ? null : m[i];
    }
    let groups = null;
    if (m.groups !== undefined) {
      groups = [];
      for (const k of Object.keys(m.groups)) {
        const v = m.groups[k];
        groups.push([k, v === undefined ? null : v]);
      }
    }
    let indices = null;
    if (m.indices !== undefined) {
      const iarr = new Array(m.indices.length);
      for (let i = 0; i < m.indices.length; i++) {
        iarr[i] = m.indices[i] === undefined ? null : m.indices[i];
      }
      let igroups = null;
      if (m.indices.groups !== undefined) {
        igroups = [];
        for (const k of Object.keys(m.indices.groups)) {
          const v = m.indices.groups[k];
          igroups.push([k, v === undefined ? null : v]);
        }
      }
      indices = { a: iarr, g: igroups };
    }
    return { c: caps, i: m.index, s: m.input, g: groups, d: indices };
  }

  globalThis.__krateRegex = function(op, source, flags, lastIndex, input, extra, limit) {
    if (op === "compile") {
      const re = new RegExp(source, flags);
      return JSON.stringify({ s: re.source, f: re.flags });
    }
    const re = getRE(source, flags);
    if (re.global || re.sticky) {
      re.lastIndex = lastIndex;
    } else {
      re.lastIndex = 0;
    }
    if (op === "exec") {
      const m = re.exec(input);
      return JSON.stringify({ m: encodeMatch(m), l: re.lastIndex });
    }
    if (op === "test") {
      const ok = re.test(input);
      return JSON.stringify({ b: ok, l: re.lastIndex });
    }
    if (op === "match") {
      if (re.global) {
        re.lastIndex = 0;
        const m = input.match(re);
        return JSON.stringify({ a: m, l: re.lastIndex });
      }
      const m = input.match(re);
      return JSON.stringify({ m: encodeMatch(m), l: re.lastIndex });
    }
    if (op === "matchAll") {
      if (!re.global) {
        throw new TypeError("String.prototype.matchAll called with a non-global RegExp argument");
      }
      const out = [];
      for (const m of input.matchAll(re)) {
        out.push(encodeMatch(m));
        if (out.length > 1000000) {
          throw new RangeError("matchAll match limit exceeded");
        }
      }
      return JSON.stringify({ a: out });
    }
    if (op === "search") {
      const idx = input.search(re);
      return JSON.stringify({ i: idx });
    }
    if (op === "split") {
      const arr = limit < 0 ? input.split(re) : input.split(re, limit);
      const out = new Array(arr.length);
      for (let i = 0; i < arr.length; i++) {
        out[i] = arr[i] === undefined ? null : arr[i];
      }
      return JSON.stringify({ a: out });
    }
    if (op === "replace") {
      const res = input.replace(re, extra);
      return JSON.stringify({ r: res, l: re.lastIndex });
    }
    if (op === "replaceMatches") {
      const out = [];
      input.replace(re, function(...args) {
        const last = args[args.length - 1];
        const hasGroups = typeof last === "object" && last !== null;
        const endCaps = hasGroups ? args.length - 3 : args.length - 2;
        const caps = new Array(endCaps);
        for (let i = 0; i < endCaps; i++) {
          caps[i] = args[i] === undefined ? null : args[i];
        }
        const offset = args[endCaps];
        let groups = null;
        if (hasGroups) {
          groups = [];
          for (const k of Object.keys(last)) {
            const v = last[k];
            groups.push([k, v === undefined ? null : v]);
          }
        }
        out.push({ c: caps, i: offset, g: groups });
        if (out.length > 1000000) {
          throw new RangeError("replace match limit exceeded");
        }
        return "";
      });
      return JSON.stringify({ a: out, l: re.lastIndex });
    }
    throw new Error("unknown regex op: " + op);
  };
})();`
