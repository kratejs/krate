package build

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
)

// goAPIMethods are the HTTP method handler names a Go route file may define,
// in registration order. A `Handler` function instead handles all methods.
var goAPIMethods = []string{"GET", "POST", "PUT", "DELETE", "PATCH", "OPTIONS", "HEAD"}

// goRouteFuncRe matches `func Handler(...)` / `func GET(...)` declarations.
var goRouteFuncRe = regexp.MustCompile(`(?m)^\s*func\s+(Handler|GET|POST|PUT|DELETE|PATCH|OPTIONS|HEAD)\s*\(`)

// goRoutePackageRe matches the package clause so copied route files can be
// rewritten into a unique `package route`.
var goRoutePackageRe = regexp.MustCompile(`(?m)^\s*package\s+[\w.]+`)

// goAPIRouteEntry is one route in the sidecar manifest written at build time.
type goAPIRouteEntry struct {
	Method string `json:"method"` // HTTP method, or "" for all methods (Handler)
	Path   string `json:"path"`   // Go 1.22 ServeMux pattern, e.g. "/api/users/{id}"
}

// goAPIManifest is the JSON file the main server reads to know which /api
// routes the Go sidecar handles.
type goAPIManifest struct {
	Routes []goAPIRouteEntry `json:"routes"`
}

const goAPIGoMod = `module krate-goapi

go 1.25
`

const goAPIRuntimeSource = `// Package runtime provides helpers for krate Go API route handlers.
package runtime

import (
	"encoding/json"
	"net/http"
	"os"
	"os/signal"
	"syscall"
)

// WriteJSON writes v as a JSON response with the given status code.
func WriteJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// Error writes a JSON error response.
func Error(w http.ResponseWriter, status int, msg string) {
	WriteJSON(w, status, map[string]string{"error": msg})
}

// Serve starts the API server on the port from KRATE_GOAPI_PORT (default 3002)
// and blocks until the process receives a termination signal.
func Serve(mux *http.ServeMux) {
	port := os.Getenv("KRATE_GOAPI_PORT")
	if port == "" {
		port = "3002"
	}
	srv := &http.Server{Addr: "127.0.0.1:" + port, Handler: mux}
	go func() {
		ch := make(chan os.Signal, 1)
		signal.Notify(ch, os.Interrupt, syscall.SIGTERM)
		<-ch
		_ = srv.Close()
	}()
	_ = srv.ListenAndServe()
}
`

// goRoute describes a single compiled Go API route.
type goRoute struct {
	ID      string   // unique package id, e.g. "r0"
	Path    string   // Go ServeMux pattern, e.g. "/api/users/{id}"
	Methods []string // resolved HTTP methods (or ["Handler"] for all-methods)
	Source  string   // absolute path of the source .go file
}

// BuildAllGoAPI scans src/api/ for .go route files, scaffolds a standalone Go
// module in .krate/goapi/, compiles it into a sidecar binary with `go build`,
// and writes the route manifest to .krate/goapi-routes.json. Returns nil when
// there are no Go API routes.
func (b *Builder) BuildAllGoAPI() error {
	apiSrcDir := filepath.Join(b.Root, "src", "api")
	if _, err := os.Stat(apiSrcDir); os.IsNotExist(err) {
		return nil
	}

	var files []string
	if err := filepath.Walk(apiSrcDir, func(path string, info os.FileInfo, err error) error {
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
	}); err != nil {
		return fmt.Errorf("scanning Go API routes: %w", err)
	}
	if len(files) == 0 {
		// Remove stale artifacts so a build that dropped all Go routes stops
		// serving them.
		_ = os.Remove(filepath.Join(b.Root, ".krate", "goapi-routes.json"))
		return nil
	}

	routes, err := b.scaffoldGoAPIModule(apiSrcDir, files)
	if err != nil {
		return err
	}

	binPath := goAPIServerBinPath(b.Root)
	if err := b.buildGoAPIBinary(binPath); err != nil {
		return err
	}

	// Write the route manifest so the main server knows what to proxy.
	manifest := goAPIManifest{}
	seen := make(map[string]bool)
	for _, r := range routes {
		for _, m := range r.Methods {
			key := m + " " + r.Path
			if seen[key] {
				return fmt.Errorf("duplicate Go API route %q (files map to the same path)", r.Path)
			}
			seen[key] = true
			method := m
			if method == "Handler" {
				method = ""
			}
			manifest.Routes = append(manifest.Routes, goAPIRouteEntry{Method: method, Path: r.Path})
		}
	}
	sort.Slice(manifest.Routes, func(i, j int) bool {
		if manifest.Routes[i].Path == manifest.Routes[j].Path {
			return manifest.Routes[i].Method < manifest.Routes[j].Method
		}
		return manifest.Routes[i].Path < manifest.Routes[j].Path
	})

	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling Go API manifest: %w", err)
	}
	manifestPath := filepath.Join(b.Root, ".krate", "goapi-routes.json")
	if err := os.MkdirAll(filepath.Dir(manifestPath), 0755); err != nil {
		return fmt.Errorf("creating .krate dir: %w", err)
	}
	if err := os.WriteFile(manifestPath, data, 0644); err != nil {
		return fmt.Errorf("writing Go API manifest: %w", err)
	}

	fmt.Printf("  %s▶ Go API%s %d route(s) → %s\n", cGreen, cReset, len(manifest.Routes), binPath)
	return nil
}

// scaffoldGoAPIModule writes the .krate/goapi/ module: go.mod, the runtime
// helper package, one directory per route (copied source + generated
// Register function), and cmd/server/main.go. It returns the parsed routes.
func (b *Builder) scaffoldGoAPIModule(apiSrcDir string, files []string) ([]goRoute, error) {
	modDir := filepath.Join(b.Root, ".krate", "goapi")
	if err := os.RemoveAll(modDir); err != nil {
		return nil, fmt.Errorf("cleaning Go API module dir: %w", err)
	}
	if err := os.MkdirAll(filepath.Join(modDir, "cmd", "server"), 0755); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Join(modDir, "runtime"), 0755); err != nil {
		return nil, err
	}

	if err := os.WriteFile(filepath.Join(modDir, "go.mod"), []byte(goAPIGoMod), 0644); err != nil {
		return nil, err
	}
	if err := os.WriteFile(filepath.Join(modDir, "runtime", "runtime.go"), []byte(goAPIRuntimeSource), 0644); err != nil {
		return nil, err
	}

	var routes []goRoute
	var imports []string
	var registers []string

	for i, file := range files {
		r, err := buildGoRoute(apiSrcDir, file, i)
		if err != nil {
			return nil, err
		}
		routes = append(routes, r)

		routeDir := filepath.Join(modDir, "routes", r.ID)
		if err := os.MkdirAll(routeDir, 0755); err != nil {
			return nil, err
		}

		src, err := os.ReadFile(file)
		if err != nil {
			return nil, fmt.Errorf("reading Go API route %s: %w", file, err)
		}
		// Rewrite the package clause so the copied file lives in its own
		// package (`route`) inside the generated module.
		copied := goRoutePackageRe.ReplaceAllString(string(src), "package route")
		if err := os.WriteFile(filepath.Join(routeDir, "route.go"), []byte(copied), 0644); err != nil {
			return nil, err
		}

		register := goRegisterSource(r)
		if err := os.WriteFile(filepath.Join(routeDir, "register.go"), []byte(register), 0644); err != nil {
			return nil, err
		}

		imports = append(imports, fmt.Sprintf("\t%s \"krate-goapi/routes/%s\"", r.ID, r.ID))
		registers = append(registers, fmt.Sprintf("\t%s.Register(mux)", r.ID))
	}

	mainSrc := goAPIMainSource(imports, registers)
	if err := os.WriteFile(filepath.Join(modDir, "cmd", "server", "main.go"), []byte(mainSrc), 0644); err != nil {
		return nil, err
	}

	return routes, nil
}

// buildGoRoute computes a route's URL path and which handler functions exist.
func buildGoRoute(apiSrcDir, file string, index int) (goRoute, error) {
	r := goRoute{ID: fmt.Sprintf("r%d", index), Source: file}

	rel, err := filepath.Rel(apiSrcDir, file)
	if err != nil {
		return r, err
	}
	rel = strings.TrimSuffix(rel, filepath.Ext(rel))

	segments := strings.Split(filepath.ToSlash(rel), "/")
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

	src, err := os.ReadFile(file)
	if err != nil {
		return r, fmt.Errorf("reading Go API route %s: %w", file, err)
	}
	defined := map[string]bool{}
	for _, match := range goRouteFuncRe.FindAllStringSubmatch(string(src), -1) {
		defined[match[1]] = true
	}

	switch {
	case defined["Handler"]:
		r.Methods = []string{"Handler"}
	default:
		for _, m := range goAPIMethods {
			if defined[m] {
				r.Methods = append(r.Methods, m)
			}
		}
		if len(r.Methods) == 0 {
			return r, fmt.Errorf("Go API route %s must define a Handler function or at least one of GET/POST/PUT/DELETE/PATCH/OPTIONS/HEAD", file)
		}
	}
	return r, nil
}

// goRegisterSource generates the per-route Register function.
func goRegisterSource(r goRoute) string {
	var sb strings.Builder
	sb.WriteString("// Code generated by krate. DO NOT EDIT.\n")
	sb.WriteString("package route\n\n")
	sb.WriteString("import \"net/http\"\n\n")
	sb.WriteString("func Register(mux *http.ServeMux) {\n")
	for _, m := range r.Methods {
		if m == "Handler" {
			fmt.Fprintf(&sb, "\tmux.HandleFunc(%q, Handler)\n", r.Path)
		} else {
			fmt.Fprintf(&sb, "\tmux.HandleFunc(%q, %s)\n", m+" "+r.Path, m)
		}
	}
	sb.WriteString("}\n")
	return sb.String()
}

// goAPIMainSource generates the sidecar entrypoint that registers every route.
func goAPIMainSource(imports, registers []string) string {
	var sb strings.Builder
	sb.WriteString("// Code generated by krate. DO NOT EDIT.\n")
	sb.WriteString("package main\n\n")
	sb.WriteString("import (\n")
	sb.WriteString("\t\"net/http\"\n\n")
	sb.WriteString("\t\"krate-goapi/runtime\"\n")
	for _, imp := range imports {
		sb.WriteString(imp)
		sb.WriteString("\n")
	}
	sb.WriteString(")\n\n")
	sb.WriteString("func main() {\n")
	sb.WriteString("\tmux := http.NewServeMux()\n")
	for _, reg := range registers {
		sb.WriteString(reg)
		sb.WriteString("\n")
	}
	sb.WriteString("\truntime.Serve(mux)\n")
	sb.WriteString("}\n")
	return sb.String()
}

// buildGoAPIBinary compiles the generated module with `go build`.
func (b *Builder) buildGoAPIBinary(binPath string) error {
	if _, err := exec.LookPath("go"); err != nil {
		return fmt.Errorf("Go API routes require the Go toolchain: %w", err)
	}

	modDir := filepath.Join(b.Root, ".krate", "goapi")
	cmd := exec.Command("go", "build", "-o", binPath, "./cmd/server")
	cmd.Dir = modDir
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("compiling Go API sidecar failed:\n%s%w", string(out), err)
	}
	return nil
}

// goAPIServerBinPath returns the absolute path of the compiled Go API sidecar.
func goAPIServerBinPath(root string) string {
	name := "goapi-server"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	return filepath.Join(root, ".krate", name)
}
