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
	p := parser.New(lexer.New(src).Tokenize())
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
	var attrs []attrFix
	collectAttrs(root, &attrs)
	if len(attrs) == 0 {
		return src, nil, nil
	}

	sort.Slice(attrs, func(i, j int) bool { return attrs[i].offset > attrs[j].offset })
	seen := map[int]bool{}
	var changes []Change
	out := src
	for _, a := range attrs {
		if seen[a.offset] {
			continue
		}
		seen[a.offset] = true
		if a.offset < 0 || a.offset+len(a.from) > len(out) || out[a.offset:a.offset+len(a.from)] != a.from {
			continue
		}
		out = out[:a.offset] + a.to + out[a.offset+len(a.from):]
		changes = append(changes, Change{Line: a.line, Col: a.col, Code: diag.CodeLowercaseEvent, From: a.from, To: a.to})
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

type attrFix struct {
	offset int
	line   int
	col    int
	from   string
	to     string
}

func collectAttrs(v any, out *[]attrFix) {
	switch t := v.(type) {
	case map[string]any:
		if t["kind"] == "JSXElement" {
			collectElementAttrs(t, out)
		}
		for _, child := range t {
			collectAttrs(child, out)
		}
	case []any:
		for _, child := range t {
			collectAttrs(child, out)
		}
	}
}

// collectElementAttrs only inspects built-in elements, matching KR5002's
// scope. Component props are free-form and may intentionally use lowercase.
func collectElementAttrs(el map[string]any, out *[]attrFix) {
	opening, _ := el["opening"].(map[string]any)
	tag, _ := opening["name"].(string)
	if tag == "" || tag[0] < 'a' || tag[0] > 'z' || strings.ContainsAny(tag, "-.:") {
		return
	}
	attrs, _ := opening["attributes"].([]any)
	for _, raw := range attrs {
		attr, _ := raw.(map[string]any)
		name, _ := attr["name"].(string)
		to, ok := irtree.LowercaseEventReplacement(name)
		if !ok || to == name || isStringLiteral(attr["value"]) {
			continue
		}
		pos, _ := attr["position"].(map[string]any)
		offset := jsonInt(pos["offset"])
		if offset < 0 {
			continue
		}
		*out = append(*out, attrFix{
			offset: offset,
			line:   jsonInt(pos["line"]),
			col:    jsonInt(pos["col"]),
			from:   name,
			to:     to,
		})
	}
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
