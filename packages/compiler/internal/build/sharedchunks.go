package build

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/evanw/esbuild/pkg/api"

	"github.com/kratejs/krate/packages/compiler/internal/irtree"
	"github.com/kratejs/krate/packages/compiler/internal/renderer"
)

// Shared component chunks.
//
// The renderer compiles the hydration scope of each client component imported
// by a page into a template (renderer.SharedScope) and leaves one call per
// instance in the page script. After every page is built, the templates are
// written once, into chunks under chunks/c/: per package (and one for the
// app's own modules) the templates two or more pages use, and per page the
// imported templates only that page uses. Chunks are classic scripts that
// register their templates; the page loads them after the runtime and before
// its own script, so a template is defined before the page calls it.

// sharedChunkDir is the output directory of the shared chunks, relative to
// outDir. The router keeps scripts from it loaded across navigations.
const sharedChunkDir = "chunks/c"

// shareOptions returns the shared-scope options for a page build, or nil when
// its scopes stay in the page script: dev builds (HMR rebuilds one page) and
// pages rendered at request time.
func (b *Builder) shareOptions(entry string, mode RenderMode, vendor []vendorImport, islands []irtree.ExternalComponent) *renderer.ShareOptions {
	if b.DevMode || mode != RenderSSG || !b.Cfg.SharedChunksOn() {
		return nil
	}
	reserved := map[string]bool{}
	for _, v := range vendor {
		reserved[v.Local] = true
	}
	for _, e := range islands {
		reserved[e.Local] = true
		if i := strings.IndexByte(e.Local, '.'); i >= 0 {
			reserved[e.Local[:i]] = true
		}
	}
	return &renderer.ShareOptions{PageFile: entry, All: b.shareAllScopes, Reserved: reserved}
}

// sharedScopesJS joins the templates of a page, for the passes that read the
// page's client code (runtime features, CSS class use, highlighting).
func sharedScopesJS(scopes []renderer.SharedScope) string {
	if len(scopes) == 0 {
		return ""
	}
	var b strings.Builder
	for _, sc := range scopes {
		b.WriteString(sc.JS)
		b.WriteString(";\n")
	}
	return b.String()
}

func joinJS(a, b string) string {
	if b == "" {
		return a
	}
	return a + "\n" + b
}

// withSharedRefs adds the global names the page's templates reference.
func withSharedRefs(refs []string, sharedJS string) []string {
	if sharedJS == "" {
		return refs
	}
	set := map[string]bool{}
	for _, r := range refs {
		set[r] = true
	}
	for name := range jsRefs(sharedJS) {
		set[name] = true
	}
	out := make([]string, 0, len(set))
	for name := range set {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// validSharedScopes reports whether every template parses. A template that
// does not is a compiler bug; the page then keeps its scopes inline.
func validSharedScopes(scopes []renderer.SharedScope) bool {
	for _, sc := range scopes {
		if v, ok := sharedScopeValid.Load(sc.ID); ok {
			if !v.(bool) {
				return false
			}
			continue
		}
		res := api.Transform("("+sc.JS+");", api.TransformOptions{})
		ok := len(res.Errors) == 0
		sharedScopeValid.Store(sc.ID, ok)
		if !ok {
			return false
		}
	}
	return true
}

var sharedScopeValid sync.Map // template id -> bool

// sharedChunk is one shared chunk being assembled.
type sharedChunk struct {
	key   string // "g:<group>", "s:<group>:<pages>" or "p:<outName>"
	slug  string
	ids   []string
	order int
	file  string // "" once merged into its page's script
	page  *PageResult
}

// writeSharedChunks writes the shared chunks for the built pages and sets
// each page's SharedJSFiles.
func (b *Builder) writeSharedChunks(results []*PageResult) error {
	type tpl struct {
		sc    renderer.SharedScope
		group string
		pages []string // out names of the pages that call it, in build order
	}
	groups := newPackageGroups(b.Root)
	tpls := map[string]*tpl{}
	groupPages := map[string]map[string]bool{}
	for _, r := range results {
		if r == nil {
			continue
		}
		r.SharedJSFiles = nil
		for _, sc := range r.SharedScopes {
			t := tpls[sc.ID]
			if t == nil {
				t = &tpl{sc: sc, group: groups.of(sc.Source)}
				tpls[sc.ID] = t
			} else if t.sc.JS != sc.JS {
				return fmt.Errorf("internal error: shared component scopes %q of %s and another page have the same id", sc.ID, r.OutName)
			}
			if n := len(t.pages); n == 0 || t.pages[n-1] != r.OutName {
				t.pages = append(t.pages, r.OutName)
			}
			if groupPages[t.group] == nil {
				groupPages[t.group] = map[string]bool{}
			}
			groupPages[t.group][r.OutName] = true
		}
	}
	if len(tpls) == 0 {
		return nil
	}

	// Packing: a template at least 80% of its package's pages call goes to
	// the package chunk; one a smaller set of pages calls goes to a chunk for
	// exactly that set; one only a page calls goes to that page's chunk. A
	// page so loads little code it does not run, from a few files.
	chunks := map[string]*sharedChunk{}
	chunkOf := map[string]*sharedChunk{}
	byOut := map[string]*PageResult{}
	for _, r := range results {
		if r != nil {
			byOut[r.OutName] = r
		}
	}
	ids := make([]string, 0, len(tpls))
	for id := range tpls {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		t := tpls[id]
		key, slug, order := "g:"+t.group, sharedChunkSlug(t.group), 0
		switch n := len(t.pages); {
		case n < 2:
			key, slug, order = "p:"+t.pages[0], "page", 2
		case n*5 < len(groupPages[t.group])*4:
			set := append([]string(nil), t.pages...)
			sort.Strings(set)
			key, order = "s:"+t.group+":"+strings.Join(set, "\x00"), 1
		}
		c := chunks[key]
		if c == nil {
			c = &sharedChunk{key: key, slug: slug, order: order}
			if order == 2 {
				c.page = byOut[t.pages[0]]
			}
			chunks[key] = c
		}
		c.ids = append(c.ids, id)
		chunkOf[id] = c
	}

	dir := filepath.Join(b.Cfg.OutDir, filepath.FromSlash(sharedChunkDir))
	keys := make([]string, 0, len(chunks))
	for k := range chunks {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		c := chunks[k]
		var src strings.Builder
		var marks []renderer.SrcMark
		src.WriteString("(function(r){\n")
		for _, id := range c.ids {
			sc := tpls[id].sc
			src.WriteString("r[")
			src.WriteString(strconv.Quote(id))
			src.WriteString("]=")
			src.WriteString(offsetMarks(sc.JS, len(marks)))
			src.WriteString(";\n")
			marks = append(marks, sc.Marks...)
		}
		src.WriteString("})(window.__krate_c||(window.__krate_c={}));\n")
		js := neutralizeClientEnv(src.String())
		inputMap, mapJSON := "", ""
		if len(marks) > 0 {
			js, inputMap = buildHydrationMap(js, marks, b.hydrationMapDir(dir))
		}
		if b.shouldMinifyJS() {
			if b.Cfg.Sourcemap && inputMap != "" {
				js, mapJSON = minifyJSWithInputMap(js, inputMap)
			} else if b.Cfg.Sourcemap {
				js, mapJSON = minifyJSWithMap(js, "krate-components.js")
			} else {
				js = minifyJS(js)
			}
		}
		js = strings.TrimSpace(js)
		if c.page != nil && b.mergeIntoPageScript(c.page, js) {
			continue
		}
		name := c.slug + "." + hashContent([]byte(js)) + ".js"
		c.file = sharedChunkDir + "/" + name
		if b.Cfg.Sourcemap {
			if mapJSON == "" {
				mapJSON = inputMap
			}
			if mapJSON == "" {
				mapJSON = generateSourcemap(js, "krate-components.js", js)
			}
			if err := writeOutput(filepath.Join(dir, name+".map"), []byte(mapJSON)); err != nil {
				return err
			}
			js = appendSourceMappingURL(js, name+".map")
		} else if b.devOnlyMaps() && inputMap != "" {
			if err := writeOutput(filepath.Join(dir, name+".map"), []byte(inputMap)); err != nil {
				return err
			}
			js = appendSourceMappingURL(js, name+".map")
		}
		if err := writeOutput(filepath.Join(dir, name), []byte(js)); err != nil {
			return err
		}
	}

	for _, r := range results {
		if r == nil || len(r.SharedScopes) == 0 {
			continue
		}
		seen := map[*sharedChunk]bool{}
		var used []*sharedChunk
		for _, sc := range r.SharedScopes {
			if c := chunkOf[sc.ID]; c != nil && !seen[c] {
				seen[c] = true
				used = append(used, c)
			}
		}
		sort.Slice(used, func(i, j int) bool {
			if used[i].order != used[j].order {
				return used[i].order < used[j].order
			}
			return used[i].key < used[j].key
		})
		for _, c := range used {
			if c.file != "" {
				r.SharedJSFiles = append(r.SharedJSFiles, c.file)
			}
		}
	}
	return nil
}

// mergeIntoPageScript puts the templates only page r calls (registration
// code js) at the start of its page script, which gets a new name. A page
// script whose name it embeds (import.meta.url) or that has a source map
// keeps them in a chunk of its own instead.
func (b *Builder) mergeIntoPageScript(r *PageResult, js string) bool {
	if b.hydrationMaps() || r.JSFile == "" || r.HydrationJS == "" || strings.Contains(r.HydrationJS, r.JSFile) {
		return false
	}
	pageDir := b.Cfg.OutDir
	if r.OutName != "." && r.OutName != "" {
		pageDir = filepath.Join(b.Cfg.OutDir, filepath.FromSlash(r.OutName))
	}
	merged := js + "\n" + r.HydrationJS
	name := "index." + hashContent([]byte(merged)) + ".js"
	if err := writeOutput(filepath.Join(pageDir, name), []byte(merged)); err != nil {
		return false
	}
	if name != r.JSFile {
		_ = os.Remove(filepath.Join(pageDir, r.JSFile))
	}
	r.JSFile, r.HydrationJS = name, merged
	return true
}

// offsetMarks shifts the source markers of a template by n, for a chunk
// that numbers the markers of all its templates in one list.
func offsetMarks(js string, n int) string {
	if n == 0 || !strings.Contains(js, renderer.SrcMarkPrefix) {
		return js
	}
	return sharedMarkRe.ReplaceAllStringFunc(js, func(m string) string {
		i, err := strconv.Atoi(m[len(renderer.SrcMarkPrefix) : len(m)-2])
		if err != nil {
			return m
		}
		return renderer.SrcMarkPrefix + strconv.Itoa(i+n) + "*/"
	})
}

var sharedMarkRe = regexp.MustCompile(`/\*@k:(\d+)\*/`)

// sharedChunkSlug is the file name stem of a package group's chunk.
func sharedChunkSlug(group string) string {
	var b strings.Builder
	dash := false
	for _, c := range strings.ToLower(group) {
		if c >= 'a' && c <= 'z' || c >= '0' && c <= '9' {
			b.WriteRune(c)
			dash = false
		} else if !dash && b.Len() > 0 {
			b.WriteByte('-')
			dash = true
		}
	}
	s := strings.TrimSuffix(b.String(), "-")
	if s == "" {
		return "app"
	}
	return s
}

// packageGroups names the package a module belongs to: the nearest
// package.json above it, or "app" for the project's own modules.
type packageGroups struct {
	root  string
	byDir map[string]string
}

func newPackageGroups(root string) *packageGroups {
	abs, err := filepath.Abs(root)
	if err == nil {
		root = abs
	}
	return &packageGroups{root: filepath.Clean(root), byDir: map[string]string{}}
}

func (g *packageGroups) of(file string) string {
	if file == "" {
		return "app"
	}
	return g.dir(filepath.Dir(filepath.Clean(file)))
}

func (g *packageGroups) dir(dir string) string {
	if v, ok := g.byDir[dir]; ok {
		return v
	}
	name := ""
	switch {
	case dir == g.root:
		name = "app"
	default:
		if data, err := os.ReadFile(filepath.Join(dir, "package.json")); err == nil {
			var pkg struct {
				Name string `json:"name"`
			}
			if json.Unmarshal(data, &pkg) == nil && pkg.Name != "" {
				name = pkg.Name
			} else {
				name = "app"
			}
		} else if parent := filepath.Dir(dir); parent != dir {
			name = g.dir(parent)
		} else {
			name = "app"
		}
	}
	g.byDir[dir] = name
	return name
}
