package mcp

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/kratejs/krate/packages/compiler/internal/astjson"
	"github.com/kratejs/krate/packages/compiler/internal/lexer"
	"github.com/kratejs/krate/packages/compiler/internal/parser"
)

// workspaceRoot is one read/index root. The primary root keeps an empty name
// so existing project-relative paths are unchanged.
type workspaceRoot struct {
	Name string
	Path string
}

type symbolOccurrence struct {
	Name       string `json:"name"`
	Kind       string `json:"kind"` // definition, reference, import, export
	SymbolKind string `json:"symbolKind,omitempty"`
	Root       string `json:"root,omitempty"`
	Path       string `json:"path"`
	Line       int    `json:"line"`
	Col        int    `json:"col"`
	Source     string `json:"source,omitempty"`
	Imported   string `json:"imported,omitempty"`
	Exported   bool   `json:"exported,omitempty"`
}

type symbolFileIndex struct {
	Root    string
	Path    string
	ModTime int64
	Size    int64
	Error   string
	Occ     []symbolOccurrence
}

type symbolIndexStats struct {
	Roots     int `json:"roots"`
	Files     int `json:"files"`
	Parsed    int `json:"parsed"`
	Reused    int `json:"reused"`
	Removed   int `json:"removed"`
	ParseErrs int `json:"parseErrors"`
}

type workspaceIndex struct {
	mu    sync.Mutex
	files map[string]*symbolFileIndex
	stats symbolIndexStats
}

func newWorkspaceIndex() *workspaceIndex {
	return &workspaceIndex{files: map[string]*symbolFileIndex{}}
}

func indexPathLabel(root, rel string) string {
	if root == "" {
		return rel
	}
	return root + ":" + rel
}

func symbolFileKey(root, rel string) string {
	return root + "\x00" + rel
}

func symbolFileKind(rel string) bool {
	if strings.HasSuffix(strings.ToLower(rel), ".d.ts") {
		return false
	}
	switch strings.ToLower(filepath.Ext(rel)) {
	case ".ts", ".tsx", ".js", ".jsx":
		return true
	}
	return false
}

// refresh re-stats every workspace root and reparses only files whose size or
// mtime changed. Parse errors are recorded per file and do not abort indexing.
func (idx *workspaceIndex) refresh(roots []workspaceRoot) symbolIndexStats {
	idx.mu.Lock()
	defer idx.mu.Unlock()
	stats := symbolIndexStats{Roots: len(roots)}
	seen := map[string]bool{}
	for _, root := range roots {
		_ = filepath.WalkDir(root.Path, func(p string, d os.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if d.IsDir() {
				if p != root.Path && listIgnoreDirs[d.Name()] {
					return filepath.SkipDir
				}
				return nil
			}
			rel, relErr := filepath.Rel(root.Path, p)
			if relErr != nil {
				return nil
			}
			rel = filepath.ToSlash(rel)
			if !symbolFileKind(rel) {
				return nil
			}
			info, statErr := d.Info()
			if statErr != nil {
				return nil
			}
			key := symbolFileKey(root.Name, rel)
			seen[key] = true
			if existing := idx.files[key]; existing != nil && existing.ModTime == info.ModTime().UnixNano() && existing.Size == info.Size() {
				stats.Reused++
				return nil
			}
			data, readErr := os.ReadFile(p)
			if readErr != nil {
				return nil
			}
			entry := parseSymbolFile(root.Name, rel, data)
			entry.ModTime = info.ModTime().UnixNano()
			entry.Size = info.Size()
			if entry.Error != "" {
				stats.ParseErrs++
			} else {
				stats.Parsed++
			}
			idx.files[key] = entry
			return nil
		})
	}
	for key := range idx.files {
		if seen[key] {
			continue
		}
		delete(idx.files, key)
		stats.Removed++
	}
	stats.Files = len(idx.files)
	idx.stats = stats
	return stats
}

// snapshot returns a stable copy of all indexed occurrences, sorted by root,
// path, line, and column.
func (idx *workspaceIndex) snapshot() ([]symbolOccurrence, symbolIndexStats) {
	idx.mu.Lock()
	defer idx.mu.Unlock()
	var out []symbolOccurrence
	for _, file := range idx.files {
		out = append(out, file.Occ...)
	}
	sort.Slice(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if a.Root != b.Root {
			return a.Root < b.Root
		}
		if a.Path != b.Path {
			return a.Path < b.Path
		}
		if a.Line != b.Line {
			return a.Line < b.Line
		}
		return a.Col < b.Col
	})
	return out, idx.stats
}

func parseSymbolFile(root, rel string, data []byte) *symbolFileIndex {
	entry := &symbolFileIndex{Root: root, Path: rel}
	p := parser.New(lexer.New(string(data)).Tokenize())
	prog := p.ParseProgram()
	if errs := p.Errors(); len(errs) > 0 {
		entry.Error = errs[0].Error()
		return entry
	}
	doc, err := astjson.EncodeProgram(prog)
	if err != nil {
		entry.Error = err.Error()
		return entry
	}
	var tree any
	if err := json.Unmarshal(doc, &tree); err != nil {
		entry.Error = err.Error()
		return entry
	}
	c := &symbolCollector{root: root, rel: rel, exported: map[string]bool{}}
	c.walk(tree, nil, "")
	for i := range c.occ {
		if c.occ[i].Kind == "definition" && c.exported[c.occ[i].Name] {
			c.occ[i].Exported = true
		}
	}
	entry.Occ = c.occ
	return entry
}

type symbolCollector struct {
	root     string
	rel      string
	occ      []symbolOccurrence
	exported map[string]bool
}

func (c *symbolCollector) add(name, kind string, line, col int) {
	if name == "" || line <= 0 || col <= 0 {
		return
	}
	c.occ = append(c.occ, symbolOccurrence{Name: name, Kind: "definition", SymbolKind: kind, Root: c.root, Path: c.rel, Line: line, Col: col})
}

func (c *symbolCollector) addReference(name string, line, col int) {
	if name == "" || line <= 0 || col <= 0 {
		return
	}
	c.occ = append(c.occ, symbolOccurrence{Name: name, Kind: "reference", Root: c.root, Path: c.rel, Line: line, Col: col})
}

func (c *symbolCollector) walk(node any, parent map[string]any, field string) {
	switch v := node.(type) {
	case []any:
		for _, item := range v {
			c.walk(item, parent, field)
		}
	case map[string]any:
		kind, _ := v["kind"].(string)
		pos := nodePos(v)
		switch kind {
		case "ImportStmt":
			source, _ := v["source"].(string)
			for _, imp := range importSpecifiers(v) {
				c.occ = append(c.occ, symbolOccurrence{
					Name: imp.local, Kind: "import", Root: c.root, Path: c.rel,
					Line: pos.line, Col: pos.col, Source: source, Imported: imp.remote,
				})
			}
		case "ExportStmt":
			for _, name := range exportLocalNames(v) {
				c.exported[name] = true
			}
			for _, exp := range exportExternals(v) {
				c.occ = append(c.occ, symbolOccurrence{
					Name: exp.name, Kind: "export", Root: c.root, Path: c.rel,
					Line: pos.line, Col: pos.col, Source: exp.source,
				})
			}
		case "VarStmt":
			for _, decl := range varDecls(v) {
				c.add(decl.name, decl.kind, pos.line, pos.col)
			}
		case "FnDecl":
			if name, _ := v["name"].(string); name != "" {
				c.add(name, "function", pos.line, pos.col)
			}
		case "ClassDecl":
			if name, _ := v["name"].(string); name != "" {
				c.add(name, "class", pos.line, pos.col)
			}
		case "ArrowFn":
			if name, _ := v["name"].(string); name != "" {
				c.add(name, "function", pos.line, pos.col)
			}
		case "Identifier":
			if name, _ := v["name"].(string); name != "" && !isMemberProperty(parent, field) {
				c.addReference(name, pos.line, pos.col)
			}
		case "JSXElement":
			if opening, ok := v["opening"].(map[string]any); ok {
				if name, _ := opening["name"].(string); name != "" {
					if i := strings.IndexAny(name, ".:"); i >= 0 {
						name = name[:i]
					}
					if name != "" && name[0] >= 'A' && name[0] <= 'Z' {
						c.addReference(name, pos.line, pos.col)
					}
				}
			}
		}
		for key, child := range v {
			if key == "position" {
				continue
			}
			c.walk(child, v, key)
		}
	}
}

type posInfo struct{ line, col int }

func nodePos(node map[string]any) posInfo {
	pos, _ := node["position"].(map[string]any)
	return posInfo{line: jsonIntValue(pos["line"]), col: jsonIntValue(pos["col"])}
}

func jsonIntValue(v any) int {
	switch n := v.(type) {
	case float64:
		return int(n)
	case int:
		return n
	case int64:
		return int(n)
	default:
		return 0
	}
}

func isMemberProperty(parent map[string]any, field string) bool {
	if parent == nil || field != "property" {
		return false
	}
	if kind, _ := parent["kind"].(string); kind != "MemberExpr" {
		return false
	}
	computed, _ := parent["computed"].(bool)
	return !computed
}

type varDeclInfo struct {
	name string
	kind string
}

func varDecls(node map[string]any) []varDeclInfo {
	var out []varDeclInfo
	decls, _ := node["decls"].([]any)
	for _, raw := range decls {
		decl, _ := raw.(map[string]any)
		name, _ := decl["name"].(string)
		if name != "" && name != "{...}" {
			out = append(out, varDeclInfo{name: name, kind: varDeclKind(decl)})
		}
		if names, ok := decl["names"].([]any); ok {
			for _, item := range names {
				if name, _ := item.(string); name != "" {
					out = append(out, varDeclInfo{name: name, kind: "variable"})
				}
			}
		}
	}
	return out
}

func varDeclKind(decl map[string]any) string {
	init, _ := decl["init"].(map[string]any)
	switch kind, _ := init["kind"].(string); kind {
	case "ArrowFn":
		if fn, _ := init["function"].(bool); fn {
			return "function"
		}
		return "variable"
	case "ClassExpr":
		return "class"
	}
	return "variable"
}

type importSpec struct {
	local  string
	remote string
}

func importSpecifiers(node map[string]any) []importSpec {
	var out []importSpec
	if name, _ := node["default"].(string); name != "" {
		out = append(out, importSpec{local: name, remote: "default"})
	}
	if name, _ := node["namespace"].(string); name != "" {
		out = append(out, importSpec{local: name, remote: "*"})
	}
	if named, ok := node["named"].([]any); ok {
		for _, raw := range named {
			m, _ := raw.(map[string]any)
			local, _ := m["local"].(string)
			remote, _ := m["remote"].(string)
			if local != "" {
				out = append(out, importSpec{local: local, remote: remote})
			}
		}
	}
	return out
}

func exportLocalNames(node map[string]any) []string {
	var out []string
	if name, _ := node["local"].(string); name != "" {
		out = append(out, name)
	}
	if named, ok := node["named"].([]any); ok {
		for _, raw := range named {
			m, _ := raw.(map[string]any)
			if local, _ := m["local"].(string); local != "" {
				out = append(out, local)
			}
		}
	}
	return out
}

type exportExternal struct {
	name   string
	source string
}

func exportExternals(node map[string]any) []exportExternal {
	var out []exportExternal
	source, _ := node["reexportSource"].(string)
	if named, ok := node["reexportNamed"].([]any); ok {
		for _, raw := range named {
			m, _ := raw.(map[string]any)
			name, _ := m["remote"].(string)
			if name == "" {
				name, _ = m["local"].(string)
			}
			if name != "" {
				out = append(out, exportExternal{name: name, source: source})
			}
		}
	}
	if namespace, _ := node["namespace"].(string); namespace != "" {
		out = append(out, exportExternal{name: namespace, source: source})
	}
	return out
}
