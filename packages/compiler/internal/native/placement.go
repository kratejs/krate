// Package native decides which code is host code in a Krate Native build and
// checks a project against the restrictions of the native target.
//
// Host code runs in the native process; UI code runs in the webview and calls
// host code as async functions. Six sources mark host code, in precedence order:
//
//  1. a function-level marker: the `@host` decorator on a function, or the
//     string "@host" as the first statement of its body (`@build` and "@build"
//     also make a function host code, and additionally evaluate it during the
//     native build)
//  2. the `// @host` comment on the first line of a file
//  3. the `.host.ts` / `.host.tsx` file name
//  4. the hostFunctions config list (function names or file paths)
//  5. the hostDirs config list
//
// plus `.go` files, which are always host code. A function-level marker
// overrides the file for that function only.
package native

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/kratejs/krate/packages/compiler/ast"
	"github.com/kratejs/krate/packages/compiler/internal/bundler"
	"github.com/kratejs/krate/packages/compiler/internal/config"
	"github.com/kratejs/krate/packages/compiler/internal/diag"
	"github.com/kratejs/krate/packages/compiler/internal/tsstrip"
)

// Source names why code is host code.
type Source string

// The sources of host code, highest precedence first.
const (
	SourceDecorator   Source = "decorator"
	SourceBodyString  Source = "body string"
	SourceDirective   Source = "directive"
	SourceFileName    Source = "file name"
	SourceConfigFunc  Source = "hostFunctions"
	SourceConfigDir   Source = "hostDirs"
	SourceGo          Source = "go file"
	sourceNone        Source = ""
	hostDirectiveName        = "@host"
)

// Func is one host function.
type Func struct {
	Name     string
	Source   Source
	Exported bool
	// Inline is true when the function sits in a module that is not itself
	// host code, so a later step must split it out.
	Inline bool
	// Start and End are byte offsets of the function in the module source,
	// including a `@host` decorator and the closing brace. Zero for Go files.
	Start, End int
	// Line is the 1-based line of the declaration.
	Line int
	// Build is true for a function marked `@build`: it also runs during the
	// native build and its result is baked into the UI (see hostts).
	Build bool
}

// Module is a module that contains host code.
type Module struct {
	// Path is absolute; Rel is slash-separated and relative to the project root.
	Path string
	Rel  string
	// Whole is true when the entire module is host code.
	Whole bool
	// Source is why the module is host code when Whole is true.
	Source Source
	// Funcs lists the host functions. For a whole module it lists the
	// top-level functions that are not overridden by a function-level marker.
	Funcs []Func
}

// Placement is the result of analyzing a project.
type Placement struct {
	Root    string
	Modules []Module
	// Diagnostics holds placement errors (conflicts, components, captures).
	Diagnostics []diag.Diagnostic
	// Warnings holds non-fatal notes, such as a hostFunctions name that matches
	// no function.
	Warnings []string
}

// Module returns the host module at path (absolute or relative to the root).
func (p *Placement) Module(path string) *Module {
	if !filepath.IsAbs(path) {
		path = filepath.Join(p.Root, path)
	}
	for i := range p.Modules {
		if p.Modules[i].Path == path {
			return &p.Modules[i]
		}
	}
	return nil
}

// IsHostModule reports whether the whole module at path is host code.
func (p *Placement) IsHostModule(path string) bool {
	m := p.Module(path)
	return m != nil && m.Whole
}

var scriptExts = map[string]bool{".ts": true, ".tsx": true, ".js": true, ".jsx": true, ".mjs": true, ".mts": true}

func isScript(path string) bool {
	return scriptExts[filepath.Ext(path)] && !strings.HasSuffix(path, ".d.ts")
}

// IsHostFileName reports whether a file name has the `.host.ts` convention.
func IsHostFileName(path string) bool {
	base := filepath.Base(path)
	for ext := range scriptExts {
		if strings.HasSuffix(base, ".host"+ext) {
			return true
		}
	}
	return false
}

// Analyze scans the project source and decides what is host code.
func Analyze(root string, cfg *config.Config) (*Placement, error) {
	files, err := projectFiles(root, cfg)
	if err != nil {
		return nil, err
	}
	p := &Placement{Root: root}
	matched := map[string]bool{}
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		res := analyzeFile(root, f, string(data), cfg)
		for n := range res.matchedNames {
			matched[n] = true
		}
		if res.module != nil {
			p.Modules = append(p.Modules, *res.module)
		}
		p.Diagnostics = append(p.Diagnostics, res.diags...)
	}
	for _, e := range cfg.HostFunctions {
		if !isPathEntry(e) && !matched[e] {
			p.Warnings = append(p.Warnings, fmt.Sprintf("hostFunctions entry %q matches no function", e))
		}
	}
	sort.Slice(p.Modules, func(i, j int) bool { return p.Modules[i].Rel < p.Modules[j].Rel })
	return p, nil
}

// AnalyzeSource analyzes one file. It returns nil when the file holds no host
// code. Use Analyze for a whole project.
func AnalyzeSource(root, path, src string, cfg *config.Config) (*Module, []diag.Diagnostic) {
	res := analyzeFile(root, path, src, cfg)
	return res.module, res.diags
}

type fileResult struct {
	module       *Module
	diags        []diag.Diagnostic
	matchedNames map[string]bool
}

// hostSrc is one file-wide source of host code.
type hostSrc struct {
	src  Source
	what string
	line int
}

func relPath(root, path string) string {
	if r, err := filepath.Rel(root, path); err == nil && !strings.HasPrefix(r, "..") {
		return filepath.ToSlash(r)
	}
	return filepath.ToSlash(path)
}

func isPathEntry(e string) bool {
	return strings.ContainsAny(e, `/\`) || scriptExts[filepath.Ext(e)] || filepath.Ext(e) == ".go"
}

func entryMatchesPath(root, entry, path string) bool {
	p := entry
	if !filepath.IsAbs(p) {
		p = filepath.Join(root, p)
	}
	p = filepath.Clean(p)
	return p == path || p == strings.TrimSuffix(path, filepath.Ext(path))
}

// fileHostSources lists the file-wide sources that make a module host code,
// highest precedence first.
func fileHostSources(root, path, src string, cfg *config.Config) []hostSrc {
	var out []hostSrc
	if filepath.Ext(path) == ".go" {
		out = append(out, hostSrc{src: SourceGo, what: "a `.go` file (Go files are always host code)"})
	}
	if bundler.HasDirective(src, hostDirectiveName) {
		out = append(out, hostSrc{src: SourceDirective, what: "the `// @host` comment", line: directiveLine(src)})
	}
	if IsHostFileName(path) {
		out = append(out, hostSrc{src: SourceFileName, what: "the `.host` file name"})
	}
	for _, e := range cfg.HostFunctions {
		if isPathEntry(e) && entryMatchesPath(root, e, path) {
			out = append(out, hostSrc{src: SourceConfigFunc, what: "`hostFunctions` in the config"})
			break
		}
	}
	if bundler.PathInDirs(path, cfg.HostDirs) {
		out = append(out, hostSrc{src: SourceConfigDir, what: "`hostDirs` in the config"})
	}
	return out
}

func directiveLine(src string) int {
	for i, line := range strings.SplitN(src, "\n", 10) {
		if strings.Contains(strings.ToLower(line), hostDirectiveName) && (strings.Contains(line, "//") || strings.Contains(line, "/*")) {
			return i + 1
		}
	}
	return 0
}

func lineAt(src string, line int) string {
	lines := strings.Split(src, "\n")
	if line >= 1 && line <= len(lines) {
		return strings.TrimRight(lines[line-1], "\r")
	}
	return ""
}

func mkDiag(rel, src string, line, col int, code diag.Code, msg, hint string) diag.Diagnostic {
	if line < 1 {
		line = 1
	}
	if col < 1 {
		col = 1
	}
	return diag.New(rel, line, col, msg, hint, lineAt(src, line)).WithCode(code)
}

var goFuncRe = regexp.MustCompile(`(?m)^func\s+([A-Z][A-Za-z0-9_]*)\s*[\[(]`)

func tierClassName(c bundler.ComponentClass) string {
	switch c {
	case bundler.ComponentClassServer:
		return "a server"
	case bundler.ComponentClassRuntime:
		return "a runtime"
	case bundler.ComponentClassStatic:
		return "a static"
	}
	return "a client"
}

func analyzeFile(root, path, src string, cfg *config.Config) fileResult {
	res := fileResult{matchedNames: map[string]bool{}}
	rel := relPath(root, path)
	isGo := filepath.Ext(path) == ".go"
	if !isGo && !isScript(path) {
		return res
	}
	if isGo && strings.HasSuffix(path, "_test.go") {
		return res
	}
	fileSrcs := fileHostSources(root, path, src, cfg)
	var names []string
	for _, e := range cfg.HostFunctions {
		if !isPathEntry(e) && strings.Contains(src, e) {
			names = append(names, e)
		}
	}
	if len(fileSrcs) == 0 && len(names) == 0 && !strings.Contains(src, "@host") && !strings.Contains(src, "@build") {
		return res
	}

	whole := len(fileSrcs) > 0
	var wholeSrc Source
	var wholeWhat string
	if whole {
		wholeSrc, wholeWhat = fileSrcs[0].src, fileSrcs[0].what
	}

	tierSrcs := bundler.FileTierSources(src, path, cfg.ServerComponents, cfg.RuntimeComponents, cfg.ServerDirs, cfg.RuntimeDirs)

	if isGo {
		if len(tierSrcs) > 0 {
			t := tierSrcs[0]
			res.diags = append(res.diags, mkDiag(rel, src, t.Line, 1, diag.CodeHostConflict,
				"conflicting placement: "+wholeWhat+" makes this file host code, but "+t.What+" makes it "+tierClassName(t.Class)+" component",
				conflictHint))
		}
		m := Module{Path: path, Rel: rel, Whole: true, Source: SourceGo}
		for _, mm := range goFuncRe.FindAllStringSubmatchIndex(src, -1) {
			m.Funcs = append(m.Funcs, Func{Name: src[mm[2]:mm[3]], Source: SourceGo, Exported: true, Line: 1 + strings.Count(src[:mm[0]], "\n")})
		}
		res.module = &m
		return res
	}

	prog, errs := tsstrip.Parse(path, src, tsstrip.Options{})
	if prog == nil || len(errs) > 0 {
		// A syntax error is reported by the build proper; only a file that
		// asked to be host code has to say so here.
		if whole || strings.Contains(src, "@host") || strings.Contains(src, "@build") {
			for _, e := range errs {
				if d, ok := e.(diag.Diagnostic); ok {
					res.diags = append(res.diags, d)
				} else {
					res.diags = append(res.diags, diag.Diagnostic{File: rel, Line: 1, Col: 1, Message: e.Error()})
				}
			}
		}
		return res
	}

	if whole {
		all := tierSrcs
		if prog.Tier != "" {
			if cls, ok := classOf(prog.Tier); ok {
				all = append(all, bundler.FileTier{What: "the @" + prog.Tier + " decorator on the default export", Class: cls})
			}
		}
		if len(all) > 0 {
			t := all[0]
			res.diags = append(res.diags, mkDiag(rel, src, t.Line, 1, diag.CodeHostConflict,
				"conflicting placement: "+wholeWhat+" makes this module host code, but "+t.What+" makes it "+tierClassName(t.Class)+" component",
				conflictHint))
		}
	}

	sites := collectSites(prog)
	mod := Module{Path: path, Rel: rel, Whole: whole, Source: wholeSrc}
	for _, s := range sites {
		marker, markSrc := "", sourceNone
		deco, body := s.decorator, bodyMarker(s.body)
		if deco != "" && body != "" && deco != body {
			res.diags = append(res.diags, mkDiag(rel, src, s.pos.Line, s.pos.Col, diag.CodeHostConflict,
				"conflicting placement on "+s.name+": the @"+deco+" decorator and the \"@"+body+"\" string in its body disagree", conflictHint))
		}
		switch {
		case deco != "":
			marker, markSrc = deco, SourceDecorator
		case body != "":
			marker, markSrc = body, SourceBodyString
		}
		if s.top {
			for _, n := range names {
				if n != s.name {
					continue
				}
				res.matchedNames[n] = true
				switch marker {
				case "":
					marker, markSrc = "host", SourceConfigFunc
				case "host", "build":
				default:
					res.diags = append(res.diags, mkDiag(rel, src, s.pos.Line, s.pos.Col, diag.CodeHostConflict,
						"conflicting placement on "+s.name+": `hostFunctions` in the config makes it host code, but its @"+marker+" marker makes it a "+marker+" function",
						conflictHint))
				}
			}
		}

		isHost := marker == "host" || marker == "build" || (marker == "" && whole && s.top)
		if !isHost || (whole && !s.top) {
			continue
		}
		fsrc := markSrc
		if fsrc == sourceNone {
			fsrc = wholeSrc
		}
		if isComponent(s) {
			res.diags = append(res.diags, mkDiag(rel, src, s.pos.Line, s.pos.Col, diag.CodeHostComponent,
				s.name+" is a component and cannot be host code (host code: "+string(fsrc)+")",
				"host code runs in the native process and cannot render UI: use @server to render at build time, or keep "+s.name+" a client component and call a host function from it"))
			continue
		}
		if len(s.chain) > 0 {
			res.diags = append(res.diags, checkCapture(rel, src, s, sites)...)
		}
		start, end := siteRange(src, s)
		mod.Funcs = append(mod.Funcs, Func{
			Name: s.name, Source: fsrc, Exported: s.exported,
			Inline: !whole || !s.top, Start: start, End: end, Line: s.pos.Line,
			Build: marker == "build",
		})
	}
	if whole || len(mod.Funcs) > 0 {
		res.module = &mod
	}
	return res
}

// isHostMark reports whether a function-level marker makes host code.
func isHostMark(m string) bool { return m == "host" || m == "build" }

const conflictHint = "a module or function is host code from one source: remove the marker that disagrees. A function-level marker (`@host`, `@client`, ...) overrides the file for that function only"

func classOf(tier string) (bundler.ComponentClass, bool) {
	switch tier {
	case "static":
		return bundler.ComponentClassStatic, true
	case "client":
		return bundler.ComponentClassClient, true
	case "server":
		return bundler.ComponentClassServer, true
	case "runtime":
		return bundler.ComponentClassRuntime, true
	}
	return bundler.ComponentClassClient, false
}

// checkCapture rejects an inline host function that reads a name declared in
// an enclosing function. Module-level declarations that are not host code are
// not checked.
func checkCapture(rel, src string, s *site, all []*site) []diag.Diagnostic {
	outer := map[string]bool{}
	for _, f := range s.chain {
		for n := range f.names {
			outer[n] = true
		}
	}
	// Other host functions declared in the same scopes move to the host with
	// this one, so calling them is fine.
	for _, o := range all {
		if o != s && o.name != "" && (isHostMark(bodyMarker(o.body)) || isHostMark(o.decorator)) {
			delete(outer, o.name)
		}
	}
	locals := declaredNames(s.params, s.body, true)
	var out []diag.Diagnostic
	reported := map[string]bool{}
	bundler.WalkStmts(s.body, func(n any, _ bool) bool {
		id, ok := n.(*ast.Identifier)
		if !ok || locals[id.Name] || !outer[id.Name] || reported[id.Name] {
			return true
		}
		reported[id.Name] = true
		out = append(out, mkDiag(rel, src, id.Position.Line, id.Position.Col, diag.CodeHostCapture,
			"host function "+s.name+" uses "+id.Name+", which is declared in the enclosing component",
			"host code runs in the native process and cannot see the component's variables or signals: pass the value as an argument, e.g. "+s.name+"(..., "+id.Name+")"))
		return true
	})
	return out
}

// projectFiles lists the source files placement and the target checks read:
// everything under src, the pages dir and the hostDirs.
func projectFiles(root string, cfg *config.Config) ([]string, error) {
	roots := []string{filepath.Join(root, "src")}
	if cfg.PagesDir != "" {
		pd := cfg.PagesDir
		if !filepath.IsAbs(pd) {
			pd = filepath.Join(root, pd)
		}
		roots = append(roots, pd)
	}
	for _, d := range cfg.HostDirs {
		if !filepath.IsAbs(d) {
			d = filepath.Join(root, d)
		}
		roots = append(roots, d)
	}
	seen := map[string]bool{}
	var files []string
	for _, r := range roots {
		info, err := os.Stat(r)
		if err != nil || !info.IsDir() {
			continue
		}
		err = filepath.WalkDir(r, func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if d.IsDir() {
				switch d.Name() {
				case "node_modules", ".krate", "dist", ".git":
					return filepath.SkipDir
				}
				return nil
			}
			if strings.HasPrefix(d.Name(), ".") {
				return nil
			}
			if (isScript(p) || filepath.Ext(p) == ".go") && !seen[p] {
				seen[p] = true
				files = append(files, p)
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	sort.Strings(files)
	return files, nil
}
