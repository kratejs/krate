package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// Tool is a callable MCP tool. InputSchema is a hand-written JSON Schema
// object (no schema library dependency).
type Tool struct {
	Name        string
	Title       string
	Description string
	InputSchema map[string]any
	// Annotations describe tool behavior so clients can auto-approve safe
	// calls and warn on destructive ones (readOnlyHint/destructiveHint/
	// idempotentHint/openWorldHint, protocol 2025-03-26+).
	Annotations map[string]any
	Handler     func(ctx context.Context, args map[string]any) (ToolResult, *rpcError)
}

// ToolResult is the MCP tool-call payload. Content is a list of content blocks;
// IsError marks an application-level failure (the tool ran but the operation
// failed), distinct from a protocol error.
type ToolResult struct {
	Content []Content
	IsError bool
}

// Content is a single MCP content block. Only text and resource are needed.
type Content struct {
	Type     string `json:"type"`
	Text     string `json:"text,omitempty"`
	Resource any    `json:"resource,omitempty"`
}

// TextContent builds a text content block.
func TextContent(text string) Content {
	return Content{Type: "text", Text: text}
}

// TextResult builds a successful text-only tool result.
func TextResult(text string) ToolResult {
	return ToolResult{Content: []Content{TextContent(text)}}
}

// ErrorResult builds an application-level error result.
func ErrorResult(text string) ToolResult {
	return ToolResult{Content: []Content{TextContent(text)}, IsError: true}
}

// JSONResult builds a tool result containing pretty-printed JSON.
func JSONResult(v any) (ToolResult, *rpcError) {
	data, err := marshalJSON(v)
	if err != nil {
		return ToolResult{}, Errorf("encoding result: %v", err)
	}
	return TextResult(string(data)), nil
}

// marshalJSON encodes indented JSON with HTML escaping disabled so page source,
// JSX, and diffs stay readable in tool output.
func marshalJSON(v any) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return bytes.TrimRight(buf.Bytes(), "\n"), nil
}

// Resource is a readable MCP resource.
type Resource struct {
	URI         string
	Name        string
	Description string
	MIMEType    string
	Read        func(ctx context.Context, uri string) (ResourceContents, *rpcError)
}

// ResourceTemplate is a parameterized MCP resource exposed through
// resources/templates/list. URI templates use the RFC 6570 form (a single
// {param} per segment, e.g. "krate://page/{route}").
type ResourceTemplate struct {
	URITemplate string
	Name        string
	Title       string
	Description string
	MIMEType    string
	// Read receives the concrete URI and the extracted parameters.
	Read func(ctx context.Context, uri string, params map[string]string) (ResourceContents, *rpcError)
}

// ResourceContents is the payload returned by resources/read.
type ResourceContents struct {
	URI      string `json:"uri"`
	MIMEType string `json:"mimeType"`
	Text     string `json:"text"`
}

// PromptArgument is a parameter a prompt accepts.
type PromptArgument struct {
	Name        string
	Description string
	Required    bool
}

// Prompt is a curated, templated workflow surfaced via prompts/list and
// prompts/get. Messages are rendered with simple {{arg}} substitution.
type Prompt struct {
	Name        string
	Description string
	Arguments   []PromptArgument
	Messages    []PromptMessage
	// Resolve optionally injects dynamic context (called after argument
	// substitution). Returning an error fails prompts/get.
	Resolve func(ctx context.Context, args map[string]any, rendered []PromptMessage) ([]PromptMessage, *rpcError)
}

// PromptMessage is one message in a prompt; Role is typically "user".
type PromptMessage struct {
	Role    string  `json:"role"`
	Content Content `json:"content"`
}

// completionFn completes an argument value for resource templates and prompts.
type completionFn func(ctx context.Context, value string) ([]string, error)

// render returns the prompt's messages after argument substitution and any
// dynamic resolution.
func (p *Prompt) render(args map[string]any) (map[string]any, *rpcError) {
	msgs := make([]PromptMessage, len(p.Messages))
	copy(msgs, p.Messages)
	for i, m := range msgs {
		if m.Content.Type == "text" {
			msgs[i].Content.Text = renderTemplateText(m.Content.Text, args)
		}
	}
	if p.Resolve != nil {
		resolved, rerr := p.Resolve(context.Background(), args, msgs)
		if rerr != nil {
			return nil, rerr
		}
		msgs = resolved
	}
	if msgs == nil {
		msgs = []PromptMessage{}
	}
	return map[string]any{
		"description": p.Description,
		"messages":    msgs,
	}, nil
}

// renderTemplateText substitutes {{key}} placeholders with stringified args.
func renderTemplateText(text string, args map[string]any) string {
	if len(args) == 0 {
		return text
	}
	var b bytes.Buffer
	for {
		start := bytes.Index([]byte(text), []byte("{{"))
		if start < 0 {
			b.WriteString(text)
			break
		}
		b.WriteString(text[:start])
		rest := text[start+2:]
		end := bytes.Index([]byte(rest), []byte("}}"))
		if end < 0 {
			b.WriteString("{{")
			text = rest
			continue
		}
		key := rest[:end]
		if v, ok := args[string(key)]; ok {
			fmt.Fprintf(&b, "%v", v)
		} else {
			b.WriteString("{{")
			b.WriteString(string(key))
			b.WriteString("}}")
		}
		text = rest[end+2:]
	}
	return b.String()
}

func refKey(refType, refName, argName string) string {
	return refType + "\x00" + refName + "\x00" + argName
}

func (s *Server) callTool(raw json.RawMessage, op *pendingOp) (any, *rpcError) {
	var params struct {
		Name      string         `json:"name"`
		Arguments map[string]any `json:"arguments"`
	}
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &params); err != nil {
			return nil, &rpcError{Code: codeInvalidParams, Message: "invalid params: " + err.Error()}
		}
	}
	tool, ok := s.tools[params.Name]
	if !ok {
		return nil, &rpcError{Code: codeInvalidParams, Message: "unknown tool: " + params.Name}
	}
	if tool.Handler == nil {
		return nil, Errorf("tool %s has no handler", params.Name)
	}
	if params.Arguments == nil {
		params.Arguments = map[string]any{}
	}
	result, rerr := tool.Handler(op.ctx, params.Arguments)
	if rerr != nil {
		return nil, rerr
	}
	content := result.Content
	if content == nil {
		content = []Content{}
	}
	return map[string]any{
		"content": content,
		"isError": result.IsError,
	}, nil
}

func (s *Server) readResource(raw json.RawMessage) (any, *rpcError) {
	var params struct {
		URI string `json:"uri"`
	}
	if err := json.Unmarshal(raw, &params); err != nil {
		return nil, &rpcError{Code: codeInvalidParams, Message: "invalid params: " + err.Error()}
	}
	if r, ok := s.resources[params.URI]; ok {
		if r.Read == nil {
			return nil, Errorf("resource %s has no reader", params.URI)
		}
		contents, rerr := r.Read(context.Background(), r.URI)
		if rerr != nil {
			return nil, rerr
		}
		return map[string]any{"contents": []ResourceContents{contents}}, nil
	}
	// Templated resource match.
	if tmpl, tparams, ok := s.matchTemplate(params.URI); ok {
		if tmpl.Read == nil {
			return nil, Errorf("template %s has no reader", tmpl.URITemplate)
		}
		contents, rerr := tmpl.Read(context.Background(), params.URI, tparams)
		if rerr != nil {
			return nil, rerr
		}
		return map[string]any{"contents": []ResourceContents{contents}}, nil
	}
	return nil, &rpcError{Code: codeInvalidParams, Message: "unknown resource: " + params.URI}
}

// matchTemplate matches a concrete URI against the registered resource
// templates, returning the template and its extracted parameters.
func (s *Server) matchTemplate(uri string) (*ResourceTemplate, map[string]string, bool) {
	for _, t := range s.templates {
		params, ok := expandURITemplate(t.URITemplate, uri)
		if ok {
			return t, params, true
		}
	}
	return nil, nil, false
}

// expandURITemplate matches uri against a template containing one or more
// {param} segments (RFC 6570 subset). Returns extracted params.
func expandURITemplate(tmpl, uri string) (map[string]string, bool) {
	// Split into segments; a segment may be a literal or a single {name}.
	ts := splitTemplateSegments(tmpl)
	us := splitURISegments(uri)
	if len(ts) == 0 {
		return nil, false
	}
	params := map[string]string{}
	for i, seg := range ts {
		if len(seg) >= 2 && seg[0] == '{' && seg[len(seg)-1] == '}' {
			name := seg[1 : len(seg)-1]
			// A final {name} segment may match a multi-segment remainder so
			// nested paths (e.g. krate://docs/features/mcp) resolve.
			if i == len(ts)-1 {
				if i >= len(us) {
					return nil, false
				}
				rest := make([]string, len(us)-i)
				for j := i; j < len(us); j++ {
					rest[j-i] = us[j]
				}
				params[name] = decodeURISegment(strings.Join(rest, "/"))
				return params, true
			}
			if i >= len(us) {
				return nil, false
			}
			params[name] = decodeURISegment(us[i])
			continue
		}
		if i >= len(us) || seg != us[i] {
			return nil, false
		}
	}
	if len(ts) != len(us) {
		return nil, false
	}
	return params, true
}

func splitTemplateSegments(tmpl string) []string {
	// "krate://page/{route}" -> ["krate:", "", "page", "{route}"]
	return splitPath(tmpl)
}

func splitURISegments(uri string) []string {
	return splitPath(uri)
}

func splitPath(p string) []string {
	var out []string
	cur := ""
	for i := 0; i < len(p); i++ {
		if p[i] == '/' {
			out = append(out, cur)
			cur = ""
			continue
		}
		cur += string(p[i])
	}
	out = append(out, cur)
	return out
}

func decodeURISegment(s string) string {
	// Minimal percent-decoding for the one segment we use (routes).
	var b bytes.Buffer
	for i := 0; i < len(s); i++ {
		if s[i] == '%' && i+2 < len(s) {
			if hi := hexVal(s[i+1]); hi >= 0 {
				if lo := hexVal(s[i+2]); lo >= 0 {
					b.WriteByte(byte(hi<<4 | lo))
					i += 2
					continue
				}
			}
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

func hexVal(c byte) int {
	switch {
	case c >= '0' && c <= '9':
		return int(c - '0')
	case c >= 'a' && c <= 'f':
		return int(c-'a') + 10
	case c >= 'A' && c <= 'F':
		return int(c-'A') + 10
	}
	return -1
}

func (s *Server) complete(raw json.RawMessage) (any, *rpcError) {
	var params struct {
		Ref struct {
			Type string `json:"type"`
			URI  string `json:"uri,omitempty"`
			Name string `json:"name,omitempty"`
		} `json:"ref"`
		Argument struct {
			Name  string `json:"name"`
			Value string `json:"value"`
		} `json:"argument"`
	}
	if err := json.Unmarshal(raw, &params); err != nil {
		return nil, &rpcError{Code: codeInvalidParams, Message: "invalid params: " + err.Error()}
	}
	var refName string
	switch params.Ref.Type {
	case "ref/resource", "resource":
		refName = params.Ref.URI
	case "ref/prompt", "prompt", "ref/tool", "tool":
		refName = params.Ref.Name
	default:
		return nil, &rpcError{Code: codeInvalidParams, Message: "unsupported ref type: " + params.Ref.Type}
	}
	fn, ok := s.completes[refKey(params.Ref.Type, refName, params.Argument.Name)]
	if !ok {
		return nil, &rpcError{Code: codeInvalidParams, Message: "no completion for ref"}
	}
	values, err := fn(context.Background(), params.Argument.Value)
	if err != nil {
		return nil, Errorf("completing: %v", err)
	}
	if values == nil {
		values = []string{}
	}
	return map[string]any{
		"completion": map[string]any{
			"values":  values,
			"hasMore": false,
		},
	}, nil
}

// argString extracts a string argument, returning "" when absent.
func argString(args map[string]any, key string) string {
	if v, ok := args[key].(string); ok {
		return v
	}
	return ""
}

// argBool extracts a boolean argument with a default.
func argBool(args map[string]any, key string, def bool) bool {
	if v, ok := args[key].(bool); ok {
		return v
	}
	return def
}

// argStringMap extracts a map[string]any argument.
func argStringMap(args map[string]any, key string) map[string]any {
	if m, ok := args[key].(map[string]any); ok {
		return m
	}
	return nil
}

// requireString extracts a required string argument.
func requireString(args map[string]any, key string) (string, *rpcError) {
	v, ok := args[key].(string)
	if !ok || v == "" {
		return "", &rpcError{Code: codeInvalidParams, Message: fmt.Sprintf("missing required argument %q", key)}
	}
	return v, nil
}
