package build

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"krate-compiler/internal/config"
	"krate-compiler/internal/fsutil"
)

func Watch(root string, cfg *config.Config, pollInterval time.Duration, reload chan<- []string) error {
	b := New(root, cfg)
	b.DevMode = reload != nil

	// Initial build to populate dep graph
	prev := make(map[string]time.Time)
	updateFileMap(prev, root, cfg)

	fmt.Printf("%s  Watching %s for changes...%s\n", cBlue, root, cReset)

	for {
		time.Sleep(pollInterval)

		curr := make(map[string]time.Time)
		updateFileMap(curr, root, cfg)

		changed := findChanges(prev, curr)
		if len(changed) > 0 {
			for _, p := range changed {
				fmt.Printf("  %sChange detected:%s %s\n", cYellow, cReset, p)
			}

			affectedEntries := b.affectedPages(changed)

			var pagesToBuild []string
			var apiToBuild []string
			var goAPIChanged bool

			// Go files outside src/api/ are not krate-managed; ignore them so
			// they don't trigger a full rebuild.
			var filtered []string
			for _, p := range changed {
				if strings.HasSuffix(p, ".go") && !strings.Contains(filepath.ToSlash(p), "src/api/") {
					continue
				}
				filtered = append(filtered, p)
			}
			changed = filtered

			for _, p := range changed {
				if strings.Contains(filepath.ToSlash(p), "src/api/") {
					if strings.HasSuffix(p, ".go") {
						goAPIChanged = true
						continue
					}
					// Only compile files that are direct endpoints (ignore private files starting with '_')
					if !strings.HasPrefix(filepath.Base(p), "_") && (strings.HasSuffix(p, ".ts") || strings.HasSuffix(p, ".js")) {
						apiToBuild = append(apiToBuild, p)
					}
				}
			}

			for _, entry := range affectedEntries {
				if strings.Contains(filepath.ToSlash(entry), "src/api/") {
					apiToBuild = append(apiToBuild, entry)
				} else {
					pagesToBuild = append(pagesToBuild, entry)
				}
			}

			apiToBuild = uniqueStrings(apiToBuild)
			pagesToBuild = uniqueStrings(pagesToBuild)

			var routes []string

			if len(pagesToBuild) > 0 {
				for _, p := range pagesToBuild {
					route := pageToOutput(p, cfg.PagesDir)
					if route == "." {
						route = "/"
					} else {
						route = "/" + route
					}
					routes = append(routes, route)
				}
				fmt.Printf("  %sAffected pages:%s %v\n", cBlue, cReset, routes)
				if err := b.BuildPages(pagesToBuild); err != nil {
					fmt.Fprintf(os.Stderr, "  %sUI Compilation Error: %v%s\n", cRed, err, cReset)
				}
			}

			if len(apiToBuild) > 0 {
				fmt.Printf("  %sCompiling changed API endpoints...%s\n", cCyan, cReset)
				if err := b.CompileAPIRoutes(apiToBuild); err != nil {
					fmt.Fprintf(os.Stderr, "  %sAPI Compilation Error: %v%s\n", cRed, err, cReset)
				}
			}

			if goAPIChanged {
				fmt.Printf("  %sCompiling changed Go API routes...%s\n", cCyan, cReset)
				if err := b.BuildAllGoAPI(); err != nil {
					fmt.Fprintf(os.Stderr, "  %sGo API Compilation Error: %v%s\n", cRed, err, cReset)
				}
			}

			if len(pagesToBuild) == 0 && len(apiToBuild) == 0 && !goAPIChanged {
				fmt.Printf("  %sNo dependency tracking matches; rebuilding all...%s\n", cYellow, cReset)
				if err := b.BuildAll(); err != nil {
					fmt.Fprintf(os.Stderr, "  %sError: %v%s\n", cRed, err, cReset)
				}
				if err := b.BuildAllAPI(); err != nil {
					fmt.Fprintf(os.Stderr, "  %sError: %v%s\n", cRed, err, cReset)
				}
			}

			if reload != nil {
				select {
				case reload <- routes:
				default:
				}
			}
			fmt.Printf("%s  Watching %s for changes...%s\n", cBlue, root, cReset)
		}

		prev = curr
	}
}

func uniqueStrings(slice []string) []string {
	keys := make(map[string]bool)
	var list []string
	for _, entry := range slice {
		if _, value := keys[entry]; !value {
			keys[entry] = true
			list = append(list, entry)
		}
	}
	return list
}

func updateFileMap(m map[string]time.Time, root string, cfg *config.Config) {
	extensions := map[string]bool{
		".tsx": true, ".ts": true, ".jsx": true, ".js": true,
		".md": true, ".mdx": true, ".css": true, ".json": true, ".go": true,
	}
	skipDirs := map[string]bool{
		"node_modules": true,
		".git":         true,
		".krate":       true,
	}

	absOut, _ := filepath.Abs(cfg.OutDir)

	fsutil.WalkExt(root, extensions, skipDirs, func(path string, info os.FileInfo) error {
		absPath, _ := filepath.Abs(path)
		if absPath != absOut {
			m[path] = info.ModTime()
		}
		return nil
	})
}

func findChanges(prev, curr map[string]time.Time) []string {
	var changed []string
	for p, t := range curr {
		if oldT, ok := prev[p]; !ok || !t.Equal(oldT) {
			changed = append(changed, p)
		}
	}
	for p := range prev {
		if _, ok := curr[p]; !ok {
			changed = append(changed, p)
		}
	}
	return changed
}
