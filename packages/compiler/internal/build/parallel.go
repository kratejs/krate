package build

import (
	"fmt"
	"hash/fnv"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/kratejs/krate/packages/compiler/ast"
	"github.com/kratejs/krate/packages/compiler/internal/diag"
	"github.com/kratejs/krate/packages/compiler/internal/fsutil"
	"github.com/kratejs/krate/packages/compiler/internal/irtree"
	"github.com/kratejs/krate/packages/compiler/internal/renderer"
)

// Parallel and intercepting routes.
//
// A layout at directory D may own slots: subdirectories D/@name. The layout
// receives each slot as a prop of the same name ({children, modal}). For a
// page under D, a slot renders the slot page whose path (relative to the slot
// directory) equals the page's path relative to D, else the slot's
// default.tsx. Slot pages whose path starts with an intercepting segment
// ((.)x, (..)x, (..)(..)x, (...)x) never render on a hard load: they are
// built as fragments (served under /_krate/i/<slot key>/<target path>) that
// the SPA router swaps into the slot on a soft navigation to the target URL.

var (
	interceptCache sync.Map
	slotCache      sync.Map
)

type slotCacheEntry struct {
	slots []routeSlot
	err   error
}

// cachedSlots memoizes findSlots for one build.
func cachedSlots(dir, pagesDir string) ([]routeSlot, error) {
	if v, ok := slotCache.Load(dir); ok {
		e := v.(slotCacheEntry)
		return e.slots, e.err
	}
	slots, err := findSlots(dir, pagesDir)
	slotCache.Store(dir, slotCacheEntry{slots: slots, err: err})
	return slots, err
}

// cachedInterceptForPage memoizes interceptForPage for one build.
func cachedInterceptForPage(page, pagesDir string) *interceptPageInfo {
	if v, ok := interceptCache.Load(page); ok {
		return v.(*interceptPageInfo)
	}
	info := interceptForPage(page, pagesDir)
	interceptCache.Store(page, info)
	return info
}

// isFragmentOut reports whether an OutName is an intercepting-route fragment
// (not a navigable route).
func isFragmentOut(outName string) bool {
	return strings.HasPrefix(outName, interceptPrefix+"/")
}

// isLayoutFile reports whether path is a _layout file.
func isLayoutFile(path string) bool {
	base := filepath.Base(path)
	for _, n := range layoutNames {
		if base == n {
			return true
		}
	}
	return false
}

// interceptPrefix is the output directory of intercepting-route fragments.
const interceptPrefix = "_krate/i"

type slotPage struct {
	File string
	// Route is the page's URL pattern relative to the layout directory
	// (groups stripped), "" for the layout's own route.
	Route     string
	Intercept bool
	// Target is the intercepted URL pattern without a leading slash.
	Target string
}

type routeSlot struct {
	Name    string
	Dir     string
	Key     string
	Default string
	Pages   []slotPage
}

var pageExts = map[string]bool{".tsx": true, ".ts": true, ".jsx": true, ".js": true, ".md": true, ".mdx": true}

// slotKey returns the stable id of a slot (also its marker and fragment
// directory name). It hashes the slot directory relative to pagesDir.
func slotKey(slotDir, pagesDir string) string {
	key := filepath.ToSlash(slotDir)
	if rel, err := filepath.Rel(pagesDir, slotDir); err == nil {
		key = filepath.ToSlash(rel)
	}
	h := fnv.New32a()
	_, _ = h.Write([]byte(key))
	return "_s" + strconv.FormatUint(uint64(h.Sum32()), 36) + "_" + strings.TrimPrefix(filepath.Base(slotDir), "@")
}

// isSlotDir reports whether a directory name is a parallel-route slot.
func isSlotDir(name string) bool {
	return len(name) > 1 && name[0] == '@'
}

// pathHasSlot reports whether a path (relative to pagesDir) is inside a slot.
func pathHasSlot(rel string) bool {
	for _, seg := range strings.Split(filepath.ToSlash(rel), "/") {
		if isSlotDir(seg) {
			return true
		}
	}
	return false
}

// interceptLevels parses an intercepting segment: "(.)photo" -> 0, "photo";
// "(..)photo" -> 1; "(..)(..)photo" -> 2; "(...)photo" -> -1 (root).
func interceptLevels(seg string) (int, string, bool) {
	if strings.HasPrefix(seg, "(...)") {
		return -1, seg[len("(...)"):], true
	}
	if strings.HasPrefix(seg, "(.)") {
		return 0, seg[len("(.)"):], true
	}
	n := 0
	rest := seg
	for strings.HasPrefix(rest, "(..)") {
		n++
		rest = rest[len("(..)"):]
	}
	if n > 0 {
		return n, rest, true
	}
	return 0, seg, false
}

// urlSegsOfDir returns the URL segments of a directory relative to pagesDir
// (route groups and slots removed).
func urlSegsOfDir(dir, pagesDir string) []string {
	rel, err := filepath.Rel(pagesDir, dir)
	if err != nil || rel == "." || strings.HasPrefix(rel, "..") {
		return nil
	}
	var out []string
	for _, seg := range strings.Split(filepath.ToSlash(rel), "/") {
		if isRouteGroup(seg) || isSlotDir(seg) {
			continue
		}
		out = append(out, seg)
	}
	return out
}

// findSlots returns the slots owned by the layout directory dir, sorted by
// name. Invalid slot structures are diagnostics.
func findSlots(dir, pagesDir string) ([]routeSlot, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, nil
	}
	var slots []routeSlot
	for _, e := range entries {
		if !e.IsDir() || !isSlotDir(e.Name()) {
			continue
		}
		slotDir := filepath.Join(dir, e.Name())
		s := routeSlot{Name: e.Name()[1:], Dir: slotDir, Key: slotKey(slotDir, pagesDir)}
		layoutURL := urlSegsOfDir(dir, pagesDir)
		var walkErr error
		_ = fsutil.WalkExt(slotDir, pageExts, nil, func(path string, _ os.FileInfo) error {
			base := filepath.Base(path)
			if strings.HasPrefix(base, "_") || isUseServerFile(path) {
				return nil
			}
			rel, _ := filepath.Rel(slotDir, path)
			rel = filepath.ToSlash(strings.TrimSuffix(rel, filepath.Ext(rel)))
			if rel == "default" {
				s.Default = path
				return nil
			}
			if isRouteConventionFile(base) {
				return nil
			}
			if pathHasSlot(rel) {
				walkErr = slotDiag(path, "nested parallel-route slots are not supported", "move the inner @slot next to its own layout outside the parent slot")
				return nil
			}
			sp, perr := classifySlotPage(path, rel, layoutURL)
			if perr != nil {
				walkErr = perr
				return nil
			}
			s.Pages = append(s.Pages, sp)
			return nil
		})
		if walkErr != nil {
			return nil, walkErr
		}
		sort.Slice(s.Pages, func(i, j int) bool { return s.Pages[i].File < s.Pages[j].File })
		slots = append(slots, s)
	}
	sort.Slice(slots, func(i, j int) bool { return slots[i].Name < slots[j].Name })
	return slots, nil
}

func classifySlotPage(file, rel string, layoutURL []string) (slotPage, error) {
	segs := strings.Split(rel, "/")
	if segs[len(segs)-1] == "index" {
		segs = segs[:len(segs)-1]
	}
	sp := slotPage{File: file}
	var routeSegs []string
	for i, seg := range segs {
		if isRouteGroup(seg) {
			continue
		}
		levels, name, ok := interceptLevels(seg)
		if !ok {
			routeSegs = append(routeSegs, seg)
			continue
		}
		if sp.Intercept {
			return sp, slotDiag(file, "a slot page may contain only one intercepting segment", "keep a single (.)/(..)/(...) segment in the path")
		}
		sp.Intercept = true
		base := append(append([]string{}, layoutURL...), routeSegs...)
		switch {
		case levels < 0:
			base = nil
		case levels > len(base):
			return sp, slotDiag(file, fmt.Sprintf("intercepting segment %q goes above the app root", seg), "use (...) to intercept from the root, or fewer (..) levels")
		default:
			base = base[:len(base)-levels]
		}
		rest := []string{}
		if name != "" {
			rest = append(rest, name)
		}
		for _, r := range segs[i+1:] {
			if !isRouteGroup(r) {
				rest = append(rest, r)
			}
		}
		sp.Target = strings.Join(append(base, rest...), "/")
		break
	}
	if !sp.Intercept {
		sp.Route = strings.Join(routeSegs, "/")
		if isDynamicPattern(sp.Route) {
			return sp, slotDiag(file, "dynamic segments in non-intercepting slot pages are not supported yet", "render the slot from a static path, or use an intercepting route ((.)x/[id]) for dynamic modals")
		}
	}
	return sp, nil
}

func slotDiag(file, msg, hint string) error {
	return diag.Diagnostic{File: file, Line: 1, Col: 1, Message: msg, Hint: hint}
}

// interceptPageInfo describes a page file that is an intercepting route.
type interceptPageInfo struct {
	Slot   routeSlot
	Page   slotPage
	OutDir string
}

// interceptForPage returns the intercepting-route info of a slot page file,
// or nil when page is not an intercepting slot page.
func interceptForPage(page, pagesDir string) *interceptPageInfo {
	rel, err := filepath.Rel(pagesDir, page)
	if err != nil || strings.HasPrefix(rel, "..") || !pathHasSlot(rel) {
		return nil
	}
	parts := strings.Split(filepath.ToSlash(rel), "/")
	for i, seg := range parts {
		if !isSlotDir(seg) {
			continue
		}
		slotDir := filepath.Join(pagesDir, filepath.FromSlash(strings.Join(parts[:i+1], "/")))
		layoutDir := filepath.Dir(slotDir)
		slots, err := cachedSlots(layoutDir, pagesDir)
		if err != nil {
			return nil
		}
		for _, s := range slots {
			if s.Dir != slotDir {
				continue
			}
			for _, sp := range s.Pages {
				if sp.File == page && sp.Intercept {
					out := interceptPrefix + "/" + s.Key
					if sp.Target != "" {
						out += "/" + sp.Target
					}
					return &interceptPageInfo{Slot: s, Page: sp, OutDir: out}
				}
			}
		}
		return nil
	}
	return nil
}

// findInterceptPages returns every intercepting slot page under pagesDir
// (built as fragments next to the regular pages).
func findInterceptPages(pagesDir string) []string {
	var out []string
	_ = fsutil.WalkExt(pagesDir, pageExts, nil, func(path string, _ os.FileInfo) error {
		if interceptForPage(path, pagesDir) != nil {
			out = append(out, path)
		}
		return nil
	})
	sort.Strings(out)
	return out
}

// validateRouteTree reports route-structure errors: two pages resolving to
// the same URL (route groups), slots without a layout, and intercepting
// routes outside a slot.
func validateRouteTree(pages []string, pagesDir string) error {
	byURL := map[string]string{}
	var errs []string
	for _, p := range pages {
		rel, err := filepath.Rel(pagesDir, p)
		if err != nil || strings.HasPrefix(rel, "..") {
			continue
		}
		relSlash := filepath.ToSlash(rel)
		for _, seg := range strings.Split(relSlash, "/") {
			if _, _, ok := interceptLevels(seg); ok && !pathHasSlot(relSlash) {
				return slotDiag(p, "intercepting routes must live inside a parallel-route slot (e.g. @modal/(.)photo/[id].tsx)", "move the page under a @slot directory next to the layout that renders the slot")
			}
		}
		base := strings.TrimSuffix(filepath.Base(p), filepath.Ext(p))
		if base == "404" || base == "500" {
			continue
		}
		if base == "default" {
			(&Builder{}).printRouteWarning(diag.Diagnostic{
				File:     p,
				Message:  "default.tsx outside a @slot directory is built as a regular page (" + routeFromOutName(pageToOutput(p, pagesDir)) + ")",
				Hint:     "default.tsx only has a meaning inside a parallel-route slot (@name/default.tsx); rename the page if /default is intended",
				Severity: diag.SeverityWarning,
			})
		}
		out := pageToOutput(p, pagesDir)
		if prev, ok := byURL[out]; ok {
			a, _ := filepath.Rel(pagesDir, prev)
			b, _ := filepath.Rel(pagesDir, p)
			errs = append(errs, fmt.Sprintf("%s and %s both resolve to %s", filepath.ToSlash(a), filepath.ToSlash(b), routeFromOutName(out)))
			continue
		}
		byURL[out] = p
	}
	if len(errs) > 0 {
		sort.Strings(errs)
		return diag.Diagnostic{
			File:    pagesDir,
			Message: "conflicting routes: " + strings.Join(errs, "; "),
			Hint:    "route groups (name) do not appear in URLs; rename or move one of the pages",
		}
	}
	// Every slot needs a layout in its parent directory to render it.
	var slotErr error
	_ = filepath.WalkDir(pagesDir, func(path string, d os.DirEntry, err error) error {
		if err != nil || !d.IsDir() || !isSlotDir(d.Name()) || slotErr != nil {
			return nil
		}
		if firstExisting(filepath.Dir(path), layoutNames) == "" {
			slotErr = slotDiag(path, "parallel-route slot "+d.Name()+" has no _layout in its parent directory", "add a _layout.tsx next to "+d.Name()+" that renders {"+d.Name()[1:]+"}")
		}
		return filepath.SkipDir
	})
	return slotErr
}

// slotChoice picks the content a slot renders for a page on a hard load:
// the matching non-intercepting slot page, else default.tsx.
func slotChoice(s routeSlot, pageOut string, layoutDir, pagesDir string) (file, src string, err error) {
	pageSegs := []string{}
	if pageOut != "." && pageOut != "" {
		pageSegs = strings.Split(pageOut, "/")
	}
	layoutURL := urlSegsOfDir(layoutDir, pagesDir)
	rel := ""
	if len(pageSegs) >= len(layoutURL) {
		rel = strings.Join(pageSegs[len(layoutURL):], "/")
	}
	for _, sp := range s.Pages {
		if !sp.Intercept && sp.Route == rel {
			return sp.File, "page", nil
		}
	}
	if s.Default != "" {
		return s.Default, "default", nil
	}
	return "", "", slotDiag(s.Dir, fmt.Sprintf("slot @%s has no page for %s and no default.tsx", s.Name, routeFromOutName(pageOut)), "add "+filepath.ToSlash(filepath.Join("@"+s.Name, "default.tsx"))+" (render null to show nothing)")
}

// slotPlaceholder is the element the slot-prop rewrite renders in place of a
// slot prop; applyLayoutStack replaces it with the slot's content.
func slotPlaceholder(name string) string {
	return `<krate-slot name="` + name + `"></krate-slot>`
}

// rewriteSlotProps replaces `{name}` / `{props.name}` JSX children of a
// layout with a <krate-slot name="name"/> placeholder for every slot name.
func rewriteSlotProps(prog *ast.Program, names map[string]bool) {
	for _, stmt := range prog.Body {
		rewriteSlotStmt(stmt, names)
	}
}

func rewriteSlotStmt(node ast.Node, names map[string]bool) {
	switch n := node.(type) {
	case *ast.ExportStmt:
		if n.Declaration != nil {
			rewriteSlotStmt(n.Declaration, names)
		}
	case *ast.FnDecl:
		for _, s := range n.Body {
			rewriteSlotStmt(s, names)
		}
	case *ast.ReturnStmt:
		n.Value = rewriteSlotExpr(n.Value, names)
	case *ast.ExprStmt:
		n.Expression = rewriteSlotExpr(n.Expression, names)
	case *ast.IfStmt:
		for _, s := range n.Consequent {
			rewriteSlotStmt(s, names)
		}
		for _, s := range n.Alternate {
			rewriteSlotStmt(s, names)
		}
	case *ast.BlockStmt:
		for _, s := range n.Body {
			rewriteSlotStmt(s, names)
		}
	case *ast.VarStmt:
		for _, d := range n.Decls {
			if d.Init != nil {
				d.Init = rewriteSlotExpr(d.Init, names)
			}
		}
	}
}

func slotPropName(e ast.Expr, names map[string]bool) string {
	switch x := e.(type) {
	case *ast.Identifier:
		if names[x.Name] {
			return x.Name
		}
	case *ast.MemberExpr:
		obj, ok := x.Object.(*ast.Identifier)
		prop, ok2 := x.Property.(*ast.Identifier)
		if ok && ok2 && obj.Name == "props" && !x.Computed && names[prop.Name] {
			return prop.Name
		}
	}
	return ""
}

func rewriteSlotChildren(children []ast.JSXChild, names map[string]bool) {
	for i, child := range children {
		switch c := child.(type) {
		case *ast.JSXElementChild:
			rewriteSlotExpr(c.Element, names)
		case *ast.JSXFragmentChild:
			rewriteSlotExpr(c.Fragment, names)
		case *ast.JSXExprContainer:
			if c.Expression == nil {
				continue
			}
			if name := slotPropName(c.Expression, names); name != "" {
				children[i] = &ast.JSXElementChild{Element: &ast.JSXElement{
					Opening: &ast.JSXOpening{
						Name:        "krate-slot",
						Attributes:  []*ast.JSXAttr{{Name: "name", Value: &ast.Literal{Kind: ast.StringLit, Value: name}}},
						SelfClosing: true,
					},
				}}
				continue
			}
			c.Expression = rewriteSlotExpr(c.Expression, names)
		}
	}
}

func rewriteSlotExpr(expr ast.Expr, names map[string]bool) ast.Expr {
	switch e := expr.(type) {
	case *ast.JSXElement:
		rewriteSlotChildren(e.Children, names)
	case *ast.JSXFragment:
		rewriteSlotChildren(e.Children, names)
	case *ast.ConditionalExpr:
		e.Consequent = rewriteSlotExpr(e.Consequent, names)
		e.Alternate = rewriteSlotExpr(e.Alternate, names)
	case *ast.BinaryExpr:
		e.Right = rewriteSlotExpr(e.Right, names)
	case *ast.ArrowFn:
		for _, s := range e.Body {
			rewriteSlotStmt(s, names)
		}
	}
	return expr
}

type slotRenderResult struct {
	info       []routeSlotInfo
	css        string
	deps       []string
	hasSignals bool
}

// renderSlots renders every slot of the layout at layoutPath for page into
// layoutRes: each <krate-slot> placeholder becomes the slot content
// bracketed by <!--ks:KEY--> markers, and the content's hydration joins the
// page under its own route scope.
func (b *Builder) renderSlots(page, layoutPath string, layoutRes *renderer.EmitResult) (slotRenderResult, error) {
	var res slotRenderResult
	layoutDir := filepath.Dir(layoutPath)
	slots, err := cachedSlots(layoutDir, b.Cfg.PagesDir)
	if err != nil || len(slots) == 0 {
		return res, err
	}
	pageOut := pageToOutput(page, b.Cfg.PagesDir)
	if info := cachedInterceptForPage(page, b.Cfg.PagesDir); info != nil {
		pageOut = info.Page.Target
	}
	var sigs []irtree.ComponentSignature
	for _, sl := range slots {
		placeholder := slotPlaceholder(sl.Name)
		if !strings.Contains(layoutRes.HTML, placeholder) {
			b.printRouteWarning(diag.Diagnostic{
				File:     layoutPath,
				Message:  fmt.Sprintf("layout does not render its @%s slot", sl.Name),
				Hint:     fmt.Sprintf("destructure %s from the layout props and render {%s}", sl.Name, sl.Name),
				Severity: diag.SeverityWarning,
			})
			continue
		}
		file, src, cerr := slotChoice(sl, pageOut, layoutDir, b.Cfg.PagesDir)
		if cerr != nil {
			return res, cerr
		}
		slotEmit, slotCSS, slotDeps, slotSignals, perr := b.executeLayoutPipeline(file, "", nil)
		if perr != nil {
			return res, perr
		}
		scope := b.layoutIDPrefix(file)
		marked := "<!--ks:" + sl.Key + "-->" + slotEmit.HTML + "<!--/ks:" + sl.Key + "-->"
		layoutRes.HTML = strings.ReplaceAll(layoutRes.HTML, placeholder, marked)
		layoutRes.HeadHTML += slotEmit.HeadHTML
		layoutRes.ScriptHTML += slotEmit.ScriptHTML
		layoutRes.StyleHTML += slotEmit.StyleHTML
		for _, sig := range slotEmit.Signatures {
			sig.RouteScope = scope
			sigs = append(sigs, sig)
		}
		layoutRes.ListComponents = append(layoutRes.ListComponents, slotEmit.ListComponents...)
		if slotEmit.HasLinks {
			layoutRes.HasLinks = true
		}
		res.css += slotCSS
		res.deps = append(res.deps, slotDeps...)
		res.hasSignals = res.hasSignals || slotSignals
		info := routeSlotInfo{Name: sl.Name, Key: sl.Key, Scope: scope, Src: src}
		for _, sp := range sl.Pages {
			if sp.Intercept {
				info.Intercepts = append(info.Intercepts, sp.Target)
				res.deps = append(res.deps, sp.File)
			}
		}
		res.info = append(res.info, info)
	}
	// Slot components hydrate under their own scopes (the caller tags only
	// unscoped signatures with the layout's). Copy: the slice may be shared
	// with the layout emit cache.
	layoutRes.Signatures = append(append([]irtree.ComponentSignature{}, layoutRes.Signatures...), sigs...)
	return res, nil
}
