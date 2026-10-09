package build

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"

	"github.com/kratejs/krate/packages/compiler/internal/config"
)

// goAPIMethods are the HTTP method handler names a Go route file may define,
// in registration order. A `Handler` function instead handles all methods.
var goAPIMethods = []string{"GET", "POST", "PUT", "DELETE", "PATCH", "OPTIONS", "HEAD"}

// goRouteFuncRe matches `func Handler(...)` / `func GET(...)` declarations.
var goRouteFuncRe = regexp.MustCompile(`(?m)^\s*func\s+(Handler|GET|POST|PUT|DELETE|PATCH|OPTIONS|HEAD)\s*\(`)

// goPackageRe matches a Go file's package clause.
var goPackageRe = regexp.MustCompile(`(?m)^\s*package\s+([A-Za-z_][A-Za-z0-9_]*)`)

// goPackageRewriteRe matches the package clause so a route file copied into its
// own generated package can be rewritten to `package route`.
var goPackageRewriteRe = regexp.MustCompile(`(?m)^\s*package\s+[A-Za-z_][A-Za-z0-9_]*`)

// goNameSanitizeRe replaces characters Go rejects in import paths / file names.
var goNameSanitizeRe = regexp.MustCompile(`[^A-Za-z0-9_]+`)

// sanitizeGoName makes a path segment safe for a Go file/import path.
func sanitizeGoName(s string) string {
	if s == "" {
		return "_"
	}
	return goNameSanitizeRe.ReplaceAllString(s, "_")
}

// sanitizeGoRelDir sanitizes each segment of a slash-relative dir.
func sanitizeGoRelDir(dir string) string {
	if dir == "" || dir == "." {
		return ""
	}
	parts := strings.Split(filepath.ToSlash(dir), "/")
	for i, p := range parts {
		parts[i] = sanitizeGoName(p)
	}
	return strings.Join(parts, "/")
}

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

// goRoute describes a single compiled Go API route. Route files are placed in
// their own generated package (like the original flattening) so multiple route
// files can define `Handler` without colliding, while sibling helper packages
// are preserved separately.
type goRoute struct {
	ID         string   // unique register id, e.g. "r0"
	Path       string   // Go ServeMux pattern, e.g. "/api/users/{id}"
	Methods    []string // resolved HTTP methods (or ["Handler"] for all-methods)
	Source     string   // absolute path of the source .go file
	ImportPath string   // module import path of the generated route package
	Register   string   // unique exported register function, e.g. "RegisterR0"
}

// BuildAllGoAPI scans src/api/ for .go route files, scaffolds a standalone Go
// module in .krate/goapi/, compiles it into a sidecar binary with `go build`,
// and writes the route manifest to .krate/goapi-routes.json. Returns nil when
// there are no Go API routes.
func (b *Builder) BuildAllGoAPI() error {
	if !b.Cfg.GoAPI.EnabledOrDefault() {
		return nil
	}
	apiSrcDir := filepath.Join(b.Root, "src", "api")
	if _, err := os.Stat(apiSrcDir); os.IsNotExist(err) {
		return nil
	}

	files, err := scanGoAPIFiles(apiSrcDir)
	if err != nil {
		return err
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
	if len(routes) == 0 {
		// Only helper packages, no routes: nothing to serve.
		_ = os.Remove(filepath.Join(b.Root, ".krate", "goapi-routes.json"))
		return nil
	}

	binPath := goAPIServerBinPath(b.Root)
	if err := b.buildGoAPIBinary(binPath); err != nil {
		return err
	}

	if err := b.writeGoAPIManifest(routes); err != nil {
		return err
	}

	fmt.Printf("  %s▶ Go API%s %d route(s) → %s\n", cGreen, cReset, len(routes), binPath)
	return nil
}

// scanGoAPIFiles returns every non-underscore `.go` file under src/api.
func scanGoAPIFiles(apiSrcDir string) ([]string, error) {
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

// scaffoldGoAPIModule writes the .krate/goapi/ module: go.mod (resolved),
// the runtime helper package, a preserved copy of the src/api tree under
// routes/, per-route register files, and cmd/server/main.go.
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

	modulePath, err := writeGoAPIModule(modDir, apiSrcDir, b.Root, b.Cfg.GoAPI)
	if err != nil {
		return nil, err
	}
	if err := os.WriteFile(filepath.Join(modDir, "runtime", "runtime.go"), []byte(goAPIRuntimeSource), 0644); err != nil {
		return nil, err
	}

	var routes []goRoute
	// Preserve the source tree: helper files keep their (sanitized) directory
	// and package clause so sibling/internal packages stay importable (e.g.
	// krate-goapi/routes/lib), while each route file is isolated in its own
	// generated `route` package so multiple files can define `Handler` without
	// colliding and `[param]` file names stay valid.
	for _, file := range files {
		rel, err := filepath.Rel(apiSrcDir, file)
		if err != nil {
			return nil, err
		}
		src, err := os.ReadFile(file)
		if err != nil {
			return nil, fmt.Errorf("reading Go API file %s: %w", file, err)
		}
		if goFilePackage(src) == "" {
			return nil, fmt.Errorf("Go API file %s has no package clause", file)
		}

		r, hasRoute, err := parseGoRoute(apiSrcDir, file, src)
		if err != nil {
			return nil, err
		}
		relDir := sanitizeGoRelDir(filepath.Dir(rel))
		base := sanitizeGoName(strings.TrimSuffix(filepath.Base(rel), ".go"))

		if !hasRoute {
			dstDir := filepath.Join(modDir, "routes", filepath.FromSlash(relDir))
			if err := os.MkdirAll(dstDir, 0755); err != nil {
				return nil, err
			}
			if err := os.WriteFile(filepath.Join(dstDir, base+".go"), src, 0644); err != nil {
				return nil, err
			}
			continue
		}

		routeDir := filepath.Join(modDir, "routes", filepath.FromSlash(relDir), base)
		if err := os.MkdirAll(routeDir, 0755); err != nil {
			return nil, err
		}
		copied := goPackageRewriteRe.ReplaceAllString(string(src), "package route")
		if err := os.WriteFile(filepath.Join(routeDir, "route.go"), []byte(copied), 0644); err != nil {
			return nil, err
		}
		r.ID = fmt.Sprintf("r%d", len(routes))
		r.Register = fmt.Sprintf("RegisterR%d", len(routes))
		impParts := []string{modulePath, "routes"}
		if relDir != "" {
			impParts = append(impParts, relDir)
		}
		impParts = append(impParts, base)
		r.ImportPath = strings.Join(impParts, "/")
		if err := os.WriteFile(filepath.Join(routeDir, "register.go"), []byte(goRegisterSource(r)), 0644); err != nil {
			return nil, err
		}
		routes = append(routes, r)
	}

	if len(routes) == 0 {
		return nil, nil
	}

	mainSrc := goAPIMainSource(modulePath, routes)
	if err := os.WriteFile(filepath.Join(modDir, "cmd", "server", "main.go"), []byte(mainSrc), 0644); err != nil {
		return nil, err
	}
	return routes, nil
}

// goFilePackage extracts the package clause name from Go source.
func goFilePackage(src []byte) string {
	if m := goPackageRe.FindSubmatch(src); len(m) == 2 {
		return string(m[1])
	}
	return ""
}

// buildGoRoute computes a route's URL path and handler set. hasRoute is false
// for helper files that define no handler.
func parseGoRoute(apiSrcDir, file string, src []byte) (goRoute, bool, error) {
	rel, err := filepath.Rel(apiSrcDir, file)
	if err != nil {
		return goRoute{}, false, err
	}
	r := goRoute{Source: file}

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
			return goRoute{}, false, nil // helper file
		}
	}

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

// writeGoAPIModule resolves and writes .krate/goapi/go.mod (plus go.sum when
// available), returning the module path used for import rewriting.
//
// Precedence:
//  1. src/api/go.mod — copied verbatim (full user control).
//  2. project-root go.mod — module path kept (default "krate-goapi"), copying
//     the root's go version and require/replace/exclude directives, rebasing
//     relative replace targets for the deeper directory.
//  3. generated module — "krate-goapi" + config deps/replaces.
func writeGoAPIModule(modDir, apiSrcDir, root string, cfg config.GoAPICfg) (string, error) {
	modulePath := cfg.Module
	if modulePath == "" {
		modulePath = "krate-goapi"
	}

	srcGoMod := filepath.Join(apiSrcDir, "go.mod")
	if data, err := os.ReadFile(srcGoMod); err == nil {
		if err := os.WriteFile(filepath.Join(modDir, "go.mod"), data, 0644); err != nil {
			return "", err
		}
		if sum, err := os.ReadFile(filepath.Join(apiSrcDir, "go.sum")); err == nil {
			_ = os.WriteFile(filepath.Join(modDir, "go.sum"), sum, 0644)
		}
		return modulePath, nil
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "module %s\n\n", modulePath)

	goVersion := "1.25"
	rootGoMod := filepath.Join(root, "go.mod")
	if data, err := os.ReadFile(rootGoMod); err == nil {
		v, directives := splitGoMod(string(data))
		if v != "" {
			goVersion = v
		}
		directives = rebaseGoModReplacePaths(directives)
		fmt.Fprintf(&sb, "go %s\n\n", goVersion)
		if directives != "" {
			sb.WriteString(directives)
			sb.WriteString("\n")
		}
		if sum, err := os.ReadFile(filepath.Join(root, "go.sum")); err == nil {
			_ = os.WriteFile(filepath.Join(modDir, "go.sum"), sum, 0644)
		}
	} else {
		fmt.Fprintf(&sb, "go %s\n\n", goVersion)
	}

	for _, d := range cfg.Deps {
		if d.Path == "" {
			continue
		}
		fmt.Fprintf(&sb, "require %s %s\n", d.Path, d.Version)
	}
	for _, r := range cfg.Replaces {
		if r.From == "" {
			continue
		}
		fmt.Fprintf(&sb, "replace %s => %s\n", r.From, r.To)
	}

	if err := os.WriteFile(filepath.Join(modDir, "go.mod"), []byte(sb.String()), 0644); err != nil {
		return "", err
	}
	return modulePath, nil
}

// splitGoMod returns the `go` version and all directives (everything except the
// module clause and the go clause), preserving require/replace/exclude blocks.
func splitGoMod(content string) (version string, directives string) {
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

// rebaseGoModReplacePaths rewrites relative replace targets so they remain valid
// from the generated module's deeper directory (.krate/goapi/). Relative paths
// in the root go.mod are relative to the project root, i.e. two levels up.
func rebaseGoModReplacePaths(directives string) string {
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

// goRegisterSource generates a per-route register function in the generated
// `route` package.
func goRegisterSource(r goRoute) string {
	var sb strings.Builder
	sb.WriteString("// Code generated by krate. DO NOT EDIT.\n")
	sb.WriteString("package route\n\n")
	sb.WriteString("import \"net/http\"\n\n")
	fmt.Fprintf(&sb, "func %s(mux *http.ServeMux) {\n", r.Register)
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
// Each route file is its own package, so every route gets a unique import alias.
func goAPIMainSource(modulePath string, routes []goRoute) string {
	var sb strings.Builder
	sb.WriteString("// Code generated by krate. DO NOT EDIT.\n")
	sb.WriteString("package main\n\n")
	sb.WriteString("import (\n")
	sb.WriteString("\t\"net/http\"\n\n")
	fmt.Fprintf(&sb, "\t\"%s/runtime\"\n", modulePath)
	for i, r := range routes {
		fmt.Fprintf(&sb, "\troute%d \"%s\"\n", i, r.ImportPath)
	}
	sb.WriteString(")\n\n")
	sb.WriteString("func main() {\n")
	sb.WriteString("\tmux := http.NewServeMux()\n")
	for i, r := range routes {
		fmt.Fprintf(&sb, "\troute%d.%s(mux)\n", i, r.Register)
	}
	sb.WriteString("\truntime.Serve(mux)\n")
	sb.WriteString("}\n")
	return sb.String()
}

func (b *Builder) writeGoAPIManifest(routes []goRoute) error {
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
	return nil
}

// buildGoAPIBinary compiles the generated module with `go build`, skipping the
// compile entirely when the module content and config are unchanged.
func (b *Builder) buildGoAPIBinary(binPath string) error {
	modDir := filepath.Join(b.Root, ".krate", "goapi")
	key := dirContentHash(modDir) + "|" + b.moduleConfigHash()
	cacheDir := filepath.Join(b.Root, ".krate", "cache", "goapi")
	keyPath := filepath.Join(cacheDir, "key")
	if _, err := os.Stat(binPath); err == nil {
		if prev, err := os.ReadFile(keyPath); err == nil && strings.TrimSpace(string(prev)) == key {
			return nil // unchanged since last successful build
		}
	}

	if _, err := exec.LookPath("go"); err != nil {
		return fmt.Errorf("Go API routes require the Go toolchain: %w", err)
	}

	if cfg := b.Cfg.GoAPI; cfg.Tidy != nil && *cfg.Tidy {
		tidy := exec.Command("go", "mod", "tidy")
		tidy.Dir = modDir
		if out, err := tidy.CombinedOutput(); err != nil {
			return fmt.Errorf("go mod tidy failed:\n%s%w", string(out), err)
		}
	}

	cmd := exec.Command("go", "build", "-o", binPath, "./cmd/server")
	cmd.Dir = modDir
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("compiling Go API sidecar failed (add goApi.deps/goApi.replaces or goApi.tidy for third-party modules):\n%s%w", string(out), err)
	}

	if err := os.MkdirAll(cacheDir, 0755); err == nil {
		_ = os.WriteFile(keyPath, []byte(key), 0644)
	}
	return nil
}

// moduleConfigHash folds the resolved goApi config into the binary cache key.
func (b *Builder) moduleConfigHash() string {
	data, _ := json.Marshal(b.Cfg.GoAPI)
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// dirContentHash returns a stable hash of every file under dir (path + bytes).
func dirContentHash(dir string) string {
	h := sha256.New()
	_ = filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		rel, _ := filepath.Rel(dir, path)
		h.Write([]byte(filepath.ToSlash(rel)))
		h.Write([]byte{0})
		if data, err := os.ReadFile(path); err == nil {
			h.Write(data)
		}
		h.Write([]byte{0})
		return nil
	})
	return hex.EncodeToString(h.Sum(nil))
}

// goAPIServerBinPath returns the absolute path of the compiled Go API sidecar.
func goAPIServerBinPath(root string) string {
	name := "goapi-server"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	return filepath.Join(root, ".krate", name)
}
