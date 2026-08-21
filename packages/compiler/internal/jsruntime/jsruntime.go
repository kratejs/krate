package jsruntime

import (
	"fmt"
	"time"

	"modernc.org/quickjs"
)

const (
	// EvalGlobal evaluates in the global scope
	EvalGlobal = quickjs.EvalGlobal
	// EvalModule evaluates as an ES module
	EvalModule = quickjs.EvalModule
)

// Runtime wraps a quickjs VM with krate-specific Web API polyfills
type Runtime struct {
	vm          *quickjs.VM
	projectRoot string // root directory for fs.readFile resolution
}

// New creates a new JS runtime with Web API polyfills
func New() (*Runtime, error) {
	vm, err := quickjs.NewVM()
	if err != nil {
		return nil, fmt.Errorf("failed to create JS VM: %w", err)
	}

	// Set memory limit (32MB default)
	vm.SetMemoryLimit(32 * 1024 * 1024)

	// Set stack size (256KB)
	vm.SetMaxStackSize(256 * 1024)

	// Set eval timeout (30 seconds)
	if err := vm.SetEvalTimeout(30 * time.Second); err != nil {
		vm.Close()
		return nil, fmt.Errorf("failed to set eval timeout: %w", err)
	}

	r := &Runtime{vm: vm}

	// Inject Web API polyfills
	if err := r.injectPolyfills(); err != nil {
		vm.Close()
		return nil, fmt.Errorf("failed to inject polyfills: %w", err)
	}

	return r, nil
}

// Execute evaluates JavaScript code and returns the result
func (r *Runtime) Execute(code string) (any, error) {
	return r.vm.Eval(code, EvalGlobal)
}

// DrainJobs executes pending promise jobs (microtasks). Call after Execute to resolve promises.
func (r *Runtime) DrainJobs() {
	for {
		n, err := r.vm.ExecutePendingJobs()
		if n == 0 || err != nil {
			break
		}
	}
}

// ExecuteModule evaluates JavaScript code as an ES module
func (r *Runtime) ExecuteModule(code string) (any, error) {
	return r.vm.Eval(code, EvalModule)
}

// Call calls a global function with the given arguments
func (r *Runtime) Call(function string, args ...any) (any, error) {
	return r.vm.Call(function, args...)
}

// CallValue calls a global function and returns a Value
func (r *Runtime) CallValue(function string, args ...any) (quickjs.Value, error) {
	return r.vm.CallValue(function, args...)
}

// SetGlobal sets a global variable
func (r *Runtime) SetGlobal(name string, value any) error {
	atom, err := r.vm.NewAtom(name)
	if err != nil {
		return err
	}

	global := r.vm.GlobalObject()
	defer global.Free()

	return global.SetProperty(atom, value)
}

// GetGlobal gets a global variable
func (r *Runtime) GetGlobal(name string) (any, error) {
	atom, err := r.vm.NewAtom(name)
	if err != nil {
		return nil, err
	}

	global := r.vm.GlobalObject()
	defer global.Free()

	return r.vm.GetProperty(global, atom)
}

// RegisterFunc registers a Go function as a global JS function
func (r *Runtime) RegisterFunc(name string, f func(args []any) (any, error)) error {
	return r.vm.RegisterHostFunc(name, func(args []any) (any, error) {
		return f(args)
	})
}

// RegisterFuncWithThis registers a Go function that receives the this object
func (r *Runtime) RegisterFuncWithThis(name string, f func(this quickjs.Value, args []any) (any, error)) error {
	return r.vm.RegisterFunc(name, func(this quickjs.Value, args []any) (any, error) {
		return f(this, args)
	}, true)
}

// VM returns the underlying quickjs VM
func (r *Runtime) VM() *quickjs.VM {
	return r.vm
}

// Close releases all resources
func (r *Runtime) Close() error {
	return r.vm.Close()
}

// ToNative converts a quickjs result to a native Go type.
// JS objects are JSON-deserialized; primitives pass through.
func ToNative(v any) any {
	switch val := v.(type) {
	case *quickjs.Object:
		var m any
		if err := val.Into(&m); err != nil {
			return val.String()
		}
		return m
	default:
		return v
	}
}

// EvaluateExpr evaluates a self-contained JS expression in a fresh QuickJS VM
// and returns its String() representation. Because QuickJS is a full ECMAScript
// engine, every built-in (Date, Math, String, Number, Array, Object, ...) is
// available — so SSR evaluation of globals like Date.now() produces a genuine
// JS-engine value rather than a Go approximation. Returns an error when the
// expression throws (e.g. it references an identifier that isn't defined).
func EvaluateExpr(code string) (string, error) {
	rt, err := New()
	if err != nil {
		return "", fmt.Errorf("creating JS runtime: %w", err)
	}
	defer rt.Close()

	result, err := rt.Execute("String(" + code + ")")
	if err != nil {
		return "", fmt.Errorf("evaluating expression: %w", err)
	}
	switch v := result.(type) {
	case string:
		return v, nil
	case nil:
		return "", nil
	default:
		return fmt.Sprintf("%v", v), nil
	}
}

// ToMap converts a quickjs object result to a Go map.
func ToMap(v any) (map[string]any, bool) {
	m, ok := ToNative(v).(map[string]any)
	return m, ok
}
