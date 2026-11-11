package gobind

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"go/ast"
	"go/importer"
	"go/parser"
	"go/scanner"
	"go/token"
	"go/types"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// listedPkg is the part of `go list -json` output that gobind reads.
type listedPkg struct {
	ImportPath string
	Dir        string
	Name       string
	Export     string
	GoFiles    []string
	CgoFiles   []string
	ImportMap  map[string]string
	Module     *struct {
		Path string
		Dir  string
	}
	Error *struct {
		Err string
	}
}

// listSpec says how one package directory is listed: in which directory,
// with which package pattern (default "./<dir relative to the directory>") and
// environment (default the process's).
type listSpec struct {
	root    string
	pattern string
	env     []string
}

// goListIn runs goList once per listing directory and merges the results.
func goListIn(specs map[string]listSpec, dirs []string) (map[string]*listedPkg, map[string]string, string, error) {
	type group struct {
		env  []string
		pats []string
	}
	groups := map[string]*group{}
	var order []string
	for _, d := range dirs {
		sp := specs[filepath.Clean(d)]
		pat := sp.pattern
		if pat == "" {
			rel, err := filepath.Rel(sp.root, d)
			if err != nil {
				return nil, nil, "", err
			}
			pat = "./" + filepath.ToSlash(rel)
		}
		g, ok := groups[sp.root]
		if !ok {
			g = &group{env: sp.env}
			groups[sp.root] = g
			order = append(order, sp.root)
		}
		g.pats = append(g.pats, pat)
	}
	pkgs := map[string]*listedPkg{}
	byDir := map[string]string{}
	var msgs []string
	for _, r := range order {
		p, bd, msg, err := goList(r, groups[r].pats, groups[r].env)
		if err != nil {
			return nil, nil, "", err
		}
		for k, v := range p {
			pkgs[k] = v
		}
		for k, v := range bd {
			byDir[k] = v
		}
		if msg != "" {
			msgs = append(msgs, msg)
		}
	}
	return pkgs, byDir, strings.Join(msgs, "\n"), nil
}

// goList runs `go list -export -deps` in root for the package directories and
// returns every listed package by import path, plus the import path each
// directory resolved to.
func goList(root string, pats []string, env []string) (map[string]*listedPkg, map[string]string, string, error) {
	goBin, err := exec.LookPath("go")
	if err != nil {
		return nil, nil, "", errors.New("the go command was not found on PATH; Go host code needs the Go toolchain")
	}
	args := []string{"list", "-e", "-export", "-deps",
		"-json=ImportPath,Dir,Name,Export,GoFiles,CgoFiles,ImportMap,Module,Error"}
	args = append(args, pats...)
	cmd := exec.Command(goBin, args...)
	cmd.Dir = root
	if env != nil {
		cmd.Env = env
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	runErr := cmd.Run()

	pkgs := map[string]*listedPkg{}
	byDir := map[string]string{}
	dec := json.NewDecoder(&stdout)
	for {
		var p listedPkg
		if err := dec.Decode(&p); err != nil {
			if err != io.EOF && runErr == nil {
				return nil, nil, "", fmt.Errorf("reading go list output: %w", err)
			}
			break
		}
		pkgs[p.ImportPath] = &p
		byDir[filepath.Clean(p.Dir)] = p.ImportPath
	}
	msg := ""
	if runErr != nil {
		msg = strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = runErr.Error()
		}
	}
	return pkgs, byDir, msg, nil
}

// exportImporter reads compiler export data for every dependency go list
// reported.
type exportImporter struct {
	inner   types.Importer
	mapping map[string]string
	source  bool
}

func newExportImporter(fset *token.FileSet, pkgs map[string]*listedPkg) *exportImporter {
	lookup := func(path string) (io.ReadCloser, error) {
		p := pkgs[path]
		if p == nil || p.Export == "" {
			return nil, fmt.Errorf("no export data for %q", path)
		}
		return os.Open(p.Export)
	}
	return &exportImporter{inner: importer.ForCompiler(fset, "gc", lookup)}
}

// forPackage returns an importer that applies the package's vendor mapping.
func (e *exportImporter) forPackage(p *listedPkg) types.Importer {
	return mappedImporter{e: e, m: p.ImportMap, dir: p.Dir}
}

// useSource switches to type-checking dependencies from source. It is the
// fallback when the go command writes export data this binary's go/types
// cannot read (the go command is newer than the Go that built krate).
func (e *exportImporter) useSource(fset *token.FileSet) {
	e.inner = importer.ForCompiler(fset, "source", nil)
	e.source = true
}

// safeCheckPackage is checkPackage that reports an unreadable export data
// panic as ok=false instead of crashing.
func safeCheckPackage(fset *token.FileSet, imp types.Importer, p *listedPkg) (up *userPkg, errs []loadErr, ok bool) {
	defer func() {
		if r := recover(); r != nil {
			if msg := fmt.Sprint(r); strings.Contains(msg, "export data") {
				up, errs, ok = nil, nil, false
				return
			}
			panic(r)
		}
	}()
	up, errs = checkPackage(fset, imp, p)
	return up, errs, true
}

type mappedImporter struct {
	e   *exportImporter
	m   map[string]string
	dir string
}

func (mi mappedImporter) Import(path string) (*types.Package, error) {
	if r, ok := mi.m[path]; ok {
		path = r
	}
	if mi.e.source {
		if from, ok := mi.e.inner.(types.ImporterFrom); ok {
			return from.ImportFrom(path, mi.dir, 0)
		}
	}
	return mi.e.inner.Import(path)
}

// userPkg is a Go package type-checked from source.
type userPkg struct {
	dir        string
	importPath string
	name       string
	modPath    string
	modDir     string
	files      []string // absolute paths of the non-test Go files
	pkg        *types.Package
	asts       []*ast.File
}

// loadErr is a positioned parse or type error.
type loadErr struct {
	pos token.Position
	msg string
}

// checkPackage parses and type-checks p from source.
func checkPackage(fset *token.FileSet, imp types.Importer, p *listedPkg) (*userPkg, []loadErr) {
	up := &userPkg{dir: p.Dir, importPath: p.ImportPath, name: p.Name}
	if p.Module != nil {
		up.modPath, up.modDir = p.Module.Path, p.Module.Dir
	}
	var errs []loadErr
	for _, f := range p.GoFiles {
		path := filepath.Join(p.Dir, f)
		up.files = append(up.files, path)
		af, err := parser.ParseFile(fset, path, nil, parser.ParseComments|parser.SkipObjectResolution)
		if list, ok := err.(scanner.ErrorList); ok {
			for _, e := range list {
				errs = append(errs, loadErr{pos: e.Pos, msg: e.Msg})
			}
			continue
		} else if err != nil {
			errs = append(errs, loadErr{pos: token.Position{Filename: path}, msg: err.Error()})
			continue
		}
		up.asts = append(up.asts, af)
	}
	if len(errs) > 0 {
		return up, errs
	}
	conf := types.Config{
		Importer: imp,
		Error: func(err error) {
			if te, ok := err.(types.Error); ok && !te.Soft {
				errs = append(errs, loadErr{pos: te.Fset.Position(te.Pos), msg: te.Msg})
			}
		},
	}
	pkg, _ := conf.Check(p.ImportPath, fset, up.asts, nil)
	up.pkg = pkg
	return up, errs
}
