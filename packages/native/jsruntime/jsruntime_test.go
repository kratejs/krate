package jsruntime

import (
	"testing"
)

func toInt(v any) int {
	switch n := v.(type) {
	case int:
		return n
	case int64:
		return int(n)
	case float64:
		return int(n)
	default:
		return 0
	}
}

func TestNew(t *testing.T) {
	rt, err := New()
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}
	defer rt.Close()
}

func TestExecute(t *testing.T) {
	rt, err := New()
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}
	defer rt.Close()

	result, err := rt.Execute("1 + 2")
	if err != nil {
		t.Fatalf("Execute() failed: %v", err)
	}
	if toInt(result) != 3 {
		t.Errorf("Expected 3, got %v (%T)", result, result)
	}
}

func TestString(t *testing.T) {
	rt, err := New()
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}
	defer rt.Close()

	result, err := rt.Execute("'hello' + ' ' + 'world'")
	if err != nil {
		t.Fatalf("Execute() failed: %v", err)
	}
	if result.(string) != "hello world" {
		t.Errorf("Expected 'hello world', got %v", result)
	}
}

func TestObject(t *testing.T) {
	rt, err := New()
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}
	defer rt.Close()

	result, err := rt.Execute("({ a: 1, b: 2 })")
	if err != nil {
		t.Fatalf("Execute() failed: %v", err)
	}
	native := ToNative(result)
	obj := native.(map[string]any)
	if toInt(obj["a"]) != 1 || toInt(obj["b"]) != 2 {
		t.Errorf("Expected {a:1, b:2}, got %v", obj)
	}
}

func TestRegisterFunc(t *testing.T) {
	rt, err := New()
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}
	defer rt.Close()

	err = rt.RegisterFunc("add", func(args []any) (any, error) {
		a := toInt(args[0])
		b := toInt(args[1])
		return a + b, nil
	})
	if err != nil {
		t.Fatalf("RegisterFunc() failed: %v", err)
	}

	result, err := rt.Execute("add(3, 4)")
	if err != nil {
		t.Fatalf("Execute() failed: %v", err)
	}
	if toInt(result) != 7 {
		t.Errorf("Expected 7, got %v", result)
	}
}

func TestConsole(t *testing.T) {
	rt, err := New()
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}
	defer rt.Close()

	_, err = rt.Execute("console.log('test message')")
	if err != nil {
		t.Fatalf("Execute() failed: %v", err)
	}
}

func TestSetGlobal(t *testing.T) {
	rt, err := New()
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}
	defer rt.Close()

	err = rt.SetGlobal("myVar", 42)
	if err != nil {
		t.Fatalf("SetGlobal() failed: %v", err)
	}

	result, err := rt.Execute("myVar")
	if err != nil {
		t.Fatalf("Execute() failed: %v", err)
	}
	if toInt(result) != 42 {
		t.Errorf("Expected 42, got %v", result)
	}
}

func TestFetchExists(t *testing.T) {
	rt, err := New()
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}
	defer rt.Close()

	result, err := rt.Execute("typeof fetch === 'function'")
	if err != nil {
		t.Fatalf("Execute() failed: %v", err)
	}
	if result.(bool) != true {
		t.Error("Expected fetch to be a function")
	}
}

func TestSetEnv(t *testing.T) {
	rt, err := New()
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}
	defer rt.Close()

	err = rt.SetEnv(map[string]string{
		"API_KEY":  "test123",
		"NODE_ENV": "development",
	})
	if err != nil {
		t.Fatalf("SetEnv() failed: %v", err)
	}

	result, err := rt.Execute("process.env.API_KEY")
	if err != nil {
		t.Fatalf("Execute() failed: %v", err)
	}
	if result.(string) != "test123" {
		t.Errorf("Expected 'test123', got %v", result)
	}
}

func TestTextEncoder(t *testing.T) {
	rt, err := New()
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}
	defer rt.Close()

	result, err := rt.Execute("new TextEncoder().encode('hi').length")
	if err != nil {
		t.Fatalf("Execute() failed: %v", err)
	}
	if toInt(result) != 2 {
		t.Errorf("Expected 2, got %v", result)
	}
}

func TestCall(t *testing.T) {
	rt, err := New()
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}
	defer rt.Close()

	err = rt.RegisterFunc("multiply", func(args []any) (any, error) {
		a := toInt(args[0])
		b := toInt(args[1])
		return a * b, nil
	})
	if err != nil {
		t.Fatalf("RegisterFunc() failed: %v", err)
	}

	// Call via Execute
	_, err = rt.Execute("function multiplyWrapper(a,b){ return multiply(a,b) }")
	if err != nil {
		t.Fatalf("Execute() failed: %v", err)
	}

	result, err := rt.Call("multiplyWrapper", 5, 6)
	if err != nil {
		t.Fatalf("Call() failed: %v", err)
	}
	if toInt(result) != 30 {
		t.Errorf("Expected 30, got %v", result)
	}
}
