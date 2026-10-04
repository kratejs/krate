package mcp

// objSchema builds a JSON Schema object. Properties defaults to an empty object
// so tools that take no arguments produce a valid schema. required lists
// property names that must be present.
func objSchema(props map[string]any, required ...string) map[string]any {
	if props == nil {
		props = map[string]any{}
	}
	s := map[string]any{
		"type":       "object",
		"properties": props,
	}
	if len(required) > 0 {
		s["required"] = required
	}
	return s
}

func strSchema(desc string) map[string]any {
	return map[string]any{"type": "string", "description": desc}
}

func numSchema(desc string) map[string]any {
	return map[string]any{"type": "number", "description": desc}
}

func boolSchema(desc string) map[string]any {
	return map[string]any{"type": "boolean", "description": desc}
}

func objOnlySchema(desc string) map[string]any {
	return map[string]any{"type": "object", "description": desc}
}

func enumSchema(desc string, values ...string) map[string]any {
	vals := make([]any, 0, len(values))
	for _, v := range values {
		vals = append(vals, v)
	}
	return map[string]any{"type": "string", "description": desc, "enum": vals}
}

// ToolAnnotations builders encode behavior hints the MCP spec (2025-03-26+)
// defines. Clients treat read-only tools as safe to auto-approve and warn on
// destructive ones.
func readOnlyAnnotations(title string) map[string]any {
	return map[string]any{
		"title":           title,
		"readOnlyHint":    true,
		"destructiveHint": false,
		"idempotentHint":  true,
		"openWorldHint":   false,
	}
}

func additiveToolAnnotations(title string) map[string]any {
	return map[string]any{
		"title":           title,
		"readOnlyHint":    false,
		"destructiveHint": false,
		"idempotentHint":  false,
		"openWorldHint":   false,
	}
}

func destructiveToolAnnotations(title string) map[string]any {
	return map[string]any{
		"title":           title,
		"readOnlyHint":    false,
		"destructiveHint": true,
		"idempotentHint":  false,
		"openWorldHint":   false,
	}
}

func rebuildToolAnnotations(title string) map[string]any {
	// build/check modify outDir (not source), so not read-only, but they are
	// non-destructive to authored files and repeatable.
	return map[string]any{
		"title":           title,
		"readOnlyHint":    false,
		"destructiveHint": false,
		"idempotentHint":  true,
		"openWorldHint":   false,
	}
}
