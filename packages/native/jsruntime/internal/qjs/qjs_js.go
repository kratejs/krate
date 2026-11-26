//go:build js

package qjs

import (
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"sync"
	"syscall/js"
	"time"
)

const (
	// EvalGlobal evaluates in the global scope.
	EvalGlobal = 0
	// EvalModule evaluates as an ES module. Modules are approximated by a
	// strict function wrapper, so import and export statements fail.
	EvalModule = 1 << 0
)

// Undefined is the Go form of the JavaScript value undefined.
type Undefined struct{}

// String implements fmt.Stringer.
func (Undefined) String() string { return "undefined" }

// Unsupported is the Go form of a JavaScript value with no Go equivalent.
type Unsupported struct{}

// String implements fmt.Stringer.
func (Unsupported) String() string { return "unsupported Javascript type" }

// Object holds the JSON form of a JavaScript object result.
type Object struct{ json string }

// String implements fmt.Stringer.
func (o *Object) String() string { return o.json }

// MarshalJSON implements json.Marshaler.
func (o *Object) MarshalJSON() ([]byte, error) { return []byte(o.json), nil }

// Into deserializes the object into v with encoding/json.
func (o *Object) Into(v any) error { return json.Unmarshal([]byte(o.json), v) }

// Atom is a property name.
type Atom string

// HostFunc is a Go function exposed to JavaScript.
type HostFunc func(args []any) (any, error)

// Value is a handle to a JavaScript value. The zero Value is undefined.
type Value struct {
	vm *VM
	v  js.Value
}

// UndefinedValue is a Value holding undefined.
var UndefinedValue = Value{}

// ValueOf wraps a host JS value as a Value of vm.
func ValueOf(vm *VM, v js.Value) Value { return Value{vm: vm, v: v} }

// JS returns the host value.
func (v Value) JS() js.Value { return v.v }

// VM returns the VM the value belongs to.
func (v *Value) VM() *VM { return v.vm }

// Free is a no-op: the host engine owns the value.
func (v *Value) Free() {}

// Dup returns the same value.
func (v Value) Dup() Value { return v }

// IsUndefined reports whether v is undefined.
func (v Value) IsUndefined() bool { return v.v.IsUndefined() }

// Any converts v to a Go value.
func (v Value) Any() (any, error) { return fromJS(v.v) }

// MarshalJSON implements json.Marshaler.
func (v Value) MarshalJSON() ([]byte, error) {
	s, err := stringify(v.v)
	return []byte(s), err
}

// SetProperty sets a property of v from a Go value.
func (v Value) SetProperty(prop Atom, val any) error {
	jv, err := toJS(val)
	if err != nil {
		return err
	}
	return try(func() { v.v.Set(string(prop), jv) })
}

// SetPropertyValue sets a property of v.
func (v Value) SetPropertyValue(prop Atom, val Value) error {
	return try(func() { v.v.Set(string(prop), val.v) })
}

// GetPropertyValue returns a property of v.
func (v Value) GetPropertyValue(prop Atom) (r Value, err error) {
	err = try(func() { r = Value{vm: v.vm, v: v.v.Get(string(prop))} })
	return r, err
}

// GetProperty returns a property of this as a Go value.
func (v Value) GetProperty(this Value, prop Atom) (any, error) {
	return v.vm.GetProperty(this, prop)
}

// VM is a JavaScript engine instance backed by the host engine.
type VM struct {
	mu     sync.Mutex
	funcs  []js.Func
	names  map[string]js.Value
	closed bool
}

// NewVM creates a VM.
func NewVM() (*VM, error) {
	if err := ensureHelpers(); err != nil {
		return nil, err
	}
	return &VM{names: map[string]js.Value{}}, nil
}

// SetMemoryLimit is a no-op on the host engine.
func (m *VM) SetMemoryLimit(uintptr) {}

// SetMaxStackSize is a no-op on the host engine.
func (m *VM) SetMaxStackSize(uintptr) {}

// SetEvalTimeout is a no-op on the host engine.
func (m *VM) SetEvalTimeout(time.Duration) error { return nil }

// Close releases the host functions this VM registered.
func (m *VM) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return nil
	}
	m.closed = true
	g := js.Global()
	for name, wrapper := range m.names {
		if g.Get(name).Equal(wrapper) {
			g.Delete(name)
		}
	}
	for _, f := range m.funcs {
		f.Release()
	}
	m.funcs, m.names = nil, nil
	return nil
}

const bytecodeMagic = 1

// Eval evaluates javascript in the global scope of the host engine.
func (m *VM) Eval(javascript string, flags int) (any, error) {
	v, err := m.EvalValue(javascript, flags)
	if err != nil {
		return nil, err
	}
	return v.Any()
}

// EvalValue is Eval returning a Value.
func (m *VM) EvalValue(javascript string, flags int) (r Value, err error) {
	if flags&EvalModule != 0 {
		javascript = "(function(){\"use strict\";\n" + javascript + "\n})()"
	}
	err = try(func() { r = Value{vm: m, v: helpers.Get("eval").Invoke(javascript)} })
	return r, err
}

// Compile returns a form of the script EvalBytecode accepts. The host engine
// has no portable bytecode, so it is the source text with a flag byte.
func (m *VM) Compile(javascript string, flags int) ([]byte, error) {
	if flags&EvalModule != 0 {
		javascript = "(function(){\"use strict\";\n" + javascript + "\n})()"
	}
	return append([]byte{bytecodeMagic}, javascript...), nil
}

// EvalBytecode evaluates the output of Compile.
func (m *VM) EvalBytecode(bytecode []byte) (any, error) {
	if len(bytecode) == 0 || bytecode[0] != bytecodeMagic {
		return nil, errors.New("invalid bytecode")
	}
	return m.Eval(string(bytecode[1:]), EvalGlobal)
}

// ExecutePendingJobs reports no jobs: promise jobs run when the host event
// loop does, which cannot happen inside a synchronous Go call.
func (m *VM) ExecutePendingJobs() (int, error) { return 0, nil }

// GlobalObject returns the host global object.
func (m *VM) GlobalObject() Value { return Value{vm: m, v: js.Global()} }

// NewString returns a string Value.
func (m *VM) NewString(s string) (Value, error) { return Value{vm: m, v: js.ValueOf(s)}, nil }

// NewInt returns a number Value.
func (m *VM) NewInt(n int) Value { return Value{vm: m, v: js.ValueOf(n)} }

// NewFloat64 returns a number Value.
func (m *VM) NewFloat64(n float64) Value { return Value{vm: m, v: js.ValueOf(n)} }

// NewAtom returns the property name s.
func (m *VM) NewAtom(s string) (Atom, error) { return Atom(s), nil }

// GetProperty returns a property of this as a Go value.
func (m *VM) GetProperty(this Value, prop Atom) (r any, err error) {
	err = try(func() { r, err = fromJS(this.v.Get(string(prop))) })
	return r, err
}

// Call calls the global function named function (a dotted path is allowed).
func (m *VM) Call(function string, args ...any) (any, error) {
	v, err := m.CallValue(function, args...)
	if err != nil {
		return nil, err
	}
	return v.Any()
}

// CallValue is Call returning a Value.
func (m *VM) CallValue(function string, args ...any) (r Value, err error) {
	jsArgs := make([]any, len(args))
	for i, a := range args {
		jv, cerr := toJS(a)
		if cerr != nil {
			return Value{}, cerr
		}
		jsArgs[i] = jv
	}
	err = try(func() {
		owner, name := js.Global(), function
		if i := strings.LastIndexByte(function, '.'); i >= 0 {
			owner = js.Global()
			for _, p := range strings.Split(function[:i], ".") {
				owner = owner.Get(p)
			}
			name = function[i+1:]
		}
		if owner.Get(name).Type() != js.TypeFunction {
			panic(fmt.Errorf("%s is not a function", function))
		}
		r = Value{vm: m, v: owner.Call(name, jsArgs...)}
	})
	return r, err
}

// RegisterHostFunc exposes fn as a global function; fn gets every call
// argument converted to Go and a returned error is thrown as a TypeError.
func (m *VM) RegisterHostFunc(name string, fn HostFunc) error {
	if fn == nil {
		return fmt.Errorf("host func %q cannot be nil", name)
	}
	return m.register(name, func(args []js.Value) (js.Value, error) {
		goArgs := make([]any, len(args))
		for i, a := range args {
			v, err := fromJS(a)
			if err != nil {
				return js.Undefined(), err
			}
			goArgs[i] = v
		}
		out, err := fn(goArgs)
		if err != nil {
			return js.Undefined(), err
		}
		return toJS(out)
	})
}

// RegisterFunc exposes f as a global function. Only the shapes jsruntime uses
// are supported: func(...Value) Value and func(Value, []any) (any, error).
func (m *VM) RegisterFunc(name string, f any, wantThis bool) error {
	switch fn := f.(type) {
	case func(...Value) Value:
		return m.register(name, func(args []js.Value) (js.Value, error) {
			vals := make([]Value, len(args))
			for i, a := range args {
				vals[i] = Value{vm: m, v: a}
			}
			return fn(vals...).v, nil
		})
	case func(Value, []any) (any, error):
		return m.register(name, func(args []js.Value) (js.Value, error) {
			goArgs := make([]any, len(args))
			for i, a := range args {
				v, err := fromJS(a)
				if err != nil {
					return js.Undefined(), err
				}
				goArgs[i] = v
			}
			out, err := fn(Value{vm: m}, goArgs)
			if err != nil {
				return js.Undefined(), err
			}
			return toJS(out)
		})
	}
	return fmt.Errorf("RegisterFunc %q: unsupported function type %T on js/wasm", name, f)
}

// register installs a global function whose Go body reports failure as an
// error; the JS wrapper rethrows it, since a Go panic would end the program.
func (m *VM) register(name string, body func(args []js.Value) (js.Value, error)) error {
	if name == "" {
		return errors.New("function name cannot be empty")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return errors.New("VM is closed")
	}
	fn := js.FuncOf(func(this js.Value, args []js.Value) (ret any) {
		defer func() {
			if rec := recover(); rec != nil {
				ret = helpers.Get("fail").Invoke(fmt.Sprintf("host function %q panicked: %v", name, rec))
			}
		}()
		v, err := body(args)
		if err != nil {
			return helpers.Get("fail").Invoke(err.Error())
		}
		return helpers.Get("ok").Invoke(v)
	})
	wrapper := helpers.Get("wrap").Invoke(fn, name)
	m.funcs = append(m.funcs, fn)
	m.names[name] = wrapper
	js.Global().Set(name, wrapper)
	return nil
}

var (
	helpersOnce sync.Once
	helpers     js.Value
	helpersErr  error
)

// helperSource builds the small JS toolkit: indirect eval, a syntax check, the
// result envelope that lets Go report errors as exceptions, and conversion
// helpers. Created once; it needs the host to allow Function().
const helperSource = `
const evalFn = globalThis.eval;
return {
  eval: function (src) { return evalFn(src); },
  ok: function (v) { return [0, v]; },
  fail: function (msg) { return [1, msg]; },
  wrap: function (goFn, name) {
    const f = function () {
      const r = goFn.apply(this, arguments);
      if (r[0] === 1) throw new TypeError(r[1]);
      return r[1];
    };
    Object.defineProperty(f, 'name', { value: name });
    return f;
  },
  kind: function (v) {
    if (v === null) return 'null';
    const t = typeof v;
    if (t === 'number') {
      if (Number.isInteger(v) && v >= -2147483648 && v <= 2147483647 && !Object.is(v, -0)) return 'int';
      return 'float';
    }
    if (t === 'bigint') return 'bigint';
    return t;
  },
  json: function (v) {
    const s = JSON.stringify(v);
    return s === undefined ? 'undefined' : s;
  },
  fromJSON: function (s) { return JSON.parse(s); },
  bigint: function (s) { return BigInt(s); },
  bytes: function (n) { return new Uint8Array(n); },
  errString: function (e) {
    try { return String(e); } catch (_) { return 'unknown error'; }
  },
};`

func ensureHelpers() error {
	helpersOnce.Do(func() {
		err := try(func() {
			helpers = js.Global().Get("Function").New(helperSource).Invoke()
		})
		if err != nil {
			helpersErr = fmt.Errorf("the JS host does not allow Function(): %w", err)
		}
	})
	return helpersErr
}

// try runs f and turns a JS exception (a js.Error panic) or a panicked error
// into an error carrying the exception text.
func try(f func()) (err error) {
	defer func() {
		switch rec := recover().(type) {
		case nil:
		case js.Error:
			err = errors.New(errText(rec.Value))
		case error:
			err = rec
		default:
			panic(rec)
		}
	}()
	f()
	return nil
}

func errText(v js.Value) string {
	if helpers.IsUndefined() {
		return "JavaScript error"
	}
	return helpers.Get("errString").Invoke(v).String()
}

func stringify(v js.Value) (s string, err error) {
	err = try(func() { s = helpers.Get("json").Invoke(v).String() })
	return s, err
}

// fromJS converts a host value the way QuickJS results convert: numbers to int
// or float64, objects to *Object holding their JSON.
func fromJS(v js.Value) (r any, err error) {
	err = try(func() {
		switch kind := helpers.Get("kind").Invoke(v).String(); kind {
		case "null":
			r = nil
		case "undefined":
			r = Undefined{}
		case "boolean":
			r = v.Bool()
		case "string":
			r = v.String()
		case "int":
			r = v.Int()
		case "float":
			r = v.Float()
		case "bigint":
			n := new(big.Int)
			if _, ok := n.SetString(v.Call("toString", 16).String(), 16); !ok {
				panic(errors.New("big.Int.SetString failed"))
			}
			r = n
		case "object", "function":
			s, serr := stringify(v)
			if serr != nil {
				panic(serr)
			}
			r = &Object{json: s}
		default:
			r = Unsupported{}
		}
	})
	return r, err
}

// toJS converts a Go value to a host value.
func toJS(a any) (js.Value, error) {
	switch x := a.(type) {
	case nil:
		return js.Null(), nil
	case Undefined:
		return js.Undefined(), nil
	case Value:
		return x.v, nil
	case js.Value:
		return x, nil
	case bool, string, int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64, float32, float64:
		return js.ValueOf(x), nil
	case []byte:
		u8 := helpers.Get("bytes").Invoke(len(x))
		js.CopyBytesToJS(u8, x)
		return u8, nil
	case *big.Int:
		return helpers.Get("bigint").Invoke(x.String()), nil
	case *Object:
		return fromJSON(x.json)
	case []any:
		arr := js.Global().Get("Array").New(len(x))
		for i, e := range x {
			jv, err := toJS(e)
			if err != nil {
				return js.Undefined(), err
			}
			arr.SetIndex(i, jv)
		}
		return arr, nil
	case map[string]any:
		obj := js.Global().Get("Object").New()
		for k, e := range x {
			jv, err := toJS(e)
			if err != nil {
				return js.Undefined(), err
			}
			obj.Set(k, jv)
		}
		return obj, nil
	}
	b, err := json.Marshal(a)
	if err != nil {
		return js.Undefined(), fmt.Errorf("cannot convert %T to a JavaScript value: %w", a, err)
	}
	return fromJSON(string(b))
}

func fromJSON(s string) (r js.Value, err error) {
	err = try(func() { r = helpers.Get("fromJSON").Invoke(s) })
	return r, err
}
