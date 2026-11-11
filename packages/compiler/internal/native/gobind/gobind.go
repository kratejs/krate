package gobind

import (
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/kratejs/krate/packages/compiler/internal/diag"
	"github.com/kratejs/krate/packages/compiler/internal/gotsgen"
)

// Import is one .go file or package directory the UI imports from.
type Import struct {
	// File is the .go file, or a package directory, absolute or relative to
	// the project root.
	File string
	// Names are the imported names.
	Names []string
	// From is the UI source file that has the import, with the 1-based
	// position of the import, for diagnostics. Optional.
	From      string
	Line, Col int
	// ID, when set, is the module id of the import instead of the path
	// relative to the project root; the import may then be outside the root
	// (a plugin). File must be a package directory.
	ID string
	// ModuleDir is the directory `go list` runs in for this import: the root
	// of the Go module that holds File. Default: the project root.
	ModuleDir string
	// Rename maps Go names to the names the UI and the registry ids use
	// (plugin ops are lowerCamel: "CPULoad" is "cpuLoad").
	Rename map[string]string
	// Pattern and Env, when set, replace the package pattern go list is given
	// (default "./<directory>" relative to ModuleDir) and its environment.
	Pattern string
	Env     []string
}

// PkgInfo describes a user package for Options.GlueDir.
type PkgInfo struct {
	// Dir and ImportPath are the package's; ModulePath is its Go module.
	Dir, ImportPath, ModulePath string
	// ModuleDir is the directory of the module.
	ModuleDir string
	// ID is the module id of the import the package serves.
	ID string
}

// Options tune GenerateWith.
type Options struct {
	// GlueDir, when set, decides where the glue package of a user package
	// goes: its directory and the import path of that directory. Default:
	// outDir/glue/<package dir relative to the project root>, which must be
	// inside the package's module.
	GlueDir func(p PkgInfo) (dir, importPath string, err error)
}

// Glue is one generated Go package.
type Glue struct {
	// ImportPath is how Go code imports the package; its Register function
	// takes a *scheme.Registry.
	ImportPath string
	Dir        string
	File       string
}

// Module is one imported .go file or directory.
type Module struct {
	ID     string
	Source string
	// Stub and DTS are the generated files; empty when the module imports
	// only components.
	Stub string
	DTS  string
	// Host lists the imported host functions, streams and signals.
	Host []string
	// Funcs, Streams and Signals are the registry ids ("<module id>.<name>")
	// of the imported host items by kind, sorted.
	Funcs, Streams, Signals []string
	// Components lists the imported names that are Go components.
	Components []string
	// Build lists the imported functions marked //krate:build, sorted. They
	// are in Funcs too, but the UI reads their baked values instead.
	Build []string
}

// Result lists what Generate wrote.
type Result struct {
	Glue    []Glue
	Modules []Module
}

// GoPackages returns the import paths of the generated glue packages.
func (r *Result) GoPackages() []string {
	var out []string
	for _, g := range r.Glue {
		out = append(out, g.ImportPath)
	}
	return out
}

var invalidPathChars = regexp.MustCompile(`[^A-Za-z0-9._~-]`)

func glueDirName(rel string) string {
	if rel == "." {
		return "root"
	}
	parts := strings.Split(filepath.ToSlash(rel), "/")
	for i, p := range parts {
		p = invalidPathChars.ReplaceAllString(p, "_")
		if strings.HasPrefix(p, ".") {
			p = "_" + p[1:]
		}
		parts[i] = p
	}
	return strings.Join(parts, "/")
}

// Generate type-checks the Go code behind the imports and writes glue, stubs
// and declarations into outDir (see the package comment).
func Generate(root string, imports []Import, outDir string) (*Result, []diag.Diagnostic, error) {
	return GenerateWith(root, imports, outDir, Options{})
}

// GenerateWith is Generate with options.
func GenerateWith(root string, imports []Import, outDir string, opts Options) (*Result, []diag.Diagnostic, error) {
	root, err := filepath.Abs(root)
	if err != nil {
		return nil, nil, err
	}
	if !filepath.IsAbs(outDir) {
		outDir = filepath.Join(root, outDir)
	}
	outDir = filepath.Clean(outDir)
	fset := token.NewFileSet()
	a := newAnalyzer(root, fset)

	mods := a.modules(imports)
	if len(a.diags) > 0 {
		return nil, a.diags, nil
	}
	if len(mods) == 0 {
		return &Result{}, nil, nil
	}

	// One package per directory.
	dirSet := map[string]bool{}
	for _, m := range mods {
		dirSet[a.pkgDir(m)] = true
	}
	var dirs []string
	for d := range dirSet {
		dirs = append(dirs, d)
	}
	sort.Strings(dirs)

	// Directories are listed in the module they belong to: the project root,
	// or the ModuleDir of the import that names them.
	listRoot := map[string]listSpec{}
	for _, m := range mods {
		listRoot[filepath.Clean(a.pkgDir(m))] = listSpec{root: m.listRoot(root), pattern: m.imp.Pattern, env: m.imp.Env}
	}
	listed, byDir, goErr, lerr := goListIn(listRoot, dirs)
	if lerr != nil {
		a.add(diag.New("", 0, 0, lerr.Error(), "install Go and make sure the go command is on PATH", "").WithCode(diag.CodeGoBindLoad))
		return nil, a.diags, nil
	}
	if goErr != "" {
		a.add(diag.New("", 0, 0, "go list failed: "+trimLines(goErr, 12),
			"run go build ./... in the project to see the full output; the packages the UI imports must compile", "").WithCode(diag.CodeGoBindLoad))
		return nil, a.diags, nil
	}
	exp := newExportImporter(fset, listed)
	pkgs := map[string]*userPkg{}
	for _, d := range dirs {
		lp := listed[byDir[filepath.Clean(d)]]
		if lp == nil {
			a.add(diag.New(a.display(d), 0, 0, "go list did not report a package in this directory"+listedRoots(listed),
				"the directory needs .go files that build for this platform", "").WithCode(diag.CodeGoBindLoad))
			continue
		}
		if msg := loadProblem(lp); msg != "" {
			a.add(diag.New(a.display(d), 0, 0, msg, "host code must be an importable package inside the project's Go module", "").WithCode(diag.CodeGoBindLoad))
			continue
		}
		up, errs, ok := safeCheckPackage(fset, exp.forPackage(lp), lp)
		if !ok && !exp.source {
			exp.useSource(fset)
			up, errs, ok = safeCheckPackage(fset, exp.forPackage(lp), lp)
		}
		if !ok {
			a.add(diag.New(a.display(d), 0, 0, "cannot read the compiler export data of the Go dependencies",
				"use the Go toolchain that krate was built with (GOTOOLCHAIN) or a newer krate", "").WithCode(diag.CodeGoBindLoad))
			continue
		}
		for _, e := range errs {
			a.add(a.at(e.pos, diag.CodeGoBindLoad, e.msg, "fix the Go error; the package must compile before bindings can be generated"))
		}
		if len(errs) == 0 {
			pkgs[filepath.Clean(d)] = up
		}
	}
	if len(a.diags) > 0 {
		return nil, a.diags, nil
	}

	mp := newMapper(a)
	for _, m := range mods {
		m.pkg = pkgs[filepath.Clean(a.pkgDir(m))]
		if m.pkg.modPath == "" {
			a.add(a.at(token.Position{Filename: m.src}, diag.CodeGoBindModule, "the package is not inside a Go module",
				"add a go.mod at the project root that requires github.com/kratejs/krate/packages/native"))
			continue
		}
		if !m.isDir && !contains(m.pkg.files, m.src) {
			a.add(a.at(a.importPos(m.imp, m.src), diag.CodeGoBindModule, a.display(m.src)+" is not part of the package build",
				"the file is excluded by a build constraint or its name; remove the constraint or import another file"))
			continue
		}
		comps := componentNames(m)
		m.build = buildNames(m)
		seen := map[string]bool{}
		for _, n := range m.names {
			if seen[n] {
				continue
			}
			seen[n] = true
			imp := m.imp
			a.classify(m, mp, n, comps, func() token.Position { return a.importPos(imp, "") })
		}
	}
	if len(a.diags) > 0 {
		return nil, a.diags, nil
	}

	res := &Result{}
	byPkg := map[string][]*hostItem{}
	pkgOrder := []string{}
	glueDirs := map[string]string{}
	for _, m := range mods {
		key := filepath.Clean(m.pkg.dir)
		if _, ok := byPkg[key]; !ok {
			pkgOrder = append(pkgOrder, key)
			byPkg[key] = nil
		}
		byPkg[key] = append(byPkg[key], m.items...)
	}
	sort.Strings(pkgOrder)
	for _, key := range pkgOrder {
		items := byPkg[key]
		if len(items) == 0 {
			continue
		}
		var up *userPkg
		for _, m := range mods {
			if filepath.Clean(m.pkg.dir) == key {
				up = m.pkg
				break
			}
		}
		rel, _ := filepath.Rel(root, up.dir)
		gdir := filepath.Join(outDir, "glue", filepath.FromSlash(glueDirName(rel)))
		customImport := ""
		if opts.GlueDir != nil {
			id := ""
			for _, m := range mods {
				if filepath.Clean(m.pkg.dir) == key {
					id = m.id
				}
			}
			d, ip, err := opts.GlueDir(PkgInfo{Dir: up.dir, ImportPath: up.importPath, ModulePath: up.modPath, ModuleDir: up.modDir, ID: id})
			if err != nil {
				return nil, nil, err
			}
			gdir, customImport = d, ip
		}
		if prev, dup := glueDirs[gdir]; dup {
			a.add(diag.New(a.display(up.dir), 0, 0, "the glue directory for this package collides with the one for "+a.display(prev),
				"rename one of the directories", "").WithCode(diag.CodeGoBindModule))
			continue
		}
		glueDirs[gdir] = up.dir
		grel, err := filepath.Rel(up.modDir, gdir)
		if customImport == "" && (err != nil || strings.HasPrefix(grel, "..")) {
			a.add(diag.New(a.display(outDir), 0, 0, "the output directory is outside the Go module at "+up.modDir,
				"generate into a directory inside the module, such as .krate/native", "").WithCode(diag.CodeGoBindModule))
			continue
		}
		src, err := glueSource(up, items)
		if err != nil {
			return nil, nil, err
		}
		file := filepath.Join(gdir, "glue.go")
		if err := writeIfChanged(file, src); err != nil {
			return nil, nil, err
		}
		importPath := up.modPath + "/" + filepath.ToSlash(grel)
		if customImport != "" {
			importPath = customImport
		}
		res.Glue = append(res.Glue, Glue{ImportPath: importPath, Dir: gdir, File: file})
	}
	if len(a.diags) > 0 {
		return nil, a.diags, nil
	}

	for _, m := range mods {
		out := Module{ID: m.id, Source: m.src, Components: sortedUnique(m.components)}
		for _, it := range m.items {
			out.Host = append(out.Host, it.name)
			if it.build {
				out.Build = append(out.Build, it.name)
			}
			switch it.kind {
			case itemStream:
				out.Streams = append(out.Streams, it.id)
			case itemSignal:
				out.Signals = append(out.Signals, it.id)
			default:
				out.Funcs = append(out.Funcs, it.id)
			}
		}
		sort.Strings(out.Host)
		sort.Strings(out.Build)
		sort.Strings(out.Funcs)
		sort.Strings(out.Streams)
		sort.Strings(out.Signals)
		if len(m.items) > 0 {
			out.Stub = filepath.Join(outDir, "gostubs", filepath.FromSlash(m.id)+".go.js")
			out.DTS = filepath.Join(outDir, "types", filepath.FromSlash(m.id)+".go.d.ts")
			if err := writeIfChanged(out.Stub, []byte(stub(m.items))); err != nil {
				return nil, nil, err
			}
			if err := writeIfChanged(out.DTS, []byte(dts(m.items))); err != nil {
				return nil, nil, err
			}
		}
		res.Modules = append(res.Modules, out)
	}
	return res, nil, nil
}

func loadProblem(lp *listedPkg) string {
	switch {
	case lp.Error != nil:
		return lp.Error.Err
	case lp.Module == nil:
		return "the package is not inside a Go module"
	case lp.Name == "main":
		return "the package is package main, which cannot be imported by the generated glue; move the host code into a library package"
	case len(lp.CgoFiles) > 0:
		return "packages that use cgo are not supported as host code"
	case len(lp.GoFiles) == 0:
		return "the package has no Go files for this platform"
	}
	return ""
}

func trimLines(s string, n int) string {
	lines := strings.Split(s, "\n")
	if len(lines) > n {
		lines = append(lines[:n], "...")
	}
	return strings.Join(lines, "\n")
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if filepath.Clean(x) == filepath.Clean(s) {
			return true
		}
	}
	return false
}

func sortedUnique(in []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range in {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	sort.Strings(out)
	return out
}

func writeIfChanged(path string, data []byte) error {
	if old, err := os.ReadFile(path); err == nil && string(old) == string(data) {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

// importPos is the position to blame for an import: the UI import if known,
// else fallback.
func (a *analyzer) importPos(imp Import, fallback string) token.Position {
	if imp.From != "" {
		from := imp.From
		if !filepath.IsAbs(from) {
			from = filepath.Join(a.root, from)
		}
		return token.Position{Filename: from, Line: imp.Line, Column: imp.Col}
	}
	if fallback != "" {
		return token.Position{Filename: fallback}
	}
	return token.Position{}
}

// listRoot is the directory go list runs in for m.
func (m *module) listRoot(root string) string {
	if m.imp.ModuleDir != "" {
		return m.imp.ModuleDir
	}
	return root
}

func (a *analyzer) pkgDir(m *module) string {
	if m.isDir {
		return m.src
	}
	return filepath.Dir(m.src)
}

// modules validates the imports, assigns module ids and merges repeats.
func (a *analyzer) modules(imports []Import) []*module {
	byPath := map[string]*module{}
	byID := map[string]*module{}
	var out []*module
	bad := func(imp Import, path, msg, hint string) {
		a.add(a.at(a.importPos(imp, path), diag.CodeGoBindModule, msg, hint))
	}
	for _, imp := range imports {
		p := imp.File
		if !filepath.IsAbs(p) {
			p = filepath.Join(a.root, p)
		}
		p = filepath.Clean(p)
		st, err := os.Stat(p)
		if err != nil {
			bad(imp, "", imp.File+" does not exist", "check the path of the .go import")
			continue
		}
		if imp.ID == "" && !a.inRoot(p) {
			bad(imp, "", a.display(p)+" is outside the project root", "host code must live inside the project")
			continue
		}
		isDir := st.IsDir()
		if !isDir && (filepath.Ext(p) != ".go" || strings.HasSuffix(p, "_test.go")) {
			bad(imp, "", a.display(p)+" is not a non-test .go file", "import a .go file or a package directory")
			continue
		}
		rel, _ := filepath.Rel(a.root, p)
		id := filepath.ToSlash(rel)
		if !isDir {
			id = strings.TrimSuffix(id, ".go")
		}
		if imp.ID != "" {
			if !isDir {
				bad(imp, "", a.display(p)+" is not a package directory", "a plugin is a Go package directory")
				continue
			}
			id = imp.ID
		}
		if id == "." {
			bad(imp, "", "the project root cannot be imported as a package", "move the host code into a subdirectory")
			continue
		}
		if m, ok := byPath[p]; ok {
			m.names = append(m.names, imp.Names...)
			continue
		}
		if other, ok := byID[id]; ok {
			bad(imp, "", a.display(p)+" and "+a.display(other.src)+" both have the module id "+id,
				"rename the directory or the file so the two ids differ")
			continue
		}
		m := &module{id: id, src: p, isDir: isDir, names: append([]string(nil), imp.Names...), imp: imp, rename: imp.Rename}
		byPath[p], byID[id] = m, m
		out = append(out, m)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].id < out[j].id })
	return out
}

var buildMarkRe = regexp.MustCompile(`^//\s*krate:build\b`)
var funcDeclRe = regexp.MustCompile(`^func\s+([A-Za-z_][A-Za-z0-9_]*)\s*[\[(]`)

// buildNames returns the functions of the module marked //krate:build: the
// directive sits in the doc comment of the function.
func buildNames(m *module) map[string]bool {
	out := map[string]bool{}
	files := []string{m.src}
	if m.isDir {
		files = m.pkg.files
	}
	for _, f := range files {
		src, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		marked := false
		for _, line := range strings.Split(string(src), "\n") {
			line = strings.TrimRight(line, "\r")
			switch {
			case buildMarkRe.MatchString(line):
				marked = true
			case strings.HasPrefix(line, "//"):
			default:
				if mm := funcDeclRe.FindStringSubmatch(line); marked && mm != nil {
					out[mm[1]] = true
				}
				marked = false
			}
		}
	}
	return out
}

// componentNames returns the Go components declared in the module's source.
func componentNames(m *module) map[string]bool {
	out := map[string]bool{}
	files := []string{m.src}
	if m.isDir {
		files = m.pkg.files
	}
	for _, f := range files {
		src, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		comps, err := gotsgen.ParseFile(f, src)
		if err != nil {
			continue
		}
		for _, c := range comps {
			out[c.Name] = true
		}
	}
	return out
}

// listedRoots describes the packages go list matched by pattern, for a
// diagnostic that cannot find the package of a directory.
func listedRoots(listed map[string]*listedPkg) string {
	var out []string
	for _, p := range listed {
		if len(p.Match) > 0 {
			out = append(out, p.ImportPath+" in "+p.Dir)
		}
	}
	if len(out) == 0 {
		return " (go list matched no package)"
	}
	sort.Strings(out)
	return " (go list matched " + strings.Join(out, ", ") + ")"
}
