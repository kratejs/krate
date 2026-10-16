package gohost

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/kratejs/krate/packages/compiler/internal/gotsgen"
)

var (
	goNameSanitizeRe   = regexp.MustCompile(`[^A-Za-z0-9_]+`)
	goPackageRe        = regexp.MustCompile(`(?m)^\s*package\s+([A-Za-z_][A-Za-z0-9_]*)`)
	goPackageRewriteRe = regexp.MustCompile(`(?m)^\s*package\s+[A-Za-z_][A-Za-z0-9_]*`)
)

// apiMethods are the HTTP method handler names a route file may define, in
// registration order. A `Handler` function instead handles all methods.
var apiMethods = []string{"GET", "POST", "PUT", "DELETE", "PATCH", "OPTIONS", "HEAD"}

// ScanRouteFiles returns every non-underscore `.go` file under apiSrcDir.
func ScanRouteFiles(apiSrcDir string) ([]string, error) {
	var files []string
	err := filepath.Walk(apiSrcDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		if strings.HasSuffix(path, ".go") && !strings.HasPrefix(filepath.Base(path), "_") {
			files = append(files, path)
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("scanning Go API routes: %w", err)
	}
	sort.Strings(files)
	return files, nil
}

// GoFilePackage extracts the package clause name from Go source.
func GoFilePackage(src []byte) string {
	if m := goPackageRe.FindSubmatch(src); len(m) == 2 {
		return string(m[1])
	}
	return ""
}

// RouteMethods returns the handler function names defined in a route file:
// ["Handler"] if a `Handler` exists, else the defined HTTP-method funcs.
func RouteMethods(src []byte) []string {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "", src, 0)
	if err != nil {
		return nil
	}
	defined := map[string]bool{}
	for _, d := range f.Decls {
		if fn, ok := d.(*ast.FuncDecl); ok && fn.Recv == nil {
			defined[fn.Name.Name] = true
		}
	}
	if defined["Handler"] {
		return []string{"Handler"}
	}
	var out []string
	for _, m := range apiMethods {
		if defined[m] {
			out = append(out, m)
		}
	}
	return out
}

// ParseRoute computes a route's URL path and handler set. hasRoute is false for
// helper files that define no handler.
func ParseRoute(apiSrcDir, file string, src []byte) (Route, bool, error) {
	rel, err := filepath.Rel(apiSrcDir, file)
	if err != nil {
		return Route{}, false, err
	}
	r := Route{Source: file}
	methods := RouteMethods(src)
	if len(methods) == 0 {
		return Route{}, false, nil // helper file
	}
	r.Methods = methods

	trimmed := strings.TrimSuffix(filepath.ToSlash(rel), filepath.Ext(rel))
	segments := strings.Split(trimmed, "/")
	if len(segments) > 0 && segments[len(segments)-1] == "index" {
		segments = segments[:len(segments)-1]
	}
	var sb strings.Builder
	sb.WriteString("/api")
	for _, seg := range segments {
		if seg == "" {
			continue
		}
		sb.WriteString("/")
		if strings.HasPrefix(seg, "[") && strings.HasSuffix(seg, "]") {
			sb.WriteString("{" + seg[1:len(seg)-1] + "}")
		} else {
			sb.WriteString(seg)
		}
	}
	r.Path = sb.String()
	if r.Path == "" {
		r.Path = "/"
	}
	return r, true, nil
}

// RouteMatches reports whether any route entry matches the method and path.
// Paths may contain {param} wildcards that match a single path segment.
func RouteMatches(method, path string, routes []RouteEntry) bool {
	for _, r := range routes {
		if r.Method != "" && !strings.EqualFold(r.Method, method) {
			continue
		}
		pathSegs := strings.Split(strings.Trim(path, "/"), "/")
		patSegs := strings.Split(strings.Trim(r.Path, "/"), "/")
		if len(pathSegs) != len(patSegs) {
			continue
		}
		ok := true
		for i, ps := range patSegs {
			if strings.HasPrefix(ps, "{") && strings.HasSuffix(ps, "}") {
				continue
			}
			if ps != pathSegs[i] {
				ok = false
				break
			}
		}
		if ok {
			return true
		}
	}
	return false
}

// ScanComponents walks the component dirs, grouping files by directory so a
// component package may span multiple files. Returns the packages, the files
// that need typegen, and the flat component count.
func ScanComponents(dirs []string, root string) ([]ComponentPackage, []ComponentFile, error) {
	seenName := map[string]string{}
	var packages []ComponentPackage
	var typeFiles []ComponentFile
	for _, dir := range dirs {
		absRoot := dir
		if !filepath.IsAbs(absRoot) {
			absRoot = filepath.Join(root, dir)
		}
		if _, err := os.Stat(absRoot); os.IsNotExist(err) {
			continue
		}
		byDir := map[string][]string{}
		err := filepath.Walk(absRoot, func(path string, info os.FileInfo, err error) error {
			if err != nil || info.IsDir() {
				return err
			}
			if !strings.HasSuffix(path, ".go") || strings.HasPrefix(filepath.Base(path), "_") {
				return nil
			}
			byDir[filepath.Dir(path)] = append(byDir[filepath.Dir(path)], path)
			return nil
		})
		if err != nil {
			return nil, nil, err
		}
		dirsSorted := make([]string, 0, len(byDir))
		for d := range byDir {
			dirsSorted = append(dirsSorted, d)
		}
		sort.Strings(dirsSorted)
		for _, dir := range dirsSorted {
			files := byDir[dir]
			sort.Strings(files)
			var pkgComps []gotsgen.Component
			pkgName := ""
			if data, err := os.ReadFile(files[0]); err == nil {
				pkgName = GoFilePackage(data)
			}
			for _, f := range files {
				src, err := os.ReadFile(f)
				if err != nil {
					return nil, nil, err
				}
				parsed, err := gotsgen.ParseFile(f, src)
				if err != nil {
					return nil, nil, err
				}
				if len(parsed) == 0 {
					continue
				}
				typeFiles = append(typeFiles, ComponentFile{Path: f, Comps: parsed})
				pkgComps = append(pkgComps, parsed...)
				for _, c := range parsed {
					if prev, dup := seenName[c.Name]; dup {
						return nil, nil, fmt.Errorf("Go component %q is defined in both %s and %s (component names must be unique)", c.Name, prev, f)
					}
					seenName[c.Name] = f
				}
			}
			if len(pkgComps) == 0 {
				continue
			}
			rel := ""
			if r, rerr := filepath.Rel(absRoot, dir); rerr == nil && r != "." {
				rel = SanitizeRelDir(filepath.ToSlash(r))
			}
			packages = append(packages, ComponentPackage{Rel: rel, Dir: dir, Pkg: pkgName, Files: files, Comps: pkgComps})
		}
	}
	sort.Slice(packages, func(i, j int) bool { return packages[i].Rel < packages[j].Rel })
	sort.Slice(typeFiles, func(i, j int) bool { return typeFiles[i].Path < typeFiles[j].Path })
	return packages, typeFiles, nil
}

// SanitizeName makes a path segment safe for a Go file/import path.
func SanitizeName(s string) string {
	if s == "" {
		return "_"
	}
	return goNameSanitizeRe.ReplaceAllString(s, "_")
}

// SanitizeRelDir sanitizes each segment of a slash-relative dir.
func SanitizeRelDir(dir string) string {
	if dir == "" || dir == "." {
		return ""
	}
	parts := strings.Split(filepath.ToSlash(dir), "/")
	for i, p := range parts {
		parts[i] = SanitizeName(p)
	}
	return strings.Join(parts, "/")
}
