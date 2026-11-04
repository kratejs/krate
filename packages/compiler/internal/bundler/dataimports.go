package bundler

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/kratejs/krate/packages/compiler/ast"
	"github.com/kratejs/krate/packages/compiler/internal/diag"
	"github.com/kratejs/krate/packages/compiler/internal/resolver"
)

// splitImportQuery splits "./a.txt?raw" into ("./a.txt", "raw").
func splitImportQuery(spec string) (base, query string, ok bool) {
	i := strings.LastIndexByte(spec, '?')
	if i <= 0 || i == len(spec)-1 {
		return spec, "", false
	}
	return spec[:i], spec[i+1:], true
}

// isDataImport reports whether an import is inlined as a constant: JSON, or a
// ?raw / ?url query import.
func isDataImport(spec string) bool {
	if _, _, ok := splitImportQuery(spec); ok {
		return true
	}
	return strings.EqualFold(filepath.Ext(spec), ".json")
}

// resolveQueryImport loads the file behind a ?raw or ?url import. ?raw needs no
// module (the text is inlined); ?url registers the file as a hashed asset.
func (b *Bundler) resolveQueryImport(mod *Module, imp, base, query string) error {
	resolved := b.resolveImportForModule(mod.Path, base)
	if resolved == "" {
		return b.unresolvedImportError(mod, imp)
	}
	switch query {
	case "raw":
		b.order = append(b.order, &Module{Path: resolved, IsExternal: true})
		return nil
	case "url":
		abs := resolved
		if !filepath.IsAbs(abs) {
			abs = filepath.Join(b.root, abs)
		}
		st := resolver.StatPath(abs)
		return b.resolveAssetModule(resolved, abs, st.Mtime, st.Size, st.Exists && !st.IsDir)
	}
	d := b.importDiag(mod, imp, "unsupported import query ?"+query,
		"supported queries are ?raw (file text as a string) and ?url (hashed asset URL)")
	return d
}

// importDiag builds a diagnostic located at the import statement of imp in mod.
func (b *Bundler) importDiag(mod *Module, imp, msg, hint string) diag.Diagnostic {
	d := diag.Diagnostic{File: mod.Path, Message: msg, Hint: hint}
	if mod.Program != nil {
		for _, stmt := range mod.Program.Body {
			if is, ok := stmt.(*ast.ImportStmt); ok && strings.Trim(is.Source, "\"'") == imp {
				d.Line, d.Col = is.Position.Line, is.Position.Col
				break
			}
		}
	}
	if mod.SourceCode != "" && d.Line > 0 {
		lines := strings.Split(mod.SourceCode, "\n")
		if d.Line <= len(lines) {
			d.Source = strings.TrimRight(lines[d.Line-1], "\r")
		}
	}
	return d
}

// rewriteDataImports replaces JSON, ?raw and ?url import statements with
// module-level constants holding the file's value, in every module that
// imports one. A constant is something the IR already folds into SSR text and
// emits into hydration scope; a bare import binding is neither.
func (b *Bundler) rewriteDataImports() error {
	for _, mod := range b.order {
		if mod.Program == nil || mod.IsExternal || !mod.Rewritable {
			continue
		}
		var body []ast.Stmt
		changed := false
		for _, stmt := range mod.Program.Body {
			imp, ok := stmt.(*ast.ImportStmt)
			if !ok {
				body = append(body, stmt)
				continue
			}
			spec := strings.Trim(imp.Source, "\"'")
			if !isDataImport(spec) {
				body = append(body, stmt)
				continue
			}
			decl, err := b.dataImportDecl(mod, imp, spec)
			if err != nil {
				return err
			}
			if decl == nil {
				body = append(body, stmt)
				continue
			}
			changed = true
			body = append(body, decl...)
		}
		if changed {
			mod.Program.Body = body
		}
	}
	return nil
}

func constDecl(pos ast.Pos, name string, init ast.Expr) ast.Stmt {
	return &ast.VarStmt{Position: pos, Kind: ast.VarConst, Decls: []*ast.VarDecl{{Name: name, Names: []string{name}, Init: init}}}
}

func (b *Bundler) dataImportDecl(mod *Module, imp *ast.ImportStmt, spec string) ([]ast.Stmt, error) {
	base, query, hasQuery := splitImportQuery(spec)
	resolved := b.resolveImportForModule(mod.Path, base)
	if resolved == "" {
		return nil, nil
	}
	abs := resolved
	if !filepath.IsAbs(abs) {
		abs = filepath.Join(b.root, abs)
	}
	switch {
	case hasQuery && query == "raw":
		if imp.Default == "" && imp.Namespace == "" {
			return nil, b.importDiag(mod, spec, "a ?raw import needs a default binding", "write `import text from "+strconv.Quote(spec)+"`")
		}
		data, err := os.ReadFile(abs)
		if err != nil {
			return nil, b.importDiag(mod, spec, "cannot read "+base+": "+err.Error(), "")
		}
		return bindDefault(imp, func() ast.Expr {
			return &ast.Literal{Position: imp.Position, Kind: ast.StringLit, Value: string(data)}
		}), nil
	case hasQuery && query == "url":
		url, ok := b.assets[filepath.Clean(abs)]
		if !ok {
			return nil, b.importDiag(mod, spec, "asset "+base+" was not registered", "")
		}
		return bindDefault(imp, func() ast.Expr {
			return &ast.Literal{Position: imp.Position, Kind: ast.StringLit, Value: url}
		}), nil
	case !hasQuery && strings.EqualFold(filepath.Ext(base), ".json"):
		data, err := os.ReadFile(abs)
		if err != nil {
			return nil, b.importDiag(mod, spec, "cannot read "+base+": "+err.Error(), "")
		}
		val, jerr := parseOrderedJSON(data)
		if jerr != nil {
			return nil, jsonDiagnostic(abs, data, jerr)
		}
		var out []ast.Stmt
		if imp.Default != "" {
			out = append(out, constDecl(imp.Position, imp.Default, orderedToExpr(val)))
		}
		if imp.Namespace != "" {
			ns := &orderedObject{keys: []string{"default"}, vals: map[string]any{"default": val}}
			if obj, ok := val.(*orderedObject); ok {
				ns.keys = append(append([]string(nil), obj.keys...), "default")
				ns.vals = map[string]any{"default": val}
				for k, v := range obj.vals {
					ns.vals[k] = v
				}
			}
			out = append(out, constDecl(imp.Position, imp.Namespace, orderedToExpr(ns)))
		}
		for _, n := range imp.Named {
			local := n.Local
			if local == "" {
				local = n.Remote
			}
			obj, ok := val.(*orderedObject)
			if !ok {
				return nil, b.importDiag(mod, spec, "cannot import "+strconv.Quote(n.Remote)+" by name from "+base+": the JSON root is not an object", "use a default import")
			}
			v, present := obj.vals[n.Remote]
			if !present {
				if n.Remote == "default" {
					out = append(out, constDecl(imp.Position, local, orderedToExpr(val)))
					continue
				}
				return nil, b.importDiag(mod, spec, base+" has no top-level key "+strconv.Quote(n.Remote), "check the name, or use a default import and read the property")
			}
			out = append(out, constDecl(imp.Position, local, orderedToExpr(v)))
		}
		if out == nil {
			// `import "./x.json"` for side effects only.
			out = []ast.Stmt{}
		}
		return out, nil
	}
	return nil, nil
}

func bindDefault(imp *ast.ImportStmt, value func() ast.Expr) []ast.Stmt {
	var out []ast.Stmt
	if imp.Default != "" {
		out = append(out, constDecl(imp.Position, imp.Default, value()))
	}
	if imp.Namespace != "" {
		out = append(out, constDecl(imp.Position, imp.Namespace, value()))
	}
	if out == nil {
		out = []ast.Stmt{}
	}
	return out
}

// orderedObject is a JSON object that keeps its source key order.
type orderedObject struct {
	keys []string
	vals map[string]any
}

func parseOrderedJSON(data []byte) (any, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	v, err := decodeOrdered(dec)
	if err != nil {
		return nil, err
	}
	if _, err := dec.Token(); err != io.EOF {
		if err == nil {
			return nil, fmt.Errorf("unexpected data after the top-level value")
		}
		return nil, err
	}
	return v, nil
}

func decodeOrdered(dec *json.Decoder) (any, error) {
	tok, err := dec.Token()
	if err != nil {
		return nil, err
	}
	switch t := tok.(type) {
	case json.Delim:
		switch t {
		case '{':
			obj := &orderedObject{vals: map[string]any{}}
			for dec.More() {
				kt, err := dec.Token()
				if err != nil {
					return nil, err
				}
				k := kt.(string)
				v, err := decodeOrdered(dec)
				if err != nil {
					return nil, err
				}
				if _, dup := obj.vals[k]; !dup {
					obj.keys = append(obj.keys, k)
				}
				obj.vals[k] = v
			}
			if _, err := dec.Token(); err != nil {
				return nil, err
			}
			return obj, nil
		case '[':
			arr := []any{}
			for dec.More() {
				v, err := decodeOrdered(dec)
				if err != nil {
					return nil, err
				}
				arr = append(arr, v)
			}
			if _, err := dec.Token(); err != nil {
				return nil, err
			}
			return arr, nil
		}
	}
	return tok, nil
}

func orderedToExpr(val any) ast.Expr {
	switch v := val.(type) {
	case nil:
		return &ast.Literal{Kind: ast.NullLit, Value: "null"}
	case bool:
		if v {
			return &ast.Literal{Kind: ast.BoolLit, Value: "true"}
		}
		return &ast.Literal{Kind: ast.BoolLit, Value: "false"}
	case json.Number:
		return &ast.Literal{Kind: ast.NumberLit, Value: v.String()}
	case string:
		return &ast.Literal{Kind: ast.StringLit, Value: v}
	case []any:
		elems := make([]ast.Expr, len(v))
		for i, e := range v {
			elems[i] = orderedToExpr(e)
		}
		return &ast.ArrayExpr{Elements: elems}
	case *orderedObject:
		props := make([]*ast.ObjectProp, 0, len(v.keys))
		for _, k := range v.keys {
			props = append(props, &ast.ObjectProp{Key: k, Value: orderedToExpr(v.vals[k])})
		}
		return &ast.ObjectExpr{Properties: props}
	}
	return &ast.Literal{Kind: ast.NullLit, Value: "null"}
}

// jsonToAST turns a JSON file into a module whose default export is its value.
func jsonToAST(data []byte) (*ast.Program, error) {
	val, err := parseOrderedJSON(data)
	if err != nil {
		return nil, err
	}
	return &ast.Program{
		Body: []ast.Stmt{
			&ast.ExportStmt{
				Default:     true,
				Declaration: &ast.ExprStmt{Expression: orderedToExpr(val)},
			},
		},
	}, nil
}

// jsonDiagnostic locates a JSON syntax error as file:line:col.
func jsonDiagnostic(path string, data []byte, err error) diag.Diagnostic {
	d := diag.Diagnostic{File: path, Line: 1, Col: 1, Message: "invalid JSON: " + err.Error(), Hint: "fix the JSON syntax (no comments or trailing commas), or import a .js/.ts module instead"}
	var off int64 = -1
	if se, ok := err.(*json.SyntaxError); ok {
		off = se.Offset
	} else if ute, ok := err.(*json.UnmarshalTypeError); ok {
		off = ute.Offset
	} else if err == io.ErrUnexpectedEOF {
		off = int64(len(data))
	}
	if off > int64(len(data)) {
		off = int64(len(data))
	}
	if off >= 0 {
		line, col := 1, 1
		lineStart := 0
		for i := 0; i < int(off) && i < len(data); i++ {
			if data[i] == '\n' {
				line++
				col = 1
				lineStart = i + 1
			} else {
				col++
			}
		}
		d.Line, d.Col = line, col
		end := bytes.IndexByte(data[lineStart:], '\n')
		if end < 0 {
			end = len(data) - lineStart
		}
		d.Source = strings.TrimRight(string(data[lineStart:lineStart+end]), "\r")
	}
	return d
}
