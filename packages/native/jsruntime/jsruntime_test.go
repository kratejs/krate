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
