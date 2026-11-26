package bundler

import (
	"os"
	"path/filepath"
	"regexp"
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

// projectExportExts are the source files whose exports the named-import check
// can enumerate. Everything else (JSON, CSS, Markdown, Go, assets) is skipped.
var projectExportExts = map[string]bool{".ts": true, ".tsx": true, ".js": true, ".jsx": true, ".mjs": true, ".mts": true}

var (
	// Type-only and ambient declarations are erased by the parser, so they are
	// read from the source text.
	typeExportRe    = regexp.MustCompile(`(?m)^[ \t]*export\s+(?:declare\s+)?(?:const\s+)?(?:type|interface|enum|namespace|module)\s+([A-Za-z_$][\w$]*)`)
	declareExportRe = regexp.MustCompile(`(?m)^[ \t]*export\s+declare\s+(?:abstract\s+)?(?:const|let|var|function|class)\s+([A-Za-z_$][\w$]*)`)
	exportListRe    = regexp.MustCompile(`export\s+(?:type\s*)?\{([^}]*)\}`)
	// Shapes whose export set the check does not model.
	opaqueModuleRe = regexp.MustCompile(`(?m)^[ \t]*(?:export\s*=|export\s+as\s+namespace|declare\s+(?:module|global)\b)|\bmodule\.exports\b|(?:^|[^.\w$])exports\.[A-Za-z_$]`)
)

type projectExportInfo struct {
	mtime time.Time
	size  int64
	// names are the module's own exports; stars are the `export *` sources.
	names map[string]bool
	stars []string
	// ok is false when the module's exports cannot be enumerated.
	ok bool
}

var (
	projectExportMu    sync.Mutex
	projectExportCache = map[string]projectExportInfo{}
)

func readProjectExports(file string) projectExportInfo {
	st := resolver.StatPath(file)
	if !st.Exists || st.IsDir {
		return projectExportInfo{}
	}
	projectExportMu.Lock()
	if e, ok := projectExportCache[file]; ok && e.mtime.Equal(st.Mtime) && e.size == st.Size {
		projectExportMu.Unlock()
		return e
	}
	projectExportMu.Unlock()

	info := projectExportInfo{mtime: st.Mtime, size: st.Size}
	if data, err := os.ReadFile(file); err == nil {
		info = scanProjectExports(file, string(data), info)
	}
	projectExportMu.Lock()
	projectExportCache[file] = info
	projectExportMu.Unlock()
	return info
}

func scanProjectExports(file, src string, info projectExportInfo) projectExportInfo {
	ext := strings.ToLower(filepath.Ext(file))
	if !projectExportExts[ext] || strings.HasSuffix(strings.ToLower(file), ".d.ts") || opaqueModuleRe.MatchString(src) {
		return info
	}
	p := parser.New(lexer.ForFile(file, src).Tokenize())
	p.Filename = file
	prog := p.ParseProgram()
	if len(p.Errors()) > 0 {
		return info
	}
	names := map[string]bool{}
	for _, stmt := range prog.Body {
		exp, ok := stmt.(*ast.ExportStmt)
		if !ok {
			continue
		}
		switch {
		case exp.Default:
			names["default"] = true
		case exp.StarReexport:
			info.stars = append(info.stars, strings.Trim(exp.ReexportSource, "\"'`"))
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
	for _, m := range typeExportRe.FindAllStringSubmatch(src, -1) {
		names[m[1]] = true
	}
	for _, m := range declareExportRe.FindAllStringSubmatch(src, -1) {
		names[m[1]] = true
	}
	// `export { type A, b as c }` and `export type { A } from "x"`: the last
	// word of each specifier is the exported name.
	for _, m := range exportListRe.FindAllStringSubmatch(src, -1) {
		for _, spec := range strings.Split(m[1], ",") {
			if f := strings.Fields(spec); len(f) > 0 {
				names[strings.Trim(f[len(f)-1], "\"'")] = true
			}
		}
	}
	info.names = names
	info.ok = true
	return info
}

// projectExportNames returns every name a project module exports, following
// `export *` through project files. ok is false when the set is not known.
func (b *Bundler) projectExportNames(file string, seen map[string]bool, depth int) (map[string]bool, bool) {
	if seen[file] {
		return nil, true
	}
	seen[file] = true
	if depth > 16 || !b.checkableProjectFile(file) {
		return nil, false
	}
	info := readProjectExports(file)
	if !info.ok {
		return nil, false
	}
	out := make(map[string]bool, len(info.names))
	for n := range info.names {
		out[n] = true
	}
	for _, star := range info.stars {
		target := b.resolveImportForModule(file, star)
		if target == "" {
			return nil, false
		}
		sub, ok := b.projectExportNames(target, seen, depth+1)
		if !ok {
			return nil, false
		}
		for n := range sub {
			if n != "default" {
				out[n] = true
			}
		}
	}
	return out, true
}

// checkableProjectFile reports whether file is the project's own source.
func (b *Bundler) checkableProjectFile(file string) bool {
	if !projectExportExts[strings.ToLower(filepath.Ext(file))] {
		return false
	}
	slash := filepath.ToSlash(file)
	if strings.Contains(slash, "/node_modules/") || resolver.IsCompiledDependency(file) {
		return false
	}
	rel, err := filepath.Rel(b.root, file)
	return err == nil && !strings.HasPrefix(rel, "..")
}

// checkNamedImports reports a named import of a project module (a relative
// path or a tsconfig alias) that the module does not export. The import would
// bundle as an undefined binding and fail only in the browser.
func (b *Bundler) checkNamedImports(mod *Module) error {
	if !ownProjectCode(mod) {
		return nil
	}
	for _, stmt := range mod.Program.Body {
		imp, ok := stmt.(*ast.ImportStmt)
		// Imports without a position were added by the compiler itself.
		if !ok || imp.Position.Line == 0 || len(imp.Named) == 0 || len(imp.Attributes) > 0 {
			continue
		}
		spec := strings.Trim(imp.Source, "\"'`")
		if spec == "" || strings.ContainsAny(spec, "?#") || spec == "krate" || strings.HasPrefix(spec, "krate/") || strings.HasPrefix(spec, "@krate/") {
			continue
		}
		target := b.resolveImportForModule(mod.Path, spec)
		if target == "" || !b.checkableProjectFile(target) {
			continue
		}
		var names map[string]bool
		known := false
		for _, n := range imp.Named {
			if n.Remote == "" || n.Remote == "default" {
				continue
			}
			if !known {
				names, ok = b.projectExportNames(target, map[string]bool{}, 0)
				if !ok {
					break
				}
				known = true
			}
			if names[n.Remote] {
				continue
			}
			rel, _ := filepath.Rel(b.root, target)
			msg := strconv.Quote(n.Remote) + " is not exported by " + strconv.Quote(spec) + " (" + filepath.ToSlash(rel) + ")"
			return importDiag(mod, spec, msg, namedImportHint(n.Remote, names), diag.CodeNamedImportMissing)
		}
	}
	return nil
}

func namedImportHint(name string, exports map[string]bool) string {
	lower := strings.ToLower(name)
	var near []string
	for e := range exports {
		if e == "default" {
			continue
		}
		if le := strings.ToLower(e); le == lower || editDistance(le, lower) <= 2 {
			near = append(near, e)
		}
	}
	sort.Strings(near)
	if len(near) > 3 {
		near = near[:3]
	}
	if len(near) > 0 {
		return "did you mean " + strings.Join(near, ", ") + "? Otherwise export " + name + " from that module or fix the import path"
	}
	return "export " + name + " from that module, or fix the import path"
}
