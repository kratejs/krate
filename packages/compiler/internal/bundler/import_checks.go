package bundler

import (
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/kratejs/krate/packages/compiler/ast"
	"github.com/kratejs/krate/packages/compiler/internal/diag"
	"github.com/kratejs/krate/packages/compiler/internal/lexer"
	"github.com/kratejs/krate/packages/compiler/internal/parser"
	"github.com/kratejs/krate/packages/compiler/internal/resolver"
)

// importDiag builds a diagnostic located at the import of imp in mod.
func importDiag(mod *Module, imp, message, hint string, code diag.Code) diag.Diagnostic {
	var pos ast.Pos
	for _, stmt := range mod.Program.Body {
		if st, ok := stmt.(*ast.ImportStmt); ok && st.Position.Line > 0 && strings.Trim(st.Source, "\"'`") == imp {
			pos = st.Position
			break
		}
	}
	d := diag.Diagnostic{File: mod.Path, Line: pos.Line, Col: pos.Col, Message: message, Hint: hint}
	if mod.SourceCode != "" && pos.Line > 0 {
		lines := strings.Split(mod.SourceCode, "\n")
		if pos.Line <= len(lines) {
			d.Source = strings.TrimRight(lines[pos.Line-1], "\r")
		}
	}
	return d.WithCode(code)
}

// ownProjectCode reports whether mod is source the project can fix.
func ownProjectCode(mod *Module) bool {
	if mod.IsExternal || mod.Program == nil {
		return false
	}
	return !strings.Contains(filepath.ToSlash(mod.Path), "/node_modules/") && !resolver.IsCompiledDependency(mod.Path)
}

// unsupportedStyleExts are stylesheet languages the CSS pipeline cannot compile.
var unsupportedStyleExts = map[string]string{
	".scss": "Sass", ".sass": "Sass", ".less": "Less", ".styl": "Stylus", ".stylus": "Stylus",
}

// checkUnsupportedStylesheet reports an import of a Sass, Less or Stylus file,
// which would otherwise be dropped without a trace.
func checkUnsupportedStylesheet(mod *Module, imp string) error {
	if !ownProjectCode(mod) {
		return nil
	}
	base, _, _ := splitImportQuery(imp)
	lang, ok := unsupportedStyleExts[strings.ToLower(filepath.Ext(base))]
	if !ok {
		return nil
	}
	return importDiag(mod, imp,
		lang+" files are not supported: "+strconv.Quote(imp)+" would add no styles",
		"Krate has no "+lang+" compiler. Compile it to CSS first (for example `sass src/styles.scss src/styles.css` in a prebuild script) and import the .css file, or port the styles to CSS or Tailwind",
		diag.CodeUnsupportedStylesheet)
}

// checkRuntimeImports reports named imports of @krate/runtime that the
// package does not export. Such an import compiles to nothing, so the name is
// undefined in the browser.
func (b *Bundler) checkRuntimeImports(mod *Module) error {
	if !ownProjectCode(mod) {
		return nil
	}
	for _, stmt := range mod.Program.Body {
		imp, ok := stmt.(*ast.ImportStmt)
		// Imports without a position were added by the compiler itself.
		if !ok || imp.Position.Line == 0 || len(imp.Named) == 0 {
			continue
		}
		spec := strings.Trim(imp.Source, "\"'`")
		if spec != "@krate/runtime" && !strings.HasPrefix(spec, "@krate/runtime/") {
			continue
		}
		target := b.resolveImportForModule(mod.Path, spec)
		if target == "" {
			continue
		}
		exports := runtimeExports(target)
		if exports == nil {
			continue
		}
		for _, n := range imp.Named {
			if n.Remote == "" || n.Remote == "default" || exports[n.Remote] {
				continue
			}
			// The compiler provides these tags, so no module exports them.
			if spec == "@krate/runtime" && builtinComponentName(n.Remote) {
				continue
			}
			hint := runtimeExportHint(n.Remote, spec, exports)
			return importDiag(mod, spec, strconv.Quote(n.Remote)+" is not exported by "+strconv.Quote(spec), hint, diag.CodeRuntimeExportMissing)
		}
	}
	return nil
}

func runtimeExportHint(name, spec string, exports map[string]bool) string {
	switch name {
	case "usePathname", "useSearchParams", "useRouter", "useParams", "useLocation", "useNavigate":
		return "read the URL with getNavigationState() and subscribe with subscribeNavigation(); navigate() changes it (see the runtime API reference)"
	}
	lower := strings.ToLower(name)
	var near []string
	for e := range exports {
		le := strings.ToLower(e)
		if strings.Contains(le, lower) || strings.Contains(lower, le) && len(le) > 3 || editDistance(le, lower) <= 2 {
			near = append(near, e)
		}
	}
	sort.Strings(near)
	if len(near) > 4 {
		near = near[:4]
	}
	if len(near) > 0 {
		return "did you mean " + strings.Join(near, ", ") + "? Check the runtime API reference for the names " + strconv.Quote(spec) + " exports"
	}
	return "check the runtime API reference for the names " + strconv.Quote(spec) + " exports"
}

func editDistance(a, b string) int {
	prev := make([]int, len(b)+1)
	cur := make([]int, len(b)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(a); i++ {
		cur[0] = i
		for j := 1; j <= len(b); j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			cur[j] = min(prev[j]+1, cur[j-1]+1, prev[j-1]+cost)
		}
		prev, cur = cur, prev
	}
	return prev[len(b)]
}

type exportSetEntry struct {
	mtime time.Time
	size  int64
	names map[string]bool
}

var (
	exportSetMu    sync.Mutex
	exportSetCache = map[string]exportSetEntry{}
)

// runtimeExports returns the export names of a package entry file, following
// `export *` and named re-exports through relative files. It returns nil when
// the entry is not plain JavaScript or cannot be read and parsed, so the check
// never fails a build over a package layout it does not understand.
func runtimeExports(entry string) map[string]bool {
	switch strings.ToLower(filepath.Ext(entry)) {
	case ".js", ".mjs":
	default:
		return nil
	}
	st := resolver.StatPath(entry)
	if !st.Exists || st.IsDir {
		return nil
	}
	exportSetMu.Lock()
	if e, ok := exportSetCache[entry]; ok && e.mtime.Equal(st.Mtime) && e.size == st.Size {
		exportSetMu.Unlock()
		return e.names
	}
	exportSetMu.Unlock()

	names := map[string]bool{}
	if !collectExportNames(entry, names, map[string]bool{}, 0) {
		names = nil
	}
	exportSetMu.Lock()
	exportSetCache[entry] = exportSetEntry{mtime: st.Mtime, size: st.Size, names: names}
	exportSetMu.Unlock()
	return names
}

func collectExportNames(file string, names, seen map[string]bool, depth int) bool {
	if seen[file] || depth > 24 {
		return true
	}
	seen[file] = true
	data, err := os.ReadFile(file)
	if err != nil {
		return false
	}
	p := parser.New(lexer.New(string(data)).Tokenize())
	p.Filename = file
	prog := p.ParseProgram()
	if len(p.Errors()) > 0 {
		return false
	}
	for _, stmt := range prog.Body {
		exp, ok := stmt.(*ast.ExportStmt)
		if !ok {
			continue
		}
		switch {
		case exp.Default:
			names["default"] = true
		case exp.StarReexport:
			target := resolveImport(file, strings.Trim(exp.ReexportSource, "\"'`"), resolver.Exists, nil)
			if target == "" || !collectExportNames(target, names, seen, depth+1) {
				return false
			}
		case exp.Namespace != "":
			names[exp.Namespace] = true
		case exp.ReexportSource != "":
			for _, n := range exp.ReexportNamed {
				names[n.Remote] = true
			}
		default:
			for _, n := range exp.Named {
				names[n.Remote] = true
			}
			for _, name := range declaredNames(exp.Declaration) {
				names[name] = true
			}
		}
	}
	return true
}
