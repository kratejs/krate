package mcp

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Shared helpers for the insight tools (dev server, page analysis, project
// metadata, quality). Every tool that returns a list uses paginate or an
// explicit limit, and every path returned is project-relative.

// clampInt reads an integer argument and bounds it to [1, max].
func clampInt(args map[string]any, key string, def, max int) int {
	n := argInt(args, key, def)
	if n <= 0 {
		n = def
	}
	if n > max {
		n = max
	}
	return n
}

// paginate slices items by an opaque cursor. next is empty on the last page.
func paginate[T any](items []T, args map[string]any, def, max int) (page []T, next string, rerr *rpcError) {
	offset, err := decodeCursor(argString(args, "cursor"))
	if err != nil {
		return nil, "", &rpcError{Code: codeInvalidParams, Message: err.Error()}
	}
	limit := clampInt(args, "limit", def, max)
	if offset > len(items) {
		offset = len(items)
	}
	end := offset + limit
	if end > len(items) {
		end = len(items)
	}
	if end < len(items) {
		next = encodeCursor(end)
	}
	page = items[offset:end]
	if page == nil {
		page = []T{}
	}
	return page, next, nil
}

// truncateText cuts s to at most max bytes on a rune boundary.
func truncateText(s string, max int) (string, bool) {
	if max <= 0 || len(s) <= max {
		return s, false
	}
	cut := max
	for cut > 0 && (s[cut]&0xC0) == 0x80 {
		cut--
	}
	return s[:cut], true
}

// relToRoot returns a slash path relative to the project root, or "" when abs
// is outside it.
func (s *Service) relToRoot(abs string) string {
	rel, err := filepath.Rel(s.root, abs)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return ""
	}
	return filepath.ToSlash(rel)
}

// readProjectText reads a project file for an insight tool, applying the same
// read policy as read_file. It never reads outside the project root.
func (s *Service) readProjectText(rel string, maxBytes int64) (string, error) {
	abs, clean, err := s.resolveProjectPath(rel)
	if err != nil {
		return "", err
	}
	if s.policy != nil && s.policy.denied(clean) {
		return "", &policyError{"refusing to read " + clean + ": it " + s.policy.deniedReason(clean)}
	}
	if maxBytes <= 0 {
		maxBytes = 2 << 20
	}
	f, err := os.Open(abs)
	if err != nil {
		return "", err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, maxBytes))
	if err != nil {
		return "", err
	}
	return string(data), nil
}

type policyError struct{ msg string }

func (e *policyError) Error() string { return e.msg }

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func uniqueSorted(in []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(in))
	for _, v := range in {
		if v != "" && !seen[v] {
			seen[v] = true
			out = append(out, v)
		}
	}
	sort.Strings(out)
	return out
}

// taskAllowed reports whether policy lets agents start the named long-running
// or project-code task. Read-only profiles allow none.
func (s *Service) taskAllowed(task string) bool {
	return s.allowedTasks()[task]
}

// denyIfReadOnly is the standard refusal for tools that run project code.
func (s *Service) denyIfReadOnly(tool string) *ToolResult {
	if s.policy != nil && s.policy.ReadOnlyMode {
		r := ErrorResult(tool + " is disabled by readOnlyMode in .krate/mcp.json")
		return &r
	}
	return nil
}

// reg registers a tool whose output never carries the machine path of the
// project root: absolute paths under it become relative.
func (s *Service) reg(srv *Server, t Tool) {
	inner := t.Handler
	t.Handler = func(ctx context.Context, args map[string]any) (ToolResult, *rpcError) {
		res, rerr := inner(ctx, args)
		if rerr != nil {
			rerr.Message = s.scrubRoot(rerr.Message)
			return res, rerr
		}
		for i := range res.Content {
			res.Content[i].Text = s.scrubRoot(res.Content[i].Text)
		}
		if res.Structured != nil {
			if raw, err := json.Marshal(res.Structured); err == nil {
				clean := s.scrubRoot(string(raw))
				var v any
				if json.Unmarshal([]byte(clean), &v) == nil {
					res.Structured = v
				}
			}
		}
		return res, nil
	}
	srv.RegisterTool(t)
}

// scrubRoot replaces the absolute project root in text with ".".
func (s *Service) scrubRoot(text string) string {
	if s.root == "" || s.root == "/" {
		return text
	}
	root := strings.ReplaceAll(s.root, `\`, "/")
	// Results are scrubbed as JSON too, where a Windows path has its backslashes
	// doubled. Off Windows the root has none, so escaped equals s.root.
	escaped := strings.ReplaceAll(s.root, `\`, `\\`)
	text = strings.ReplaceAll(text, escaped+`\\`, "")
	text = strings.ReplaceAll(text, s.root+`\`, "")
	text = strings.ReplaceAll(text, s.root+"/", "")
	text = strings.ReplaceAll(text, root+"/", "")
	text = strings.ReplaceAll(text, escaped, ".")
	text = strings.ReplaceAll(text, s.root, ".")
	if root != s.root {
		text = strings.ReplaceAll(text, root, ".")
	}
	return text
}

// emptyIfNil keeps JSON arrays as [] instead of null.
func emptyIfNil[T any](s []T) []T {
	if s == nil {
		return []T{}
	}
	return s
}
