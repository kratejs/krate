package build

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/evanw/esbuild/pkg/api"
	"github.com/kratejs/krate/packages/compiler/internal/bundler"
	"github.com/kratejs/krate/packages/compiler/internal/jseval"
)

// Faster paths for build-time metadata (see metadataeval.go).
//
// A layout whose metadata is a static export (no generateMetadata) has the
// same value for every page under it, so it is evaluated once per build and
// its value is fed to each page's chain as data, in place of the module. A
// page whose own metadata the Go literal reader resolves (see metaliteral.go)
// is data too; such all-data chains are merged in one run for the whole build.
// The merge and the rendering stay the runtime's (metadataHeadTags), so the
// tags are the same. Values cross between runs in a tagged JSON form that keeps
// undefined and URL objects; anything else that JSON cannot carry exactly
// makes the layout or page take the full per-page evaluation instead. So does
// any run that reports an error, so warnings come from the same run as before.

// metadataFastPath enables the paths above; tests turn it off to compare.
var metadataFastPath = true

// metadataRuns counts metadata evaluations by kind, for tests.
var metadataRuns struct {
	// Layout counts evaluations of one static layout's metadata.
	Layout atomic.Int64
	// Page counts runs of one page's module with pre-evaluated layouts.
	Page atomic.Int64
	// Data counts runs of one page's all-data chain.
	Data atomic.Int64
	// Batch counts runs that merge the all-data chains of many pages.
	Batch atomic.Int64
	// Chain counts full chain evaluations (evalMetadataModules).
	Chain atomic.Int64
}

// metadataStaticState is the per-build memo of the paths above.
type metadataStaticState struct {
	mu      sync.Mutex
	layouts map[string]*staticLayoutValue
	lits    *litCache

	batches map[string]*metadataBatch // by layoutChainKey
}

// metadataBatch is one run over the all-data pages that share a layout chain.
type metadataBatch struct {
	once  sync.Once
	pages map[string]batchMetadata
}

type staticLayoutValue struct {
	once sync.Once
	enc  string // tagged JSON of the module's {metadata, viewport}
	ok   bool
}

type batchMetadata struct {
	params    string // paramsKey of the one param set
	segs      []metaSegment
	pageFiles string
	result    metadataResult
}

var metadataStaticStates sync.Map // *Builder -> *metadataStaticState

func (b *Builder) metadataStatic() *metadataStaticState {
	if v, ok := metadataStaticStates.Load(b); ok {
		return v.(*metadataStaticState)
	}
	v, _ := metadataStaticStates.LoadOrStore(b, newMetadataStaticState())
	return v.(*metadataStaticState)
}

func newMetadataStaticState() *metadataStaticState {
	return &metadataStaticState{layouts: map[string]*staticLayoutValue{}, lits: newLitCache(), batches: map[string]*metadataBatch{}}
}

// resetMetadataStatic drops the memo: layout values depend on the files and
// the env of one build.
func (b *Builder) resetMetadataStatic() {
	metadataStaticStates.Store(b, newMetadataStaticState())
}

// resolveMetadataChain is evalMetadata through the faster paths when they
// apply and succeed without errors.
func (b *Builder) resolveMetadataChain(page string, segs []metaSegment, pageFiles string, sets []map[string]string, bnd *bundler.Bundler) ([]metadataResult, error) {
	if metadataFastPath {
		if out, ok := b.fastMetadata(page, segs, pageFiles, sets, bnd); ok {
			return out, nil
		}
	}
	return b.evalMetadata(page, segs, pageFiles, sets, bnd)
}

func (b *Builder) fastMetadata(page string, segs []metaSegment, pageFiles string, sets []map[string]string, bnd *bundler.Bundler) ([]metadataResult, bool) {
	abs, err := filepath.Abs(page)
	if err != nil {
		return nil, false
	}
	pre, ok := b.staticLayouts(segs, bnd)
	if !ok {
		return nil, false
	}
	if pageEnc, lit := b.literalPageModule(abs, bnd); lit {
		if len(sets) == 1 {
			if r, ok := b.batchedMetadata(abs, segs, pageFiles, sets[0], bnd); ok {
				return []metadataResult{r}, true
			}
		}
		metadataRuns.Data.Add(1)
		src := func(print bool) string {
			return metadataBatchSource([][]chainItem{chainItems(abs, segs, pre, pageEnc, pageFiles, b.Root)}, [][]map[string]string{sets}, print)
		}
		raw, err := b.evalMetadataEntry(abs, src, nil, bnd, b.serverGlobalsTransform)
		if err != nil {
			return nil, false
		}
		var out [][]metadataResult
		if json.Unmarshal(raw, &out) != nil || len(out) != 1 || len(out[0]) != len(sets) || anyErrors(out[0]) {
			return nil, false
		}
		return out[0], true
	}
	return b.pageWithStaticLayouts(abs, segs, pre, pageFiles, sets, bnd)
}

// staticLayouts returns the tagged value of every layout in segs, by index;
// ok is false when one of them is not static or could not be evaluated alone.
func (b *Builder) staticLayouts(segs []metaSegment, bnd *bundler.Bundler) ([]string, bool) {
	pre := make([]string, len(segs))
	for i, sg := range segs {
		if sg.Path == "" {
			continue
		}
		enc, ok := b.staticLayout(sg.Path, bnd)
		if !ok {
			return nil, false
		}
		pre[i] = enc
	}
	return pre, true
}

// staticLayout evaluates a layout's metadata exports once per build.
func (b *Builder) staticLayout(path string, bnd *bundler.Bundler) (string, bool) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", false
	}
	st := b.metadataStatic()
	st.mu.Lock()
	v := st.layouts[abs]
	if v == nil {
		v = &staticLayoutValue{}
		st.layouts[abs] = v
	}
	st.mu.Unlock()
	v.once.Do(func() { v.enc, v.ok = b.evalStaticLayout(abs, bnd) })
	return v.enc, v.ok
}

func (b *Builder) evalStaticLayout(abs string, bnd *bundler.Bundler) (string, bool) {
	data, err := os.ReadFile(abs)
	if err != nil {
		return "", false
	}
	if _, fn := moduleMetadataExports(string(data)); fn {
		return "", false
	}
	metadataRuns.Layout.Add(1)
	imp := abs
	var virtual map[string]string
	if sl, ok := sliceModule(abs, metadataSliceExports); ok {
		imp = sl.Path
		virtual = map[string]string{sl.Path: sl.Source}
	}
	src := func(print bool) string { return staticLayoutSource(imp, print) }
	raw, err := b.evalMetadataEntry(abs, src, virtual, bnd, b.serverGlobalsTransform)
	if err != nil {
		return "", false
	}
	var res struct {
		OK  bool            `json:"ok"`
		Mod json.RawMessage `json:"mod"`
	}
	if json.Unmarshal(raw, &res) != nil || !res.OK || !json.Valid(res.Mod) {
		return "", false
	}
	return string(res.Mod), true
}

// pageWithStaticLayouts runs the page's module with its layouts as data.
func (b *Builder) pageWithStaticLayouts(abs string, segs []metaSegment, pre []string, pageFiles string, sets []map[string]string, bnd *bundler.Bundler) ([]metadataResult, bool) {
	metadataRuns.Page.Add(1)
	imp := abs
	var virtual map[string]string
	if sl, ok := sliceModule(abs, metadataSliceExports); ok {
		imp = sl.Path
		virtual = map[string]string{sl.Path: sl.Source}
	}
	// A layout the page's own imports reach would run twice where it ran
	// once, so such a page keeps the full chain.
	layouts := map[string]bool{}
	for _, sg := range segs {
		if sg.Path != "" {
			layouts[filepath.Clean(sg.Path)] = true
		}
	}
	var mu sync.Mutex
	reached := false
	transform := func(path, src string) string {
		if layouts[filepath.Clean(path)] {
			mu.Lock()
			reached = true
			mu.Unlock()
		}
		return b.serverGlobalsTransform(path, src)
	}
	items := chainItems(abs, segs, pre, "", pageFiles, b.Root)
	items[len(items)-1].imp = imp
	src := func(print bool) string {
		return metadataBatchSource([][]chainItem{items}, [][]map[string]string{sets}, print)
	}
	raw, err := b.evalMetadataEntry(abs, src, virtual, bnd, transform)
	mu.Lock()
	r := reached
	mu.Unlock()
	if err != nil || r {
		return nil, false
	}
	var out [][]metadataResult
	if json.Unmarshal(raw, &out) != nil || len(out) != 1 || len(out[0]) != len(sets) || anyErrors(out[0]) {
		return nil, false
	}
	return out[0], true
}

// layoutChainKey names the layouts of a chain, in order.
func layoutChainKey(segs []metaSegment) string {
	var sb strings.Builder
	for _, sg := range segs {
		if sg.Path != "" {
			sb.WriteString(filepath.Clean(sg.Path))
			sb.WriteByte(0)
		}
	}
	return sb.String()
}

// batchedMetadata returns the result for a page from the build's single run
// over the pages with the same layouts whose chain is all data, each resolved
// with the params its page build uses: none, or a dynamic route's sentinels.
// A batch only evaluates layouts the asking page needs anyway, so a partial
// rebuild does not evaluate the layouts of pages it does not build.
func (b *Builder) batchedMetadata(abs string, segs []metaSegment, pageFiles string, params map[string]string, bnd *bundler.Bundler) (metadataResult, bool) {
	st := b.metadataStatic()
	key := layoutChainKey(segs)
	st.mu.Lock()
	bt := st.batches[key]
	if bt == nil {
		bt = &metadataBatch{}
		st.batches[key] = bt
	}
	st.mu.Unlock()
	bt.once.Do(func() { bt.pages = b.runMetadataBatch(key, bnd) })
	e, ok := bt.pages[abs]
	if !ok || e.params != paramsKey(params) || e.pageFiles != pageFiles || len(e.segs) != len(segs) || len(e.result.Errors) > 0 {
		return metadataResult{}, false
	}
	for i := range segs {
		if filepath.Clean(segs[i].Path) != filepath.Clean(e.segs[i].Path) || segs[i].Files != e.segs[i].Files {
			return metadataResult{}, false
		}
	}
	return e.result, true
}

func (b *Builder) runMetadataBatch(key string, bnd *bundler.Bundler) map[string]batchMetadata {
	if b.Cfg == nil {
		return nil
	}
	pages, err := findPages(b.Cfg.PagesDir)
	if err != nil {
		return nil
	}
	type entry struct {
		abs       string
		params    map[string]string
		segs      []metaSegment
		pageFiles string
		items     []chainItem
	}
	var entries []entry
	for _, page := range pages {
		switch strings.ToLower(filepath.Ext(page)) {
		case ".md", ".mdx":
			continue
		}
		var params map[string]string
		if isDynamicRoute(page, b.Cfg.PagesDir) {
			params = dynamicRouteSentinels(extractParamNames(page, b.Cfg.PagesDir))
		}
		abs, err := filepath.Abs(page)
		if err != nil {
			continue
		}
		segs, pageFiles := b.metadataSegments(page)
		if (len(segs) == 0 && pageFiles == "") || layoutChainKey(segs) != key {
			continue
		}
		pre, ok := b.staticLayouts(segs, bnd)
		if !ok {
			continue
		}
		pageEnc, lit := b.literalPageModule(abs, bnd)
		if !lit {
			continue
		}
		entries = append(entries, entry{abs, params, segs, pageFiles, chainItems(abs, segs, pre, pageEnc, pageFiles, b.Root)})
	}
	if len(entries) == 0 {
		return nil
	}
	metadataRuns.Batch.Add(1)
	chains := make([][]chainItem, len(entries))
	sets := make([][]map[string]string, len(entries))
	for i, e := range entries {
		chains[i] = e.items
		sets[i] = []map[string]string{e.params}
	}
	src := func(print bool) string { return metadataBatchSource(chains, sets, print) }
	anchor := filepath.Join(b.Cfg.PagesDir, "krate-metadata-batch.tsx")
	if a, err := filepath.Abs(anchor); err == nil {
		anchor = a
	}
	raw, err := b.evalMetadataEntry(anchor, src, nil, bnd, b.serverGlobalsTransform)
	if err != nil {
		return nil
	}
	var out [][]metadataResult
	if json.Unmarshal(raw, &out) != nil || len(out) != len(entries) {
		return nil
	}
	m := make(map[string]batchMetadata, len(entries))
	for i, e := range entries {
		if len(out[i]) != 1 {
			continue
		}
		m[e.abs] = batchMetadata{params: paramsKey(e.params), segs: e.segs, pageFiles: e.pageFiles, result: out[i][0]}
	}
	return m
}

// evalMetadataEntry runs an entry in the embedded engine and, when that fails,
// in the configured runtime, the way evalMetadataModules does. A run that
// produced its result in the engine but reported errors is not retried here:
// callers then fall back to the full chain, which retries.
func (b *Builder) evalMetadataEntry(abs string, src func(print bool) string, virtual map[string]string, bnd *bundler.Bundler, transform func(path, src string) string) ([]byte, error) {
	resolve := func(importer, spec string) (string, bool) {
		if bnd != nil {
			if p := bnd.ResolveForNode(importer, spec); p != "" {
				return p, true
			}
		}
		return "", false
	}
	raw, eerr := jseval.Eval(jseval.Options{
		Source:        src(false),
		ResolveDir:    filepath.Dir(abs),
		Sourcefile:    "krate-metadata-entry.ts",
		Loader:        api.LoaderTSX,
		Env:           b.scriptEnv(),
		Await:         true,
		ResolveImport: resolve,

		JSXImportSource: "@krate/runtime/server",
		VirtualFiles:    virtual,
		SourceTransform: transform,
	})
	if eerr == nil {
		return raw, nil
	}
	var be *jseval.BundleError
	if errors.As(eerr, &be) {
		return nil, eerr
	}
	return b.runBootstrapBundle(abs, src(true), "metadata", bnd, virtual)
}

// chainItem is one segment of an entry's chain: a module to import (imp), or
// a tagged value (enc), or neither (a convention-files segment).
type chainItem struct {
	file  string
	imp   string
	enc   string
	files string
}

func chainItems(page string, segs []metaSegment, pre []string, pageEnc, pageFiles, root string) []chainItem {
	items := make([]chainItem, 0, len(segs)+1)
	for i, sg := range segs {
		it := chainItem{files: sg.Files}
		if sg.Path != "" {
			it.file = relTo(root, sg.Path)
			it.enc = pre[i]
		}
		items = append(items, it)
	}
	return append(items, chainItem{file: relTo(root, page), enc: pageEnc, imp: page, files: pageFiles})
}

// metadataDecoderJS turns the tagged JSON back into values: {o: [[k, v]...]}
// an object (keys assigned in order), {a: [...]} an array, {u: 1} undefined,
// {url: href} a URL.
const metadataDecoderJS = `function __krateDec(v) {
  if (v === null || typeof v !== "object") return v;
  if ("o" in v) { const o = {}; for (const [k, x] of v.o) o[k] = __krateDec(x); return o; }
  if ("a" in v) return v.a.map(__krateDec);
  if ("url" in v) return new URL(v.url);
  return undefined;
}
`

// metadataEncoderJS is the inverse of metadataDecoderJS. It throws
// __krateBail for a value the tagged form cannot carry exactly: a class
// instance (other than URL), a getter, a non-enumerable, frozen or shared
// object, a symbol key, NaN, -0, a lone surrogate.
const metadataEncoderJS = `const __krateBail = {};
function __krateEnc(v, seen) {
  if (v === undefined) return { u: 1 };
  if (v === null || typeof v === "boolean") return v;
  if (typeof v === "string") {
    for (let i = 0; i < v.length; i++) {
      const c = v.charCodeAt(i);
      if (c >= 0xd800 && c <= 0xdbff) {
        const d = v.charCodeAt(i + 1);
        if (!(d >= 0xdc00 && d <= 0xdfff)) throw __krateBail;
        i++;
      } else if (c >= 0xdc00 && c <= 0xdfff) throw __krateBail;
    }
    return v;
  }
  if (typeof v === "number") {
    if (!Number.isFinite(v) || Object.is(v, -0)) throw __krateBail;
    return v;
  }
  if (typeof v !== "object" || seen.has(v)) throw __krateBail;
  seen.add(v);
  const proto = Object.getPrototypeOf(v);
  if (typeof URL === "function" && proto === URL.prototype) {
    const href = v.href;
    if (typeof href !== "string" || new URL(href).href !== href) throw __krateBail;
    return { url: href };
  }
  if (!Object.isExtensible(v)) throw __krateBail;
  const keys = Reflect.ownKeys(v);
  const data = (k) => {
    const d = Object.getOwnPropertyDescriptor(v, k);
    if (!d || !("value" in d) || !d.enumerable || !d.writable || !d.configurable) throw __krateBail;
    return d.value;
  };
  if (Array.isArray(v)) {
    if (proto !== Array.prototype || keys.length !== v.length + 1) throw __krateBail;
    const out = [];
    for (let i = 0; i < v.length; i++) out.push(__krateEnc(data(String(i)), seen));
    return { a: out };
  }
  if (proto !== Object.prototype) throw __krateBail;
  const out = [];
  for (const k of keys) {
    if (typeof k !== "string" || k === "__proto__") throw __krateBail;
    out.push([k, __krateEnc(data(k), seen)]);
  }
  return { o: out };
}
`

// staticLayoutSource evaluates a layout module and reports its metadata and
// viewport in the tagged form, or ok: false when they cannot be carried.
func staticLayoutSource(imp string, print bool) string {
	var b strings.Builder
	fmt.Fprintf(&b, "import * as __m from %s;\n", strconv.Quote(filepath.ToSlash(imp)))
	b.WriteString(metadataEncoderJS)
	b.WriteString(`let __r;
try {
  if (__m.generateMetadata !== undefined) throw __krateBail;
  const seen = new Set();
  const pairs = [];
  if (__m.metadata !== undefined) pairs.push(["metadata", __krateEnc(__m.metadata, seen)]);
  if (__m.viewport !== undefined) pairs.push(["viewport", __krateEnc(__m.viewport, seen)]);
  __r = { ok: true, mod: { o: pairs } };
} catch (e) {
  if (e !== __krateBail) throw e;
  __r = { ok: false };
}
`)
	if print {
		b.WriteString("console.log(JSON.stringify(__r));\n")
	} else {
		fmt.Fprintf(&b, "globalThis.%s = __r;\n", jseval.ResultGlobal)
	}
	return b.String()
}

// metadataBatchSource resolves several chains, each for its param sets, with
// the loop of metadataEntrySource; the result is one array of results per
// chain. Tagged values are decoded per chain, as a module is evaluated per
// page run.
func metadataBatchSource(chains [][]chainItem, sets [][]map[string]string, print bool) string {
	var b strings.Builder
	b.WriteString("import { metadataHeadTags } from \"@krate/runtime/server\";\n")
	imports := map[string]string{}
	consts := map[string]string{}
	var constOrder []string
	ref := func(it chainItem) string {
		if it.enc != "" {
			name, ok := consts[it.enc]
			if !ok {
				name = fmt.Sprintf("__v%d", len(consts))
				consts[it.enc] = name
				constOrder = append(constOrder, it.enc)
			}
			return "__krateDec(" + name + ")"
		}
		if it.imp != "" {
			name, ok := imports[it.imp]
			if !ok {
				name = fmt.Sprintf("__m%d", len(imports))
				imports[it.imp] = name
				fmt.Fprintf(&b, "import * as %s from %s;\n", name, strconv.Quote(filepath.ToSlash(it.imp)))
			}
			return name
		}
		return ""
	}
	var mks []string
	for _, chain := range chains {
		var segs []string
		for _, it := range chain {
			var fields []string
			if mod := ref(it); mod != "" {
				fields = append(fields, "file: "+strconv.Quote(it.file), "mod: "+mod)
			}
			if it.files != "" {
				fields = append(fields, "files: "+it.files)
			}
			segs = append(segs, "{ "+strings.Join(fields, ", ")+" }")
		}
		mks = append(mks, "() => ["+strings.Join(segs, ", ")+"]")
	}
	b.WriteString(metadataDecoderJS)
	for _, enc := range constOrder {
		fmt.Fprintf(&b, "const %s = %s;\n", consts[enc], enc)
	}
	setsJSON, _ := json.Marshal(sets)
	fmt.Fprintf(&b, "const __sets = %s;\nconst __chains = [%s];\n", setsJSON, strings.Join(mks, ",\n"))
	b.WriteString(`const __results = [];
const __run = async () => {
  for (let i = 0; i < __chains.length; i++) {
    const __chain = __chains[i]();
    const out = [];
    for (const params of __sets[i]) {
      const errors = [];
      const errorFiles = [];
      let tags = "";
      try {
        tags = await metadataHeadTags(__chain, { params, searchParams: {} }, (msg, err) => {
          errors.push(String((err && err.message) || err));
          const m = /^generateMetadata in (.*) failed;/.exec(msg);
          errorFiles.push(m ? m[1] : "");
        });
      } catch (err) {
        errors.push(String((err && err.message) || err));
        errorFiles.push("");
      }
      out.push({ tags, errors, errorFiles });
    }
    __results.push(out);
  }
`)
	if print {
		b.WriteString("  console.log(JSON.stringify(__results));\n")
		b.WriteString("};\n__run().catch((err) => { console.error(err); process.exit(1); });\n")
	} else {
		fmt.Fprintf(&b, "  globalThis.%s = __results;\n", jseval.ResultGlobal)
		fmt.Fprintf(&b, "};\n__run().catch((err) => { globalThis.%s = String((err && err.message) || err); });\n", jseval.ErrorGlobal)
	}
	return b.String()
}
