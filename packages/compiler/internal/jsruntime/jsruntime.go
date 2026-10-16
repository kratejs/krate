package jsruntime

import (
	"fmt"
	"io"
	"os"
	"strconv"
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
	vm        *quickjs.VM
	logPrefix string // optional prefix for console output (e.g. "[plugin:demo]")
}

// SetLogPrefix sets a prefix prepended to every console line. Plugin hosts set
// it so console.log/error/warn output is attributable to the plugin that ran.
func (r *Runtime) SetLogPrefix(prefix string) { r.logPrefix = prefix }

// LogPrefix returns the current console prefix.
func (r *Runtime) LogPrefix() string { return r.logPrefix }

// writeLog prints one console line to w, applying the tag and log prefix.
func (r *Runtime) writeLog(w io.Writer, tag string, args []any) {
	line := tag + fmt.Sprint(args...)
	if r.logPrefix != "" {
		line = r.logPrefix + " " + line
	}
	fmt.Fprintln(w, line)
}

// Log writes a console-style line to stdout.
func (r *Runtime) Log(args []any) { r.writeLog(os.Stdout, "", args) }

// Warn writes a warning line to stderr.
func (r *Runtime) Warn(args []any) { r.writeLog(os.Stderr, "WARN: ", args) }

// Error writes an error line to stderr.
func (r *Runtime) Error(args []any) { r.writeLog(os.Stderr, "ERROR: ", args) }

// New creates a new JS runtime with Web API polyfills
func New() (*Runtime, error) {
	vm, err := quickjs.NewVM()
	if err != nil {
		return nil, fmt.Errorf("failed to create JS VM: %w", err)
	}

	// Resource limits are configurable so an unusual project can raise them:
	// KRATE_JS_MEMORY_MB (default 32), KRATE_JS_STACK_KB (default 256),
	// KRATE_JS_TIMEOUT (Go duration, default 30s).
	vm.SetMemoryLimit(envUnits("KRATE_JS_MEMORY_MB", 32, 1<<20))
	vm.SetMaxStackSize(envUnits("KRATE_JS_STACK_KB", 256, 1024))

	if err := vm.SetEvalTimeout(envDuration("KRATE_JS_TIMEOUT", 30*time.Second)); err != nil {
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

// envUnits reads a positive integer from key and multiplies it by unit, falling
// back to def*unit when unset/invalid.
func envUnits(key string, def int, unit int) uintptr {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			return uintptr(n * unit)
		}
	}
	return uintptr(def * unit)
}

// envDuration reads a positive Go duration from key, falling back to def.
func envDuration(key string, def time.Duration) time.Duration {
	if v := os.Getenv(key); v != "" {
		if d, err := time.ParseDuration(v); err == nil && d > 0 {
			return d
		}
	}
	return def
}

// Execute evaluates JavaScript code and returns the result
func (r *Runtime) Execute(code string) (any, error) {
	return r.vm.Eval(code, EvalGlobal)
}

// Compile compiles JavaScript to quickjs bytecode for later evaluation with
// EvalBytecode. Caching bytecode lets a long-lived VM skip re-parsing a script
// on every call.
func (r *Runtime) Compile(code string, flags int) ([]byte, error) {
	return r.vm.Compile(code, flags)
}

// EvalBytecode evaluates previously compiled quickjs bytecode.
func (r *Runtime) EvalBytecode(bytecode []byte) (any, error) {
	return r.vm.EvalBytecode(bytecode)
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

// RegisterFunc registers a Go function as a global JS function. A panic in the
// host function is converted into a JS-catchable error rather than unwinding
// across the QuickJS FFI boundary.
func (r *Runtime) RegisterFunc(name string, f func(args []any) (any, error)) error {
	return r.vm.RegisterHostFunc(name, func(args []any) (res any, err error) {
		defer func() {
			if rec := recover(); rec != nil {
				err = fmt.Errorf("host function %q panicked: %v", name, rec)
			}
		}()
		return f(args)
	})
}

// RegisterFuncWithThis registers a Go function that receives the this object
// (with the same panic containment as RegisterFunc).
func (r *Runtime) RegisterFuncWithThis(name string, f func(this quickjs.Value, args []any) (any, error)) error {
	return r.vm.RegisterFunc(name, func(this quickjs.Value, args []any) (res any, err error) {
		defer func() {
			if rec := recover(); rec != nil {
				err = fmt.Errorf("host function %q panicked: %v", name, rec)
			}
		}()
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
// available - so SSR evaluation of globals like Date.now() produces a genuine
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
