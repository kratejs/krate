package build

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/kratejs/krate/packages/compiler/internal/check"
)

// checksConfig builds the check package config from the project's `checks` key.
// An absent key yields an inactive config so compiler-enforced gates are
// strictly opt-in; `krate check` uses check.DefaultConfig() instead when the
// key is absent (explicit invocation should run the built-ins).
func (b *Builder) checksConfig() (check.Config, error) {
	cfg, err := check.FromMap(b.Cfg.Checks, b.Root, b.Env)
	if err != nil {
		return check.Config{}, err
	}
	// Resolve custom rule module paths relative to the project root.
	for i, m := range cfg.Custom {
		if m != "" && !filepath.IsAbs(m) {
			cfg.Custom[i] = filepath.Join(b.Root, m)
		}
	}
	return cfg, nil
}

// runQualityChecks evaluates the configured rules against the built pages,
// prints findings, and returns an error when any finding meets the fail-on
// threshold. Called at the end of BuildAll. A no-op when checks are absent.
func (b *Builder) runQualityChecks(results []*PageResult, runtimeJSFile string) error {
	cfg, err := b.checksConfig()
	if err != nil {
		return fmt.Errorf("checks config: %w", err)
	}
	if !cfg.Active {
		return nil
	}

	pages := b.checkPages(results, runtimeJSFile)
	cfg.Routes = routeSet(pages)
	findings, err := check.Run(cfg, pages)
	if err != nil {
		return fmt.Errorf("running checks: %w", err)
	}
	if len(findings) == 0 {
		if b.Verbose {
			fmt.Printf("  %s✓%s Quality checks passed (%d pages)\n", cGreen, cReset, len(pages))
		}
		return nil
	}

	// In-build reporting stays quiet: print only error findings (verbose also
	// shows warnings), then a one-line summary. `krate check` prints the full
	// report via check.Format.
	fopts := check.DefaultFormatOptions()
	errs, warns := check.Counts(findings)
	shown := findings
	if !b.Verbose {
		shown = nil
		for _, f := range findings {
			if f.Severity == check.Error {
				shown = append(shown, f)
			}
		}
	}
	if len(shown) > 0 {
		fmt.Print(check.FormatWith(shown, fopts))
	}
	fmt.Printf("  %s⚡%s Checks: %s\n", cCyan, cReset, check.Summary(errs, warns, fopts))

	if check.Failing(findings, cfg.FailOn) {
		return fmt.Errorf("quality checks failed (%d error(s))", errs)
	}
	return nil
}

// checkPages converts page results into the check package's page surface,
// computing each route's client JS weight (page hydration + shared runtime).
func (b *Builder) checkPages(results []*PageResult, runtimeJSFile string) []check.Page {
	runtimeBytes := fileSize(filepath.Join(b.Cfg.OutDir, runtimeJSFile))
	pages := make([]check.Page, 0, len(results))
	for _, r := range results {
		if r == nil || r.IsErrorPage {
			continue
		}
		jsBytes := runtimeBytes + len(r.HydrationJS)
		pages = append(pages, check.Page{
			Route:     routeFromOutName(r.OutName),
			RelSource: r.SourcePath,
			HTML:      r.FinalHTML,
			JSBytes:   jsBytes,
			Program:   r.Program,
		})
	}
	return pages
}

// CheckSite runs the quality-gate rules against an already-built output
// directory by re-reading the emitted HTML. This backs the `krate check`
// command, which validates the shipped artifact exactly as a host would serve
// it. When `checks` is absent the built-in defaults are used.
func (b *Builder) CheckSite(useDefaults bool) ([]check.Finding, check.Config, error) {
	cfg, err := b.checksConfig()
	if err != nil {
		return nil, cfg, fmt.Errorf("checks config: %w", err)
	}
	if useDefaults && b.Cfg.Checks == nil {
		cfg = check.DefaultConfig()
		cfg.Root = b.Root
		cfg.Env = b.Env
	}
	if !cfg.Active {
		return nil, cfg, nil
	}

	pages, err := b.loadBuiltPages()
	if err != nil {
		return nil, cfg, err
	}
	cfg.Routes = routeSet(pages)
	findings, err := check.Run(cfg, pages)
	if err != nil {
		return nil, cfg, err
	}
	return findings, cfg, nil
}

// loadBuiltPages walks the output directory collecting each page's final HTML
// and JS weight from disk.
func (b *Builder) loadBuiltPages() ([]check.Page, error) {
	manifest, err := readManifestPages(b.Cfg.OutDir)
	if err != nil {
		return nil, err
	}
	runtimeBytes := fileSize(filepath.Join(b.Cfg.OutDir, manifest.runtimeJS()))

	var pages []check.Page
	err = filepath.WalkDir(b.Cfg.OutDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			// Skip asset dirs that never contain pages.
			switch d.Name() {
			case "chunks", "_krate", "node_modules", ".krate":
				return filepath.SkipDir
			}
			return nil
		}
		if d.Name() != "index.html" {
			return nil
		}
		rel, err := filepath.Rel(b.Cfg.OutDir, filepath.Dir(path))
		if err != nil {
			return nil
		}
		route := routeFromRel(rel)

		data, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		jsBytes := runtimeBytes + pageJSBytes(filepath.Dir(path))
		pages = append(pages, check.Page{
			Route:     route,
			RelSource: manifest.sourceFor(route),
			HTML:      string(data),
			JSBytes:   jsBytes,
		})
		return nil
	})
	if err != nil {
		return nil, err
	}
	return pages, nil
}

// routeFromRel converts a dist-relative directory to a URL route.
func routeFromRel(rel string) string {
	rel = filepath.ToSlash(rel)
	if rel == "." || rel == "" {
		return "/"
	}
	return "/" + strings.TrimPrefix(rel, "/")
}

// pageJSBytes sums the size of every .js file colocated with a page's
// index.html (the hashed hydration bundle).
func pageJSBytes(dir string) int {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0
	}
	total := 0
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.HasSuffix(name, ".js") || strings.HasSuffix(name, ".map") {
			continue
		}
		total += fileSize(filepath.Join(dir, name))
	}
	return total
}

func fileSize(path string) int {
	if path == "" {
		return 0
	}
	info, err := os.Stat(path)
	if err != nil {
		return 0
	}
	return int(info.Size())
}

// manifestInfo is the subset of dist/manifest.json needed to map routes to
// source files and locate the shared runtime chunk. Page mode arrives as a
// number in manifest.json (RenderMode) but as a string in server-manifest.json,
// so it is decoded leniently.
type manifestInfo struct {
	Pages            []manifestPageInfo `json:"pages"`
	RuntimeJS        string             `json:"runtimeJS"`
	StaticOnlyRoutes []string           `json:"staticOnlyRoutes"`
}

type manifestPageInfo struct {
	Route  string          `json:"route"`
	Source string          `json:"source"`
	Mode   json.RawMessage `json:"mode"`
}

// modeString renders a page's stored render mode as its label.
func (p manifestPageInfo) modeString() string {
	if len(p.Mode) == 0 {
		return ""
	}
	var s string
	if err := json.Unmarshal(p.Mode, &s); err == nil {
		return s
	}
	var n int
	if err := json.Unmarshal(p.Mode, &n); err == nil {
		return RenderMode(n).String()
	}
	return ""
}

func (m manifestInfo) runtimeJS() string { return m.RuntimeJS }

func (m manifestInfo) sourceFor(route string) string {
	for _, p := range m.Pages {
		if p.Route == route {
			return p.Source
		}
	}
	return ""
}

// readManifestPages loads dist/manifest.json. A missing manifest is not an
// error (e.g. a plugin-only build); callers fall back to route-derived data.
func readManifestPages(outDir string) (manifestInfo, error) {
	var m manifestInfo
	data, err := os.ReadFile(filepath.Join(outDir, "manifest.json"))
	if err != nil {
		return m, nil
	}
	if err := json.Unmarshal(data, &m); err != nil {
		return m, err
	}
	return m, nil
}

// routeSet builds the normalized route inventory (with and without trailing
// slashes) used by the broken-link rule.
func routeSet(pages []check.Page) map[string]bool {
	set := map[string]bool{}
	for _, p := range pages {
		r := p.Route
		if r == "" {
			r = "/"
		}
		trimmed := strings.TrimSuffix(r, "/")
		set[r] = true
		set[trimmed] = true
		set[trimmed+"/"] = true
	}
	return set
}
