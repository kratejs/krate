//go:build js

package qjs

import (
	"errors"
	"testing"
)

func TestHostFuncError(t *testing.T) {
	vm, err := NewVM()
	if err != nil {
		t.Fatal(err)
	}
	defer vm.Close()
	if err := vm.RegisterHostFunc("__qjs_test_fail", func([]any) (any, error) { return nil, errors.New("boom") }); err != nil {
		t.Fatal(err)
	}
	got, err := vm.Eval("try { __qjs_test_fail(); 'none' } catch (e) { e.message }", EvalGlobal)
	if err != nil || got != "boom" {
		t.Errorf("host error = %v, %v", got, err)
	}
	vm.Close()
	if got, _ := vm.Eval("typeof __qjs_test_fail", EvalGlobal); got != "undefined" {
		t.Errorf("Close left the host function behind: %v", got)
	}
}
