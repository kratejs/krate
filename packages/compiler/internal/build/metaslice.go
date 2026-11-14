package build

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/kratejs/krate/packages/compiler/ast"
	"github.com/kratejs/krate/packages/compiler/internal/lexer"
	"github.com/kratejs/krate/packages/compiler/internal/parser"
)

// Build-time evaluation of metadata and generateStaticParams needs a few
// exports of a page, not the page: the default component, the JSX around it and
// every import only it uses would also run, and a component that touches
// `document` at module scope would break the build of a page whose metadata is
// one call. sliceModule cuts a module down to the named exports and what they
// depend on, so only that closure is bundled and run.

// moduleSlice is a module reduced to the dependency closure of some exports.
type moduleSlice struct {
	// Path is where the slice pretends to live: next to the original, so its
	// relative imports resolve the same way.
	Path string
	// Source is the reduced module.
	Source string
}

// sliceExt lists the module kinds sliceModule understands.
var sliceExt = map[string]bool{".tsx": true, ".ts": true, ".jsx": true, ".js": true, ".mts": true, ".mjs": true}

// sliceModule returns the module at path reduced to the closure of the named
// exports. ok is false when the closure cannot be isolated safely (the module
// does not parse, an export is not a plain declaration, or other top-level code
// could change what the closure reads); callers then evaluate the whole module.
func sliceModule(path string, exports []string) (moduleSlice, bool) {
	if !sliceExt[strings.ToLower(filepath.Ext(path))] {
		return moduleSlice{}, false
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return moduleSlice{}, false
	}
	src := string(data)
	toks := lexer.ForFile(path, src).Tokenize()
	p := parser.New(toks)
	p.Filename = path
	p.SetSource(src)
	prog := p.ParseProgram()
	if len(p.Errors()) > 0 || len(prog.Body) == 0 {
		return moduleSlice{}, false
	}

	n := len(prog.Body)
	starts := make([]int, n+1)
	for i, st := range prog.Body {
		starts[i] = st.Pos().Offset
		if i > 0 && starts[i] <= starts[i-1] {
			return moduleSlice{}, false
		}
	}
	starts[n] = len(src)
	if starts[n-1] > len(src) {
		return moduleSlice{}, false
	}

	// refs[i] holds the identifiers statement i mentions (an over-approximation:
	// it includes local names that shadow a top-level one).
	refs := identifierRefs(toks, starts)

	type binding struct {
		stmt    int
		mutable bool
	}
	decls := map[string]binding{}
	type importRef struct {
		stmt int
		spec string // local name
	}
	imports := map[string]importRef{}
	addDecl := func(i int, d ast.Stmt) bool {
		switch d := d.(type) {
		case *ast.VarStmt:
			for _, vd := range d.Decls {
				names := vd.Names
				if vd.Name != "" {
					names = append([]string{vd.Name}, names...)
				}
				if vd.RestName != "" {
					names = append(names, vd.RestName)
				}
				for _, nm := range names {
					decls[nm] = binding{stmt: i, mutable: d.Kind != ast.VarConst}
				}
			}
		case *ast.FnDecl:
			decls[d.Name] = binding{stmt: i}
		case *ast.ClassDecl:
			decls[d.Name] = binding{stmt: i}
		default:
			return false
		}
		return true
	}
	for i, st := range prog.Body {
		switch s := st.(type) {
		case *ast.ImportStmt:
			if s.Default != "" {
				imports[s.Default] = importRef{i, s.Default}
			}
			if s.Namespace != "" {
				imports[s.Namespace] = importRef{i, s.Namespace}
			}
			for _, nm := range s.Named {
				imports[nm.Local] = importRef{i, nm.Local}
			}
		case *ast.ExportStmt:
			if s.Declaration != nil {
				addDecl(i, s.Declaration)
			}
		default:
			addDecl(i, st)
		}
	}

	// Roots: the statements that declare the requested exports.
	roots := map[int]bool{}
	for _, name := range exports {
		idx, found := exportedDecl(prog, name)
		if found == exportNone {
			continue
		}
		if found == exportOpaque {
			return moduleSlice{}, false
		}
		roots[idx] = true
	}
	if len(roots) == 0 {
		return moduleSlice{}, false
	}

	included := map[int]bool{}
	needImport := map[string]bool{}
	var queue []int
	for i := range roots {
		included[i] = true
		queue = append(queue, i)
	}
	for len(queue) > 0 {
		i := queue[0]
		queue = queue[1:]
		for name := range refs[i] {
			if im, ok := imports[name]; ok {
				needImport[im.spec] = true
				continue
			}
			if d, ok := decls[name]; ok && !included[d.stmt] {
				included[d.stmt] = true
				queue = append(queue, d.stmt)
			}
		}
	}

	// Safety: top-level code that is not part of the closure must not touch
	// what the closure reads. Declarations and functions only run when called;
	// anything else (a call, an assignment, a loop) runs at load.
	local := map[string]bool{}
	for name, d := range decls {
		if included[d.stmt] {
			local[name] = true
		}
	}
	for i, st := range prog.Body {
		if included[i] {
			continue
		}
		target := st
		if ex, ok := st.(*ast.ExportStmt); ok {
			if ex.Declaration == nil {
				continue
			}
			target = ex.Declaration
		}
		onlyMutable := false
		switch target.(type) {
		case *ast.ImportStmt, *ast.FnDecl, *ast.ClassDecl, *ast.EmptyStmt:
			continue
		case *ast.VarStmt:
			// An initializer can mutate through a call, so it may not touch a
			// binding that can change; a const is read, not changed.
			onlyMutable = true
		}
		for name := range refs[i] {
			if local[name] && (!onlyMutable || decls[name].mutable) {
				return moduleSlice{}, false
			}
		}
	}
	// A let or var in the closure may be assigned by a function that runs
	// later; only a binding nobody else mentions is safe to copy.
	for name, d := range decls {
		if !included[d.stmt] || !d.mutable {
			continue
		}
		for i := range prog.Body {
			if !included[i] && refs[i][name] {
				return moduleSlice{}, false
			}
		}
	}

	var out strings.Builder
	for _, st := range prog.Body {
		if imp, ok := st.(*ast.ImportStmt); ok {
			writeSliceImport(&out, imp, needImport)
		}
	}
	for i, st := range prog.Body {
		if !included[i] || isImportStmt(st) {
			continue
		}
		text := src[starts[i]:starts[i+1]]
		if !roots[i] {
			text = stripExportKeyword(text)
		}
		out.WriteString(text)
		out.WriteString("\n")
	}
	dir := filepath.Dir(path)
	return moduleSlice{
		Path:   filepath.Join(dir, ".krate-slice-"+filepath.Base(path)),
		Source: injectServerGlobals(out.String()),
	}, true
}

func isImportStmt(s ast.Stmt) bool {
	_, ok := s.(*ast.ImportStmt)
	return ok
}

const (
	exportNone   = iota // the module does not export the name
	exportDecl          // exported by a plain declaration
	exportOpaque        // exported some other way (a list, a re-export, a default)
)

// exportedDecl finds the top-level statement that declares the export name.
func exportedDecl(prog *ast.Program, name string) (int, int) {
	idx, kind := -1, exportNone
	for i, st := range prog.Body {
		ex, ok := st.(*ast.ExportStmt)
		if !ok {
			continue
		}
		switch d := ex.Declaration.(type) {
		case *ast.VarStmt:
			for _, vd := range d.Decls {
				if vd.Name == name || containsString(vd.Names, name) {
					if len(d.Decls) > 1 || vd.IsDestructuring {
						return -1, exportOpaque
					}
					idx, kind = i, exportDecl
				}
			}
		case *ast.FnDecl:
			if d.Name == name && !ex.Default {
				idx, kind = i, exportDecl
			}
		}
		for _, nm := range ex.Named {
			if nm.Remote == name {
				return -1, exportOpaque
			}
		}
		for _, nm := range ex.ReexportNamed {
			if nm.Remote == name {
				return -1, exportOpaque
			}
		}
		if ex.StarReexport && ex.ReexportSource != "" {
			return -1, exportOpaque
		}
	}
	return idx, kind
}

// stripExportKeyword turns `export const x`, `export async function f` and
// `export default function f` into the plain declaration.
func stripExportKeyword(text string) string {
	rest := strings.TrimPrefix(text, "export")
	if rest == text {
		return text
	}
	rest = strings.TrimLeft(rest, " \t\r\n")
	if strings.HasPrefix(rest, "default") {
		rest = strings.TrimLeft(strings.TrimPrefix(rest, "default"), " \t\r\n")
	}
	return rest
}

// writeSliceImport writes the part of an import the slice needs: only the
// bindings in need, nothing for a side-effect import.
func writeSliceImport(out *strings.Builder, imp *ast.ImportStmt, need map[string]bool) {
	var named []string
	for _, nm := range imp.Named {
		if !need[nm.Local] {
			continue
		}
		if nm.Remote == "" || nm.Remote == nm.Local {
			named = append(named, nm.Local)
		} else {
			named = append(named, nm.Remote+" as "+nm.Local)
		}
	}
	def := ""
	if imp.Default != "" && need[imp.Default] {
		def = imp.Default
	}
	ns := ""
	if imp.Namespace != "" && need[imp.Namespace] {
		ns = imp.Namespace
	}
	attrs := ""
	if len(imp.Attributes) > 0 {
		var kv []string
		for _, a := range imp.Attributes {
			kv = append(kv, a.Key+": "+strconv.Quote(a.Value))
		}
		attrs = " with { " + strings.Join(kv, ", ") + " }"
	}
	src := strconv.Quote(strings.Trim(imp.Source, `'"`))
	switch {
	case ns != "":
		if def != "" {
			out.WriteString("import " + def + " from " + src + attrs + ";\n")
		}
		out.WriteString("import * as " + ns + " from " + src + attrs + ";\n")
	case def != "" || len(named) > 0:
		clause := def
		if len(named) > 0 {
			if clause != "" {
				clause += ", "
			}
			clause += "{ " + strings.Join(named, ", ") + " }"
		}
		out.WriteString("import " + clause + " from " + src + attrs + ";\n")
	}
}

// identifierRefs returns, for the statement that starts at starts[i] and ends
// at starts[i+1], the identifiers its tokens mention as bindings: not property
// names after a dot, and not object keys.
func identifierRefs(toks []lexer.Token, starts []int) []map[string]bool {
	out := make([]map[string]bool, len(starts)-1)
	for i := range out {
		out[i] = map[string]bool{}
	}
	stmt := 0
	var prev lexer.Token
	for i, t := range toks {
		if t.Offset < starts[0] {
			continue
		}
		for stmt < len(out)-1 && t.Offset >= starts[stmt+1] {
			stmt++
		}
		if t.Offset >= starts[len(starts)-1] {
			break
		}
		switch t.Kind {
		case lexer.String, lexer.Number, lexer.Regexp, lexer.JSXText, lexer.TEMPLATE_START, lexer.TEMPLATE_MID, lexer.TEMPLATE_END:
			prev = t
			continue
		}
		if isIdentifierName(t.Value) && prev.Kind != lexer.DOT && prev.Kind != lexer.QUESTION_DOT {
			key := i+1 < len(toks) && toks[i+1].Kind == lexer.COLON && (prev.Kind == lexer.LBRACE || prev.Kind == lexer.COMMA)
			if !key {
				out[stmt][t.Value] = true
			}
		}
		prev = t
	}
	return out
}

func isIdentifierName(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c == '_' || c == '$' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= 0x80:
		case c >= '0' && c <= '9' && i > 0:
		default:
			return false
		}
	}
	return true
}
