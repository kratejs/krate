package mcp

import (
	"encoding/json"
	"strings"
	"testing"
)

type promptEvalCase struct {
	args      map[string]any
	required  []string
	forbidden []string
}

func TestPromptEvaluation(t *testing.T) {
	svc := newTestService(t, map[string]string{
		"src/pages/index.tsx": "export default function Page() { return <h1>Hi</h1>; }\n",
	})
	list := mustResp(t, serve(t, svc, `{"jsonrpc":"2.0","id":1,"method":"prompts/list"}`)[0])["prompts"].([]any)
	listed := map[string]map[string]any{}
	for _, raw := range list {
		prompt := raw.(map[string]any)
		name := prompt["name"].(string)
		listed[name] = prompt
		if strings.TrimSpace(prompt["description"].(string)) == "" {
			t.Errorf("prompt %s has no description", name)
		}
	}
	cases := map[string]promptEvalCase{
		"add-page": {
			args:     map[string]any{"route": "/pricing"},
			required: []string{"read_page", "create_page", "build", "check", "diff", "the page title", "static"},
		},
		"publish-content": {
			args:     map[string]any{"collection": "blog", "slug": "hello-world"},
			required: []string{"create_content", "read_content", "schema", "diff", "the entry title"},
		},
		"fix-checks": {
			required: []string{"get_diagnostics", "apply_safe_fixes", "apply_fix", "apply_patch", "run_task", "the whole site"},
		},
		"repair-diagnostics": {
			required: []string{"get_diagnostics", "apply_safe_fixes", "dry-run", "run_task", "build"},
		},
		"refactor": {
			args:     map[string]any{"target": "Greeting"},
			required: []string{"search_project", "find_references", "impact", "apply_patch", "batch_edit", "build", "check", "the requested change"},
		},
		"audit-dependencies": {
			required: []string{"package.json", "search_project", "workspace_symbols", "Do not modify files", "the whole project"},
		},
		"scaffold-component": {
			args:     map[string]any{"name": "StatusBadge"},
			required: []string{"create_component", "read a nearby component", "build", "check", "client"},
		},
		"explore": {
			required: []string{"list_routes", "krate://routes", "krate://config", "check", "read_page", "no specific route"},
		},
	}
	for name := range listed {
		if _, ok := cases[name]; !ok {
			t.Errorf("prompt %s has no evaluation case", name)
		}
	}
	for name, eval := range cases {
		meta, ok := listed[name]
		if !ok {
			t.Errorf("evaluated prompt %s is not registered", name)
			continue
		}
		argsMeta, _ := meta["arguments"].([]any)
		for _, raw := range argsMeta {
			arg := raw.(map[string]any)
			if strings.TrimSpace(arg["description"].(string)) == "" {
				t.Errorf("prompt %s argument %v has no description", name, arg["name"])
			}
		}
		text := promptText(t, svc, name, eval.args)
		if strings.Contains(text, "{{") || strings.Contains(text, "}}") {
			t.Errorf("prompt %s left an unresolved placeholder:\n%s", name, text)
		}
		for _, want := range eval.required {
			if !strings.Contains(text, want) {
				t.Errorf("prompt %s is missing %q:\n%s", name, want, text)
			}
		}
		for _, bad := range eval.forbidden {
			if strings.Contains(text, bad) {
				t.Errorf("prompt %s contains forbidden %q:\n%s", name, bad, text)
			}
		}
	}
}

func promptText(t *testing.T, svc *Service, name string, args map[string]any) string {
	t.Helper()
	req, _ := json.Marshal(map[string]any{
		"jsonrpc": "2.0", "id": 1, "method": "prompts/get",
		"params": map[string]any{"name": name, "arguments": args},
	})
	resp := mustResp(t, serve(t, svc, string(req))[0])
	messages, _ := resp["messages"].([]any)
	var b strings.Builder
	for _, raw := range messages {
		msg := raw.(map[string]any)
		content, _ := msg["content"].(map[string]any)
		text, _ := content["text"].(string)
		b.WriteString(text)
		b.WriteByte('\n')
	}
	return b.String()
}

func TestPromptRequiredArguments(t *testing.T) {
	svc := newTestService(t, map[string]string{
		"src/pages/index.tsx": "export default function Page() { return <h1>Hi</h1>; }\n",
	})
	resp := serve(t, svc, `{"jsonrpc":"2.0","id":1,"method":"prompts/get","params":{"name":"add-page","arguments":{}}}`)[0]
	if resp["error"] == nil {
		t.Fatalf("expected missing required prompt argument error, got %v", resp)
	}
}
