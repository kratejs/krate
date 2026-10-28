package migrate

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/kratejs/krate/packages/compiler/ast"
	"github.com/kratejs/krate/packages/compiler/internal/lexer"
	"github.com/kratejs/krate/packages/compiler/internal/parser"
)

// mergeKrateConfig adds missing Next-derived values to an existing config.
// Existing keys always win and are reported back to the caller.
func mergeKrateConfig(root string, next nextConfigData) (PlannedFile, []string, error) {
	values := nextConfigValues(next)
	if len(values) == 0 {
		return PlannedFile{}, nil, nil
	}
	var existing string
	for _, name := range []string{"krate.config.ts", "krate.config.js", "krate.config.mjs", "krate.config.json"} {
		if path := filepath.Join(root, name); fileExists(path) {
			existing = path
			break
		}
	}
	if existing == "" {
		return PlannedFile{}, nil, fmt.Errorf("no existing Krate config")
	}
	rel, _ := filepath.Rel(root, existing)
	rel = filepath.ToSlash(rel)
	data, err := os.ReadFile(existing)
	if err != nil {
		return PlannedFile{}, nil, err
	}
	if strings.HasSuffix(existing, ".json") {
		out, notes, err := mergeJSONConfig(data, values)
		if err != nil {
			return PlannedFile{}, nil, err
		}
		return PlannedFile{Kind: KindConfig, Source: rel, Target: rel, Content: out}, notes, nil
	}

	prog := parser.New(lexer.New(string(data)).Tokenize()).ParseProgram()
	obj := configObject(prog)
	if obj == nil {
		return PlannedFile{}, nil, fmt.Errorf("config is not a static object or defineConfig({...})")
	}
	var additions []string
	var notes []string
	for _, key := range []string{"basePath", "redirects", "rewrites", "headers", "images", "site"} {
		value, ok := values[key]
		if !ok {
			continue
		}
		if prop(obj, key) != nil {
			notes = append(notes, "existing "+key+" was kept; merge Next.js "+key+" manually if needed")
			continue
		}
		enc, err := json.MarshalIndent(value, "  ", "  ")
		if err != nil {
			return PlannedFile{}, nil, err
		}
		additions = append(additions, fmt.Sprintf("  %q: %s,", key, enc))
	}
	if len(additions) == 0 {
		return PlannedFile{}, notes, nil
	}
	end, ok := objectEnd(string(data), obj.Position.Offset)
	if !ok {
		return PlannedFile{}, nil, fmt.Errorf("could not find the end of the config object")
	}
	insert := "\n" + strings.Join(additions, "\n") + "\n"
	prefix := string(data[:end])
	if last := lastNonSpace(prefix); last != "" && last != "{" && last != "," {
		insert = "," + insert
	}
	out := prefix + insert + string(data[end:])
	p := parser.New(lexer.New(out).Tokenize())
	if prog := p.ParseProgram(); prog == nil || len(p.Errors()) > 0 {
		return PlannedFile{}, nil, fmt.Errorf("merged config does not parse: %v", p.Errors())
	}
	return PlannedFile{Kind: KindConfig, Source: rel, Target: rel, Content: []byte(out)}, notes, nil
}

func nextConfigValues(next nextConfigData) map[string]any {
	values := map[string]any{}
	if next.BasePath != "" {
		values["basePath"] = next.BasePath
	}
	if len(next.Redirects) > 0 {
		values["redirects"] = next.Redirects
	}
	if len(next.Rewrites) > 0 {
		values["rewrites"] = next.Rewrites
	}
	if len(next.Headers) > 0 {
		values["headers"] = next.Headers
	}
	if len(next.Images) > 0 {
		values["images"] = next.Images
	}
	if len(next.Site) > 0 {
		values["site"] = next.Site
	}
	return values
}

func mergeJSONConfig(data []byte, values map[string]any) ([]byte, []string, error) {
	var obj map[string]any
	if err := json.Unmarshal(data, &obj); err != nil {
		return nil, nil, err
	}
	var notes []string
	for _, key := range []string{"basePath", "redirects", "rewrites", "headers", "images", "site"} {
		value, ok := values[key]
		if !ok {
			continue
		}
		if _, exists := obj[key]; exists {
			notes = append(notes, "existing "+key+" was kept; merge Next.js "+key+" manually if needed")
			continue
		}
		obj[key] = value
	}
	out, err := json.MarshalIndent(obj, "", "  ")
	if err != nil {
		return nil, nil, err
	}
	return append(out, '\n'), notes, nil
}

// configObject finds the object literal used by `export default {...}` or
// `export default defineConfig({...})` (including a function-form defineConfig).
func configObject(prog *ast.Program) *ast.ObjectExpr {
	if prog == nil {
		return nil
	}
	for _, stmt := range prog.Body {
		exp, ok := stmt.(*ast.ExportStmt)
		if !ok || !exp.Default {
			continue
		}
		es, ok := exp.Declaration.(*ast.ExprStmt)
		if !ok {
			continue
		}
		if obj, ok := es.Expression.(*ast.ObjectExpr); ok {
			return obj
		}
		call, ok := es.Expression.(*ast.CallExpr)
		if !ok || len(call.Args) == 0 || !isDefineConfig(call.Callee) {
			continue
		}
		switch arg := call.Args[0].(type) {
		case *ast.ObjectExpr:
			return arg
		case *ast.ArrowFn:
			if len(arg.Body) == 1 {
				if ret, ok := arg.Body[0].(*ast.ReturnStmt); ok {
					if obj, ok := ret.Value.(*ast.ObjectExpr); ok {
						return obj
					}
				}
			}
		}
	}
	return nil
}

func isDefineConfig(e ast.Expr) bool {
	id, ok := e.(*ast.Identifier)
	return ok && id.Name == "defineConfig"
}

func objectEnd(src string, start int) (int, bool) {
	if start < 0 || start >= len(src) || src[start] != '{' {
		return 0, false
	}
	depth := 0
	var quote byte
	lineComment, blockComment := false, false
	for i := start; i < len(src); i++ {
		c := src[i]
		if lineComment {
			if c == '\n' {
				lineComment = false
			}
			continue
		}
		if blockComment {
			if c == '*' && i+1 < len(src) && src[i+1] == '/' {
				blockComment = false
				i++
			}
			continue
		}
		if quote != 0 {
			if c == '\\' {
				i++
				continue
			}
			if c == quote {
				quote = 0
			}
			continue
		}
		switch c {
		case '/':
			if i+1 < len(src) {
				if src[i+1] == '/' {
					lineComment = true
					i++
					continue
				}
				if src[i+1] == '*' {
					blockComment = true
					i++
					continue
				}
			}
		case '\'', '"', '`':
			quote = c
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return i, true
			}
		}
	}
	return 0, false
}

func lastNonSpace(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	return s[len(s)-1:]
}
