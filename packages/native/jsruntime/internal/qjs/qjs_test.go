package qjs

import (
	"strings"
	"testing"
)

func TestEvalResults(t *testing.T) {
	vm, err := NewVM()
	if err != nil {
		t.Fatal(err)
	}
	defer vm.Close()
	cases := []struct {
		src  string
		want any
	}{
		{"1 + 2", 3},
		{"1.5 * 2 + 0.25", 3.25},
		{"'a' + 'b'", "ab"},
		{"true", true},
		{"null", nil},
		{"undefined", Undefined{}},
	}
	for _, c := range cases {
		got, err := vm.Eval(c.src, EvalGlobal)
		if err != nil {
			t.Fatalf("%s: %v", c.src, err)
		}
		if got != c.want {
			t.Errorf("%s = %#v, want %#v", c.src, got, c.want)
		}
	}
	got, err := vm.Eval("({a: 1, b: [2, 3]})", EvalGlobal)
	if err != nil {
		t.Fatal(err)
	}
	obj, ok := got.(*Object)
	if !ok {
		t.Fatalf("object result is %T", got)
	}
	var m map[string]any
	if err := obj.Into(&m); err != nil || m["a"] != float64(1) {
		t.Errorf("Into = %v, %v", m, err)
	}
}

func TestEvalError(t *testing.T) {
	vm, err := NewVM()
	if err != nil {
		t.Fatal(err)
	}
	defer vm.Close()
	_, err = vm.Eval("throw new TypeError('boom')", EvalGlobal)
	if err == nil || !strings.Contains(err.Error(), "boom") {
		t.Errorf("error = %v, want one naming boom", err)
	}
	if _, err = vm.Eval("(", EvalGlobal); err == nil {
		t.Error("a syntax error did not fail")
	}
}

func TestHostFuncAndCall(t *testing.T) {
	vm, err := NewVM()
	if err != nil {
		t.Fatal(err)
	}
	defer vm.Close()
	err = vm.RegisterHostFunc("__qjs_test_add", func(args []any) (any, error) {
		return args[0].(int) + args[1].(int), nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if got, err := vm.Eval("__qjs_test_add(2, 3)", EvalGlobal); err != nil || got != 5 {
		t.Errorf("host call = %v, %v", got, err)
	}
	if _, err := vm.Eval("globalThis.__qjs_test_f = function (a, b) { return a + b }", EvalGlobal); err != nil {
		t.Fatal(err)
	}
	if got, err := vm.Call("__qjs_test_f", "x", "y"); err != nil || got != "xy" {
		t.Errorf("Call = %v, %v", got, err)
	}
}

func TestGlobalsAndBytecode(t *testing.T) {
	vm, err := NewVM()
	if err != nil {
		t.Fatal(err)
	}
	defer vm.Close()
	atom, err := vm.NewAtom("__qjs_test_g")
	if err != nil {
		t.Fatal(err)
	}
	g := vm.GlobalObject()
	defer g.Free()
	if err := g.SetProperty(atom, "v"); err != nil {
		t.Fatal(err)
	}
	if got, err := vm.GetProperty(g, atom); err != nil || got != "v" {
		t.Errorf("GetProperty = %v, %v", got, err)
	}
	bc, err := vm.Compile("__qjs_test_g + '!'", EvalGlobal)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := vm.EvalBytecode(bc); err != nil || got != "v!" {
		t.Errorf("EvalBytecode = %v, %v", got, err)
	}
}
