package build

import (
	"errors"
	"fmt"
	"io/fs"
	"net"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/fsnotify/fsnotify"

	"github.com/kratejs/krate/packages/compiler/internal/diag"
	"github.com/kratejs/krate/packages/compiler/internal/safe"
)

// ErrDevRestart is returned by Watch and the dev server when a config or env
// file changed and the caller should rebuild everything from the new settings.
var ErrDevRestart = errors.New("dev server restart requested")

// Watch watches the builder's project root for filesystem changes using native
// OS events (inotify, FSEvents, kqueue, ReadDirectoryChangesW via fsnotify).
// It takes the Builder that ran the initial build rather than constructing a
// fresh one: page builds populate the dependency graph on the builder (see
// recordDeps), and a brand-new Builder would start with an empty graph, so
// every save would miss partial tracking and fall through to a full rebuild.
// Each debounced batch of changes is fed through the dependency graph to
// rebuild only the affected pages/APIs, and the rebuilt routes are sent on
// reload. debounceDelay is the coalescing window: a single save (or an
// atomic-rename editor) can emit many events in a burst, so only one rebuild
// runs per burst. It returns an error only if the watcher fails to start, or
// ErrDevRestart when Builder.DevReload accepted a config/env change. It returns
// nil when the context from Builder.SetContext is cancelled.
func Watch(b *Builder, debounceDelay time.Duration, hub *DevHub) error {
	// DevMode is owned by the caller (`krate dev` sets it so output stays
	// readable); `krate build --watch` leaves it off and keeps minifying.
	root := b.Root
	cfg := b.Cfg

	// Safety net: if the builder has no dependency graph yet (no initial
	// BuildAll ran), do one now so partial rebuilds work instead of every
	// change taking the full-rebuild fallback in processChanges.
	b.depMu.Lock()
	needsInitialBuild := len(b.depGraph) == 0
	b.depMu.Unlock()
	if needsInitialBuild {
		if err := b.BuildAll(); err != nil {
			return fmt.Errorf("initial build: %w", err)
		}
	}

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

	publicDir := b.publicDir()

	// Register every non-ignored directory so new and renamed files in any
	// tracked location are observed without a full-tree rescan on each tick.
	dirs, err := watchedDirs(root, outDir)
	if err != nil {
		return fmt.Errorf("scanning directories to watch: %w", err)
	}
	watched := make(map[string]bool, len(dirs))
	for _, d := range dirs {
		if err := watcher.Add(d); err != nil {
			return fmt.Errorf("watching %s: %w", d, err)
		}
		watched[d] = true
	}

	// addTree watches dir and every subdirectory and returns the files found.
	// A directory created (or renamed in) after startup may already hold files
	// whose own create events fired before the watch existed, so they are
	// returned for the caller to queue.
	addTree := func(dir string) []string {
		var files []string
		_ = filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if d.IsDir() {
				if skippedDir(p) || isWithin(p, outDir) {
					return filepath.SkipDir
				}
				if !watched[p] {
					if watcher.Add(p) == nil {
						watched[p] = true
					}
				}
				return nil
			}
			files = append(files, p)
			return nil
		})
		return files
	}
	// dropTree stops watching dir and everything beneath it.
	dropTree := func(dir string) bool {
		found := false
		for d := range watched {
			if d == dir || isWithin(d, dir) {
				_ = watcher.Remove(d)
				delete(watched, d)
				found = true
			}
		}
		return found
	}

	// Dependencies outside the project root (a stylesheet shared from a
	// sibling workspace package) get their directory watched, non-recursively.
	watchExternalDeps := func() {
		for _, d := range b.externalDepDirs() {
			if !watched[d] && watcher.Add(d) == nil {
				watched[d] = true
			}
		}
	}
	watchExternalDeps()

	printf("%s  Watching %s for changes...%s\n", cBlue, root, cReset)

	debounce := debounceDelay
	if debounce <= 0 {
		debounce = 200 * time.Millisecond
	}

	var pending []string
	var publicOps []string
	var restartReasons []string
	var timer *time.Timer
	var timerC <-chan time.Time

	arm := func() {
		if timer == nil {
			timer = time.NewTimer(debounce)
			timerC = timer.C
			return
		}
		if !timer.Stop() {
			select {
			case <-timer.C:
			default:
			}
		}
		timer.Reset(debounce)
	}

	flush := func() error {
		if timer != nil {
			timer.Stop()
			timer = nil
		}
		timerC = nil
		if len(restartReasons) > 0 {
			reasons := uniqueStrings(restartReasons)
			restartReasons = nil
			if b.DevReload == nil {
				printf("  %s%s changed; restart the process to apply it.%s\n", cYellow, strings.Join(reasons, ", "), cReset)
			} else if b.DevReload(strings.Join(reasons, ", ")) {
				if hub != nil {
					hub.RequestRestart()
				}
				return ErrDevRestart
			}
		}
		publicChanged := false
		if len(publicOps) > 0 {
			publicChanged = b.syncPublic(publicDir, uniqueStrings(publicOps))
			publicOps = nil
		}
		if len(pending) == 0 {
			if publicChanged && hub != nil {
				hub.Publish(DevEvent{Type: "reload"})
			}
			return nil
		}
		changed := uniqueStrings(pending)
		pending = nil
		if b.DevHandleChanges != nil {
			b.DevHandleChanges(changed, hub)
		} else {
			b.processChanges(changed, hub)
		}
		watchExternalDeps()
		printf("%s  Watching %s for changes...%s\n", cBlue, root, cReset)
		return nil
	}

	var stop <-chan struct{}
	if b.ctx != nil {
		stop = b.ctx.Done()
	}
	for {
		select {
		case <-stop:
			return nil
		case ev, ok := <-watcher.Events:
			if !ok {
				return nil
			}
			if !managedPath(ev.Name, outDir) {
				continue
			}
			if reason := restartTrigger(root, ev.Name); reason != "" {
				restartReasons = append(restartReasons, reason)
				arm()
				continue
			}
			isDirEvent := false
			if ev.Op.Has(fsnotify.Create) && isDir(ev.Name) {
				isDirEvent = true
			}
			removed := ev.Op.Has(fsnotify.Remove) || ev.Op.Has(fsnotify.Rename)
			wasDir := false
			if removed {
				wasDir = dropTree(ev.Name)
			}

			// public/ is mirrored into the output directory rather than
			// compiled, and dotfiles there (.well-known) are real assets.
			if publicDir != "" && ev.Name != publicDir && isWithin(ev.Name, publicDir) {
				if isDirEvent {
					publicOps = append(publicOps, addTree(ev.Name)...)
				}
				publicOps = append(publicOps, ev.Name)
				arm()
				continue
			}

			// Skip krate's own internal artifacts: files whose basename starts
			// with '_' or '.' are private/convention-skipped everywhere else in
			// the compiler (private API files, _layout.*, runtime-component
			// privates). The SSR bundler writes transient _tmp_*.tsx sources
			// next to their originals so relative imports resolve; ignoring
			// them here prevents those rewrites from re-triggering the watcher
			// in an infinite loop.
			if (internalName(ev.Name) && !layoutOrLoadingFile(ev.Name)) || generatedDeclaration(ev.Name) {
				continue
			}
			// Keep the recursive watch in sync as directories are created,
			// removed, or renamed (editors and atomic renames produce these),
			// and queue the files already inside a new directory.
			if isDirEvent {
				for _, f := range addTree(ev.Name) {
					if (internalName(f) && !layoutOrLoadingFile(f)) || generatedDeclaration(f) {
						continue
					}
					if b.watchedFile(f) {
						pending = append(pending, f)
					}
				}
				arm()
				continue
			}
			if wasDir {
				// A removed or renamed directory takes its pages with it; the
				// directory path itself marks the batch as structural.
				pending = append(pending, ev.Name)
				arm()
				continue
			}
			if !b.watchedFile(ev.Name) {
				continue
			}
			pending = append(pending, ev.Name)
			arm()

		case err, ok := <-watcher.Errors:
			if !ok {
				return nil
			}
			if err != nil {
				fmt.Fprintf(os.Stderr, "  %sWatcher error:%s %v\n", cRed, cReset, err)
			}

		case <-timerC:
			if err := flush(); err != nil {
				return err
			}
		}
	}
}

// restartTrigger reports a human-readable reason when path is a project file
// whose change cannot be applied by an incremental rebuild: the krate and
// tailwind configs, tsconfig.json, and .env files at the project root.
func restartTrigger(root, p string) string {
	if filepath.Dir(p) != filepath.Clean(root) {
		return ""
	}
	base := filepath.Base(p)
	switch {
	case strings.HasPrefix(base, "krate.config."), strings.HasPrefix(base, "tailwind.config."), base == "tsconfig.json":
		return base
	case base == ".env" || strings.HasPrefix(base, ".env."):
		return base
	}
	return ""
}

// publicDir returns the absolute public directory, or "" when it is unset or
// would swallow the whole project.
func (b *Builder) publicDir() string {
	dir := b.Cfg.PublicDir
	if dir == "" {
		dir = "public"
	}
	if !filepath.IsAbs(dir) {
		dir = filepath.Join(b.Root, dir)
	}
	dir = filepath.Clean(dir)
	if dir == filepath.Clean(b.Root) || isWithin(b.Root, dir) {
		return ""
	}
	return dir
}

// syncPublic mirrors changed public/ entries into the output directory (copy
// when the source exists, delete when it is gone) and reports whether anything
// was applied.
func (b *Builder) syncPublic(publicDir string, paths []string) bool {
	if publicDir == "" {
		return false
	}
	out := b.Cfg.OutDir
	if !filepath.IsAbs(out) {
		out = filepath.Join(b.Root, out)
	}
	applied := false
	for _, p := range paths {
		rel, err := filepath.Rel(publicDir, p)
		if err != nil || rel == "." || strings.HasPrefix(rel, "..") {
			continue
		}
		dst := filepath.Join(out, rel)
		info, err := os.Stat(p)
		switch {
		case err != nil:
			if rmErr := os.RemoveAll(dst); rmErr == nil {
				applied = true
			}
		case info.IsDir():
			if mkErr := os.MkdirAll(dst, 0755); mkErr == nil {
				applied = true
			}
		default:
			data, rdErr := os.ReadFile(p)
			if rdErr != nil {
				continue
			}
			if mkErr := os.MkdirAll(filepath.Dir(dst), 0755); mkErr != nil {
				continue
			}
			if wrErr := os.WriteFile(dst, data, 0644); wrErr != nil {
				fmt.Fprintf(os.Stderr, "  %sWarning: copying public/%s:%s %v\n", cYellow, filepath.ToSlash(rel), cReset, wrErr)
				continue
			}
			applied = true
		}
		if applied {
			printf("  %s*%s public/%s synced\n", cCyan, cReset, filepath.ToSlash(rel))
		}
	}
	return applied
}

// isManagedGoFile reports whether a Go file is one krate compiles: an API
// route under a configured go.apiDirs entry (default src/api) or a Go
// component under a configured component dir.
func (b *Builder) isManagedGoFile(p string) bool {
	slash := filepath.ToSlash(p)
	eg := b.Cfg.EffectiveGo()
	dirs := append(append([]string{}, eg.APIDirList()...), eg.ComponentDirList()...)
	for _, d := range dirs {
		dd := strings.Trim(filepath.ToSlash(d), "/")
		if dd == "" {
			continue
		}
		if filepath.IsAbs(d) {
			if isWithin(p, filepath.Clean(d)) {
				return true
			}
			continue
		}
		if strings.Contains(slash, "/"+dd+"/") || strings.HasSuffix(slash, "/"+dd) {
			return true
		}
	}
	return false
}

// isStructuralChange reports whether the batch adds, removes or renames
// pages/directories, which a dependency-graph rebuild cannot express: removed
// files must lose their outputs and new files have no graph entry yet.
func (b *Builder) isStructuralChange(changed []string) bool {
	b.depMu.Lock()
	defer b.depMu.Unlock()
	for _, p := range changed {
		info, err := os.Stat(p)
		if err != nil || info.IsDir() {
			return true
		}
		if len(b.depGraph[p]) == 0 && len(b.pageDeps[p]) == 0 && isWithin(p, b.Cfg.PagesDir) {
			return true
		}
	}
	return false
}

// devRouteLabel names the route a changed page serves, for logs and reload
// events. Error pages are labelled by file ("/404") instead of the root.
func devRouteLabel(page, pagesDir string) string {
	name := strings.TrimSuffix(filepath.Base(page), filepath.Ext(page))
	if kind, ok := segmentKindOfBase(name); ok {
		if key := segmentURLDir(segmentDirKey(page, pagesDir)); key != "" {
			return "/" + key + "/" + strings.TrimSuffix(kind.htmlName(), ".html")
		}
		return "/" + strings.TrimSuffix(kind.htmlName(), ".html")
	}
	route := pageToOutput(page, pagesDir)
	if route == "." {
		return "/"
	}
	return "/" + route
}

// processChanges routes a batch of changed files through the dependency graph
// and rebuilds exactly what depends on them, falling back to a full rebuild when
// nothing matches dependency tracking. Rebuilt routes are sent to reload.
func (b *Builder) processChanges(changed []string, hub *DevHub) {
	// A panic during an incremental rebuild must not kill the dev server; report
	// it and keep watching.
	defer func() {
		if rec := recover(); rec != nil {
			fmt.Fprintf(os.Stderr, "  %sDev rebuild error:%s %v\n", cRed, cReset, safe.Recover(rec))
		}
	}()

	// Only krate-managed Go files (API routes + Go components) trigger
	// rebuilds; other .go files in the project are ignored.
	var filtered []string
	for _, p := range changed {
		if strings.HasSuffix(p, ".go") && !b.isManagedGoFile(p) {
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
	structural := b.isStructuralChange(changed)

	for _, p := range changed {
		if strings.HasSuffix(p, ".go") {
			goAPIChanged = true
			continue
		}
		if strings.Contains(filepath.ToSlash(p), "src/api/") {
			// Only compile files that are direct endpoints (ignore private
			// files starting with '_').
			if !strings.HasPrefix(filepath.Base(p), "_") && (strings.HasSuffix(p, ".ts") || strings.HasSuffix(p, ".js")) {
				apiToBuild = append(apiToBuild, p)
			}
		}
	}

	if !structural {
		affectedEntries := b.affectedPages(changed)
		for _, entry := range affectedEntries {
			if strings.Contains(filepath.ToSlash(entry), "src/api/") {
				apiToBuild = append(apiToBuild, entry)
			} else {
				pagesToBuild = append(pagesToBuild, entry)
			}
		}
	}

	apiToBuild = uniqueStrings(apiToBuild)
	pagesToBuild = uniqueStrings(pagesToBuild)

	var routes []string
	var diags []diag.Diagnostic
	addDiags := func(prefix string, err error) {
		for _, d := range DiagnosticsFromError(err) {
			if prefix != "" && d.Message != "" {
				d.Message = prefix + d.Message
			}
			diags = append(diags, d)
		}
	}

	if structural {
		printf("  %sFiles or directories were added, removed or renamed; rebuilding all...%s\n", cYellow, cReset)
		if err := b.BuildAll(); err != nil {
			fmt.Fprintf(os.Stderr, "  %sError: %v%s\n", cRed, err, cReset)
			addDiags("", err)
		}
		if err := b.BuildAllAPI(); err != nil {
			fmt.Fprintf(os.Stderr, "  %sError: %v%s\n", cRed, err, cReset)
			addDiags("", err)
		}
		if hub != nil {
			hub.Publish(DevEvent{Type: "reload", Diagnostics: diags})
		}
		return
	}

	cssOnly := len(changed) > 0
	for _, p := range changed {
		if strings.ToLower(filepath.Ext(p)) != ".css" {
			cssOnly = false
			break
		}
	}

	if len(pagesToBuild) > 0 {
		seen := map[string]bool{}
		for _, p := range pagesToBuild {
			route := devRouteLabel(p, b.Cfg.PagesDir)
			if !seen[route] {
				seen[route] = true
				routes = append(routes, route)
			}
		}
		printf("  %sAffected pages:%s %v\n", cBlue, cReset, routes)
		if _, err := b.BuildPages(pagesToBuild); err != nil {
			fmt.Fprintf(os.Stderr, "  %sUI Compilation Error: %v%s\n", cRed, err, cReset)
			addDiags("UI: ", err)
		}
	}

	if len(apiToBuild) > 0 {
		printf("  %sCompiling changed API endpoints...%s\n", cCyan, cReset)
		if err := b.CompileAPIRoutes(apiToBuild); err != nil {
			fmt.Fprintf(os.Stderr, "  %sAPI Compilation Error: %v%s\n", cRed, err, cReset)
			addDiags("API: ", err)
		}
	}

	if goAPIChanged {
		printf("  %sCompiling changed Go API routes...%s\n", cCyan, cReset)
		if err := b.BuildAllGoAPI(); err != nil {
			fmt.Fprintf(os.Stderr, "  %sGo API Compilation Error: %v%s\n", cRed, err, cReset)
			addDiags("Go API: ", err)
		}
	}

	if len(pagesToBuild) == 0 && len(apiToBuild) == 0 && !goAPIChanged {
		printf("  %sNo dependency tracking matches; rebuilding all...%s\n", cYellow, cReset)
		if err := b.BuildAll(); err != nil {
			fmt.Fprintf(os.Stderr, "  %sError: %v%s\n", cRed, err, cReset)
			addDiags("", err)
		}
		if err := b.BuildAllAPI(); err != nil {
			fmt.Fprintf(os.Stderr, "  %sError: %v%s\n", cRed, err, cReset)
			addDiags("", err)
		}
	}

	if hub != nil {
		hub.Publish(DevEvent{Type: "reload", Routes: routes, Diagnostics: diags, CSSOnly: cssOnly && len(diags) == 0})
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
// (dotfiles) as internal everywhere, so the watcher must not rebuild on them -
// the SSR bundler, for one, writes transient _tmp_*.tsx sources into the
// source tree during each rebuild.
func internalName(path string) bool {
	base := filepath.Base(path)
	return strings.HasPrefix(base, "_") || strings.HasPrefix(base, ".")
}

// layoutOrLoadingFile reports whether path is a `_layout.*` or `loading.*`
// file. These are the only leading-underscore source files a user edits that
// affect a built page; every other `_`-prefixed name is a private/internal
// artifact the watcher must ignore (notably the SSR bundler's transient
// `_tmp_*` sources). Without this exemption, editing _layout.tsx would never
// rebuild the pages it wraps.
func layoutOrLoadingFile(path string) bool {
	base := filepath.Base(path)
	if strings.HasPrefix(base, "loading.") {
		return trackedFile(path)
	}
	for _, name := range layoutNames {
		if base == name {
			return true
		}
	}
	for _, name := range templateNames {
		if base == name {
			return true
		}
	}
	return false
}

// generatedDeclaration reports whether path is a declaration file krate writes
// into the source tree during a full build (`krate-env.d.ts`,
// `krate-content.d.ts`, and any other *.d.ts it emits). A full build rewrites
// these beside user source, so unless they are ignored the watcher sees its own
// output and rebuilds forever. User-authored .d.ts files never need a rebuild
// on their own either: they are consumed through the imports of tracked source,
// which will trigger a rebuild when they change.
func generatedDeclaration(path string) bool {
	return strings.HasSuffix(strings.ToLower(filepath.Base(path)), ".d.ts")
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

// watchedFile reports whether a change to path should trigger a rebuild: a
// tracked source extension, or any file (such as a url() image or font) that a
// built page recorded as a dependency.
func (b *Builder) watchedFile(path string) bool {
	if trackedFile(path) {
		return true
	}
	b.depMu.Lock()
	defer b.depMu.Unlock()
	return len(b.depGraph[path]) > 0
}

// externalDepDirs lists the directories of known dependencies that live outside
// the project root and outside node_modules.
func (b *Builder) externalDepDirs() []string {
	root := filepath.Clean(b.Root)
	b.depMu.Lock()
	defer b.depMu.Unlock()
	seen := map[string]bool{}
	var dirs []string
	for f := range b.depGraph {
		if !filepath.IsAbs(f) || isWithin(f, root) || strings.Contains(filepath.ToSlash(f), "/node_modules/") {
			continue
		}
		if d := filepath.Dir(f); !seen[d] {
			seen[d] = true
			dirs = append(dirs, d)
		}
	}
	sort.Strings(dirs)
	return dirs
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

// bindServerListener binds the HTTP listener. A port that is already taken is
// an error with a hint for `krate serve`; in dev it moves to the next free
// port and says so, since a second dev server in another terminal is common.
func bindServerListener(host string, port int, dev bool) (net.Listener, error) {
	try := func(p int) (net.Listener, error) {
		addr := fmt.Sprintf(":%d", p)
		if host != "" {
			addr = net.JoinHostPort(host, strconv.Itoa(p))
		}
		return net.Listen("tcp", addr)
	}
	ln, err := try(port)
	if err == nil {
		return ln, nil
	}
	if !dev || port == 0 || !isAddrInUse(err) {
		if isAddrInUse(err) {
			return nil, fmt.Errorf("port %d is already in use (%w); stop the other process or choose another port with --port or server.port", port, err)
		}
		return nil, fmt.Errorf("listen on port %d: %w", port, err)
	}
	for next := port + 1; next <= port+50 && next <= 65535; next++ {
		if ln, nerr := try(next); nerr == nil {
			fmt.Fprintf(os.Stderr, "  %s! Port %d is in use, using %d instead%s\n", cYellow, port, next, cReset)
			return ln, nil
		}
	}
	return nil, fmt.Errorf("port %d is already in use and no free port was found in %d-%d; set devServer.port to a free port", port, port+1, port+50)
}

func isAddrInUse(err error) bool {
	return err != nil && strings.Contains(strings.ToLower(err.Error()), "address already in use") ||
		err != nil && strings.Contains(strings.ToLower(err.Error()), "only one usage of each socket address")
}
