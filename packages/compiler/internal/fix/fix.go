// Package fix applies conservative, parse-gated source migrations for stable
// diagnostics. It only rewrites patterns whose intent is unambiguous; every
// other diagnostic has a manual migration path.
package fix

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/kratejs/krate/packages/compiler/internal/astjson"
	"github.com/kratejs/krate/packages/compiler/internal/diag"
	"github.com/kratejs/krate/packages/compiler/internal/fsutil"
	"github.com/kratejs/krate/packages/compiler/internal/irtree"
	"github.com/kratejs/krate/packages/compiler/internal/lexer"
	"github.com/kratejs/krate/packages/compiler/internal/parser"
	"github.com/kratejs/krate/packages/compiler/internal/tsstrip"
)

// Change is one safe rewrite.
type Change struct {
	File string
	Line int
	Col  int
	Code diag.Code
	From string
	To   string
}

// Skipped records a source file that could not be parsed, so `krate fix`
// leaves it untouched instead of guessing.
type Skipped struct {
	File   string
	Reason string
}

// Result is the outcome of a fix run.
type Result struct {
	Changes []Change
	Skipped []Skipped
	Applied bool
}

// Run scans a project for safe fixes. Dry-run is the default; apply writes
// changed files atomically. Files that fail to parse are skipped.
func Run(root string, apply bool) (Result, error) {
	var res Result
	res.Applied = apply
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			switch d.Name() {
			case "node_modules", "dist", ".krate", ".git":
				return filepath.SkipDir
			}
			return nil
		}
		if !fixableFile(path) {
			return nil
		}
		src, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		out, changes, parseErr := fixSource(string(src))
		rel, _ := filepath.Rel(root, path)
		if parseErr != nil {
			res.Skipped = append(res.Skipped, Skipped{File: rel, Reason: parseErr.Error()})
			return nil
		}
		if len(changes) == 0 {
			return nil
		}
		for i := range changes {
			changes[i].File = rel
		}
		res.Changes = append(res.Changes, changes...)
		if apply {
			info, statErr := os.Stat(path)
			mode := os.FileMode(0644)
			if statErr == nil {
				mode = info.Mode().Perm()
			}
			if err := fsutil.WriteFileAtomic(path, []byte(out), mode); err != nil {
				return fmt.Errorf("writing %s: %w", rel, err)
			}
		}
		return nil
	})
	if err != nil {
		return res, err
	}
	return res, nil
}

// Plan returns the rewritten source for every safe fix without writing
// anything. Callers (such as the MCP transaction layer) can then stage and
// commit the changes under their own policy and conflict checks.
func Plan(root string) (map[string]string, []Change, []Skipped, error) {
	return PlanWithCodes(root, nil)
}

// PlanWithCodes is Plan restricted to the given diagnostic codes. An empty
// list means every safe fix.
func PlanWithCodes(root string, codes []string) (map[string]string, []Change, []Skipped, error) {
	allow := map[string]bool{}
	for _, code := range codes {
		if code = strings.TrimSpace(code); code != "" {
			allow[code] = true
		}
	}
	if len(allow) == 0 {
		allow = nil
	}
	files := map[string]string{}
	var changes []Change
	var skipped []Skipped
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			switch d.Name() {
			case "node_modules", "dist", ".krate", ".git":
				return filepath.SkipDir
			}
			return nil
		}
		if !fixableFile(path) {
			return nil
		}
		src, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		out, fileChanges, parseErr := fixSourceFiltered(string(src), allow)
		rel, _ := filepath.Rel(root, path)
		rel = filepath.ToSlash(rel)
		if parseErr != nil {
			skipped = append(skipped, Skipped{File: rel, Reason: parseErr.Error()})
			return nil
		}
		if len(fileChanges) == 0 {
			return nil
		}
		for i := range fileChanges {
			fileChanges[i].File = rel
		}
		changes = append(changes, fileChanges...)
		files[rel] = out
		return nil
	})
	if err != nil {
		return nil, nil, nil, err
	}
	return files, changes, skipped, nil
}

func fixableFile(path string) bool {
	base := strings.ToLower(filepath.Base(path))
	if strings.HasSuffix(base, ".d.ts") {
		return false
	}
	switch strings.ToLower(filepath.Ext(base)) {
	case ".tsx", ".jsx", ".ts", ".js":
		return true
	default:
		return false
	}
}

// fixSource returns the rewritten source and the changes it would make. A
// parse error is returned instead of a partial rewrite.
func fixSource(src string) (string, []Change, error) {
	return fixSourceFiltered(src, nil)
}

func fixSourceFiltered(src string, allow map[string]bool) (string, []Change, error) {
	// Blanking keeps every offset, so edits still apply to src.
	p := parser.New(lexer.New(tsstrip.BlankTierDecorators("source.tsx", src)).Tokenize())
	prog := p.ParseProgram()
	if errs := p.Errors(); len(errs) > 0 {
		return src, nil, errs[0]
	}
	doc, err := astjson.EncodeProgram(prog)
	if err != nil {
		return src, nil, err
	}
	var root any
	if err := json.Unmarshal(doc, &root); err != nil {
		return src, nil, err
	}
	var edits []sourceEdit
	collectSafeEdits(root, src, &edits)
	if allow != nil {
		filtered := edits[:0]
		for _, e := range edits {
			if allow[string(e.code)] {
				filtered = append(filtered, e)
			}
		}
		edits = filtered
	}
	if len(edits) == 0 {
		return src, nil, nil
	}

	sort.Slice(edits, func(i, j int) bool { return edits[i].start > edits[j].start })
	var changes []Change
	out := src
	lastStart := len(src) + 1
	for _, e := range edits {
		if e.start < 0 || e.end < e.start || e.end > len(out) || e.end > lastStart {
			continue
		}
		if out[e.start:e.end] != e.from {
			continue
		}
		out = out[:e.start] + e.to + out[e.end:]
		lastStart = e.start
		changes = append(changes, Change{Line: e.line, Col: e.col, Code: e.code, From: e.from, To: e.to})
	}
	// Changes are reported in source order even though edits apply backwards.
	sort.Slice(changes, func(i, j int) bool {
		if changes[i].Line != changes[j].Line {
			return changes[i].Line < changes[j].Line
		}
		return changes[i].Col < changes[j].Col
	})
	return out, changes, nil
}

type sourceEdit struct {
	start int
	end   int
	line  int
	col   int
	from  string
	to    string
	code  diag.Code
}

func collectSafeEdits(v any, src string, out *[]sourceEdit) {
	switch t := v.(type) {
	case map[string]any:
		if t["kind"] == "JSXElement" {
			collectElementEdits(t, src, out)
		}
		for _, child := range t {
			collectSafeEdits(child, src, out)
		}
	case []any:
		for _, child := range t {
			collectSafeEdits(child, src, out)
		}
	}
}

// collectElementEdits only inspects built-in elements. Component props are
// free-form and may intentionally use lowercase or ignore DOM semantics.
func collectElementEdits(el map[string]any, src string, out *[]sourceEdit) {
	opening, _ := el["opening"].(map[string]any)
	tag, _ := opening["name"].(string)
	if tag == "" || tag[0] < 'a' || tag[0] > 'z' || strings.ContainsAny(tag, "-.:") {
		return
	}
	attrs, _ := opening["attributes"].([]any)
	byName := map[string]map[string]any{}
	for _, raw := range attrs {
		attr, _ := raw.(map[string]any)
		name, _ := attr["name"].(string)
		if name != "" {
			byName[name] = attr
		}
	}
	for _, raw := range attrs {
		attr, _ := raw.(map[string]any)
		name, _ := attr["name"].(string)
		pos, _ := attr["position"].(map[string]any)
		offset := jsonInt(pos["offset"])
		if offset < 0 {
			continue
		}
		if to, ok := irtree.LowercaseEventReplacement(name); ok && to != name && !isStringLiteral(attr["value"]) {
			*out = append(*out, sourceEdit{
				start: offset, end: offset + len(name), line: jsonInt(pos["line"]), col: jsonInt(pos["col"]),
				from: name, to: to, code: diag.CodeLowercaseEvent,
			})
		}
	}
	if tag != "input" && tag != "textarea" && tag != "select" {
		if byName["contentEditable"] != nil && byName["onChange"] != nil && byName["onInput"] == nil {
			attr := byName["onChange"]
			pos, _ := attr["position"].(map[string]any)
			offset := jsonInt(pos["offset"])
			if offset >= 0 {
				*out = append(*out, sourceEdit{
					start: offset, end: offset + len("onChange"), line: jsonInt(pos["line"]), col: jsonInt(pos["col"]),
					from: "onChange", to: "onInput", code: diag.CodeContentEditableChange,
				})
			}
		}
	}
	for _, pair := range [][2]string{{"value", "defaultValue"}, {"checked", "defaultChecked"}} {
		ctl, def := byName[pair[0]], byName[pair[1]]
		if ctl == nil || def == nil || isUndefinedJSON(ctl["value"]) {
			continue
		}
		pos, _ := def["position"].(map[string]any)
		offset := jsonInt(pos["offset"])
		if offset < 0 {
			continue
		}
		end := attributeEnd(src, offset)
		if end <= offset || end > len(src) {
			continue
		}
		start := offset
		for start > 0 && isJSXSpace(src[start-1]) {
			start--
		}
		*out = append(*out, sourceEdit{
			start: start, end: end, line: jsonInt(pos["line"]), col: jsonInt(pos["col"]),
			from: src[start:end], to: "", code: diag.CodeValueAndDefault,
		})
	}
}

// attributeEnd returns the exclusive end offset of the JSX attribute starting
// at start. It understands quoted values and balanced {expressions}.
func attributeEnd(src string, start int) int {
	i := start
	for i < len(src) && !isJSXSpace(src[i]) && src[i] != '=' && src[i] != '>' && src[i] != '/' {
		i++
	}
	if i >= len(src) {
		return -1
	}
	nameEnd := i
	for i < len(src) && isJSXSpace(src[i]) {
		i++
	}
	if i >= len(src) || src[i] != '=' {
		return nameEnd
	}
	i++
	for i < len(src) && isJSXSpace(src[i]) {
		i++
	}
	if i >= len(src) {
		return -1
	}
	switch src[i] {
	case '"', '\'':
		quote := src[i]
		i++
		for i < len(src) {
			if src[i] == '\\' {
				i += 2
				continue
			}
			if src[i] == quote {
				return i + 1
			}
			i++
		}
		return -1
	case '{':
		return scanJSXBraces(src, i)
	default:
		for i < len(src) && !isJSXSpace(src[i]) && src[i] != '>' && src[i] != '/' {
			i++
		}
		return i
	}
}

func scanJSXBraces(src string, start int) int {
	depth := 0
	for i := start; i < len(src); i++ {
		switch src[i] {
		case '"', '\'':
			quote := src[i]
			i++
			for i < len(src) {
				if src[i] == '\\' {
					i++
				} else if src[i] == quote {
					break
				}
				i++
			}
		case '`':
			i++
			for i < len(src) {
				if src[i] == '\\' {
					i++
				} else if src[i] == '`' {
					break
				}
				i++
			}
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return i + 1
			}
		}
	}
	return -1
}

func isJSXSpace(c byte) bool {
	return c == ' ' || c == '\t' || c == '\r' || c == '\n'
}

func isUndefinedJSON(v any) bool {
	m, ok := v.(map[string]any)
	if !ok {
		return false
	}
	switch m["kind"] {
	case "Identifier":
		name, _ := m["name"].(string)
		return name == "undefined"
	case "Literal":
		return jsonInt(m["literalKind"]) == 3
	case "UnaryExpr":
		op, _ := m["op"].(string)
		return op == "void"
	}
	return false
}

func isStringLiteral(v any) bool {
	m, ok := v.(map[string]any)
	if !ok || m["kind"] != "Literal" {
		return false
	}
	return jsonInt(m["literalKind"]) == 0
}

func jsonInt(v any) int {
	switch n := v.(type) {
	case float64:
		return int(n)
	case int:
		return n
	case int64:
		return int(n)
	default:
		return -1
	}
}
