package plugins

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"text/template"

	"github.com/kratejs/krate/packages/compiler/internal/diag"
)

// ScaffoldOptions tune Scaffold.
type ScaffoldOptions struct {
	// Name is the plugin's name (see ValidName).
	Name string
	// Module is the Go module path; default "example.com/krate-plugin-<name>".
	Module string
	// NativeReplace, when set, is the path of the packages/native checkout the
	// go.mod replaces the native module with (monorepo use).
	NativeReplace string
}

type scaffoldData struct {
	Name, Module, Package, Title string
	NativeReplace                string
}

var scaffoldFiles = []struct{ dest, tmpl string }{
	{"krate-plugin.json", scaffoldManifest},
	{"go.mod", scaffoldGoMod},
	{"{{.Package}}.go", scaffoldGo},
	{"{{.Package}}_test.go", scaffoldTest},
	{"README.md", scaffoldReadme},
}

// Scaffold writes a new plugin into dir (created if needed) and returns the
// files it wrote, or a diagnostic when the name is invalid or a file exists.
func Scaffold(dir string, o ScaffoldOptions) ([]string, *diag.Diagnostic) {
	fail := func(msg, hint string) ([]string, *diag.Diagnostic) {
		d := diag.New(dir, 1, 1, msg, hint, "").WithCode(diag.CodePluginManifestInvalid)
		return nil, &d
	}
	if !ValidName(o.Name) {
		return fail(fmt.Sprintf("%q is not a valid plugin name", o.Name), "use lowercase words joined by hyphens, 2 to 40 characters, for example \"system-info\"")
	}
	d := scaffoldData{Name: o.Name, Module: o.Module, NativeReplace: filepath.ToSlash(o.NativeReplace)}
	if d.Module == "" {
		d.Module = "example.com/krate-plugin-" + o.Name
	}
	d.Package = strings.ReplaceAll(o.Name, "-", "")
	d.Title = strings.ToUpper(d.Package[:1]) + d.Package[1:]
	var wrote []string
	for _, f := range scaffoldFiles {
		dest, err := render(f.dest, d)
		if err != nil {
			return nil, diagFrom(dir, err)
		}
		path := filepath.Join(dir, dest)
		if _, err := os.Stat(path); err == nil {
			return fail(path+" already exists", "pick another directory; scaffolding does not overwrite files")
		}
	}
	for _, f := range scaffoldFiles {
		dest, _ := render(f.dest, d)
		body, err := render(f.tmpl, d)
		if err != nil {
			return nil, diagFrom(dir, err)
		}
		path := filepath.Join(dir, dest)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return nil, diagFrom(dir, err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			return nil, diagFrom(dir, err)
		}
		wrote = append(wrote, filepath.ToSlash(dest))
	}
	return wrote, nil
}

func diagFrom(dir string, err error) *diag.Diagnostic {
	d := diag.New(dir, 1, 1, err.Error(), "check that the directory is writable", "").WithCode(diag.CodePluginManifestInvalid)
	return &d
}

func render(tmpl string, d scaffoldData) (string, error) {
	t, err := template.New("p").Parse(tmpl)
	if err != nil {
		return "", err
	}
	var sb strings.Builder
	if err := t.Execute(&sb, d); err != nil {
		return "", err
	}
	return sb.String(), nil
}

const scaffoldManifest = `{
  "name": "{{.Name}}",
  "version": "0.1.0",
  "description": "A Krate Native plugin.",
  "krate": "0.5.0",
  "go": "{{.Module}}",
  "permissions": [
    { "name": "use", "description": "call the {{.Name}} plugin" }
  ],
  "ops": [
    { "name": "Hello", "permission": "use" },
    { "name": "Ticks", "permission": "use" }
  ]
}
`

const scaffoldGoMod = `module {{.Module}}

go 1.26.1

require github.com/kratejs/krate/packages/native v0.0.0
{{if .NativeReplace}}
// Inside the Krate monorepo the native module is the sibling package.
replace github.com/kratejs/krate/packages/native => {{.NativeReplace}}
{{end}}`

const scaffoldGo = `// Package {{.Package}} is a Krate Native plugin. The exported functions and
// streams that krate-plugin.json lists as ops are callable from pages as
//
//	import { Hello, Ticks } from "krate/plugin/{{.Name}}";
//
// with generated codecs and types; there is no glue to write. Init is
// optional: it receives the plugin context (platform access, lifecycle hooks).
package {{.Package}}

import (
	"fmt"
	"time"

	"github.com/kratejs/krate/packages/native/krate"
	"github.com/kratejs/krate/packages/native/plugin"
)

// Hello greets name.
func Hello(name string) string {
	return fmt.Sprintf("Hello, %s, from the {{.Name}} plugin", name)
}

// Ticks streams a counter every interval milliseconds until the page stops reading.
func Ticks(ctx krate.Ctx, interval int) <-chan int {
	if interval < 50 {
		interval = 50
	}
	out := make(chan int)
	go func() {
		defer close(out)
		t := time.NewTicker(time.Duration(interval) * time.Millisecond)
		defer t.Stop()
		for n := 1; ; n++ {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				select {
				case out <- n:
				case <-ctx.Done():
					return
				}
			}
		}
	}()
	return out
}

// Init runs once while the shell loads. Use c.OnStart for anything that needs
// the platform, c.Dispatch to run on the main thread, and c.DataDir for a
// directory of the plugin's own.
func Init(c *plugin.Context) error {
	c.OnStart(func() error {
		c.Logf("started in %s", c.AppName())
		return nil
	})
	return nil
}
`

const scaffoldTest = `package {{.Package}}

import "testing"

func TestHello(t *testing.T) {
	if got := Hello("Ada"); got != "Hello, Ada, from the {{.Name}} plugin" {
		t.Errorf("Hello = %q", got)
	}
}
`

const scaffoldReadme = `# {{.Name}}

A Krate Native plugin.

## Use it

In an app's krate.config.ts:

    native: { plugins: ['./path/to/{{.Name}}'] }

and in a page:

    import { Hello } from 'krate/plugin/{{.Name}}';

The ops need the permission plugin:{{.Name}}.use; a release build derives it
from the code that calls them.

## Publish it

- As a Go module: tag the module; apps list "go:{{.Module}}@v0.1.0".
- As an npm package: ship this directory (krate-plugin.json, go.mod and the Go
  sources) in the package; apps list the package name.
`
