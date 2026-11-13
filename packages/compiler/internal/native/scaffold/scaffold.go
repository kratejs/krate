// Package scaffold writes the starter app of `krate native init`.
package scaffold

import (
	"bytes"
	"embed"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"text/template"

	"github.com/kratejs/krate/packages/compiler/internal/diag"
)

//go:embed templates
var templates embed.FS

// Options tune Init.
type Options struct {
	// Go adds a Go host module (go.mod and src/lib/tasks.go).
	Go bool
	// Name is the project name; default: the directory name.
	Name string
	// NativeReplace, when set, is the path of the packages/native checkout
	// the generated go.mod replaces the host module with (monorepo use).
	NativeReplace string
}

type data struct {
	Name, Product, AppID, Module string
	NativeRequire                bool
	NativeReplace                string
}

var nonID = regexp.MustCompile(`[^a-z0-9]+`)

func slug(s string) string {
	s = strings.Trim(nonID.ReplaceAllString(strings.ToLower(s), "-"), "-")
	if s == "" {
		return "app"
	}
	return s
}

func titleOf(name string) string {
	var out []string
	for _, w := range strings.FieldsFunc(name, func(r rune) bool { return r == '-' || r == '_' || r == ' ' || r == '.' }) {
		out = append(out, strings.ToUpper(w[:1])+w[1:])
	}
	if len(out) == 0 {
		return "My App"
	}
	return strings.Join(out, " ")
}

// file is one output file: template name, destination relative to the project.
type file struct{ tmpl, dest string }

// Init writes the starter app into dir (created if needed) and returns the
// relative paths it wrote, or an error diagnostic when a file already exists.
func Init(dir string, opts Options) ([]string, *diag.Diagnostic) {
	name := opts.Name
	if name == "" {
		abs, err := filepath.Abs(dir)
		if err != nil {
			abs = dir
		}
		name = filepath.Base(abs)
	}
	slugged := slug(name)
	d := data{
		Name: slugged, Product: titleOf(name),
		AppID:  "com.example." + strings.ReplaceAll(slugged, "-", ""),
		Module: slugged, NativeRequire: opts.NativeReplace != "", NativeReplace: filepath.ToSlash(opts.NativeReplace),
	}
	files := []file{
		{"templates/app/package.json.tmpl", "package.json"},
		{"templates/app/krate.config.ts.tmpl", "krate.config.ts"},
		{"templates/app/tsconfig.json.tmpl", "tsconfig.json"},
		{"templates/app/gitignore.tmpl", ".gitignore"},
		{"templates/app/greet.host.ts.tmpl", "src/lib/greet.host.ts"},
	}
	if opts.Go {
		files = append(files,
			file{"templates/go/go.mod.tmpl", "go.mod"},
			file{"templates/go/tasks.go.tmpl", "src/lib/tasks.go"},
			file{"templates/go/index.tsx.tmpl", "src/pages/index.tsx"})
	} else {
		files = append(files, file{"templates/app/index.tsx.tmpl", "src/pages/index.tsx"})
	}
	for _, f := range files {
		if _, err := os.Stat(filepath.Join(dir, filepath.FromSlash(f.dest))); err == nil {
			x := diag.New(f.dest, 1, 1, f.dest+" already exists", "run krate native init in an empty directory, or remove the file", "").WithCode(diag.CodeNativeInit)
			return nil, &x
		}
	}
	var written []string
	for _, f := range files {
		raw, err := templates.ReadFile(f.tmpl)
		if err != nil {
			x := diag.New(f.dest, 1, 1, err.Error(), "this is a bug in the compiler's templates", "").WithCode(diag.CodeNativeInit)
			return nil, &x
		}
		t, err := template.New(f.tmpl).Parse(string(raw))
		if err != nil {
			x := diag.New(f.dest, 1, 1, err.Error(), "this is a bug in the compiler's templates", "").WithCode(diag.CodeNativeInit)
			return nil, &x
		}
		var buf bytes.Buffer
		if err := t.Execute(&buf, d); err != nil {
			x := diag.New(f.dest, 1, 1, err.Error(), "this is a bug in the compiler's templates", "").WithCode(diag.CodeNativeInit)
			return nil, &x
		}
		out := filepath.Join(dir, filepath.FromSlash(f.dest))
		if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
			x := diag.New(f.dest, 1, 1, err.Error(), "check that the directory is writable", "").WithCode(diag.CodeNativeInit)
			return nil, &x
		}
		if err := os.WriteFile(out, buf.Bytes(), 0o644); err != nil {
			x := diag.New(f.dest, 1, 1, err.Error(), "check that the directory is writable", "").WithCode(diag.CodeNativeInit)
			return nil, &x
		}
		written = append(written, f.dest)
	}
	return written, nil
}
