package jsruntime

import (
	"fmt"
	"io"
	"os"
	"runtime"
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
	vm       *quickjs.VM
	memLimit uintptr
	// curBody is the request body of the in-flight API request; the JS side
	// reads it lazily through the __go_body_* host functions.
	curBody   []byte
	logPrefix string // optional prefix for console output (e.g. "[plugin:demo]")
	// dataCache, when set, caches fetch responses requested with Next-style
	// options ({ next: { revalidate, tags } } / cache: "force-cache").
	dataCache *DataCache
	// loaded records the prelude features installed so far, hosts the host
	// functions registered (both per VM).
	loaded map[string]bool
	hosts  map[string]bool
	// bin is set when bytes cross the boundary as ArrayBuffers (qjsbytes.go)
	// rather than base64. hostErr is the failure of the last binary host
	// call, take the bytes it left for __go_take.
	bin     bool
	hostErr string
	take    []byte
	// headroom is the current allowance above memLimit.
	headroom uintptr
	// timeout is the configured eval timeout. deadline is when the outermost
	// evaluation in progress (depth > 0) times out; see enter.
	timeout  time.Duration
	deadline time.Time
	depth    int
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

// Options configures a runtime created by NewWithOptions.
type Options struct {
	// Preload lists the web API prelude features (compat.json feature names)
	// installed when the VM is created; names outside the prelude are ignored.
	// Every other feature loads the first time one of its globals is read.
	Preload []string
	// PreloadAll installs every prelude feature up front.
	PreloadAll bool
}

// New creates a JS runtime with the web API prelude, Node's process.env and a
// minimal require. Prelude features load on first use.
func New() (*Runtime, error) {
	return NewWithOptions(Options{})
}

// NewWithOptions is New with control over which prelude features are
// installed up front.
func NewWithOptions(o Options) (*Runtime, error) {
	r, err := newRuntime()
	if err != nil {
		return nil, err
	}
	preload := o.Preload
	if o.PreloadAll {
		preload = nil
	} else if preload == nil {
		preload = []string{}
	}
	if err := r.installPrelude(preload); err != nil {
		r.vm.Close()
		return nil, fmt.Errorf("failed to install web APIs: %w", err)
	}
	if err := r.injectNodeGlobals(); err != nil {
		r.vm.Close()
		return nil, fmt.Errorf("failed to inject Node globals: %w", err)
	}
	return r, nil
}

// newRuntime creates a VM with the configured resource limits and no globals
// beyond the engine's.
func newRuntime() (*Runtime, error) {
	vm, err := quickjs.NewVM()
	if err != nil {
		return nil, fmt.Errorf("failed to create JS VM: %w", err)
	}

	// Resource limits are configurable so an unusual project can raise them:
	// KRATE_JS_MEMORY_MB (default 32), KRATE_JS_STACK_KB (default 256),
	// KRATE_JS_TIMEOUT (Go duration, default 30s).
	memLimit := envUnits("KRATE_JS_MEMORY_MB", 32, 1<<20)
	vm.SetMemoryLimit(memLimit)
	vm.SetMaxStackSize(envUnits("KRATE_JS_STACK_KB", 256, 1024))

	timeout := envDuration("KRATE_JS_TIMEOUT", 30*time.Second)
	if err := vm.SetEvalTimeout(timeout); err != nil {
		vm.Close()
		return nil, fmt.Errorf("failed to set eval timeout: %w", err)
	}
	r := &Runtime{vm: vm, memLimit: memLimit, timeout: timeout, loaded: map[string]bool{}, hosts: map[string]bool{}, bin: binaryTransfer && !forceBase64}
	// core.js reads (and deletes) the flag to choose how bytes cross.
	if err := r.SetGlobal("__krate_bin", r.bin); err != nil {
		vm.Close()
		return nil, fmt.Errorf("failed to set up the JS VM: %w", err)
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

// setMemoryHeadroom raises (or restores, with 0) the VM memory limit so a
// request with a large body fits beside the configured base limit.
func (r *Runtime) setMemoryHeadroom(extra uintptr) {
	r.headroom = extra
	r.vm.SetMemoryLimit(r.memLimit + extra)
}

// Request body headroom, as multiples of the body length, granted when the
// handler first reads the body in that form. Measured with 8 MiB bodies.
const (
	// Bytes: the body as an ArrayBuffer (1x) plus the copy a handler that
	// echoes it makes (new Response(buffer) copies).
	headroomBytes = 2
	// Text, and everything in the base64 fallback: echoing non-Latin-1 text
	// back peaks at about 4.2x (a UTF-16 string of up to 2x, the JSON head
	// that carries it to Go and its UTF-8 copy); base64 text plus the decoded
	// bytes and their copy stays under 2.5x.
	headroomText = 5
)

// growBodyHeadroom makes room for the in-flight request body read in a form
// needing factor times its length.
func (r *Runtime) growBodyHeadroom(factor uintptr) {
	if !r.bin {
		factor = headroomText
	}
	if h := uintptr(len(r.curBody)) * factor; h > r.headroom {
		r.setMemoryHeadroom(h)
	}
}

// enter brackets every evaluation; use it as defer r.leave(r.enter()). The
// bindings re-arm the VM deadline to now+timeout on each evaluation. That is
// right for an outermost one, but an evaluation nested inside another (a web
// API module loaded from a host function) must not extend the enclosing
// deadline, so it is given only the time the outermost one has left.
func (r *Runtime) enter() (nested bool) {
	r.depth++
	if r.depth == 1 {
		r.deadline = time.Now().Add(r.timeout)
		return false
	}
	rem := time.Until(r.deadline)
	if rem <= 0 {
		// Zero would disable the timeout.
		rem = time.Nanosecond
	}
	_ = r.vm.SetEvalTimeout(rem)
	return true
}

func (r *Runtime) leave(nested bool) {
	r.depth--
	if nested {
		_ = r.vm.SetEvalTimeout(r.timeout)
	}
}

// Execute evaluates JavaScript code and returns the result
func (r *Runtime) Execute(code string) (any, error) {
	defer r.leave(r.enter())
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
	defer r.leave(r.enter())
	return r.vm.EvalBytecode(bytecode)
}

// DrainJobs executes pending promise jobs (microtasks). Call after Execute to resolve promises.
func (r *Runtime) DrainJobs() {
	for {
		n, err := r.executePendingJobs()
		if n == 0 || err != nil {
			break
		}
	}
}

func (r *Runtime) executePendingJobs() (int, error) {
	defer r.leave(r.enter())
	return r.vm.ExecutePendingJobs()
}

// ExecuteModule evaluates JavaScript code as an ES module
func (r *Runtime) ExecuteModule(code string) (any, error) {
	defer r.leave(r.enter())
	return r.vm.Eval(code, EvalModule)
}

// Call calls a global function with the given arguments
func (r *Runtime) Call(function string, args ...any) (any, error) {
	defer r.leave(r.enter())
	return r.vm.Call(function, args...)
}

// CallValue calls a global function and returns a Value
func (r *Runtime) CallValue(function string, args ...any) (quickjs.Value, error) {
	defer r.leave(r.enter())
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

// exprVMPool holds idle expression-eval VMs (NewBare). Creating a QuickJS VM is
// the dominant cost of EvaluateExpr, so VMs are pooled and reused across calls. A VM is handed to one goroutine at a time (QuickJS VMs are
// not concurrency-safe) and the pool is bounded to GOMAXPROCS idle VMs; under
// bursty concurrency extra calls simply create a fresh VM as before.
var exprVMPool = make(chan *Runtime, max(1, runtime.GOMAXPROCS(0)))

// exprVMPoolEnabled can be disabled with KRATE_NO_EXPR_VM_POOL (used for
// before/after measurement, and as an escape hatch).
func exprVMPoolEnabled() bool { return os.Getenv("KRATE_NO_EXPR_VM_POOL") == "" }

func acquireExprVM() (*Runtime, error) {
	if !exprVMPoolEnabled() {
		return NewBare()
	}
	select {
	case rt := <-exprVMPool:
		return rt, nil
	default:
		return NewBare()
	}
}

func releaseExprVM(rt *Runtime) {
	if !exprVMPoolEnabled() {
		_ = rt.Close()
		return
	}
	select {
	case exprVMPool <- rt:
	default:
		_ = rt.Close()
	}
}

// EvaluateExpr evaluates a self-contained JS expression for compile-time
// folding and returns its String() representation. It runs in a bare VM
// (NewBare): the ECMAScript built-ins (Math, String, Array, Object, JSON, ...)
// plus structuredClone and console, with no network, filesystem, randomness or
// request state, and with the locale- and time-zone-dependent built-ins
// throwing. Returns an error when the expression throws (e.g. it references an
// identifier that isn't defined).
//
// The expression is evaluated inside a strict IIFE so it cannot leak implicit
// globals into the pooled VM (a fresh VM previously discarded any such state).
// The eval-timeout deadline is re-armed on every Execute by the bindings, so a
// VM is safe to reuse even after a thrown expression or a timeout.
func EvaluateExpr(code string) (string, error) {
	rt, err := acquireExprVM()
	if err != nil {
		return "", fmt.Errorf("creating JS runtime: %w", err)
	}

	result, err := rt.Execute("String((function(){\"use strict\";return (\n" + code + "\n);})())")
	if err != nil {
		releaseExprVM(rt)
		return "", fmt.Errorf("evaluating expression: %w", err)
	}
	releaseExprVM(rt)

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
