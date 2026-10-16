package gohost

import (
	"embed"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"text/template"

	"github.com/kratejs/krate/packages/compiler/goext"
	"github.com/kratejs/krate/packages/compiler/internal/gotsgen"
)

//go:embed templates/*.tmpl
var templateFS embed.FS

var (
	renderTmpl   = template.Must(template.ParseFS(templateFS, "templates/render.go.tmpl"))
	registerTmpl = template.Must(template.ParseFS(templateFS, "templates/register.go.tmpl"))
	mainTmpl     = template.Must(template.ParseFS(templateFS, "templates/main.go.tmpl"))
)

type registerData struct {
	Pkg         string
	ModulePath  string
	KrateImport string
	HasRequest  bool
	Components  []gotsgen.Component
}

type mainData struct {
	HasComponents  bool
	GoextRuntime   string
	ModulePath     string
	CompImports    []string
	RouteImports   []string
	RouteRegisters []string
}

func execTemplate(t *template.Template, data any) (string, error) {
	var b strings.Builder
	if err := t.Execute(&b, data); err != nil {
		return "", err
	}
	return b.String(), nil
}

// RenderSource returns the generated render registry package source.
func RenderSource() (string, error) { return execTemplate(renderTmpl, nil) }

// RegisterSource returns the generated register file for a component package.
func RegisterSource(modulePath, pkg string, comps []gotsgen.Component) (string, error) {
	hasReq := false
	for _, c := range comps {
		if c.HasRequest {
			hasReq = true
			break
		}
	}
	return execTemplate(registerTmpl, registerData{
		Pkg: pkg, ModulePath: modulePath, KrateImport: KrateImport, HasRequest: hasReq, Components: comps,
	})
}

// MainSource returns the generated host main package source.
func MainSource(modulePath string, routes []Route, hasComponents bool, compImports []string) (string, error) {
	routeImports := make([]string, 0, len(routes))
	routeRegisters := make([]string, 0, len(routes))
	for i, r := range routes {
		routeImports = append(routeImports, "route"+strconv.Itoa(i)+" "+strconv.Quote(r.ImportPath))
		routeRegisters = append(routeRegisters, "route"+strconv.Itoa(i)+"."+r.Register+"(mux)")
	}
	return execTemplate(mainTmpl, mainData{
		HasComponents:  hasComponents,
		GoextRuntime:   GoextRuntimeImport,
		ModulePath:     modulePath,
		CompImports:    compImports,
		RouteImports:   routeImports,
		RouteRegisters: routeRegisters,
	})
}

// WriteGoextModule vendors the goext helper packages (krate, runtime) into
// <root>/.krate/goext under the real module path, so the generated host module
// can `replace` it locally (offline builds) while user Go code imports the real
// URL for intellisense.
func WriteGoextModule(root string) error {
	dir := GoextVendorDir(root)
	if err := os.RemoveAll(dir); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Join(dir, "goext", "krate"), 0755); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Join(dir, "goext", "runtime"), 0755); err != nil {
		return err
	}
	goMod := "module " + GoextModulePath + "\n\ngo 1.21\n"
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(goMod), 0644); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, "goext", "krate", "krate.go"), goext.KrateSrc, 0644); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, "goext", "krate", "request.go"), goext.KrateRequestSrc, 0644); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "goext", "runtime", "runtime.go"), goext.RuntimeSrc, 0644)
}

// EnsureGoextRequire appends the goext require + local replace to a copied
// go.mod when they are not already present.
func EnsureGoextRequire(content string) string {
	if strings.Contains(content, GoextModulePath) {
		return content
	}
	return content + "\nrequire " + GoextModulePath + " v0.0.0\nreplace " + GoextModulePath + " => ../goext\n"
}

// ParseModulePath extracts the module path from a go.mod's `module` clause.
func ParseModulePath(content string) string {
	for _, line := range strings.Split(content, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "module ") {
			return strings.TrimSpace(strings.TrimPrefix(trimmed, "module "))
		}
	}
	return ""
}

// SplitGoMod returns the `go` version and all directives (everything except the
// module clause and the go clause), preserving require/replace/exclude blocks.
func SplitGoMod(content string) (version string, directives string) {
	var kept []string
	for _, line := range strings.Split(content, "\n") {
		trimmed := strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(trimmed, "module "):
			continue
		case strings.HasPrefix(trimmed, "go "):
			version = strings.TrimSpace(strings.TrimPrefix(trimmed, "go "))
			continue
		default:
			kept = append(kept, line)
		}
	}
	return version, strings.TrimSpace(strings.Join(kept, "\n"))
}

// RebaseReplacePaths rewrites relative replace targets so they remain valid from
// the generated module's deeper directory (.krate/goapi/); paths relative to the
// project root are two levels up.
func RebaseReplacePaths(directives string) string {
	lines := strings.Split(directives, "\n")
	for i, line := range lines {
		idx := strings.Index(line, "=>")
		if idx < 0 {
			continue
		}
		lhs, rhs := line[:idx+2], line[idx+2:]
		fields := strings.Fields(rhs)
		if len(fields) == 0 {
			continue
		}
		target := fields[0]
		if strings.HasPrefix(target, "./") || strings.HasPrefix(target, "../") {
			fields[0] = "../../" + strings.TrimPrefix(target, "./")
			lines[i] = lhs + " " + strings.Join(fields, " ")
		}
	}
	return strings.Join(lines, "\n")
}
