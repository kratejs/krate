package mcp

import (
	"context"
	"encoding/json"
	"io"
	"testing"
)

// invoke calls a registered tool handler directly (through the same
// registration wrapper the server uses) and decodes its JSON text.
func invoke(t *testing.T, svc *Service, name string, args map[string]any) (map[string]any, ToolResult) {
	t.Helper()
	srv := NewServer(io.Discard, "krate", "test")
	svc.Register(srv)
	tool, ok := srv.tools[name]
	if !ok {
		t.Fatalf("tool %q is not registered", name)
	}
	if args == nil {
		args = map[string]any{}
	}
	res, rerr := tool.Handler(context.Background(), args)
	if rerr != nil {
		t.Fatalf("%s: rpc error %v", name, rerr)
	}
	var m map[string]any
	if len(res.Content) > 0 {
		_ = json.Unmarshal([]byte(res.Content[0].Text), &m)
	}
	return m, res
}

func list(t *testing.T, m map[string]any, key string) []any {
	t.Helper()
	v, ok := m[key].([]any)
	if !ok {
		t.Fatalf("%s is %T (%v), want array", key, m[key], m[key])
	}
	return v
}

func asMap(v any) map[string]any { m, _ := v.(map[string]any); return m }

func strs(vs []any) []string {
	out := make([]string, 0, len(vs))
	for _, v := range vs {
		if s, ok := v.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

func contains(vs []string, want string) bool {
	for _, v := range vs {
		if v == want {
			return true
		}
	}
	return false
}
