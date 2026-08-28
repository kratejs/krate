package build

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/fsnotify/fsnotify"

	"krate-compiler/internal/config"
)

// Watch watches root for filesystem changes using native OS events (inotify,
// FSEvents, kqueue, ReadDirectoryChangesW via fsnotify).
// Each debounced batch of changes is fed through the dependency graph to
// rebuild only the affected pages/APIs, and the rebuilt routes are sent on
// reload. debounceDelay is the coalescing window: a single save (or an
// atomic-rename editor) can emit many events in a burst, so only one rebuild
// runs per burst. It returns an error only if the watcher fails to start.
func Watch(root string, cfg *config.Config, debounceDelay time.Duration, reload chan<- []string) error {
	b := New(root, cfg)
	b.DevMode = reload != nil

	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return fmt.Errorf("starting native file watcher: %w", err)
	}
	defer watcher.Close()

	// Resolve the output directory to an absolute path so the helpers below can
	// compare against it regardless of whether the caller already resolved the
	// config. Failing to prune the output directory would have every build
	// write re-trigger the watcher (infinite rebuild loop).
	outDir := cfg.OutDir
	if !filepath.IsAbs(outDir) {
		outDir = filepath.Join(root, outDir)
	}
	outDir = filepath.Clean(outDir)

	// Register every non-ignored directory so new and renamed files in any
	// tracked location are observed without a full-tree rescan on each tick.
	dirs, err := watchedDirs(root, outDir)
	if err != nil {
		return fmt.Errorf("scanning directories to watch: %w", err)
	}
	for _, d := range dirs {
		if err := watcher.Add(d); err != nil {
			return fmt.Errorf("watching %s: %w", d, err)
		}
	}

	fmt.Printf("%s  Watching %s for changes...%s\n", cBlue, root, cReset)

	debounce := debounceDelay
	if debounce <= 0 {
		debounce = 200 * time.Millisecond
	}

	var pending []string
	var timer *time.Timer
	var timerC <-chan time.Time

	flush := func() {
		if timer != nil {
			timer.Stop()
			timer = nil
		}
		timerC = nil
		if len(pending) == 0 {
			return
		}
		changed := uniqueStrings(pending)
		pending = nil
		b.processChanges(changed, cfg, reload)
		fmt.Printf("%s  Watching %s for changes...%s\n", cBlue, root, cReset)
	}

	for {
		select {
		case ev, ok := <-watcher.Events:
			if !ok {
				return nil
			}
			if !managedPath(ev.Name, outDir) {
				continue
			}
			// Skip krate's own internal artifacts: files whose basename starts
			// with '_' or '.' are private/convention-skipped everywhere else in
			// the compiler (private API files, _layout.*, runtime-component
			// privates). The SSR bundler writes transient _tmp_*.tsx sources
			// next to their originals so relative imports resolve; ignoring
			// them here prevents those rewrites from re-triggering the watcher
			// in an infinite loop.
			if internalName(ev.Name) {
				continue
			}
			// Keep the recursive watch in sync as directories are created,
			// removed, or renamed (editors and atomic renames produce these).
			if ev.Op.Has(fsnotify.Create) && isDir(ev.Name) {
				_ = watcher.Add(ev.Name)
			}
			if (ev.Op.Has(fsnotify.Remove) || ev.Op.Has(fsnotify.Rename)) && isDir(ev.Name) {
				_ = watcher.Remove(ev.Name)
			}
			if !trackedFile(ev.Name) {
				continue
			}
			pending = append(pending, ev.Name)
			if timer == nil {
				timer = time.NewTimer(debounce)
				timerC = timer.C
			} else {
				if !timer.Stop() {
					select {
					case <-timer.C:
					default:
					}
				}
				timer.Reset(debounce)
			}

		case err, ok := <-watcher.Errors:
			if !ok {
				return nil
			}
			if err != nil {
				fmt.Fprintf(os.Stderr, "  %sWatcher error:%s %v\n", cRed, cReset, err)
			}

		case <-timerC:
			flush()
		}
	}
}

// processChanges routes a batch of changed files through the dependency graph
// and rebuilds exactly what depends on them, falling back to a full rebuild when
// nothing matches dependency tracking. Rebuilt routes are sent to reload.
func (b *Builder) processChanges(changed []string, cfg *config.Config, reload chan<- []string) {
	// Go files outside src/api/ are not krate-managed; ignore them so they
	// don't trigger a full rebuild.
	var filtered []string
	for _, p := range changed {
		if strings.HasSuffix(p, ".go") && !strings.Contains(filepath.ToSlash(p), "src/api/") {
			continue
		}
		filtered = append(filtered, p)
	}
	changed = filtered

	if len(changed) == 0 {
		return
	}

	var apiToBuild []string
	var pagesToBuild []string
	var goAPIChanged bool

	for _, p := range changed {
		if strings.Contains(filepath.ToSlash(p), "src/api/") {
			if strings.HasSuffix(p, ".go") {
				goAPIChanged = true
				continue
			}
			// Only compile files that are direct endpoints (ignore private
			// files starting with '_').
			if !strings.HasPrefix(filepath.Base(p), "_") && (strings.HasSuffix(p, ".ts") || strings.HasSuffix(p, ".js")) {
				apiToBuild = append(apiToBuild, p)
			}
		}
	}

	affectedEntries := b.affectedPages(changed)
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

// watchedDirs returns every directory under root that should be watched,
// pruning the build output directory and other ignored directories.
func watchedDirs(root string, outDir string) ([]string, error) {
	var dirs []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			return nil
		}
		if skippedDir(path) {
			return filepath.SkipDir
		}
		if isWithin(path, outDir) {
			return filepath.SkipDir
		}
		dirs = append(dirs, path)
		return nil
	})
	return dirs, err
}

// managedPath reports whether path is a location krate watches, i.e. not inside
// the build output directory or another ignored directory.
func managedPath(path string, outDir string) bool {
	if isWithin(path, outDir) {
		return false
	}
	return !skippedDir(path)
}

// internalName reports whether base is a krate-private or dotfile name. The
// compiler treats leading '_' (private pages/APIs/runtime components) and '.'
// (dotfiles) as internal everywhere, so the watcher must not rebuild on them —
// the SSR bundler, for one, writes transient _tmp_*.tsx sources into the
// source tree during each rebuild.
func internalName(path string) bool {
	base := filepath.Base(path)
	return strings.HasPrefix(base, "_") || strings.HasPrefix(base, ".")
}

// skippedDir reports whether path lies within an ignored directory
// (node_modules, .git, .krate).
func skippedDir(path string) bool {
	for _, part := range strings.Split(filepath.ToSlash(path), "/") {
		switch part {
		case "node_modules", ".git", ".krate":
			return true
		}
	}
	return false
}

// trackedFile reports whether path has an extension krate rebuilds on.
func trackedFile(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".tsx", ".ts", ".jsx", ".js", ".md", ".mdx", ".css", ".json", ".go":
		return true
	}
	return false
}

func isDir(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return info.IsDir()
}

func isWithin(path, dir string) bool {
	rel, err := filepath.Rel(dir, path)
	if err != nil {
		return false
	}
	if rel == "." {
		return true
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}
