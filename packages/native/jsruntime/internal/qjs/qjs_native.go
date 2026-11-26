//go:build !js

package qjs

import "modernc.org/quickjs"

const (
	// EvalGlobal evaluates in the global scope.
	EvalGlobal = quickjs.EvalGlobal
	// EvalModule evaluates as an ES module.
	EvalModule = quickjs.EvalModule
)

type (
	// VM is a JavaScript engine instance.
	VM = quickjs.VM
	// Value is a handle to a JavaScript value.
	Value = quickjs.Value
	// Object is the JSON form of a JavaScript object result.
	Object = quickjs.Object
	// Undefined is the Go form of the JavaScript value undefined.
	Undefined = quickjs.Undefined
	// Atom is an interned property name.
	Atom = quickjs.Atom
	// HostFunc is a Go function exposed to JavaScript.
	HostFunc = quickjs.HostFunc
)

// UndefinedValue is a Value holding undefined.
var UndefinedValue = quickjs.UndefinedValue

// NewVM creates a VM.
func NewVM() (*VM, error) { return quickjs.NewVM() }
