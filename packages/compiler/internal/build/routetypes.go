package build

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/kratejs/krate/packages/compiler/internal/config"
	"github.com/kratejs/krate/packages/compiler/internal/fsutil"
	"github.com/kratejs/krate/packages/compiler/internal/gohost"
	"github.com/kratejs/krate/packages/compiler/internal/routetypes"
)

// routeTypesDir is the project-relative directory where generated type
// declarations live. It sits under .krate/ (already gitignored and excluded
// from source scanning).
const routeTypesDir = ".krate/types"

// GenerateTypes generates route and content type declarations without running
// a full build. It discovers page routes from disk (dynamic routes contribute
// their template patterns) and validates content collections. Used by
// `krate types`. Returns a multi-error of validation failures (nil when none).
func GenerateTypes(root string, cfg *config.Config) []error {
	b := New(root, cfg)

	pages, err := findPages(cfg.PagesDir)
	if err != nil {
		return []error{fmt.Errorf("finding pages: %w", err)}
	}
	results := make([]*PageResult, 0, len(pages))
	for _, p := range pages {
		results = append(results, &PageResult{
			Page:    p,
			OutName: pageToOutput(p, cfg.PagesDir),
		})
	}

	var errs []error
	if err := b.writeRouteTypes(results); err != nil {
		errs = append(errs, err)
	}
	cres := b.prepareContent()
	errs = append(errs, cres.Warnings...)
	errs = append(errs, cres.Validation...)
	// Go component declarations (used by `import { X } from "./x.go"`).
	if _, typeFiles, gerr := gohost.ScanComponents(cfg.Go.ComponentDirList(), root); gerr != nil {
		errs = append(errs, gerr)
	} else {
		gohost.WriteTypes(root, typeFiles)
	}
	if len(errs) == 0 {
		return nil
	}
	return errs
}

// writeRouteTypes generates `.krate/types/routes.d.ts` and the project-level
// `krate-env.d.ts` bridge from the built route results. This gives `<Link
// href>`, route params, and navigation code end-to-end type safety.
//
// It runs after all pages are built so dynamic routes resolved via
// generateStaticParams and plugin-generated pages are all represented. Failures
// are warnings (never a build error) so type emission can never break a build.
func (b *Builder) writeRouteTypes(results []*PageResult) error {
	routes := make([]routetypes.Route, 0, len(results))
	for _, r := range results {
		if r == nil || r.IsErrorPage {
			continue
		}
		src := r.SourcePath
		if src == "" {
			src = r.Page
		}
		// 404/500 pages are served at the root as error handlers, not as
		// navigable routes.
		base := strings.TrimSuffix(filepath.Base(src), filepath.Ext(src))
		if base == "404" || base == "500" {
			continue
		}
		// SourcePath is already project-relative; only relativize if absolute.
		if filepath.IsAbs(src) {
			if rel, err := filepath.Rel(b.Root, src); err == nil {
				src = rel
			}
		}
		src = filepath.ToSlash(src)
		routes = append(routes, routetypes.Route{
			Pattern: routeFromOutName(r.OutName),
			Source:  src,
			Mode:    r.Mode.String(),
		})
	}

	typesDir := filepath.Join(b.Root, filepath.FromSlash(routeTypesDir))
	if err := os.MkdirAll(typesDir, 0755); err != nil {
		return fmt.Errorf("creating types dir: %w", err)
	}

	routesDTS := filepath.Join(typesDir, "routes.d.ts")
	if err := fsutil.WriteFileIfChanged(routesDTS, []byte(routetypes.Generate(routes)+"\n"), 0644); err != nil {
		return fmt.Errorf("writing routes.d.ts: %w", err)
	}

	// The bridge is placed inside the project's source directory so the default
	// `include: ["src"]` picks it up with no tsconfig changes.
	bridgePath := b.bridgePath()
	routesSpec := relImportSpec(bridgePath, routesDTS)
	contentSpec := ""
	if cc := b.contentConfig(); cc != nil && len(cc.Collections) > 0 {
		contentSpec = relImportSpec(bridgePath, filepath.Join(typesDir, "content.d.ts"))
	}
	bridge := routetypes.Bridge(routesSpec, contentSpec)
	bridge += b.contentModuleDTS
	if err := fsutil.WriteFileIfChanged(bridgePath, []byte(bridge), 0644); err != nil {
		return fmt.Errorf("writing %s: %w", bridgePath, err)
	}

	return nil
}

// bridgePath returns where the generated `krate-env.d.ts` should live: inside
// the source directory (so the default tsconfig include catches it), or at the
// project root when there is no source directory.
func (b *Builder) bridgePath() string {
	src := filepath.Join(b.Root, "src")
	if fi, err := os.Stat(src); err == nil && fi.IsDir() {
		return filepath.Join(src, "krate-env.d.ts")
	}
	return filepath.Join(b.Root, "krate-env.d.ts")
}

// relImportSpec renders an import specifier from `fromFile`'s directory to
// `toFile`, using a `.js` extension (resolved to the `.d.ts` by TypeScript).
func relImportSpec(fromFile, toFile string) string {
	rel, err := filepath.Rel(filepath.Dir(fromFile), toFile)
	if err != nil {
		rel = toFile
	}
	rel = filepath.ToSlash(rel)
	rel = strings.TrimSuffix(rel, ".d.ts") + ".js"
	if !strings.HasPrefix(rel, ".") {
		rel = "./" + rel
	}
	return rel
}
