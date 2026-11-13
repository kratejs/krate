package migrate

import (
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/kratejs/krate/packages/compiler/internal/bundler"
	"github.com/kratejs/krate/packages/compiler/internal/lexer"
	"github.com/kratejs/krate/packages/compiler/internal/parser"
)

// middlewareCandidates lists the files Next reads as request interception, in
// priority order. Next 16 renamed middleware to proxy. Krate only knows
// middleware.{ts,js}, so a proxy file is written to the middleware name.
var middlewareCandidates = []string{
	"proxy.ts", "proxy.js", "src/proxy.ts", "src/proxy.js",
	"middleware.ts", "middleware.js", "src/middleware.ts", "src/middleware.js",
}

// planMiddleware plans the proxy/middleware file and reports any other
// candidate that exists, so none is dropped silently.
func planMiddleware(abs string, rep *Report) {
	var found []string
	for _, c := range middlewareCandidates {
		if fileExists(filepath.Join(abs, filepath.FromSlash(c))) {
			found = append(found, c)
		}
	}
	if len(found) == 0 {
		return
	}
	rep.Files = append(rep.Files, plannedMiddleware(abs, found[0], rep))
	for _, other := range found[1:] {
		rep.Manual = append(rep.Manual, ManualItem{
			Source: other,
			Reason: found[0] + " and " + other + " both exist; only " + found[0] + " was migrated",
			Hint:   "Next 16 uses proxy.ts and ignores middleware.ts in favor of it; merge any logic from " + other + " into " + found[0] + " by hand",
		})
	}
}

// middlewareTarget maps a proxy source to the middleware file name Krate loads.
func middlewareTarget(source string) string {
	source = filepath.ToSlash(source)
	base := path.Base(source)
	if strings.HasPrefix(base, "proxy.") {
		return path.Join(path.Dir(source), "middleware"+strings.TrimPrefix(base, "proxy"))
	}
	return source
}

var (
	proxyFnExport    = regexp.MustCompile(`(export\s+(?:async\s+)?function\s+)proxy\b`)
	proxyConstExport = regexp.MustCompile(`export\s+const\s+proxy\b`)
)

// unmigratedSourceExts are the extensions of script files listed by
// warnUnmigratedSources.
var unmigratedSourceExts = map[string]bool{".ts": true, ".tsx": true, ".js": true, ".jsx": true, ".mjs": true, ".cjs": true, ".mts": true, ".cts": true}

// warnUnmigratedSources lists script files directly under the project root
// and src/ that the plan neither copies nor generates, so a file that matters
// (a proxy, an auth module) is never dropped without a trace. Tooling files
// (*.config.*, *.d.ts, tests) are left out.
func warnUnmigratedSources(abs string, rep *Report) {
	planned := map[string]bool{}
	for _, f := range rep.Files {
		if f.Source != "" {
			planned[f.Source] = true
		}
	}
	var names []string
	for _, dir := range []string{"", "src"} {
		entries, err := os.ReadDir(filepath.Join(abs, dir))
		if err != nil {
			continue
		}
		for _, e := range entries {
			name := e.Name()
			if e.IsDir() || strings.HasPrefix(name, ".") || !unmigratedSourceExts[strings.ToLower(filepath.Ext(name))] {
				continue
			}
			rel := path.Join(dir, name)
			stem := strings.TrimSuffix(name, filepath.Ext(name))
			switch {
			case planned[rel],
				strings.Contains(name, ".config."), strings.HasSuffix(stem, ".d"),
				strings.Contains(name, ".test."), strings.Contains(name, ".spec."), strings.Contains(name, ".setup."),
				stem == "instrumentation" || strings.HasPrefix(stem, "instrumentation-"):
				continue
			}
			names = append(names, rel)
		}
	}
	if len(names) == 0 {
		return
	}
	sort.Strings(names)
	shown := names
	more := ""
	if len(shown) > 8 {
		shown, more = shown[:8], fmt.Sprintf(" (+%d more)", len(names)-8)
	}
	rep.Warnings = append(rep.Warnings, "script files at the project root or src/ were not migrated and stay where they are: "+strings.Join(shown, ", ")+more+"; check that none of them carries behavior the app needs (auth, request interception, startup code)")
}

// planInstrumentation lists Next instrumentation files as manual work: Krate
// has no server startup hook that calls register().
func planInstrumentation(abs string, rep *Report) {
	for _, dir := range []string{"", "src"} {
		entries, err := os.ReadDir(filepath.Join(abs, dir))
		if err != nil {
			continue
		}
		for _, e := range entries {
			name := e.Name()
			ext := filepath.Ext(name)
			stem := strings.TrimSuffix(name, ext)
			if e.IsDir() || (ext != ".ts" && ext != ".js") || (stem != "instrumentation" && !strings.HasPrefix(stem, "instrumentation-")) {
				continue
			}
			hint := "Krate has no instrumentation register() hook; run one-time setup (telemetry, Sentry init) at the top of an API route or middleware module, or from a plugin with serve hooks"
			if strings.HasPrefix(stem, "instrumentation-client") {
				hint = "import the client-side setup from a client component or layout script instead; Krate does not load instrumentation-client automatically"
			}
			rep.Manual = append(rep.Manual, ManualItem{Source: path.Join(dir, name), Reason: "Next.js instrumentation file is not migrated", Hint: hint})
		}
	}
}

var iconStem = regexp.MustCompile(`^(icon|apple-icon)\d*$`)

var iconMime = map[string]string{".ico": "image/x-icon", ".png": "image/png", ".svg": "image/svg+xml", ".jpg": "image/jpeg", ".jpeg": "image/jpeg"}

// planStaticIcon handles app-router icon files: favicon.ico, icon.* and
// apple-icon.* static images are copied to public/ and, at the app root,
// linked from the site head. It reports whether the file was one of these.
func planStaticIcon(root, prefix, rel string, rep *Report) bool {
	name := filepath.Base(rel)
	ext := strings.ToLower(filepath.Ext(name))
	stem := strings.TrimSuffix(name, filepath.Ext(name))
	mime, ok := iconMime[ext]
	isFavicon := name == "favicon.ico"
	if !ok || (!isFavicon && !iconStem.MatchString(stem)) {
		return false
	}
	if strings.HasPrefix(stem, "apple-icon") && ext != ".png" && ext != ".jpg" && ext != ".jpeg" {
		return false
	}
	dir := filepath.ToSlash(filepath.Dir(rel))
	urlRoute, _, _ := appRoute(dir)
	source := prefix + "/" + rel
	target := name
	if urlRoute != "." && urlRoute != "" {
		target = urlRoute + "/" + name
	}
	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(source)))
	if err != nil {
		rep.Warnings = append(rep.Warnings, source+": could not read source: "+err.Error())
		return true
	}
	rewrites := []string{"copied unchanged"}
	if !isFavicon {
		href := "/" + target
		rel := "icon"
		if strings.HasPrefix(stem, "apple-icon") {
			rel = "apple-touch-icon"
		}
		if urlRoute == "." || urlRoute == "" {
			rep.headLinks = append(rep.headLinks, `<link rel="`+rel+`" href="`+href+`" type="`+mime+`">`)
			rewrites = append(rewrites, "added a <"+rel+"> link to the site head config")
		} else {
			rep.Warnings = append(rep.Warnings, source+": copied to public/"+target+", but only app-root icons are linked; add a <link rel=\""+rel+"\" href=\""+href+"\"> to that segment's layout or page")
		}
	}
	rep.Files = append(rep.Files, PlannedFile{Kind: KindPublic, Source: source, Target: "public/" + target, Content: data, Rewrites: rewrites})
	return true
}

// routeRewrite builds the rewrite that keeps a route handler's Next.js public
// URL (/r/[code]) answering after it moves under /api. It returns false for
// the app root, which would shadow the home page.
func routeRewrite(urlRoute, apiPath string) (nextRewrite, bool) {
	if urlRoute == "" || urlRoute == "." {
		return nextRewrite{}, false
	}
	return nextRewrite{
		Source:      "/" + strings.Join(rewriteSegments(urlRoute), "/"),
		Destination: "/api/" + strings.Join(rewriteSegments(apiPath), "/"),
	}, true
}

// rewriteSegments converts Next dynamic segments ([id], [...rest],
// [[...rest]]) to the :param syntax Krate rewrites use.
func rewriteSegments(p string) []string {
	var out []string
	for _, seg := range strings.Split(p, "/") {
		switch {
		case strings.HasPrefix(seg, "[[...") && strings.HasSuffix(seg, "]]"):
			out = append(out, ":"+seg[5:len(seg)-2]+"*")
		case strings.HasPrefix(seg, "[...") && strings.HasSuffix(seg, "]"):
			out = append(out, ":"+seg[4:len(seg)-1]+"+")
		case strings.HasPrefix(seg, "[") && strings.HasSuffix(seg, "]"):
			out = append(out, ":"+seg[1:len(seg)-1])
		default:
			out = append(out, seg)
		}
	}
	return out
}

// addAPIRewrites folds the rewrites collected for out-of-api route handlers
// into the translated Next config, skipping any the config already has.
func addAPIRewrites(next *nextConfigData, rep *Report) {
	for _, rw := range rep.apiRewrites {
		dup := false
		for _, have := range next.Rewrites {
			if have.Source == rw.Source {
				dup = true
				break
			}
		}
		if !dup {
			next.Rewrites = append(next.Rewrites, rw)
		}
	}
}

// flagInlineUseServer adds a manual item for every copied "use client" file
// that defines a server action inline (a "use server" directive inside a
// function body). Krate hoists inline actions of server components itself;
// in a client module they are invalid in Next.js too, and the build rejects
// them with KR7001.
func flagInlineUseServer(rep *Report) {
	for _, f := range rep.Files {
		switch f.Kind {
		case KindPage, KindLayout, KindLoading, KindAsset, KindModule, KindAPI, KindNotFound, KindForbidden, KindUnauthorized, KindError, KindMetadataRoute:
		default:
			continue
		}
		src := string(f.Content)
		if !strings.Contains(src, "use server") {
			continue
		}
		p := parser.New(lexer.New(src).Tokenize())
		prog := p.ParseProgram()
		if prog == nil || len(p.Errors()) > 0 {
			continue
		}
		pos, ok := bundler.FindInlineUseServer(prog)
		if !ok || !bundler.HasUseClientDirective(src) {
			continue
		}
		source := f.Source
		if source == "" {
			source = f.Target
		}
		rep.Manual = append(rep.Manual, ManualItem{
			Source: source,
			Reason: fmt.Sprintf(`inline "use server" function (line %d) in a "use client" module fails the build with KR7001`, pos.Line),
			Hint:   `move the function into a server component, or into a module whose first statement is "use server", export it, and import it where it is used`,
		})
	}
}

func isTestSource(source string) bool {
	base := path.Base(filepath.ToSlash(source))
	return strings.Contains(base, ".test.") || strings.Contains(base, ".spec.") || strings.Contains(filepath.ToSlash(source), "/__tests__/")
}

// rewriteTestImports points the relative imports of copied test files at the
// migrated route files. Tests are copied as plain modules under src/migrated
// with their directory shape, so an import of "./page" would otherwise point
// at a file that now lives in src/pages.
func rewriteTestImports(rep *Report) {
	routes := map[string]string{}
	for _, f := range rep.Files {
		switch f.Kind {
		case KindPage, KindLayout, KindAPI, KindNotFound, KindError:
			if f.Source != "" {
				routes[f.Source] = f.Target
			}
		}
	}
	for i := range rep.Files {
		f := &rep.Files[i]
		if f.Kind != KindAsset || !isTestSource(f.Source) {
			continue
		}
		body, rewrites, _, deps := rewriteRelativeImports(string(f.Content), f.Source, f.Target, routes)
		if len(rewrites) == 0 {
			continue
		}
		f.Content = []byte(body)
		f.Rewrites = append(f.Rewrites, rewrites...)
		f.Dependencies = append(f.Dependencies, deps...)
	}
}

// planMoveAside records the original Next route trees that Apply moves into
// the backup directory once everything is written. A tree stays put when a
// planned target lies inside it, because moving it would remove migrated
// output. Files in a moved tree that the plan does not copy are warned about.
func planMoveAside(abs string, trees []string, rep *Report) {
	planned := map[string]bool{}
	for _, f := range rep.Files {
		if f.Source != "" {
			planned[f.Source] = true
		}
	}
	for _, tree := range trees {
		inside := false
		for _, f := range rep.Files {
			if strings.HasPrefix(f.Target, tree+"/") {
				inside = true
				break
			}
		}
		if inside {
			rep.Warnings = append(rep.Warnings, tree+"/ also holds migrated output, so --apply leaves it in place; delete the original Next files there by hand")
			continue
		}
		rep.MoveAside = append(rep.MoveAside, tree)
		var left []string
		_ = filepath.WalkDir(filepath.Join(abs, filepath.FromSlash(tree)), func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if d.IsDir() {
				if skipDir(d.Name()) {
					return filepath.SkipDir
				}
				return nil
			}
			rel, err := filepath.Rel(abs, p)
			if err == nil && !planned[filepath.ToSlash(rel)] {
				left = append(left, filepath.ToSlash(rel))
			}
			return nil
		})
		if len(left) > 0 {
			sort.Strings(left)
			shown, more := left, ""
			if len(shown) > 5 {
				shown, more = shown[:5], fmt.Sprintf(" (+%d more)", len(left)-5)
			}
			rep.Warnings = append(rep.Warnings, fmt.Sprintf("%d file(s) in %s/ are not migrated and move to the backup with the tree: %s%s", len(left), tree, strings.Join(shown, ", "), more))
		}
	}
}

// moveTree moves a directory, falling back to copy and remove when the
// backup lives on another filesystem.
func moveTree(src, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
		return err
	}
	if err := os.Rename(src, dst); err == nil {
		return nil
	}
	if err := copyTree(src, dst); err != nil {
		_ = os.RemoveAll(dst)
		return err
	}
	return os.RemoveAll(src)
}

func copyTree(src, dst string) error {
	return filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, p)
		if err != nil {
			return err
		}
		out := filepath.Join(dst, rel)
		info, err := d.Info()
		if err != nil {
			return err
		}
		if d.IsDir() {
			return os.MkdirAll(out, info.Mode().Perm()|0700)
		}
		if !info.Mode().IsRegular() {
			return nil
		}
		in, err := os.Open(p)
		if err != nil {
			return err
		}
		defer func() { _ = in.Close() }()
		w, err := os.OpenFile(out, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, info.Mode().Perm())
		if err != nil {
			return err
		}
		if _, err := io.Copy(w, in); err != nil {
			_ = w.Close()
			return err
		}
		return w.Close()
	})
}
