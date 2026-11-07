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

func mustOK(t *testing.T, svc *Service, name string, args map[string]any) map[string]any {
	t.Helper()
	m, res := invoke(t, svc, name, args)
	if res.IsError {
		t.Fatalf("%s failed: %s", name, res.Content[0].Text)
	}
	if m == nil {
		t.Fatalf("%s returned no JSON: %v", name, res.Content)
	}
	return m
}

func mustFail(t *testing.T, svc *Service, name string, args map[string]any) string {
	t.Helper()
	_, res := invoke(t, svc, name, args)
	if !res.IsError {
		t.Fatalf("%s should have failed, got %v", name, res.Content)
	}
	return res.Content[0].Text
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

// newBuiltService builds the project once so build-output tools have data.
func newBuiltService(t *testing.T, files map[string]string) *Service {
	t.Helper()
	svc := newTestService(t, files)
	if out, err := svc.runBuild(context.Background()); err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}
	return svc
}

func writePolicy(t *testing.T, svc *Service, policy string) {
	t.Helper()
	writeFile(t, svc.root, ".krate/mcp.json", policy)
	svc.policy = loadPolicy(svc.root)
}

// mustRPCError expects a protocol-level (invalid params) error.
func mustRPCError(t *testing.T, svc *Service, name string, args map[string]any) string {
	t.Helper()
	srv := NewServer(io.Discard, "krate", "test")
	svc.Register(srv)
	_, rerr := srv.tools[name].Handler(context.Background(), args)
	if rerr == nil {
		t.Fatalf("%s should have returned a protocol error", name)
	}
	return rerr.Message
}
