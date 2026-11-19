package build

import (
	"bytes"
	"fmt"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"

	"github.com/evanw/esbuild/pkg/api"
	"github.com/kratejs/krate/packages/compiler/internal/deploy"
	"github.com/kratejs/krate/packages/compiler/internal/jsscan"
)

// Server code asks where it lives: import.meta.url, import.meta.dirname,
// import.meta.filename and, in CommonJS, __dirname and __filename. A bundle
// has one output file, so left alone they all name that file, and a path
// built from them ("../../.env" from a workspace package) lands in the
// output tree instead of the source tree.
//
// withSourceMeta rewrites each reference into a call that names the module's
// own source file:
//
//	import.meta.url  ->  __krateMetaURL("packages/lib/src/env.ts")
//
// The path is relative to the source root: the outputFileTracingRoot when
// set, else the project. The call resolves it against the directory of the
// running output that corresponds to the source root, which the bundle
// finds from its own import.meta.url, so a build tree or a deploy output that
// moves keeps working. This is the model Next.js with Turbopack uses for
// import.meta.url; dirname, filename, __dirname and __filename are real
// per-module paths here.

const (
	metaBannerOpen  = "/*krate-meta*/"
	metaBannerClose = "/*krate-meta-end*/"
	// metaPlaceholder stands for the URL from an output file to the root
	// directory in the banner until the build ends.
	metaPlaceholder = "__KRATE_META_ROOT__"

	metaBanner = metaBannerOpen + ` import { fileURLToPath as __krateMetaPath } from 'node:url'; import { dirname as __krateMetaDirname } from 'node:path'; const __krateMetaRoot = new URL("` + metaPlaceholder + `", import.meta.url); const __krateMetaURL = (p) => new URL(p, __krateMetaRoot).href; const __krateMetaFile = (p) => __krateMetaPath(new URL(p, __krateMetaRoot)); const __krateMetaDir = (p) => __krateMetaDirname(__krateMetaFile(p)); ` + metaBannerClose

	// metaStubDecls replaces the banner of an already built module in a
	// bundle for a platform that has no file system layout to name (see
	// metaModeStub): the paths are relative to the working directory.
	metaStubDecls = `const __krateMetaCwd = () => (typeof process !== 'undefined' && typeof process.cwd === 'function' ? process.cwd() : '').replace(/\\/g, '/').replace(/\/$/, ''); const __krateMetaFile = (p) => __krateMetaCwd() + '/' + decodeURIComponent(p); const __krateMetaURL = (p) => 'file://' + (__krateMetaCwd().startsWith('/') ? '' : '/') + __krateMetaCwd() + '/' + p; const __krateMetaDir = (p) => __krateMetaFile(p).replace(/\/[^/]*$/, '');`
)

type metaMode int

const (
	// metaModeNode rewrites the references and adds the banner.
	metaModeNode metaMode = iota
	// metaModeStub leaves sources alone and only gives the modules that an
	// earlier build rewrote (the page and action bundles a deploy bundles
	// again) helpers that need no file system layout.
	metaModeStub
)

var metaBannerRe = regexp.MustCompile(`(?s)` + regexp.QuoteMeta(metaBannerOpen) + `.*?` + regexp.QuoteMeta(metaBannerClose))

// sourceMeta carries one build's source root mapping.
type sourceMeta struct {
	srcRoot string // canonical source root
	srcRaw  string // as given
	outRoot string // canonical directory of the output that srcRoot maps to
	mode    metaMode
	loaders map[string]api.Loader

	mu      sync.Mutex
	targets map[string]bool // absolute paths static references name; nil when not collecting
}

// newSourceMeta maps the source root srcRoot to the output directory outRoot
// ("" for srcRoot itself: the output stays inside the source tree).
func newSourceMeta(srcRoot, outRoot string) *sourceMeta {
	sm := &sourceMeta{srcRoot: canonicalLoose(srcRoot), srcRaw: filepath.Clean(srcRoot)}
	if outRoot == "" {
		outRoot = srcRoot
	}
	sm.outRoot = canonicalLoose(outRoot)
	return sm
}

// withSourceMeta makes the ES module server build that opts describes give
// its modules their source locations (see the comment above). Call it after
// the other plugin and banner wrappers: it wraps every plugin that has been
// added. The returned value collects the files that static references name
// (see collectTargets); in-place builds ignore it.
func withSourceMeta(opts *api.BuildOptions, srcRoot, outRoot string) *sourceMeta {
	return newSourceMeta(srcRoot, outRoot).apply(opts)
}

// withSourceMetaStub is withSourceMeta for a platform that cannot run the
// banner (Vercel and Netlify re-bundle the output without import.meta, Workers
// have no files).
func withSourceMetaStub(opts *api.BuildOptions, srcRoot string) *sourceMeta {
	sm := newSourceMeta(srcRoot, "")
	sm.mode = metaModeStub
	return sm.apply(opts)
}

func (sm *sourceMeta) apply(opts *api.BuildOptions) *sourceMeta {
	if sm.mode == metaModeNode && (opts.Format != api.FormatESModule || opts.Platform != api.PlatformNode) {
		return nil
	}
	if strings.Contains(opts.Banner["js"], metaBannerOpen) {
		return nil
	}
	sm.loaders = opts.Loader
	wrapped := make([]api.Plugin, 0, len(opts.Plugins)+1)
	for _, p := range opts.Plugins {
		wrapped = append(wrapped, sm.wrap(p))
	}
	opts.Plugins = append(wrapped, sm.plugin())
	if sm.mode == metaModeNode {
		next := map[string]string{}
		for k, v := range opts.Banner {
			next[k] = v
		}
		if prev := next["js"]; prev != "" {
			next["js"] = prev + "\n" + metaBanner
		} else {
			next["js"] = metaBanner
		}
		opts.Banner = next
	}
	return sm
}

// collectTargets makes the build remember the files that static references
// name, for targets.
func (sm *sourceMeta) collectTargets() {
	sm.mu.Lock()
	if sm.targets == nil {
		sm.targets = map[string]bool{}
	}
	sm.mu.Unlock()
}

// targetPaths returns the files and directories that static references named,
// sorted.
func (sm *sourceMeta) targetPaths() []string {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	out := make([]string, 0, len(sm.targets))
	for p := range sm.targets {
		out = append(out, p)
	}
	sort.Strings(out)
	return out
}

func (sm *sourceMeta) wrap(p api.Plugin) api.Plugin {
	setup := p.Setup
	p.Setup = func(pb api.PluginBuild) {
		onLoad := pb.OnLoad
		pb.OnLoad = func(o api.OnLoadOptions, cb func(api.OnLoadArgs) (api.OnLoadResult, error)) {
			onLoad(o, func(args api.OnLoadArgs) (api.OnLoadResult, error) {
				res, err := cb(args)
				if err == nil && args.Namespace == "file" {
					sm.applyLoad(args.Path, &res)
				}
				return res, err
			})
		}
		setup(pb)
	}
	return p
}

// applyLoad rewrites the contents another plugin returned for a file.
func (sm *sourceMeta) applyLoad(file string, res *api.OnLoadResult) {
	if !metaScriptFile(file) {
		return
	}
	switch res.Loader {
	case api.LoaderNone, api.LoaderJS, api.LoaderJSX, api.LoaderTS, api.LoaderTSX:
	default:
		return
	}
	var src string
	if res.Contents != nil {
		src = *res.Contents
	} else {
		if res.Loader == api.LoaderNone && res.ResolveDir == "" && res.PluginData == nil {
			return // not claimed
		}
		data, err := os.ReadFile(file)
		if err != nil {
			return
		}
		src = string(data)
	}
	if out, ok := sm.rewrite(file, src); ok {
		res.Contents = &out
		if res.ResolveDir == "" {
			res.ResolveDir = filepath.Dir(file)
		}
		if res.Loader == api.LoaderNone {
			res.Loader = sm.loaderFor(file)
		}
	}
}

func (sm *sourceMeta) loaderFor(file string) api.Loader {
	if l, ok := sm.loaders[filepath.Ext(file)]; ok {
		return l
	}
	return loaderForExt(file)
}

func metaScriptFile(file string) bool {
	switch strings.ToLower(filepath.Ext(file)) {
	case ".js", ".mjs", ".cjs", ".jsx", ".ts", ".mts", ".cts", ".tsx":
		return true
	}
	return false
}

// plugin loads the script files no other plugin claimed, rewritten, and
// patches the output when the build ends.
func (sm *sourceMeta) plugin() api.Plugin {
	return api.Plugin{
		Name: "krate-source-meta",
		Setup: func(pb api.PluginBuild) {
			pb.OnLoad(api.OnLoadOptions{Filter: `\.([cm]?[jt]sx?)$`, Namespace: "file"}, func(args api.OnLoadArgs) (api.OnLoadResult, error) {
				data, err := os.ReadFile(args.Path)
				if err != nil {
					return api.OnLoadResult{}, nil
				}
				if !metaMentioned(string(data), sm.mode) {
					return api.OnLoadResult{}, nil
				}
				out, ok := sm.rewrite(args.Path, string(data))
				if !ok {
					return api.OnLoadResult{}, nil
				}
				return api.OnLoadResult{Contents: &out, Loader: sm.loaderFor(args.Path), ResolveDir: filepath.Dir(args.Path)}, nil
			})
			if sm.mode != metaModeNode {
				return
			}
			write := pb.InitialOptions != nil && pb.InitialOptions.Write
			pb.OnEnd(func(res *api.BuildResult) (api.OnEndResult, error) {
				if len(res.Errors) > 0 {
					return api.OnEndResult{}, nil
				}
				return api.OnEndResult{}, sm.patchOutputs(res.OutputFiles, write)
			})
		},
	}
}

// patchOutputs gives each output file that has the banner the URL from its
// directory to the output root.
func (sm *sourceMeta) patchOutputs(files []api.OutputFile, write bool) error {
	for i := range files {
		f := &files[i]
		switch filepath.Ext(f.Path) {
		case ".js", ".mjs", ".cjs":
		default:
			continue
		}
		if !bytes.Contains(f.Contents, []byte(metaPlaceholder)) {
			continue
		}
		rel, err := sm.outputRootURL(f.Path)
		if err != nil {
			return err
		}
		f.Contents = bytes.ReplaceAll(f.Contents, []byte(metaPlaceholder), []byte(rel))
		if write {
			mode := os.FileMode(0o644)
			if info, err := os.Stat(f.Path); err == nil {
				mode = info.Mode().Perm()
			}
			if err := os.WriteFile(f.Path, f.Contents, mode); err != nil {
				return err
			}
		}
	}
	return nil
}

// outputRootURL is the relative URL, with a trailing slash, from the
// directory of the output file out to the output root.
func (sm *sourceMeta) outputRootURL(out string) (string, error) {
	dir := canonicalLoose(filepath.Dir(out))
	rel, err := filepath.Rel(dir, sm.outRoot)
	if err != nil {
		return "", fmt.Errorf("source locations: no relative path from %s to %s: %w", dir, sm.outRoot, err)
	}
	return metaURLPath(filepath.ToSlash(rel)) + "/", nil
}

// canonicalLoose is canonicalPath for a path that may not exist yet: the
// symlinks of its nearest existing ancestor are resolved.
func canonicalLoose(p string) string {
	p = filepath.Clean(p)
	if abs, err := filepath.Abs(p); err == nil {
		p = abs
	}
	var tail []string
	for cur := p; ; {
		if _, err := os.Lstat(cur); err == nil {
			out := canonicalPath(cur)
			for i := len(tail) - 1; i >= 0; i-- {
				out = filepath.Join(out, tail[i])
			}
			return out
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			return p
		}
		tail = append(tail, filepath.Base(cur))
		cur = parent
	}
}

// metaURLPath escapes each segment of a slash path for a URL; "." and ".."
// stay.
func metaURLPath(p string) string {
	segs := strings.Split(p, "/")
	for i, s := range segs {
		if s != "." && s != ".." {
			segs[i] = url.PathEscape(s)
		}
	}
	return strings.Join(segs, "/")
}

// relPath is file relative to the source root, escaped for a URL, and false
// when no relative path exists (another drive).
func (sm *sourceMeta) relPath(file string) (string, bool) {
	file = filepath.Clean(file)
	rel, err := filepath.Rel(sm.srcRoot, file)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		if alt, aerr := filepath.Rel(sm.srcRaw, file); aerr == nil && alt != ".." && !strings.HasPrefix(alt, ".."+string(filepath.Separator)) {
			return metaURLPath(filepath.ToSlash(alt)), true
		}
		if err != nil {
			return "", false
		}
	}
	return metaURLPath(filepath.ToSlash(rel)), true
}

// metaMentioned reports whether src can hold something rewrite acts on.
func metaMentioned(src string, mode metaMode) bool {
	if strings.Contains(src, metaBannerOpen) {
		return true
	}
	if mode != metaModeNode {
		return false
	}
	return strings.Contains(src, "import.meta") || strings.Contains(src, "__dirname") || strings.Contains(src, "__filename") || strings.Contains(src, "__krateMeta")
}

// rewrite returns the module at file with its source references rewritten,
// and whether anything changed. A module an earlier build rewrote (a page or
// action bundle) has its banner dropped, because the bundle this one makes
// declares the helpers once for every module.
func (sm *sourceMeta) rewrite(file, src string) (string, bool) {
	changed := false
	if loc := metaBannerRe.FindStringIndex(src); loc != nil {
		repl := ""
		if sm.mode == metaModeStub {
			repl = metaStubDecls
		}
		src = src[:loc[0]] + repl + src[loc[1]:]
		changed = true
	}
	if sm.mode != metaModeNode {
		return src, changed
	}
	if rel, ok := sm.relPath(file); ok && (strings.Contains(src, "import.meta") || strings.Contains(src, "__dirname") || strings.Contains(src, "__filename")) {
		if out, ok := rewriteSourceMeta(src, rel, strings.HasSuffix(strings.ToLower(file), "ts") || strings.HasSuffix(strings.ToLower(file), "tsx")); ok {
			src = out
			changed = true
		}
	}
	if sm.targets != nil && strings.Contains(src, "__krateMeta") {
		sm.recordTargets(src)
	}
	return src, changed
}

// rewriteSource is rewrite for source text that does not come through a
// plugin load (a page the build hands esbuild as its entry).
func (sm *sourceMeta) rewriteSource(file, src string) string {
	if sm == nil || sm.mode != metaModeNode || !metaScriptFile(file) {
		return src
	}
	if out, ok := sm.rewrite(file, src); ok {
		return out
	}
	return src
}

// rewriteSourceMeta rewrites the source references in the code of src. rel is
// the escaped path of the module relative to the source root. ts is true for
// TypeScript, where `typeof __dirname` can be a type.
func rewriteSourceMeta(src, rel string, ts bool) (string, bool) {
	relLit := jsString(rel)
	mask := jsscan.Mask(src)
	pairs := jsscan.Pairs(src, mask)

	type edit struct {
		start, end int
		text       string
	}
	type ref struct {
		name       string
		start, end int
		stack      []int
	}
	var edits []edit
	var refs []ref
	declared := map[string]bool{}
	typePos := map[int]bool{}
	var stack []int

	for i := 0; i < len(src); {
		if !mask[i] {
			i++
			continue
		}
		c := src[i]
		switch c {
		case '(', '[', '{':
			stack = append(stack, i)
			i++
			continue
		case ')', ']', '}':
			if len(stack) > 0 {
				stack = stack[:len(stack)-1]
			}
			i++
			continue
		}
		if !metaIdentStart(c) {
			i++
			continue
		}
		j := i
		for j < len(src) && mask[j] && jsscan.IsIdentChar(src[j]) {
			j++
		}
		word := src[i:j]
		start := i
		i = j
		if k := prevByte(src, mask, start); k >= 0 && (src[k] == '.' && !(k >= 2 && src[k-1] == '.' && src[k-2] == '.')) {
			continue // a member name
		}
		switch word {
		case "import":
			if e, member, ok := metaMember(src, mask, j); ok {
				fn := map[string]string{"url": "__krateMetaURL", "filename": "__krateMetaFile", "dirname": "__krateMetaDir"}[member]
				text := fn + "(" + relLit + ")" + strings.Repeat("\n", strings.Count(src[start:e], "\n"))
				edits = append(edits, edit{start, e, text})
				i = e
			}
		case "__dirname", "__filename":
			refs = append(refs, ref{word, start, j, append([]int(nil), stack...)})
		}
	}

	for _, r := range refs {
		pt, _ := tokenBefore(src, mask, r.start)
		nt, _ := tokenAfter(src, mask, r.end)
		switch {
		case pt == "const" || pt == "let" || pt == "var" || pt == "function" || pt == "class" || pt == "as" || pt == "import":
			declared[r.name] = true
		case nt == "=" || nt == "=>":
			declared[r.name] = true
		case pt == "typeof" && ts:
			// In a type position the name must stay: typeof __dirname.
			_, at := tokenBefore(src, mask, r.start)
			tt, tat := tokenBefore(src, mask, at)
			if tt == ":" || tt == "<" || tt == "|" || tt == "&" {
				typePos[r.start] = true
			} else if tt == "=" {
				_, nameAt := tokenBefore(src, mask, tat)
				if kw, _ := tokenBefore(src, mask, nameAt); kw == "type" {
					typePos[r.start] = true
				}
			}
		}
		for k := len(r.stack) - 1; k >= 0; k-- {
			o := r.stack[k]
			switch src[o] {
			case '(':
				if after, _ := tokenAfter(src, mask, pairs[o]+1); after == "=>" {
					declared[r.name] = true
				} else if after == "{" {
					if before, _ := tokenBefore(src, mask, o); before != "if" && before != "for" && before != "while" && before != "switch" && before != "with" {
						declared[r.name] = true
					}
				}
			case '{', '[':
				before, _ := tokenBefore(src, mask, o)
				if before == "const" || before == "let" || before == "var" || before == "export" || before == "import" {
					declared[r.name] = true
				}
			}
		}
	}

	for _, r := range refs {
		if declared[r.name] || typePos[r.start] {
			continue
		}
		pt, _ := tokenBefore(src, mask, r.start)
		nt, _ := tokenAfter(src, mask, r.end)
		call := "__krateMetaDir(" + relLit + ")"
		if r.name == "__filename" {
			call = "__krateMetaFile(" + relLit + ")"
		}
		if n := len(r.stack); n > 0 && src[r.stack[n-1]] == '{' && (pt == "{" || pt == ",") {
			if nt == ":" {
				continue // an object key
			}
			if nt == "," || nt == "}" {
				call = r.name + ": " + call
			}
		}
		edits = append(edits, edit{r.start, r.end, call})
	}
	if len(edits) == 0 {
		return src, false
	}
	sort.Slice(edits, func(a, b int) bool { return edits[a].start < edits[b].start })
	var b strings.Builder
	b.Grow(len(src) + len(edits)*(len(relLit)+24))
	last := 0
	for _, e := range edits {
		if e.start < last {
			continue
		}
		b.WriteString(src[last:e.start])
		b.WriteString(e.text)
		last = e.end
	}
	b.WriteString(src[last:])
	return b.String(), true
}

func metaIdentStart(c byte) bool {
	return c == '_' || c == '$' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z'
}

// prevByte returns the index of the last code byte before i that is not
// white space, or -1.
func prevByte(src string, mask []bool, i int) int {
	for i--; i >= 0; i-- {
		if !mask[i] || src[i] == ' ' || src[i] == '\t' || src[i] == '\n' || src[i] == '\r' {
			continue
		}
		return i
	}
	return -1
}

// tokenBefore returns the code token that ends before i: a word, or one
// punctuation byte.
func tokenBefore(src string, mask []bool, i int) (string, int) {
	k := prevByte(src, mask, i)
	if k < 0 {
		return "", -1
	}
	if !jsscan.IsIdentChar(src[k]) {
		return src[k : k+1], k
	}
	s := k
	for s > 0 && mask[s-1] && jsscan.IsIdentChar(src[s-1]) {
		s--
	}
	return src[s : k+1], s
}

// tokenAfter returns the code token that starts at or after i: a word, "=>",
// "==" style pairs as "==", or one punctuation byte.
func tokenAfter(src string, mask []bool, i int) (string, int) {
	for ; i < len(src); i++ {
		if !mask[i] || src[i] == ' ' || src[i] == '\t' || src[i] == '\n' || src[i] == '\r' {
			continue
		}
		break
	}
	if i >= len(src) {
		return "", -1
	}
	if jsscan.IsIdentChar(src[i]) {
		j := i
		for j < len(src) && mask[j] && jsscan.IsIdentChar(src[j]) {
			j++
		}
		return src[i:j], i
	}
	if src[i] == '=' && i+1 < len(src) && (src[i+1] == '>' || src[i+1] == '=') {
		return src[i : i+2], i
	}
	return src[i : i+1], i
}

// metaMember matches `.meta.<url|dirname|filename>` after the word import
// that ends at i, and returns the end of the member.
func metaMember(src string, mask []bool, i int) (end int, member string, ok bool) {
	next := func(want string) bool {
		t, at := tokenAfter(src, mask, i)
		if t != want || at < 0 {
			return false
		}
		i = at + len(t)
		return true
	}
	if !next(".") || !next("meta") || !next(".") {
		return 0, "", false
	}
	t, at := tokenAfter(src, mask, i)
	if at < 0 || (t != "url" && t != "dirname" && t != "filename") {
		return 0, "", false
	}
	return at + len(t), t, true
}

// Static references: files that a bundle reads through its source location
// and that the node deploy output has to carry (what Next's file tracing
// follows). They are matched in the rewritten text, so modules an earlier
// build rewrote count too.
var (
	metaURLArgRe = regexp.MustCompile(`new URL\(\s*(?:"([^"\\\n]*)"|'([^'\\\n]*)')\s*,\s*__krateMetaURL\("([^"\\\n]*)"\)\s*\)`)
	metaJoinRe   = regexp.MustCompile(`\b(?:join|resolve)\(\s*(?:__krateMetaDir\("([^"\\\n]*)"\)|(?:[\w$]+\.)?dirname\(\s*__krateMetaFile\("([^"\\\n]*)"\)\s*\)|(?:[\w$]+\.)?dirname\(\s*(?:[\w$]+\.)?fileURLToPath\(\s*__krateMetaURL\("([^"\\\n]*)"\)\s*\)\s*\))((?:\s*,\s*(?:"[^"\\\n]*"|'[^'\\\n]*'))*)\s*\)`)
	metaLitRe    = regexp.MustCompile(`"([^"\\\n]*)"|'([^'\\\n]*)'`)
)

func (sm *sourceMeta) recordTargets(src string) {
	add := func(rel string, lits []string) {
		dir, err := unescapeURLPath(path.Dir(rel))
		if err != nil {
			return
		}
		parts := []string{sm.srcRoot, filepath.FromSlash(dir)}
		for _, l := range lits {
			if l == "" || strings.Contains(l, ":") || strings.HasPrefix(l, "/") || strings.ContainsAny(l, "?#*") {
				return
			}
			parts = append(parts, filepath.FromSlash(l))
		}
		p := filepath.Join(parts...)
		sm.mu.Lock()
		sm.targets[p] = true
		sm.mu.Unlock()
	}
	for _, m := range metaURLArgRe.FindAllStringSubmatch(src, -1) {
		add(m[3], []string{m[1] + m[2]})
	}
	for _, m := range metaJoinRe.FindAllStringSubmatch(src, -1) {
		rel := m[1] + m[2] + m[3]
		var lits []string
		for _, l := range metaLitRe.FindAllStringSubmatch(m[4], -1) {
			lits = append(lits, l[1]+l[2])
		}
		if len(lits) == 0 {
			continue
		}
		add(rel, lits)
	}
}

func unescapeURLPath(p string) (string, error) {
	segs := strings.Split(p, "/")
	for i, s := range segs {
		u, err := url.PathUnescape(s)
		if err != nil {
			return "", err
		}
		segs[i] = u
	}
	return strings.Join(segs, "/"), nil
}

// sourceRoots holds the source root of each project: its outputFileTracingRoot.
var sourceRoots sync.Map

// registerSourceRoot records the outputFileTracingRoot of the project at root
// ("" for none), which sourceRootFor returns.
func registerSourceRoot(root, tracingRoot string) {
	sourceRoots.Store(canonicalLoose(root), tracingRoot)
}

// sourceRootFor is the directory that source locations are relative to: the
// outputFileTracingRoot of the project when it has one, else the project.
func sourceRootFor(root string) string {
	if v, ok := sourceRoots.Load(canonicalLoose(root)); ok && v.(string) != "" {
		return v.(string)
	}
	return root
}

// metaCopyLimit bounds the size of a directory that a static reference
// names and the node deploy output carries.
const metaCopyLimit = 64 << 20

// copyMetaTargets copies the files that static references in the node
// bundle name (new URL("../data/x.txt", import.meta.url), path.join(__dirname,
// "data")) into the deploy output at their place below the source root, as
// Next's file tracing does. Missing files are skipped; a target outside the
// source root and a directory over the size limit are reported.
func copyMetaTargets(sm *sourceMeta, layout nodeLayout, plan *deploy.ServerPlan) {
	if sm == nil {
		return
	}
	for _, target := range sm.targetPaths() {
		info, err := os.Stat(target)
		if err != nil {
			continue
		}
		rel, err := filepath.Rel(sm.srcRoot, target)
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			plan.Warnings = append(plan.Warnings, fmt.Sprintf("%s is read through import.meta.url or __dirname but lies outside %s, so it is not in the deploy output", target, sm.srcRoot))
			continue
		}
		dest := filepath.Join(layout.base, rel)
		if !info.IsDir() {
			if info.Mode().IsRegular() {
				if err := copyFileMode(target, dest, info.Mode().Perm()); err != nil {
					plan.Warnings = append(plan.Warnings, fmt.Sprintf("copying %s into the deploy output: %v", target, err))
				}
			}
			continue
		}
		var files []string
		var total int64
		_ = filepath.WalkDir(target, func(p string, d os.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if d.IsDir() {
				if p != target && (d.Name() == "node_modules" || d.Name() == ".git") {
					return filepath.SkipDir
				}
				return nil
			}
			if fi, err := d.Info(); err == nil && fi.Mode().IsRegular() {
				total += fi.Size()
				files = append(files, p)
			}
			return nil
		})
		if total > metaCopyLimit {
			plan.Warnings = append(plan.Warnings, fmt.Sprintf("%s is read through import.meta.url or __dirname, but it holds %d MB (over the %d MB limit), so it is not in the deploy output; list it in outputFileTracingIncludes to copy it", target, total>>20, metaCopyLimit>>20))
			continue
		}
		for _, f := range files {
			fr, _ := filepath.Rel(target, f)
			fi, err := os.Stat(f)
			if err != nil {
				continue
			}
			if err := copyFileMode(f, filepath.Join(dest, fr), fi.Mode().Perm()); err != nil {
				plan.Warnings = append(plan.Warnings, fmt.Sprintf("copying %s into the deploy output: %v", f, err))
			}
		}
	}
}
