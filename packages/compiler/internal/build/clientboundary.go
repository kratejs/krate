package build

import (
	"bytes"
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
	"github.com/kratejs/krate/packages/compiler/ast"
	"github.com/kratejs/krate/packages/compiler/internal/bundler"
	"github.com/kratejs/krate/packages/compiler/internal/diag"
	"github.com/kratejs/krate/packages/compiler/internal/fsutil"
	irtree "github.com/kratejs/krate/packages/compiler/internal/irtree"
	"github.com/kratejs/krate/packages/compiler/internal/resolver"
	"github.com/kratejs/krate/packages/compiler/internal/safe"
)

// Client boundaries ("use client").
//
// A "use client" module reached from a server component tree is not rendered
// as inert server markup. In the page server bundle each of its exports is
// wrapped by clientBoundary (@krate/runtime/server): the server renders the
// component's initial HTML with Preact (React semantics, like external
// islands) between <!--krate-island:{m,e,s,p}--> (module, export, chunk and
// serialized props) and <!--/krate-island-->, with no element of its own, and
// the module's client chunk hydrates those nodes in the browser with the same
// props. Children passed from the server are rendered on the server inside the
// component's HTML; the browser adopts those nodes in place rather than
// receiving them again. Pages without server components are untouched: their
// "use client" modules compile through the normal Krate pipeline.

// ClientBoundary is one "use client" module of a server-component page.
type ClientBoundary struct {
	ModuleID   string   // stable id derived from the root-relative path
	SourcePath string   // absolute module path
	Exports    []string // export names ("default" for the default export)
	ChunkURL   string   // hydration chunk URL
	// Spec is set for a package boundary: the bare specifier of a React-bound
	// package whose components a server module imports. Entry then defines
	// `__m` (the used exports) in place of importing SourcePath, ResolveDir is
	// where the specifier resolves from, and Hydrate is false for a package
	// whose components have no state or effects (icons), which ship no island.
	Spec       string
	Entry      string
	ResolveDir string
	Hydrate    bool
	// Real marks a boundary that runs on real react and react-dom: a React
	// reconciler library or a module that imports one. The server emits a
	// fallback placeholder instead of rendering it (see realreact.go).
	Real bool
	// Hints are the resource hint tags of each export of a real-React
	// boundary, collected at build time (see realhints.go).
	Hints map[string][]string

	// pending holds the check builds of a boundary prepared without waiting
	// for them (see awaitBoundaries); nil once they are applied.
	pending *boundaryJobs
}

// boundaryJobs are the unfinished check builds of one boundary.
type boundaryJobs struct {
	req   clientChunkReq
	chunk *roundJob
	hints map[string]*roundJob
}

// hydrates reports whether the boundary's components become islands.
func (cb ClientBoundary) hydrates() bool { return cb.Real || cb.Spec == "" || cb.Hydrate }

// anyBoundaryHydrates reports whether a page needs the island loader: some
// boundary of it becomes an island.
func anyBoundaryHydrates(boundaries []ClientBoundary) bool {
	for _, cb := range boundaries {
		if cb.hydrates() {
			return true
		}
	}
	return false
}

// entrySource is the module preamble that binds `__m`, the exports of the
// boundary, for the client chunk and the server render module.
func (cb ClientBoundary) entrySource() string {
	if cb.Entry != "" {
		return cb.Entry
	}
	return "import * as __m from " + strconv.Quote(cb.SourcePath) + ";\n"
}

func (cb ClientBoundary) resolveDir() string {
	if cb.ResolveDir != "" {
		return cb.ResolveDir
	}
	return filepath.Dir(cb.SourcePath)
}

// clientIslandLoaderJS mounts every island that is not mounted yet from its
// chunk. It is a classic script in a chunk file (not inline) so the SPA router
// loads it when a soft navigation arrives on a page with islands, and it is
// idempotent: it only touches islands it has not mounted.
//
// A Preact island is the nodes between <!--krate-island:{m,e,s,p}--> and its
// <!--/krate-island--> (nested islands balance in between); its chunk's mount
// gets the island record. A real-React island is a <krate-island data-src>
// element; its chunk's mount gets the element. Every island is listed in
// window.__krateIslands with its status (loading, hydrated, error) and a
// contains(node) test, for tools and tests (there is no element to mark).
//
// An island inside another island's server children mounts after it, so it
// finds the context the outer island provides there (see islandcontext.go).
// The chunks still load in parallel.
//
// A chunk's mount returns the island's handle (update, unmount, and for a
// Preact island the server children it adopted), kept on the record as
// handle: the SPA router re-renders a kept island in place through it and
// unmounts an island its swap removes (see the runtime's island-swap.ts). An
// island the router removed while its chunk loaded (gone) is unmounted as
// soon as it mounted. __krateIslandsReady settles once the islands found by
// this run mounted.
const clientIslandLoaderJS = `(function(){
var W=window,reg=W.__krateIslands||(W.__krateIslands=[]),OPEN="krate-island:",CLOSE="/krate-island";
for(var i=reg.length-1;i>=0;i--)if(!reg[i].connected())reg.splice(i,1);
function isOpen(n){return n.nodeType===8&&n.data.indexOf(OPEN)===0}
function isClose(n){return n.nodeType===8&&n.data===CLOSE}
function endOf(start){for(var d=0,n=start.nextSibling;n;n=n.nextSibling){if(isOpen(n))d++;else if(isClose(n)){if(!d)return n;d--}}return null}
function range(start){
var end=endOf(start),p;if(!end)return null;
try{p=JSON.parse(start.data.slice(OPEN.length))}catch(e){return null}
var r={start:start,end:end,module:p.m,export:p.e,src:p.s,ids:p.i,props:p.p||{},status:"loading",
nodes:function(){for(var o=[],c=start.nextSibling;c&&c!==end;c=c.nextSibling)o.push(c);return o},
contains:function(x){for(var c=start.nextSibling;c&&c!==end;c=c.nextSibling)if(c===x||(c.nodeType===1&&c.contains(x)))return true;return false},
connected:function(){return start.isConnected}};
return start.__krateIsland=r}
function host(el){
var r={el:el,module:el.getAttribute("data-module"),export:el.getAttribute("data-export"),src:el.getAttribute("data-src"),status:"loading",
contains:function(x){return x!==el&&el.contains(x)},connected:function(){return el.isConnected}};
el.setAttribute("data-krate-island-mounted","");return el.__krateIsland=r}
function around(n,open){
var h=n.parentElement&&n.parentElement.closest("krate-island[data-src]"),hr=h&&h.__krateIsland;
if(!open)return hr||null;if(!hr)return open;return h.contains(open.start)?open:hr}
var stack=[],found=[],w=document.createTreeWalker(document.body,129);
for(var n=w.nextNode();n;n=w.nextNode()){
var top=null;for(var k=stack.length-1;k>=0&&!top;k--)top=stack[k];
if(isOpen(n)){var r=n.__krateIsland;if(!r&&(r=range(n))){r.parent=around(n,top);found.push(r)}stack.push(r||null)}
else if(isClose(n))stack.pop();
else if(n.nodeType===1&&n.localName==="krate-island"&&n.hasAttribute("data-src")&&!n.hasAttribute("data-krate-island-mounted")){var hr=host(n);hr.parent=around(n,top);found.push(hr)}}
found.forEach(function(r){
reg.push(r);
var up=r.parent,ready=up&&up.mounted?up.mounted.catch(function(){}):null;
r.mounted=Promise.all([import(r.src),ready]).then(function(m){return r.gone?null:m[0].mount(r.el||r)}).then(function(h){
r.handle=h||null;if(r.gone){if(h&&h.unmount)h.unmount();return}
r.status="hydrated";try{W.dispatchEvent(new CustomEvent("krate:island",{detail:r}))}catch(e){}
}).catch(function(e){console.error("krate client component:",e);r.status="error";if(r.el)r.el.setAttribute("data-krate-island-error","")});
if(r.el)r.el.__krateMount=r.mounted});
W.__krateIslandsReady=Promise.all(found.map(function(r){return r.mounted}))})();
`

// islandLoaderHTML writes the island loader chunk (once per content) and
// returns the script tag that references it. The tag is deferred so a hard
// load runs it after the document, including spliced regions, is parsed.
// The chunk also carries the runtime's island-preserving router swap
// (krate-island-swap.js, window.__krateIslandSwap), so only pages with
// islands ship it; without it the router replaces islands as plain markup.
func (b *Builder) islandLoaderHTML() (string, error) {
	code := clientIslandLoaderJS
	if swap := loadNamedBundleFromDisk(b.Root, "krate-island-swap.js"); swap != "" {
		code += swap
	}
	name := "chunks/island-loader." + hashContent([]byte(code)) + ".js"
	if err := os.MkdirAll(filepath.Join(b.Cfg.OutDir, "chunks"), 0755); err != nil {
		return "", err
	}
	if err := fsutil.WriteFileIfChanged(filepath.Join(b.Cfg.OutDir, name), []byte(code), 0644); err != nil {
		return "", err
	}
	return `<script defer src="` + assetHref(b.Cfg.BaseURLPath(), name) + `"></script>`, nil
}

// clientReviveJS turns serialized island props back into component props:
// server-rendered children become a slot (made by the chunk's own
// __krateSlot) that adopts the server-rendered
// nodes already in the document ({__krateSlotRef}, {$: "h", r}) or the HTML
// the props carry, and server action references become callable functions. Props are
// read from the JSON script before the island (data-props on older output).
const clientReviveJS = `
function __krateTree(n, h, call, comp) {
  if (n === null || n === undefined || typeof n === "boolean") return null;
  if (typeof n === "string" || typeof n === "number") return n;
  if (Array.isArray(n)) return n.map(function (x) { return __krateTree(x, h, call, comp); });
  if (n.$ === "h") return n.r != null ? __krateSlot(h, "", n.r) : __krateSlot(h, n.h);
  var kids = (n.c || []).map(function (x) { return __krateTree(x, h, call, comp); });
  var props = __krateRevive(n.p || {}, h, call, comp);
  if (n.$ === "e") return h.apply(null, [n.t, props].concat(kids));
  var C = comp(n.m, n.e);
  if (typeof C !== "function") throw new Error("client component " + n.e + " of " + n.m + " is not loaded");
  return h.apply(null, [C, props].concat(kids));
}
function __krateBytes(b) {
  var s = atob(b), u = new Uint8Array(s.length);
  for (var i = 0; i < s.length; i++) u[i] = s.charCodeAt(i);
  return u;
}
function __krateTagged(v, h, call, comp) {
  var r = function (x) { return __krateRevive(x, h, call, comp); };
  switch (v.__krateT) {
    case "U": return undefined;
    case "D": return new Date(v.v === null ? NaN : v.v);
    case "B": return BigInt(v.v);
    case "N": return v.v === "-0" ? -0 : v.v === "NaN" ? NaN : v.v === "Infinity" ? Infinity : -Infinity;
    case "Y": return Symbol.for(v.v);
    case "M": return new Map(v.v.map(function (e) { return [r(e[0]), r(e[1])]; }));
    case "S": return new Set(v.v.map(r));
    case "A": return __krateBytes(v.v).buffer;
    case "V": return new DataView(__krateBytes(v.v).buffer);
    case "T": { var u = __krateBytes(v.v.b); return new globalThis[v.v.n](u.buffer, 0, u.length / globalThis[v.v.n].BYTES_PER_ELEMENT); }
    case "O": { var o = {}; for (var k in v.v) o[k] = r(v.v[k]); return o; }
  }
  return v;
}
function __krateActionFn(url, bound, call) {
  var fn = function () { return call(url, bound.concat(Array.prototype.slice.call(arguments))); };
  fn.actionUrl = url;
  // No $$krateAction: the island's form handling treats a function carrying it as a
  // server-rendered action with its own markup, and would leave a form with action={fn} alone.
  fn.$$krateBound = bound;
  fn.bind = function (_this) { return __krateActionFn(url, bound.concat(Array.prototype.slice.call(arguments, 1)), call); };
  fn.toString = function () { return url; };
  return fn;
}
function __krateRevive(v, h, call, comp) {
  if (Array.isArray(v)) return v.map(function (x) { return __krateRevive(x, h, call, comp); });
  if (v && typeof v === "object") {
    if (typeof v.__krateT === "string") return __krateTagged(v, h, call, comp);
    if (typeof v.__krateAction === "string") {
      return __krateActionFn(v.__krateAction, [], call);
    }
    if (typeof v.__krateSlotHTML === "string") return __krateSlot(h, v.__krateSlotHTML);
    if (typeof v.__krateSlotRef === "string") return __krateSlot(h, "", v.__krateSlotRef);
    if (v.__krateSlotTree) { var slot = __krateTree(v.__krateSlotTree, h, call, comp); return slot.length === 1 ? slot[0] : slot; }
    var out = {};
    for (var k in v) {
      if (k === "__krateChildren") out.children = typeof v[k] === "string" ? __krateSlot(h, v[k]) : __krateSlot(h, "", v[k] && v[k].__krateSlotRef);
      else if (k === "__krateTree") { var kids = __krateTree(v[k], h, call, comp); out.children = kids.length === 1 ? kids[0] : kids; }
      else out[k] = __krateRevive(v[k], h, call, comp);
    }
    return out;
  }
  return v;
}
function __krateClientRefs(n, out) {
  if (!n || typeof n !== "object") return out;
  if (Array.isArray(n)) { n.forEach(function (x) { __krateClientRefs(x, out); }); return out; }
  if (n.$ === "c") out.push({ m: n.m, e: n.e, s: n.s });
  (n.c || []).forEach(function (x) { __krateClientRefs(x, out); });
  if (n.p) for (var k in n.p) { __krateSlotRefs(n.p[k], k, out); }
  return out;
}
function __krateSlotRefs(v, k, out) {
  if (k === "__krateTree") __krateClientRefs(v, out);
  else if (v && typeof v === "object" && v.__krateSlotTree) __krateClientRefs(v.__krateSlotTree, out);
  return out;
}
function __krateProps(el) {
  if (el.hasAttribute("data-props")) return JSON.parse(el.getAttribute("data-props") || "{}");
  var s = el.previousElementSibling;
  return s && s.localName === "script" && s.hasAttribute("data-krate-props") ? JSON.parse(s.textContent || "{}") : {};
}
function __krateRootRefs(raw) {
  var out = [];
  for (var k in raw) __krateSlotRefs(raw[k], k, out);
  return out;
}
`

// clientActionCallJS posts a server action call from a client component,
// using the same protocol as the Krate runtime (callServerAction): a FormData
// last argument goes as multipart with the leading arguments in __krate_args,
// anything else as a JSON {args} body. It announces a revalidation the same way (the router refreshes the route).
const clientActionCallJS = `
async function __krateCallAction(url, args) {
  var headers = { accept: "application/json", "x-krate-action": "fetch" };
  if (typeof location !== "undefined") headers["x-krate-return"] = location.pathname + location.search;
  var body;
  var last = args[args.length - 1];
  if (typeof FormData !== "undefined" && last instanceof FormData) {
    var fd = new FormData();
    last.forEach(function (value, key) { if (key !== "__krate_args") fd.append(key, value); });
    var leading = args.slice(0, -1);
    if (leading.length > 0) fd.set("__krate_args", JSON.stringify(leading));
    body = fd;
  } else {
    headers["content-type"] = "application/json";
    body = JSON.stringify({ args: args });
  }
  var res = await fetch(url, { method: "POST", credentials: "same-origin", headers: headers, body: body });
  var payload = null;
  try { payload = await res.json(); } catch (e) { payload = null; }
  if (!res.ok || !payload || payload.ok === false) throw new Error((payload && payload.error && (payload.error.message || payload.error)) || ("server action failed: " + res.status));
  if (payload.revalidated && typeof window !== "undefined" && typeof CustomEvent === "function") {
    var info = payload.revalidation || {};
    window.dispatchEvent(new CustomEvent("krate:revalidate", { detail: { paths: Array.isArray(info.paths) ? info.paths : [], tags: Array.isArray(info.tags) ? info.tags : [] } }));
  }
  if (typeof payload.redirect === "string" && payload.redirect) {
    var to = payload.redirect;
    if (to.charAt(0) === "/" && to.charAt(1) !== "/" && to.indexOf("\\") === -1 && !/[\x00-\x1f\x7f]/.test(to)) location.assign(to);
  }
  return payload.result;
}
`

// clientModuleID is the stable boundary id of a module.
func clientModuleID(root, path string) string {
	root, path = canonicalPath(root), canonicalPath(path)
	rel, err := filepath.Rel(root, path)
	if err != nil {
		rel = path
	}
	return "c" + hashContent([]byte(filepath.ToSlash(rel)))
}

// prepareClientBoundaries validates the server tree of a page that renders
// server components and builds the client chunk of every "use client" module
// in its module graph. It returns nil for pages without server components.
func (b *Builder) prepareClientBoundaries(bundle *bundler.Bundle, ann *irtree.Annotations, serverPage bool) ([]ClientBoundary, error) {
	out, err := b.prepareClientBoundariesDeferred(bundle, ann, serverPage)
	if err != nil {
		return nil, err
	}
	if err := b.awaitBoundaries(out); err != nil {
		return nil, err
	}
	return out, nil
}

// prepareClientBoundariesDeferred is prepareClientBoundaries that queues the
// chunk and hints builds without waiting for them: the boundaries carry them
// until awaitBoundaries, so the page goes on building while they run.
func (b *Builder) prepareClientBoundariesDeferred(bundle *bundler.Bundle, ann *irtree.Annotations, serverPage bool) ([]ClientBoundary, error) {
	roots := serverComponentNames(ann)
	if serverPage {
		roots = append([]string{ann.EntryPoint}, roots...)
	}
	if len(roots) == 0 {
		return nil, nil
	}
	clientMods := map[string]*bundler.Module{}
	for _, mod := range bundle.Modules {
		if mod == nil || mod.IsExternal || mod.IsCSS || mod.Program == nil {
			continue
		}
		// A package's own "use client" modules are never bundled into the
		// server bundle (packages stay external); the package boundaries of
		// the modules that import it stand in for them.
		if resolver.IsCompiledDependency(mod.Path) {
			continue
		}
		if bundler.HasUseClientDirective(mod.SourceCode) {
			clientMods[filepath.Clean(mod.Path)] = mod
			if resolved := canonicalPath(mod.Path); resolved != filepath.Clean(mod.Path) {
				clientMods[resolved] = mod
			}
		}
	}
	// A module that imports a real-React package is a client boundary too: it
	// runs in the browser on real React, whether or not it says "use client".
	realMods := map[string]bool{}
	if bundle.Resolver != nil {
		islands := b.detectReactIslandModules(bundle.Modules, bundle.Resolver.Resolve)
		for _, mod := range bundle.Modules {
			if mod == nil || mod.Program == nil || mod.IsEntry {
				continue
			}
			// A "use client" module that renders a real-React module runs on
			// real React as a whole, like one that imports the package.
			if im, ok := islands[mod.Path]; ok && (im.Direct || im.Real && bundler.HasUseClientDirective(mod.SourceCode)) {
				clientMods[filepath.Clean(mod.Path)] = mod
				realMods[filepath.Clean(mod.Path)] = true
				// The canonical spelling registered above must be real too, or
				// a symlinked root (macOS /var) builds it as a Preact boundary.
				if resolved := canonicalPath(mod.Path); resolved != filepath.Clean(mod.Path) {
					clientMods[resolved] = mod
					realMods[resolved] = true
				}
			}
		}
	}
	// As in Next.js, "use client" marks a boundary: a component module that a
	// client module imports or re-exports is client code too, even without its
	// own directive. Only modules a server component renders become boundaries.
	if len(clientMods) > 0 {
		b.addClientReached(bundle, ann, clientMods)
	}
	if errs := checkServerTree(ann, roots, clientMods, bundle.ActionImports); len(errs) > 0 {
		return nil, errs[0]
	}
	if len(clientMods) == 0 {
		return nil, nil
	}
	paths := make([]string, 0, len(clientMods))
	for p := range clientMods {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	// Every boundary's exports are checked first and its chunk and hints
	// builds are queued; the first error in path order wins, as if the
	// boundaries were built one by one.
	var out []ClientBoundary
	var reqs []clientChunkReq
	var reqAt []int
	var firstErr error
	target := starTargets(bundle)
	for _, p := range paths {
		mod := clientMods[p]
		exports, err := clientModuleExports(mod, target)
		if err != nil {
			firstErr = err
			break
		}
		if realMods[p] {
			cb := ClientBoundary{ModuleID: clientModuleID(b.Root, mod.Path), SourcePath: mod.Path, Exports: exports, Real: true}
			cb.ChunkURL = b.registerRealBoundary(cb)
			if jobs := b.realBoundaryHintsJobs(cb); len(jobs) > 0 {
				cb.pending = &boundaryJobs{hints: jobs}
			}
			out = append(out, cb)
			continue
		}
		if err := asyncClientComponentError(mod); err != nil {
			firstErr = err
			break
		}
		reqs = append(reqs, clientChunkReq{key: mod.Path, resolveDir: filepath.Dir(mod.Path), entry: clientChunkEntry(mod.Path)})
		reqAt = append(reqAt, len(out))
		out = append(out, ClientBoundary{ModuleID: clientModuleID(b.Root, mod.Path), SourcePath: mod.Path, Exports: exports})
	}
	if err := b.queueClientChunks(out, reqs, reqAt); err != nil {
		return nil, err
	}
	if firstErr != nil {
		// A chunk before the failing module may fail first.
		if err := b.awaitBoundaries(out); err != nil {
			return nil, err
		}
		return nil, firstErr
	}
	return out, nil
}

// queueClientChunks gives each boundary out[reqAt[i]] the chunk of reqs[i]:
// its URL now, and the job checking it until awaitBoundaries.
func (b *Builder) queueClientChunks(out []ClientBoundary, reqs []clientChunkReq, reqAt []int) error {
	if len(reqs) == 0 {
		return nil
	}
	infos, jobs, err := b.clientChunkJobs(reqs)
	if err != nil {
		return err
	}
	for i, at := range reqAt {
		if jobs[i] == nil {
			out[at].ChunkURL = infos[i].url
			if out[at].Spec != "" {
				stateful, err := b.chunkStateful(infos[i])
				if err != nil {
					return err
				}
				out[at].Hydrate = stateful
			}
			continue
		}
		// The job is shared by every page that asks for the same key, and it
		// registers the chunk under the request it was queued with. A package
		// boundary's key leaves out the importer's directory, so another
		// page's request may name a chunk that is never written: take the
		// URL (and the fallback build) from the job's own request.
		req := reqs[i]
		if jr, ok := jobs[i].input.(clientChunkReq); ok {
			req = jr
		}
		out[at].ChunkURL = b.islandEntryURL(b.islandEntryName(clientChunkSource(req.entry), req.resolveDir))
		out[at].pending = &boundaryJobs{req: req, chunk: jobs[i]}
	}
	return nil
}

// awaitBoundaries waits for the queued check builds of every boundary in the
// groups and applies them (a package boundary's Hydrate, a real-React
// boundary's Hints), in place. It returns the first failing chunk's error in
// group order.
func (b *Builder) awaitBoundaries(groups ...[]ClientBoundary) error {
	var jobs []*roundJob
	for _, g := range groups {
		for _, cb := range g {
			if cb.pending == nil {
				continue
			}
			if cb.pending.chunk != nil {
				jobs = append(jobs, cb.pending.chunk)
			}
			for _, j := range cb.pending.hints {
				jobs = append(jobs, j)
			}
		}
	}
	if len(jobs) > 0 {
		b.rounds.wait(jobs...)
	}
	for _, g := range groups {
		for i := range g {
			p := g[i].pending
			if p == nil {
				continue
			}
			if p.chunk != nil {
				res, _ := p.chunk.output.(*clientChunkResult)
				if res == nil {
					// The batch that held it failed outright.
					res = b.buildClientChunk(p.req)
				}
				if res.err != nil {
					return res.err
				}
				if g[i].Spec != "" {
					stateful, err := b.chunkStateful(res.info)
					if err != nil {
						return err
					}
					g[i].Hydrate = stateful
				}
			}
			if p.hints != nil {
				g[i].Hints = realBoundaryHintsOf(p.hints)
			}
			g[i].pending = nil
		}
	}
	return nil
}

// clientChunkEntry is the chunk preamble of a "use client" module.
func clientChunkEntry(path string) string {
	return "import * as __m from " + strconv.Quote(path) + ";\n"
}

// starTargets returns a lookup from an `export *` source, as written in mod,
// to the bundled module it resolves to (nil when it is not in the bundle).
func starTargets(bundle *bundler.Bundle) func(mod *bundler.Module, src string) *bundler.Module {
	byPath := map[string]*bundler.Module{}
	for _, m := range bundle.Modules {
		if m != nil && !m.IsExternal && m.Program != nil {
			byPath[filepath.Clean(m.Path)] = m
			byPath[canonicalPath(m.Path)] = m
		}
	}
	return func(mod *bundler.Module, src string) *bundler.Module {
		src = strings.Trim(src, "\"'`")
		resolved := bundle.Resolver.Resolve(mod.Path, src)
		if resolved == "" {
			return nil
		}
		if m := byPath[filepath.Clean(resolved)]; m != nil {
			return m
		}
		return byPath[canonicalPath(resolved)]
	}
}

// addClientReached adds to clientMods the component modules that a server
// module imports through a "use client" module. As in Next.js the boundary is
// the import path: `import { V } from "./viewer-barrel"` (a client module that
// re-exports V from ./viewer) makes ./viewer's V a client component for that
// server module, while a direct `import { V } from "./viewer"` stays a server
// component.
func (b *Builder) addClientReached(bundle *bundler.Bundle, ann *irtree.Annotations, clientMods map[string]*bundler.Module) {
	rendered := map[string]bool{}
	for _, src := range ann.ComponentSources {
		if src != "" {
			rendered[filepath.Clean(src)] = true
			rendered[canonicalPath(src)] = true
		}
	}
	target := starTargets(bundle)
	isClient := func(m *bundler.Module) bool {
		_, ok := clientMods[filepath.Clean(m.Path)]
		return ok
	}
	// declarer follows name through the re-exports of m to the module that
	// declares it.
	var declarer func(m *bundler.Module, name string, depth int) *bundler.Module
	declarer = func(m *bundler.Module, name string, depth int) *bundler.Module {
		if depth > 16 {
			return nil
		}
		var stars []string
		for _, stmt := range m.Program.Body {
			exp, ok := stmt.(*ast.ExportStmt)
			if !ok {
				continue
			}
			if exp.ReexportSource == "" {
				continue
			}
			if exp.StarReexport && exp.Namespace == "" {
				stars = append(stars, exp.ReexportSource)
				continue
			}
			for _, n := range exp.ReexportNamed {
				out := n.Remote
				if out == "" {
					out = n.Local
				}
				if out == name {
					if t := target(m, exp.ReexportSource); t != nil {
						return declarer(t, n.Local, depth+1)
					}
					return nil
				}
			}
		}
		for _, src := range stars {
			if t := target(m, src); t != nil {
				if d := declarer(t, name, depth+1); d != nil {
					return d
				}
			}
		}
		return m
	}
	var found []*bundler.Module
	for _, m := range bundle.Modules {
		if m == nil || m.IsExternal || m.IsCSS || m.Program == nil || isClient(m) || resolver.IsCompiledDependency(m.Path) {
			continue
		}
		for _, stmt := range m.Program.Body {
			imp, ok := stmt.(*ast.ImportStmt)
			if !ok {
				continue
			}
			c := target(m, imp.Source)
			if c == nil || !isClient(c) {
				continue
			}
			names := []string{}
			if imp.Default != "" {
				names = append(names, "default")
			}
			for _, n := range imp.Named {
				names = append(names, n.Remote)
			}
			for _, n := range names {
				if d := declarer(c, n, 0); d != nil && d != c && rendered[filepath.Clean(d.Path)] {
					found = append(found, d)
				}
			}
		}
	}
	for _, d := range found {
		clientMods[filepath.Clean(d.Path)] = d
		if resolved := canonicalPath(d.Path); resolved != filepath.Clean(d.Path) {
			clientMods[resolved] = d
		}
	}
}

// clientModuleExports lists the export names of a "use client" module. An
// `export *` is expanded into the runtime exports of its target (types are
// already erased by the parser); it is an error only when the target cannot
// be found in the bundle, such as a package, or is part of a cycle.
func clientModuleExports(mod *bundler.Module, target func(*bundler.Module, string) *bundler.Module) ([]string, error) {
	seen := map[string]bool{}
	var names []string
	add := func(n string) {
		if n != "" && !seen[n] {
			seen[n] = true
			names = append(names, n)
		}
	}
	visiting := map[*bundler.Module]bool{}
	var collect func(m *bundler.Module, star bool) error
	collect = func(m *bundler.Module, star bool) error {
		visiting[m] = true
		defer delete(visiting, m)
		for _, stmt := range m.Program.Body {
			exp, ok := stmt.(*ast.ExportStmt)
			if !ok {
				continue
			}
			if exp.Default {
				if !star {
					add("default")
				}
				continue
			}
			if exp.StarReexport && exp.Namespace == "" {
				t := target(m, exp.ReexportSource)
				if t == nil || visiting[t] {
					src := strings.Trim(exp.ReexportSource, "\"'`")
					return diag.New(m.Path, exp.Position.Line, exp.Position.Col,
						"`export *` in a \"use client\" module cannot be wrapped as a client boundary",
						"list the re-exported names explicitly: export { A, B } from \""+src+"\"",
						lineOf(m.SourceCode, exp.Position.Line)).WithCode(diag.CodeClientBoundaryExports)
				}
				if err := collect(t, true); err != nil {
					return err
				}
				continue
			}
			add(exp.Namespace)
			switch d := exp.Declaration.(type) {
			case *ast.FnDecl:
				add(d.Name)
			case *ast.ClassDecl:
				add(d.Name)
			case *ast.VarStmt:
				for _, v := range d.Decls {
					add(v.Name)
				}
			}
			for _, n := range exp.Named {
				if n.Remote != "" {
					add(n.Remote)
				} else {
					add(n.Local)
				}
			}
			for _, n := range exp.ReexportNamed {
				if n.Remote != "" {
					add(n.Remote)
				} else {
					add(n.Local)
				}
			}
		}
		return nil
	}
	if err := collect(mod, false); err != nil {
		return nil, err
	}
	return names, nil
}

// asyncClientComponentError rejects an async function component declared in a
// "use client" module: client components run in the browser and cannot await.
func asyncClientComponentError(mod *bundler.Module) error {
	var bad *ast.FnDecl
	check := func(fn *ast.FnDecl) {
		if bad == nil && fn != nil && fn.Async && fn.Name != "" && fn.Name[0] >= 'A' && fn.Name[0] <= 'Z' {
			bad = fn
		}
	}
	for _, stmt := range mod.Program.Body {
		switch s := stmt.(type) {
		case *ast.FnDecl:
			check(s)
		case *ast.ExportStmt:
			if fn, ok := s.Declaration.(*ast.FnDecl); ok {
				check(fn)
			}
		}
	}
	if bad == nil {
		return nil
	}
	return diag.New(mod.Path, bad.Position.Line, bad.Position.Col,
		"client component "+bad.Name+" is async; components in a \"use client\" module cannot be async",
		"remove \"use client\" to make "+bad.Name+" a server component, or fetch the data in a server component and pass it as props",
		lineOf(mod.SourceCode, bad.Position.Line)).WithCode(diag.CodeAsyncClientComponent)
}

// clientOnlyHooks are React hooks and Krate primitives that need a client
// runtime. useMemo, useCallback, useId and use() also work in React server
// components and are allowed.
var clientOnlyHooks = map[string]bool{
	"useState": true, "useReducer": true, "useEffect": true, "useLayoutEffect": true,
	"useInsertionEffect": true, "useRef": true, "useContext": true, "useTransition": true,
	"useDeferredValue": true, "useOptimistic": true, "useActionState": true, "useFormStatus": true,
	"useSyncExternalStore": true, "useImperativeHandle": true,
	"createSignal": true, "createEffect": true, "createMemo": true, "createResource": true,
	"createStore": true, "onMount": true, "onCleanup": true,
}

var (
	eventAttrRe = regexp.MustCompile(`^on[A-Z]`)
	reactHookRe = regexp.MustCompile(`\b(use[A-Z]\w*)\s*\(`)
)

// checkServerTree walks the components rendered by the server roots (async
// components and an async page), stopping at "use client" modules, and
// reports client-only APIs and function props crossing into a client
// component.
func checkServerTree(ann *irtree.Annotations, roots []string, clientMods map[string]*bundler.Module, actions map[string]string) []error {
	var errs []error
	visited := map[string]bool{}
	isClient := func(name string) bool {
		_, ok := clientMods[filepath.Clean(ann.ComponentSources[name])]
		return ok
	}
	at := func(name string, pos ast.Pos, code diag.Code, msg, hint string) {
		file := ann.ComponentSources[name]
		if file == "" {
			file = ann.SourceFile
		}
		errs = append(errs, diag.New(file, pos.Line, pos.Col, msg, hint, lineOf(ann.ComponentRaw[name], pos.Line)).WithCode(code))
	}
	var visit func(name string)
	visit = func(name string) {
		if visited[name] {
			return
		}
		visited[name] = true
		fn := ann.Functions[name]
		if fn == nil || isClient(name) || resolver.IsCompiledDependency(filepath.ToSlash(ann.ComponentSources[name])) {
			return
		}
		file := filepath.Base(ann.ComponentSources[name])
		addClient := "add \"use client\" at the top of the module that defines it (" + file + ") to make it a client component, or move the interactive part into one"
		var children []string
		irtree.WalkAST(fn.Body, func(n any) bool {
			switch x := n.(type) {
			case *ast.CallExpr:
				if id, ok := x.Callee.(*ast.Identifier); ok && isClient(id.Name) && !declaresName(fn, id.Name) {
					at(name, x.Position, diag.CodeClientExportCalled,
						"`"+id.Name+"` is exported from a client module ("+filepath.Base(ann.ComponentSources[id.Name])+") and cannot be called on the server; "+name+" runs there",
						"move "+id.Name+" into a module without \"use client\" and import it from both sides; a server component can only render a client export (<"+id.Name+" />) or pass it to a client component")
				}
				if hook := calleeName(x.Callee); clientOnlyHooks[hook] {
					// The bundler lowers React hooks to Krate primitives
					// (useState -> createSignal); name what the author wrote.
					if m := reactHookRe.FindStringSubmatch(lineOf(ann.ComponentRaw[name], x.Position.Line)); m != nil && clientOnlyHooks[m[1]] {
						hook = m[1]
					}
					at(name, x.Position, diag.CodeClientAPIInServer,
						hook+"() in "+name+", which renders as a server component; it only works in a client component", addClient)
				}
			case *ast.JSXElement:
				if x.Opening == nil {
					return true
				}
				tag := x.Opening.Name
				if fnTag := ann.Functions[tag]; fnTag != nil {
					children = append(children, tag)
					if isClient(tag) {
						checkClientCallSite(x, tag, name, at)
					}
					return true
				}
				if tag != "" && tag[0] >= 'a' && tag[0] <= 'z' {
					for _, a := range x.Opening.Attributes {
						if a == nil || a.Spread || !eventAttrRe.MatchString(a.Name) || a.Value == nil {
							continue
						}
						if id, ok := a.Value.(*ast.Identifier); ok && actions[id.Name] != "" {
							continue
						}
						at(name, a.Position, diag.CodeClientAPIInServer,
							"event handler "+a.Name+" on <"+tag+"> in "+name+", which renders as a server component; server-rendered HTML has no event handlers", addClient)
					}
				}
			}
			return true
		})
		for _, c := range children {
			visit(c)
		}
	}
	for _, r := range roots {
		visit(r)
	}
	return errs
}

// declaresName reports whether fn binds name itself (a parameter or a local
// declaration), which then shadows an import of the same name.
func declaresName(fn *ast.FnDecl, name string) bool {
	for _, p := range fn.Params {
		if p != nil && (p.Name == name || strings.Contains(p.Pattern, name)) {
			return true
		}
	}
	found := false
	bound := func(d *ast.VarDecl) {
		if d == nil {
			return
		}
		if d.Name == name || d.RestName == name || strings.Contains(d.Pattern, name) {
			found = true
		}
		for _, nm := range d.Names {
			if nm == name {
				found = true
			}
		}
	}
	irtree.WalkAST(fn.Body, func(n any) bool {
		switch d := n.(type) {
		case *ast.VarStmt:
			for _, v := range d.Decls {
				bound(v)
			}
		case *ast.VarDecl:
			bound(d)
		case *ast.FnDecl:
			if d.Name == name {
				found = true
			}
		}
		return !found
	})
	return found
}

// checkClientCallSite reports function-literal props passed from a server
// component to a client component. Values only known at request time are
// checked by clientBoundary when the page renders.
func checkClientCallSite(el *ast.JSXElement, tag, owner string, at func(string, ast.Pos, diag.Code, string, string)) {
	for _, a := range el.Opening.Attributes {
		if a == nil || a.Spread || a.Value == nil {
			continue
		}
		if fn, ok := a.Value.(*ast.ArrowFn); ok {
			pos := a.Position
			if pos.Line == 0 {
				pos = fn.Position
			}
			at(owner, pos, diag.CodeClientPropNotSerializable,
				"prop `"+a.Name+"` passed from server component "+owner+" to client component <"+tag+"> is a function; props crossing to the client must be JSON-serializable",
				"define the handler inside the client component, or pass a server action (a \"use server\" export)")
		}
	}
}

// calleeName returns the hook-ish name of a call: `useState` or
// `React.useState` -> "useState".
func calleeName(e ast.Expr) string {
	switch c := e.(type) {
	case *ast.Identifier:
		return c.Name
	case *ast.MemberExpr:
		if p, ok := c.Property.(*ast.Identifier); ok && !c.Computed {
			return p.Name
		}
	}
	return ""
}

// clientChunkInfo is a built hydration chunk: its URL and whether its code
// has state or effects (a stateless package component needs no island).
type clientChunkInfo struct {
	url      string
	stateful bool
	// inputs are the source files that ended up in the chunk (absolute,
	// cleaned); tree shaking leaves out the rest of the import graph.
	inputs map[string]bool
	// sig and stamps let a dev rebuild keep the chunk: the request it was
	// built for, and the state of every file the build read.
	sig    string
	stamps stampSet
	config stampSet
	// split is set for a chunk checked in a code-split batch: stateful,
	// inputs and the chunk text are then upper bounds of a separate build's.
	split *splitChunkFacts
}

// splitChunkFacts are what a code-split check batch knows for certain about
// one chunk (see esbuildBatchSplit). The entry's own output holds code a
// separate build keeps; its other files hold that code and maybe code only
// other boundaries use. A use whose answer depends on the difference asks
// exactChunk, which checks the chunk on its own once.
type splitChunkFacts struct {
	req clientChunkReq
	// statefulSure is set when stateful is exact: no file matches, or the
	// entry's own output does.
	statefulSure bool
	ownInputs    map[string]bool
	ownText      string

	// mu guards done and the exact check's result below.
	mu    sync.Mutex
	done  bool
	exact clientChunkInfo
	text  string
	err   error
}

var statefulHookRe = regexp.MustCompile(`\buse(State|Reducer|Effect|LayoutEffect|InsertionEffect|SyncExternalStore|ImperativeHandle|Transition|Optimistic|ActionState|DeferredValue)\b|\b(Pure)?Component\b`)

// boundaryLinkShim is the global <Link> of a page, for components that run on
// Preact: client modules and the server render of package boundaries have no
// Krate runtime, so a bare `Link` would be a ReferenceError there. It renders
// the same anchor as the server renderer, which the router picks up.
const boundaryLinkShim = `import { h } from "preact";
function basePath() {
  var c = globalThis.__KRATE_CFG__;
  if (!c && typeof document !== "undefined") {
    var el = document.getElementById("__krate_cfg__");
    if (el) { try { c = globalThis.__KRATE_CFG__ = JSON.parse(el.textContent || "{}"); } catch (e) {} }
  }
  var bp = (c && c.basePath) || "";
  if (!bp || bp === "/") return "";
  return "/" + bp.replace(/^\/+|\/+$/g, "");
}
export function Link(props) {
  var href = props.href || "";
  var out = {};
  for (var k in props) {
    if (k !== "href" && k !== "prefetch" && k !== "replace" && k !== "scroll" && k !== "basePath" && k !== "children" && k !== "className") out[k] = props[k];
  }
  if (href) {
    var bp = basePath();
    out.href = bp && props.basePath !== false && href.charCodeAt(0) === 47 && href.charCodeAt(1) !== 47 ? bp + href : href;
  }
  var external = props.basePath === false || /^(https?:|mailto:|tel:|#|javascript:)/.test(href) || href.indexOf("//") === 0 || props.target === "_blank" || props.download !== undefined;
  if (external) {
    out["data-krate-external"] = "";
    if (props.basePath === false) out["data-krate-no-base"] = "";
  } else {
    out["data-krate-link"] = "";
    if (props.prefetch !== false) out["data-prefetch"] = props.prefetch === "viewport" ? "viewport" : "";
    if (props.replace) out["data-krate-replace"] = "";
    if (props.scroll === false) out["data-krate-scroll"] = "false";
  }
  if (props.target === "_blank" && props.rel === undefined) out.rel = "noopener noreferrer";
  if (props.className) out.class = props.className;
  return h("a", out, props.children);
}
`

// boundaryShims writes the globals shim file (once per content) next to the
// server bundles and returns its path for esbuild's inject option.
func boundaryShims(dotKrate string) (string, error) {
	if err := os.MkdirAll(dotKrate, 0755); err != nil {
		return "", err
	}
	path := filepath.Join(dotKrate, "boundary-link."+hashContent([]byte(boundaryLinkShim))+".mjs")
	if err := fsutil.WriteFileIfChanged(path, []byte(boundaryLinkShim), 0644); err != nil {
		return "", err
	}
	return path, nil
}

// clientChunkSig is what a chunk's check build depends on besides files.
func clientChunkSig(r clientChunkReq) string {
	return r.key + "\x00" + r.resolveDir + "\x00" + r.entry
}

// clientChunkReusable reports whether a chunk kept from an earlier dev
// rebuild is still what building r now would give.
func (b *Builder) clientChunkReusable(info clientChunkInfo, r clientChunkReq) bool {
	// A chunk always reads its own module, so a stamp set without files means
	// the inputs were not recorded: nothing shows the chunk is still current.
	if !b.DevMode || len(info.stamps) == 0 || info.sig != clientChunkSig(r) {
		return false
	}
	return info.stamps.fresh() && info.config.fresh()
}

// dropClientChunks forgets every kept client chunk.
func (b *Builder) dropClientChunks() {
	b.clientChunkMu.Lock()
	b.clientChunks = nil
	b.clientChunkText = nil
	b.clientChunkMu.Unlock()
}

// clientChunkReq names the hydration chunk of one boundary: key identifies it
// for the build, entry is the module preamble that binds `__m`, the exports
// components are looked up in by name (dotted for members of a namespace
// export such as Radix's Dialog.Root).
type clientChunkReq struct {
	key, resolveDir, entry string
}

// clientChunkResult is the outcome of a client chunk's check build.
type clientChunkResult struct {
	info clientChunkInfo
	err  error
}

// clientChunkJobs returns the chunks of reqs the build already has and a job
// for each other one (nil where the chunk is known).
func (b *Builder) clientChunkJobs(reqs []clientChunkReq) ([]clientChunkInfo, []*roundJob, error) {
	infos := make([]clientChunkInfo, len(reqs))
	jobs := make([]*roundJob, len(reqs))
	first := -1
	b.clientChunkMu.Lock()
	for i, r := range reqs {
		if info, ok := b.clientChunks[r.key]; ok && b.clientChunkReusable(info, r) {
			infos[i] = info
			continue
		}
		jobs[i] = b.roundJobs.job("client-chunk", r.key, r, b.runClientChunkJobs)
		b.rounds.enqueue(jobs[i])
		if first < 0 {
			first = i
		}
	}
	b.clientChunkMu.Unlock()
	if first < 0 {
		return infos, jobs, nil
	}
	if _, err := b.ensurePreactShared(); err != nil {
		return nil, nil, diag.Diagnostic{
			File:    reqs[first].key,
			Message: "client component runtime could not be built: " + err.Error(),
			Hint:    "\"use client\" components inside server components hydrate with Preact; install it with `pnpm add preact`, then rebuild",
		}.WithCode(diag.CodeIslandAsset)
	}
	return infos, jobs, nil
}

// clientChunkSource is the entry module of a boundary's hydration chunk.
func clientChunkSource(entry string) string {
	return "import { h, hydrate, render as __krateRender } from \"preact\";\nimport { Suspense as __KrateSuspense } from \"preact/compat\";\n" + entry +
		clientReviveJS + clientActionCallJS + preactSlotJS + `
function __resolve(name) {
  return name.split(".").reduce(function (o, k) { return o == null ? o : o[k]; }, __m);
}
export function component(name) { return __resolve(name); }
// mount hydrates the island the loader found: the nodes between its two
// comments (see clientIslandLoaderJS), under a stand-in parent that confines
// Preact to them. It returns the island's handle: update re-renders the same
// root with new server props (pool holds the nodes of server children the
// props now refer to), unmount renders nothing into it, and slots lists the
// server children adopted from the document, in document order.
export async function mount(island) {
  var name = island.export;
  var C = __resolve(name);
  if (typeof C !== "function") throw new Error("client module " + island.module + " has no component export " + name);
  var loaded = {};
  loaded[island.module + "#" + name] = C;
  function load(raw) {
    var miss = __krateRootRefs(raw).filter(function (r) { return !loaded[r.m + "#" + r.e]; });
    return miss.length ? Promise.all(miss.map(function (r) {
      return import(r.s).then(function (m) { loaded[r.m + "#" + r.e] = m.component(r.e); });
    })) : null;
  }
  await load(island.props || {});
  var slots = [];
  var pool = __krateServerSlots(island, slots);
  var root = __krateRangeRoot(island);
  if (island.ids) root.__krateIds = island.ids;
  function show(raw, hydrating) {
    __krateSlotPool = pool;
    var props;
    try {
      props = __krateRevive(raw, h, __krateCallAction, function (m, e) { return loaded[m + "#" + e]; });
    } finally {
      __krateSlotPool = null;
    }
    // The root is a Suspense boundary, as in React: a lazy() or use() with no
    // boundary of its own waits there (keeping the server HTML) instead of
    // throwing its promise out of the island.
    (hydrating ? hydrate : __krateRender)(__krateInScope(island.start, h(__KrateSuspense, { fallback: null }, h(C, props))), root);
  }
  show(island.props || {}, true);
  return {
    slots: slots.map(function (s) { return { id: s.id, html: s.html, set: (pool[s.id] || [])[0] || [] }; }),
    update: function (raw, more) {
      if (more) for (var k in more) pool[k] = more[k];
      island.props = raw;
      var wait = load(raw);
      if (!wait) return void show(raw, false);
      return wait.then(function () { if (island.props === raw) show(raw, false); });
    },
    unmount: function () { __krateRender(null, root); },
    // sync brings the slots' sets up to date with nested islands that changed
    // their nodes (the router calls it after a swap); the next render places
    // them.
    sync: function () { for (var k in pool) pool[k].forEach(function (set) { __krateSlotSync(set); }); },
  };
}
`
}

// clientChunkOptions are the esbuild options of a client chunk's check build.
func (b *Builder) clientChunkOptions(guard *unavailableGuard) (api.BuildOptions, error) {
	shim, err := boundaryShims(filepath.Join(b.Cfg.OutDir, ".krate"))
	if err != nil {
		return api.BuildOptions{}, err
	}
	opts := api.BuildOptions{
		AbsWorkingDir:     b.Root,
		Bundle:            true,
		Write:             false,
		MinifyWhitespace:  b.shouldMinifyJS(),
		MinifyIdentifiers: b.shouldMinifyJS(),
		MinifySyntax:      b.shouldMinifyJS(),
		Format:            api.FormatESModule,
		Platform:          api.PlatformBrowser,
		Target:            api.ES2020,
		JSX:               api.JSXAutomatic,
		JSXImportSource:   "preact",
		Define:            b.clientEnvDefines(),
		TsconfigRaw:       clientTsconfigRaw(b.Root),
		Alias:             islandAlias(b.Root, false),
		External:          []string{"preact", "preact/compat", "preact/hooks", "preact/jsx-runtime"},
		Inject:            []string{shim},
		Metafile:          true,
		Plugins:           []api.Plugin{purePrunePlugin(b.Root), guard.plugin(), clientOnlyMarkersPlugin(true), actionServerBundlePlugin(b.importResolver(), b.actionModules(), b.Cfg.BaseURLPath()), reactDOMHintsPlugin(b.Root), reactCompatPlugin(b.Root), routerHooksPlugin(b.Root), threeIslandPlugin(), konvaIslandPlugin()},
		LogLevel:          api.LogLevelSilent,
	}
	if err := withContextKeys(&opts, b.Root, filepath.Join(b.Cfg.OutDir, ".krate")); err != nil {
		return api.BuildOptions{}, err
	}
	withNamespaceImports(&opts)
	return opts, nil
}

// runClientChunkJobs check-builds the client chunks of jobs in one esbuild
// call. When that build fails, each chunk is built on its own so the error
// names the boundary that has it.
func (b *Builder) runClientChunkJobs(jobs []*roundJob) {
	stop := b.phase("batch:client-chunks")
	defer stop()
	// esbuild holds every entry's linked output until the call returns, so a
	// batch of hundreds would hold gigabytes: build at most a few dozen per
	// call. A call resolves imports one at a time (esbuild's resolver takes a
	// lock), so a few calls run at once.
	runSubBatches(jobs, clientChunkBatchSize, 3, b.runClientChunkBatch)
}

// clientChunkBatchSize is the most client chunk checks one esbuild call runs.
const clientChunkBatchSize = 64

// runSubBatches runs run on consecutive slices of at most size jobs, up to
// parallel of them at once.
func runSubBatches(jobs []*roundJob, size, parallel int, run func([]*roundJob)) {
	pool := newWorkerPool(parallel)
	var wg sync.WaitGroup
	for len(jobs) > 0 {
		n := min(size, len(jobs))
		part := jobs[:n]
		jobs = jobs[n:]
		wg.Add(1)
		pool.acquire()
		go func() {
			defer wg.Done()
			defer pool.release()
			run(part)
		}()
	}
	wg.Wait()
}

func (b *Builder) runClientChunkBatch(jobs []*roundJob) {
	b.runClientChunkBatchWith(jobs, !b.noClientChunkSplit)
}

// runClientChunkBatchWith checks jobs in one batch, with code splitting when
// split is set. Entries a split batch cannot stand in for (one that reaches
// an import(), or that wrapping for another entry changes) are checked again
// together in an unsplit batch, and the ones that batch flags on their own.
func (b *Builder) runClientChunkBatchWith(jobs []*roundJob, split bool) {
	guard := b.newUnavailableGuard()
	opts, err := b.clientChunkOptions(guard)
	if err == nil && len(jobs) > 1 {
		entries := make([]batchEntry, len(jobs))
		for i, j := range jobs {
			r := j.input.(clientChunkReq)
			entries[i] = batchEntry{Source: clientChunkSource(r.entry), ResolveDir: r.resolveDir, Sourcefile: "krate-client-boundary.js", Loader: api.LoaderJS}
		}
		var outs []batchOutput
		var res api.BuildResult
		if split {
			outs, res = esbuildBatchSplit(opts, entries)
		} else {
			outs, res = esbuildBatch(opts, entries)
		}
		if len(res.Errors) == 0 {
			var unsplit []*roundJob
			for i, j := range jobs {
				r := j.input.(clientChunkReq)
				files := outs[i].Files
				if split && outs[i].Unsafe && len(files) > 0 {
					unsplit = append(unsplit, j)
					continue
				}
				if outs[i].Unsafe {
					b.Prof.Add("batch:client-chunks-rebuilt", 0)
				}
				if len(files) == 0 || outs[i].Unsafe || len(guard.errors(files)) > 0 {
					j.output = b.buildClientChunk(r)
					continue
				}
				if split {
					j.output = b.recordSplitClientChunk(r, entries[i].Source, outs[i])
					continue
				}
				j.output = b.recordClientChunk(r, entries[i].Source, batchJS(files), outs[i].Metafile, outs[i].Inputs)
			}
			if len(unsplit) > 0 {
				b.Prof.Add("batch:client-chunks-unsplit", 0)
				b.runClientChunkBatchWith(unsplit, false)
			}
			return
		}
	}
	for _, j := range jobs {
		j.output = b.buildClientChunk(j.input.(clientChunkReq))
	}
}

// batchJS is the code of an entry of a batched build: its entry output, then
// the chunks it imports (split builds), newline separated. The checks read
// only facts the text holds as a whole: hook names, class name strings,
// unavailable-module markers.
func batchJS(files []api.OutputFile) []byte {
	if len(files) == 1 {
		return files[0].Contents
	}
	var out []byte
	for _, f := range files {
		if !strings.HasSuffix(f.Path, ".js") {
			continue
		}
		if len(out) > 0 {
			out = append(out, '\n')
		}
		out = append(out, f.Contents...)
	}
	return out
}

// buildClientChunk check-builds one client chunk on its own.
func (b *Builder) buildClientChunk(r clientChunkReq) *clientChunkResult {
	src := clientChunkSource(r.entry)
	guard := b.newUnavailableGuard()
	opts, err := b.clientChunkOptions(guard)
	if err != nil {
		return &clientChunkResult{err: err}
	}
	opts.Stdin = &api.StdinOptions{
		Contents:   src,
		ResolveDir: r.resolveDir,
		Sourcefile: "krate-client-boundary.js",
		Loader:     api.LoaderJS,
	}
	result := esbuildBuild(opts)
	result.Errors = append(result.Errors, guard.resultErrors(result)...)
	if len(result.Errors) > 0 || len(result.OutputFiles) == 0 {
		return &clientChunkResult{err: clientBuildError(b.Root, r.key, result.Errors)}
	}
	return b.recordClientChunk(r, src, result.OutputFiles[0].Contents, result.Metafile, nil)
}

// recordSplitClientChunk registers a chunk checked in a code-split batch,
// with upper-bound facts (see splitChunkFacts).
func (b *Builder) recordSplitClientChunk(r clientChunkReq, src string, o batchOutput) *clientChunkResult {
	own := batchJS(o.Files[:o.Own])
	out := batchJS(o.Files)
	f := &splitChunkFacts{req: r, ownInputs: metafileInputs(b.Root, o.OwnMetafile), ownText: string(own)}
	f.statefulSure = !statefulHookRe.Match(out) || statefulHookRe.Match(own)
	return b.recordClientChunkFacts(r, src, out, o.Metafile, o.Inputs, f)
}

// exactChunk returns info and the chunk text as a separate build gives them.
// A chunk of a split batch gets its own check once. The checks the pages ask
// for are batched like the other client chunk jobs (runExactChunkJobs): one
// esbuild call per round instead of one per chunk. Every caller waits on the
// round job itself, never on a lock, so a page that asks while another page's
// check is queued counts as waiting and the round can start.
func (b *Builder) exactChunk(info clientChunkInfo) (clientChunkInfo, string, error) {
	f := info.split
	if f == nil {
		b.clientChunkMu.Lock()
		text := b.clientChunkText[info.url]
		b.clientChunkMu.Unlock()
		return info, text, nil
	}
	f.mu.Lock()
	done := f.done
	f.mu.Unlock()
	if !done {
		// Keyed by f, not only the chunk: f belongs to one split check, as the
		// sync.Once it replaces did, so a later check of the chunk (another
		// build on this Builder, other options) never reuses this result.
		j := b.roundJobs.job("client-chunk-exact", fmt.Sprintf("%s\x00%p", f.req.key, f), f.req, b.runExactChunkJobs)
		b.rounds.wait(j)
		res, _ := j.output.(*clientChunkResult)
		if res == nil {
			// The job ran in a batch that failed outright.
			res = b.checkClientChunkAlone(f.req)
		}
		f.mu.Lock()
		if !f.done {
			f.done = true
			if res.err != nil {
				f.err = res.err
			} else {
				f.exact = res.info
				b.clientChunkMu.Lock()
				f.text = b.clientChunkText[res.info.url]
				b.clientChunkMu.Unlock()
			}
		}
		f.mu.Unlock()
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.exact, f.text, f.err
}

// runExactChunkJobs gives the chunks of jobs their own checks: unsplit
// batches, which build every entry as a separate build would, of at most
// clientChunkBatchSize entries, a few at once (see runClientChunkJobs).
func (b *Builder) runExactChunkJobs(jobs []*roundJob) {
	stop := b.phase("batch:client-chunks-exact-checks")
	defer stop()
	runSubBatches(jobs, clientChunkBatchSize, 3, b.runExactChunkBatch)
}

// runExactChunkBatch checks jobs in one unsplit esbuild call. An entry the
// batch cannot stand in for (one another entry's wrapping changes), or that
// fails, is checked alone as before, so its error names its own boundary.
func (b *Builder) runExactChunkBatch(jobs []*roundJob) {
	for range jobs {
		b.Prof.Add("batch:client-chunks-exact", 0)
	}
	if len(jobs) == 1 {
		jobs[0].output = b.checkClientChunkAlone(jobs[0].input.(clientChunkReq))
		return
	}
	guard := b.newUnavailableGuard()
	opts, err := b.clientChunkOptions(guard)
	if err == nil {
		entries := make([]batchEntry, len(jobs))
		for i, j := range jobs {
			r := j.input.(clientChunkReq)
			entries[i] = batchEntry{Source: clientChunkSource(r.entry), ResolveDir: r.resolveDir, Sourcefile: "krate-client-boundary.js", Loader: api.LoaderJS}
		}
		outs, res := esbuildBatch(opts, entries)
		if len(res.Errors) == 0 {
			for i, j := range jobs {
				r := j.input.(clientChunkReq)
				files := outs[i].Files
				if outs[i].Unsafe || len(files) == 0 || len(guard.errors(files)) > 0 {
					j.output = b.checkClientChunkAlone(r)
					continue
				}
				j.output = b.recordClientChunk(r, entries[i].Source, batchJS(files), outs[i].Metafile, outs[i].Inputs)
			}
			return
		}
	}
	for _, j := range jobs {
		j.output = b.checkClientChunkAlone(j.input.(clientChunkReq))
	}
}

// checkClientChunkAlone checks one chunk as an unsplit batch does, falling
// back to buildClientChunk for its errors.
func (b *Builder) checkClientChunkAlone(r clientChunkReq) *clientChunkResult {
	guard := b.newUnavailableGuard()
	opts, err := b.clientChunkOptions(guard)
	if err != nil {
		return &clientChunkResult{err: err}
	}
	src := clientChunkSource(r.entry)
	outs, res := esbuildBatch(opts, []batchEntry{{Source: src, ResolveDir: r.resolveDir, Sourcefile: "krate-client-boundary.js", Loader: api.LoaderJS}})
	if len(res.Errors) > 0 || len(outs[0].Files) == 0 || len(guard.errors(outs[0].Files)) > 0 {
		return b.buildClientChunk(r)
	}
	return b.recordClientChunk(r, src, batchJS(outs[0].Files), outs[0].Metafile, outs[0].Inputs)
}

// chunkStateful is info.stateful as a separate build gives it.
func (b *Builder) chunkStateful(info clientChunkInfo) (bool, error) {
	if info.split == nil || info.split.statefulSure {
		return info.stateful, nil
	}
	exact, _, err := b.exactChunk(info)
	return exact.stateful, err
}

// recordClientChunk registers a checked client chunk with the build.
// inputs lists the files the check build loaded when its metafile cannot (a
// batched build's has outputs only); nil reads them from the metafile.
func (b *Builder) recordClientChunk(r clientChunkReq, src string, out []byte, metafile string, inputs []string) *clientChunkResult {
	return b.recordClientChunkFacts(r, src, out, metafile, inputs, nil)
}

func (b *Builder) recordClientChunkFacts(r clientChunkReq, src string, out []byte, metafile string, inputs []string, split *splitChunkFacts) *clientChunkResult {
	// This build checks the module and tells whether it holds state. The
	// chunk the page loads is built with the island entries (writeIslandBundles),
	// so a library used by a boundary, an island and a layout is one module in
	// the browser: sonner's toast() reaches the Toaster of another chunk.
	name := b.islandEntryName(src, r.resolveDir)
	b.setIslandEntries("client-boundary\x00"+r.key, []islandEntry{{Name: name, Source: src, ResolveDir: r.resolveDir, Boundary: true}})
	info := clientChunkInfo{url: b.islandEntryURL(name), stateful: statefulHookRe.Match(out), inputs: metafileInputs(b.Root, metafile), split: split}
	if b.DevMode {
		info.sig = clientChunkSig(r)
		if inputs == nil {
			inputs = metafileAllInputs(b.Root, metafile)
		}
		info.stamps = b.stampInputs(inputs)
		info.config = b.projectConfigStamp()
	}
	b.clientChunkMu.Lock()
	if b.clientChunkText == nil {
		b.clientChunkText = map[string]string{}
	}
	b.clientChunkText[info.url] = string(out)
	if b.clientChunks == nil {
		b.clientChunks = map[string]clientChunkInfo{}
	}
	b.clientChunks[r.key] = info
	b.clientChunkMu.Unlock()
	return &clientChunkResult{info: info}
}

const serverOnlyMarker = "krate: server-only module in client bundle"

// clientOnlyMarkersPlugin resolves the `server-only` and `client-only` marker
// packages. In a client bundle `server-only` is an error naming the importer;
// everything else resolves to an empty module (the packages themselves need
// bundler conditions Krate does not use).
func clientOnlyMarkersPlugin(client bool) api.Plugin {
	return api.Plugin{
		Name: "krate-server-client-only",
		Setup: func(pb api.PluginBuild) {
			pb.OnResolve(api.OnResolveOptions{Filter: `^(server-only|client-only)$`}, func(args api.OnResolveArgs) (api.OnResolveResult, error) {
				if client && args.Path == "server-only" {
					return api.OnResolveResult{}, fmt.Errorf("%s: %s imports \"server-only\"", serverOnlyMarker, args.Importer)
				}
				return api.OnResolveResult{Path: args.Path, Namespace: "krate-marker"}, nil
			})
			pb.OnLoad(api.OnLoadOptions{Filter: `.*`, Namespace: "krate-marker"}, func(api.OnLoadArgs) (api.OnLoadResult, error) {
				empty := "export {};"
				return api.OnLoadResult{Contents: &empty, Loader: api.LoaderJS}, nil
			})
		},
	}
}

// clientBuildError converts esbuild errors of a client boundary build into a
// diagnostic, recognizing the server-only marker.
func clientBuildError(root, path string, errs []api.Message) error {
	var msgs []string
	file, line, col := path, 0, 0
	located := false
	for _, e := range errs {
		if strings.Contains(e.Text, serverOnlyMarker) {
			importer := strings.TrimSpace(strings.TrimSuffix(strings.SplitN(e.Text, serverOnlyMarker+": ", 2)[1], "imports \"server-only\""))
			return diag.New(path, 0, 0,
				"client component module "+filepath.Base(path)+" reaches a server-only module ("+importer+" imports \"server-only\")",
				"import the server-only module from a server component and pass the data to the client component as props", "").WithCode(diag.CodeServerOnlyInClient)
		}
		if e.Location != nil {
			rel := esbuildRelFile(root, root, e.Location.File)
			msgs = append(msgs, fmt.Sprintf("%s:%d:%d: %s", rel, e.Location.Line, e.Location.Column+1, e.Text))
			if !located {
				// Point at the module esbuild found the problem in, which can be
				// a package the client component imports.
				file, line, col, located = rel, e.Location.Line, e.Location.Column+1, true
			}
		} else {
			msgs = append(msgs, e.Text)
		}
	}
	joined := strings.Join(msgs, "\n")
	hint := "check the client component and its imports, then rebuild"
	if strings.Contains(joined, "Node built-ins do not exist in the browser") {
		hint = builtinFixHint + "; `krate dev --verbose` lists every import of a built-in"
	} else if strings.Contains(joined, "preact") {
		hint = "\"use client\" components inside server components hydrate with Preact; install it with `pnpm add preact`"
	}
	return diag.Diagnostic{File: file, Line: line, Col: col, Message: "client component chunk could not be built:\n" + joined, Hint: hint}.WithCode(diag.CodeIslandAsset)
}

// clientSSRImpls builds the server-side render module of each "use client"
// module (Preact + preact-render-to-string, React aliased to preact/compat)
// once per CompileServerBundles call, under server-bundles/client/.
type clientSSRImpls struct {
	root, bundleDir string
	// actions are the "use server" modules; a client module that imports one
	// gets the action proxy instead of the server source.
	actions  map[string]map[string]string
	res      *bundler.ImportResolver
	basePath string
	mu       sync.Mutex
	done     map[string]*clientSSRImpl
	// memo shares the resolutions of the modules built with one set of
	// options (see prebuild).
	memo resolveMemo
	// boundaries are the boundaries of the server bundles the impls are for.
	boundaries []ClientBoundary
	// separate builds every module on its own instead of in one combined
	// build (tests compare the two); shareless builds every page server
	// bundle as if it were alone.
	separate, shareless bool
	// combinedInputs lists the files the combined build read, set when it
	// succeeded (see devmemo.go).
	combinedInputs []string
}

// clientSSRImpl is the build of one module's server render module. Its
// builder closes ready once err is set.
type clientSSRImpl struct {
	ready chan struct{}
	err   error
}

// finish sets the result of the build and releases its waiters. A panic in
// build becomes the error, so waiters are never left blocked.
func (e *clientSSRImpl) finish(build func() error) {
	defer close(e.ready)
	e.err = safe.Run(build)
}

func (c *clientSSRImpls) implPath(cb ClientBoundary) string {
	return filepath.Join(c.bundleDir, "client", cb.ModuleID+".ssr.mjs")
}

// entry returns the build of cb's module, the first boundary of a module
// standing for it. owner is set for the one caller that must build it.
func (c *clientSSRImpls) entry(cb ClientBoundary) (e *clientSSRImpl, owner bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.done == nil {
		c.done = map[string]*clientSSRImpl{}
	}
	e, ok := c.done[cb.SourcePath]
	if !ok {
		e = &clientSSRImpl{ready: make(chan struct{})}
		c.done[cb.SourcePath] = e
	}
	return e, !ok
}

func (c *clientSSRImpls) build(cb ClientBoundary) error {
	e, owner := c.entry(cb)
	if owner {
		e.finish(func() error { return c.buildOne(cb) })
	}
	<-e.ready
	return e.err
}

// claimedImpl is a module whose build a caller took over.
type claimedImpl struct {
	cb ClientBoundary
	e  *clientSSRImpl
}

// claim takes over the builds of the modules of boundaries that no one has
// started, one boundary per module (real-React boundaries have no server
// render module).
func (c *clientSSRImpls) claim(boundaries []ClientBoundary) []claimedImpl {
	var out []claimedImpl
	for _, cb := range boundaries {
		if cb.Real {
			continue
		}
		if e, owner := c.entry(cb); owner {
			out = append(out, claimedImpl{cb, e})
		}
	}
	return out
}

// prebuild builds the server render modules of boundaries, each on its own,
// several at once. A module that fails keeps its error for the page bundle
// that needs it, so it is reported there as before.
func (c *clientSSRImpls) prebuild(boundaries []ClientBoundary) {
	c.buildEach(c.claim(boundaries))
}

func (c *clientSSRImpls) buildEach(claimed []claimedImpl) {
	pool := newWorkerPool(buildWorkerLimit())
	var wg sync.WaitGroup
	for _, ci := range claimed {
		wg.Add(1)
		pool.acquire()
		go func() {
			defer wg.Done()
			defer pool.release()
			ci.e.finish(func() error { return c.buildOne(ci.cb) })
		}()
	}
	wg.Wait()
}

// buildAll builds the server render modules of a full build's boundaries in
// one code-split esbuild build (buildCombined). If that build fails in any
// way, every module builds on its own instead (prebuild), so each error is
// reported against the module that has it.
func (c *clientSSRImpls) buildAll(boundaries []ClientBoundary) {
	claimed := c.claim(boundaries)
	if len(claimed) > 1 && !c.separate {
		if err := safe.Run(func() error { return c.buildCombined(claimed) }); err == nil {
			for _, ci := range claimed {
				ci.e.finish(func() error { return nil })
			}
			return
		}
	}
	c.buildEach(claimed)
}

// ssrChunkNames names the shared chunks of the combined build, under
// server-bundles/client/chunks/. The hash follows the content, so the server,
// which reloads a page bundle by a new query string, never keeps a stale
// chunk of changed code.
const ssrChunkNames = "chunks/[name]-[hash]"

// buildCombined builds the modules of claimed as the entry points of one
// esbuild build with code splitting: the modules they share are parsed and
// linked once and emitted once, as chunks the entries import. Each entry keeps
// the path of its separate build, which the page bundles import. It writes
// nothing unless the whole build succeeds.
func (c *clientSSRImpls) buildCombined(claimed []claimedImpl) error {
	guard := &unavailableGuard{root: c.root, nodeRuntime: true, msgs: map[string]api.Message{}}
	// The render-to-string module imports only preact, which stays external,
	// so one plugin serves every entry.
	opts, err := c.options(islandRenderToStringPlugin(c.root), guard)
	if err != nil {
		return err
	}
	outdir := filepath.Join(c.bundleDir, "client")
	// A fixed order keeps the output the same from build to build.
	claimed = append([]claimedImpl(nil), claimed...)
	sort.Slice(claimed, func(i, j int) bool { return claimed[i].cb.ModuleID < claimed[j].cb.ModuleID })
	entries := make([]batchEntry, len(claimed))
	want := make(map[string]bool, len(claimed))
	for i, ci := range claimed {
		entries[i] = batchEntry{Source: clientSSRSource(ci.cb), ResolveDir: ci.cb.resolveDir(), Sourcefile: "krate-client-ssr.js", Loader: api.LoaderJS, OutputPath: ci.cb.ModuleID + ".ssr"}
		want[ci.cb.ModuleID+".ssr.mjs"] = true
	}
	entries = withBatchEntries(&opts, entries)
	opts.Outdir = outdir
	opts.Metafile = true
	opts.Splitting = true
	opts.ChunkNames = ssrChunkNames
	opts.OutExtension = map[string]string{".js": ".mjs"}
	opts.Write = false
	res := esbuildBuildFrom(opts, 2, "")
	if len(res.Errors) > 0 {
		return fmt.Errorf("combined server render build: %s", res.Errors[0].Text)
	}
	if hits := guard.errors(res.OutputFiles); len(hits) > 0 {
		return fmt.Errorf("combined server render build: %s", hits[0].Text)
	}
	interopCJSDynamicImports(res.OutputFiles, res.Metafile, opts.AbsWorkingDir)
	inputs := metafileAllInputs(opts.AbsWorkingDir, res.Metafile)
	if !opts.MinifyWhitespace {
		// Name each entry module in the output comments as its own Stdin
		// build does, not by its batch path.
		cwd := esbuildCwd(opts, res.Metafile, batchModulePath(0, entries[0]))
		for i := range entries {
			from := []byte("// " + relSlash(cwd, batchModulePath(i, entries[i])) + "\n")
			to := []byte("// " + stdinPrettyPath(cwd, entries[i]) + "\n")
			for k, f := range res.OutputFiles {
				if bytes.Contains(f.Contents, from) {
					res.OutputFiles[k].Contents = bytes.ReplaceAll(f.Contents, from, to)
				}
			}
		}
	}
	if err := os.MkdirAll(outdir, 0755); err != nil {
		return err
	}
	// esbuild may name the output directory by its real path.
	dirs := []string{outdir}
	if real, err := filepath.EvalSymlinks(outdir); err == nil && real != outdir {
		dirs = append(dirs, real)
	}
	rels := make([]string, len(res.OutputFiles))
	for i, f := range res.OutputFiles {
		for _, dir := range dirs {
			if rel, err := filepath.Rel(dir, f.Path); err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
				rels[i] = rel
				break
			}
		}
		if rels[i] == "" {
			return fmt.Errorf("combined server render build: output %s is outside %s", f.Path, outdir)
		}
		delete(want, filepath.ToSlash(rels[i]))
	}
	if len(want) > 0 {
		return fmt.Errorf("combined server render build: %d entries have no output", len(want))
	}
	for i, f := range res.OutputFiles {
		if err := writeBuilt(filepath.Join(outdir, rels[i]), f.Contents); err != nil {
			return err
		}
	}
	c.combinedInputs = inputs
	return nil
}

// writeBuilt writes a built file, creating its directory.
func writeBuilt(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

// clientSSRSource is the entry module of a boundary's server render module.
func clientSSRSource(cb ClientBoundary) string {
	return "import { h } from \"preact\";\nimport renderToString from \"preact-render-to-string\";\n" + cb.entrySource() +
		clientReviveJS + ssrSlotJS + `
function __krateServerAction() { throw new Error("a server action passed to a client component cannot be called while the server renders it"); }
// The renderer reloads a page bundle's whole graph under a new query on each
// dev rebuild, so a component is looked up among those of this module's own
// generation: the newest copy of another page's graph has contexts of its own.
var __krateGen = (function () { try { return new URL(import.meta.url).search; } catch (e) { return ""; } })();
function __krateComp(m, e) {
  var reg = globalThis.__krateClientImpls;
  var hit = reg && reg.get(__krateGen + "|" + m + "#" + e);
  return hit && hit.impl;
}
export const __krate_module = __m;
// fwd holds the context values of the slot this component renders in; capture
// receives the values in scope at each of its own slots, by slot token.
// ids is the island's useId() prefix (see islandIdPrefix in the runtime): Preact
// numbers useId() from the nearest vnode that holds a mask, and the browser
// gives the island's root the same one.
export function __krate_render(C, props, _children, fwd, capture, ids) {
  __krateCapture = capture || null;
  __krateRaw = null;
  try {
    var tree = __krateProvide(h, h(C, __krateRevive(props, h, __krateServerAction, __krateComp)), fwd);
    if (ids && tree && typeof tree === "object") tree.__m = [ids, 0];
    return __krateRawHTML(renderToString(tree));
  } finally {
    __krateCapture = null;
    __krateRaw = null;
  }
}
__krate_render.__krateGen = __krateGen;
`
}

// options are the esbuild options of a server render module; plugin is its
// render-to-string plugin.
func (c *clientSSRImpls) options(plugin *api.Plugin, guard *unavailableGuard) (api.BuildOptions, error) {
	// The modules share their options, so the pure-module plugin shares its
	// resolutions across them.
	plugins := []api.Plugin{purePrunePluginMemo(c.root, &c.memo), guard.plugin(), serverCSSPlugin(c.root), *plugin}
	plugins = append(plugins, clientOnlyMarkersPlugin(false), actionServerBundlePlugin(c.res, c.actions, c.basePath), pathAliasPlugin(c.res), reactDOMHintsPlugin(c.root), reactServerCompatPlugin(c.root), routerHooksPlugin(c.root), threeIslandPlugin(), konvaIslandPlugin(), preactRequirePlugin())
	shim, err := boundaryShims(filepath.Dir(c.bundleDir))
	if err != nil {
		return api.BuildOptions{}, err
	}
	opts := api.BuildOptions{
		AbsWorkingDir:   c.root,
		Bundle:          true,
		Write:           true,
		Format:          api.FormatESModule,
		Platform:        api.PlatformNode,
		MainFields:      []string{"module", "main"},
		Target:          api.ES2020,
		JSX:             api.JSXAutomatic,
		JSXImportSource: "preact",
		TsconfigRaw:     clientTsconfigRaw(c.root),
		Alias:           islandAlias(c.root, false),
		External:        []string{"preact", "preact/compat", "preact/hooks", "preact/jsx-runtime"},
		Inject:          []string{shim},
		Plugins:         plugins,
		LogLevel:        api.LogLevelSilent,
	}
	if err := withContextKeys(&opts, c.root, filepath.Dir(c.bundleDir)); err != nil {
		return api.BuildOptions{}, err
	}
	withNamespaceImports(&opts)
	return opts, nil
}

func (c *clientSSRImpls) buildOne(cb ClientBoundary) error {
	out, err := c.buildOneOutput(cb)
	if err != nil {
		return err
	}
	return writeBuilt(c.implPath(cb), out)
}

// buildOneOutput builds cb's server render module on its own and returns it.
func (c *clientSSRImpls) buildOneOutput(cb ClientBoundary) ([]byte, error) {
	plugin := islandRenderToStringPlugin(cb.resolveDir())
	guard := &unavailableGuard{root: c.root, nodeRuntime: true, msgs: map[string]api.Message{}}
	opts, err := c.options(plugin, guard)
	if err != nil {
		return nil, err
	}
	opts.Stdin = &api.StdinOptions{
		Contents:   clientSSRSource(cb),
		ResolveDir: cb.resolveDir(),
		Sourcefile: "krate-client-ssr.js",
		Loader:     api.LoaderJS,
	}
	opts.Outfile = c.implPath(cb)
	opts.Write = false
	result := esbuildBuild(opts)
	if len(result.Errors) == 0 && len(result.OutputFiles) != 1 {
		result.Errors = append(result.Errors, api.Message{Text: "the server render module has no single output"})
	}
	if len(result.Errors) == 0 {
		result.Errors = append(result.Errors, guard.errors(result.OutputFiles)...)
	}
	if len(result.Errors) > 0 {
		return nil, clientBuildError(c.root, cb.SourcePath, result.Errors)
	}
	return result.OutputFiles[0].Contents, nil
}

// clientBoundaryStub is the page-bundle replacement of a "use client" module:
// every export wrapped by clientBoundary around its server render module.
func clientBoundaryStub(cb ClientBoundary, implRel string) string {
	var sb strings.Builder
	sb.WriteString(boundaryStubHeader)
	writeClientBoundaryExports(&sb, cb, implRel, "", nil)
	return sb.String()
}

const boundaryStubHeader = "import { clientBoundary as __krateCB } from \"@krate/runtime/server\";\n"

// writeClientBoundaryExports writes the exports of one boundary. suffix keeps
// the impl bindings of several boundaries in one module apart, and names
// already in seen are skipped.
func writeClientBoundaryExports(sb *strings.Builder, cb ClientBoundary, implRel, suffix string, seen map[string]bool) {
	fmt.Fprintf(sb, "import { __krate_module as __m%s, __krate_render as __r%s } from %s;\n", suffix, suffix, strconv.Quote(implRel))
	for _, name := range cb.Exports {
		if seen[name] {
			continue
		}
		if seen != nil {
			seen[name] = true
		}
		wrapped := fmt.Sprintf("__krateCB(%s, %s, %s, __m%s[%s], __r%s, %t)", strconv.Quote(cb.ModuleID), strconv.Quote(name), strconv.Quote(cb.ChunkURL), suffix, strconv.Quote(name), suffix, cb.hydrates())
		if name == "default" {
			fmt.Fprintf(sb, "export default %s;\n", wrapped)
		} else {
			fmt.Fprintf(sb, "export const %s = %s;\n", name, wrapped)
		}
	}
}

// packageBoundaryStub is the module a React-bound package specifier resolves
// to. A page can carry several boundaries for one specifier (its own and its
// async layouts', each with the exports that tree uses); the module exports
// the union, the first boundary winning a name.
func packageBoundaryStub(cbs []ClientBoundary, outPath string, impls *clientSSRImpls) (string, error) {
	var sb strings.Builder
	sb.WriteString(boundaryStubHeader)
	seen := map[string]bool{}
	for i, cb := range cbs {
		if cb.Real {
			realBoundaryExports(&sb, cb, seen)
			continue
		}
		rel, err := filepath.Rel(filepath.Dir(outPath), impls.implPath(cb))
		if err != nil {
			return "", err
		}
		rel = filepath.ToSlash(rel)
		if !strings.HasPrefix(rel, ".") {
			rel = "./" + rel
		}
		writeClientBoundaryExports(&sb, cb, rel, strconv.Itoa(i), seen)
	}
	return sb.String(), nil
}

// clientBoundaryPlugin swaps "use client" modules for their boundary stubs in
// a page server bundle, keeps the server render modules external (loaded from
// server-bundles/client/ at run time), and resolves the server-only /
// client-only markers to empty modules. files keeps the file identities for
// the run's other bundles.
func clientBoundaryPlugin(boundaries []ClientBoundary, impls *clientSSRImpls, outPath string, files *serverBundleFiles) (api.Plugin, error) {
	byPath := map[string]ClientBoundary{}
	var pkgs []ClientBoundary
	for _, cb := range boundaries {
		if !cb.Real {
			if err := impls.build(cb); err != nil {
				return api.Plugin{}, err
			}
		}
		if cb.Spec != "" {
			pkgs = append(pkgs, cb)
			continue
		}
		byPath[filepath.Clean(cb.SourcePath)] = cb
		if resolved := canonicalPath(cb.SourcePath); resolved != filepath.Clean(cb.SourcePath) {
			byPath[resolved] = cb
		}
	}
	markers := clientOnlyMarkersPlugin(false)
	return api.Plugin{
		Name: "krate-client-boundaries",
		Setup: func(pb api.PluginBuild) {
			markers.Setup(pb)
			if len(pkgs) > 0 {
				setupPackageBoundaries(pb, pkgs, outPath, impls)
			}
			if len(byPath) == 0 && len(pkgs) == 0 {
				return
			}
			pb.OnResolve(api.OnResolveOptions{Filter: `\.ssr\.mjs$`}, func(args api.OnResolveArgs) (api.OnResolveResult, error) {
				if strings.Contains(args.Path, "/client/c") || strings.Contains(args.Path, "\\client\\c") {
					return api.OnResolveResult{Path: args.Path, External: true}, nil
				}
				return api.OnResolveResult{}, nil
			})
			pb.OnLoad(api.OnLoadOptions{Filter: `\.[cm]?[jt]sx?$`, Namespace: "file"}, func(args api.OnLoadArgs) (api.OnLoadResult, error) {
				cb, ok := byPath[filepath.Clean(args.Path)]
				if !ok {
					for _, candidate := range boundaries {
						if files.sameFile(args.Path, candidate.SourcePath) {
							cb, ok = candidate, true
							break
						}
					}
				}
				if !ok {
					return api.OnLoadResult{}, nil
				}
				if cb.Real {
					stub := realBoundaryStub(cb)
					return api.OnLoadResult{Contents: &stub, Loader: api.LoaderJS, ResolveDir: filepath.Dir(args.Path)}, nil
				}
				rel, err := filepath.Rel(filepath.Dir(outPath), impls.implPath(cb))
				if err != nil {
					return api.OnLoadResult{}, err
				}
				rel = filepath.ToSlash(rel)
				if !strings.HasPrefix(rel, ".") {
					rel = "./" + rel
				}
				stub := clientBoundaryStub(cb, rel)
				return api.OnLoadResult{Contents: &stub, Loader: api.LoaderJS, ResolveDir: filepath.Dir(args.Path)}, nil
			})
		},
	}, nil
}

// clientTsconfigRaw is the project's tsconfig.json with the JSX settings
// forced to Preact's automatic runtime. A project that sets
// jsxImportSource to @krate/runtime (every migrated app does) would otherwise
// make esbuild compile client components against Krate's DOM runtime, which
// cannot render on the server and is not what the islands mount with.
func clientTsconfigRaw(root string) string {
	opts := map[string]any{}
	if data, err := os.ReadFile(filepath.Join(root, "tsconfig.json")); err == nil {
		var cfg map[string]any
		if json.Unmarshal(data, &cfg) == nil {
			if co, ok := cfg["compilerOptions"].(map[string]any); ok {
				opts = co
			}
		}
	}
	delete(opts, "jsxFactory")
	delete(opts, "jsxFragmentFactory")
	opts["jsx"] = "react-jsx"
	opts["jsxImportSource"] = "preact"
	out, err := json.Marshal(map[string]any{"compilerOptions": opts})
	if err != nil {
		return `{ "compilerOptions": { "jsx": "react-jsx", "jsxImportSource": "preact" } }`
	}
	return string(out)
}

// setupPackageBoundaries makes the page's own modules import the boundary stub
// of a React-bound package instead of the package, which the server bundle
// otherwise leaves external. Modules inside node_modules and the render
// modules under server-bundles/client/ are separate builds and never see it.
func setupPackageBoundaries(pb api.PluginBuild, pkgs []ClientBoundary, outPath string, impls *clientSSRImpls) {
	bySpec := map[string][]ClientBoundary{}
	var specs []string
	for _, cb := range pkgs {
		if len(bySpec[cb.Spec]) == 0 {
			specs = append(specs, specFilterEscape.ReplaceAllStringFunc(cb.Spec, func(s string) string { return `\` + s }))
		}
		bySpec[cb.Spec] = append(bySpec[cb.Spec], cb)
	}
	const ns = "krate-pkg-boundary"
	pb.OnResolve(api.OnResolveOptions{Filter: "^(" + strings.Join(specs, "|") + ")$"}, func(args api.OnResolveArgs) (api.OnResolveResult, error) {
		if strings.Contains(filepath.ToSlash(args.Importer), "/node_modules/") {
			return api.OnResolveResult{}, nil
		}
		return api.OnResolveResult{Path: args.Path, Namespace: ns}, nil
	})
	pb.OnLoad(api.OnLoadOptions{Filter: ".*", Namespace: ns}, func(args api.OnLoadArgs) (api.OnLoadResult, error) {
		stub, err := packageBoundaryStub(bySpec[args.Path], outPath, impls)
		if err != nil {
			return api.OnLoadResult{}, err
		}
		return api.OnLoadResult{Contents: &stub, Loader: api.LoaderJS, ResolveDir: filepath.Dir(outPath)}, nil
	})
}

var specFilterEscape = regexp.MustCompile(`[.*+?^${}()|\[\]\\/-]`)

// metafileInputs lists the source files with code in the build's output.
func metafileInputs(root, metafile string) map[string]bool {
	var meta struct {
		Outputs map[string]struct {
			Inputs map[string]struct {
				BytesInOutput int `json:"bytesInOutput"`
			} `json:"inputs"`
		} `json:"outputs"`
	}
	if metafile == "" || json.Unmarshal([]byte(metafile), &meta) != nil {
		return nil
	}
	out := map[string]bool{}
	for _, o := range meta.Outputs {
		for in, v := range o.Inputs {
			if v.BytesInOutput > 0 && !strings.Contains(in, ":") {
				p := filepath.Join(root, filepath.FromSlash(in))
				out[filepath.Clean(p)] = true
				out[canonicalPath(p)] = true
			}
		}
	}
	return out
}

// preactRequireTargets maps the React specifiers a bundled CommonJS module may
// require() to the Preact module that stands in for them (the same mapping as
// islandAlias, which only covers import statements).
var preactRequireTargets = map[string]string{
	"react":                 "preact/compat",
	"react-dom":             "preact/compat",
	"react-dom/client":      "preact/compat",
	"react/jsx-runtime":     "preact/jsx-runtime",
	"react/jsx-dev-runtime": "preact/jsx-runtime",
	"preact":                "preact",
	"preact/compat":         "preact/compat",
	"preact/hooks":          "preact/hooks",
	"preact/jsx-runtime":    "preact/jsx-runtime",
}

// preactRequirePlugin turns require() of React or Preact in a bundled CommonJS
// module into a small ESM wrapper over the external Preact module. A plain
// external require would load Preact's CommonJS build, a second copy next to
// the ESM one the rest of the bundle imports, with its own hook state.
func preactRequirePlugin() api.Plugin {
	const ns = "krate-preact-require"
	return api.Plugin{
		Name: "krate-preact-require",
		Setup: func(b api.PluginBuild) {
			b.OnResolve(api.OnResolveOptions{Filter: `^(react|react-dom|react-dom/client|react/jsx-runtime|react/jsx-dev-runtime|preact|preact/compat|preact/hooks|preact/jsx-runtime)$`}, func(args api.OnResolveArgs) (api.OnResolveResult, error) {
				if args.Kind != api.ResolveJSRequireCall {
					return api.OnResolveResult{}, nil
				}
				return api.OnResolveResult{Path: preactRequireTargets[args.Path], Namespace: ns}, nil
			})
			b.OnLoad(api.OnLoadOptions{Filter: `.*`, Namespace: ns}, func(args api.OnLoadArgs) (api.OnLoadResult, error) {
				q := strconv.Quote(args.Path)
				src := "export * from " + q + ";\n"
				if args.Path == "preact/compat" {
					src += "export { default } from " + q + ";\n"
				}
				return api.OnLoadResult{Contents: &src, Loader: api.LoaderJS}, nil
			})
		},
	}
}

// keepClientDynImports drops from bundle.DynImportFiles the lazy imports that
// no browser code reaches. In a page with a server tree, only the client
// boundaries (and, for a page that still hydrates, the entry) ship to the
// browser; a dynamic import in a module only the server tree reaches stays a
// server-side import and must not be compiled as a browser chunk.
func keepClientDynImports(bundle *bundler.Bundle, boundaries []ClientBoundary, entry string, serverPage bool) {
	if len(bundle.DynImportFiles) == 0 || bundle.Resolver == nil {
		return
	}
	byPath := map[string]*bundler.Module{}
	for _, m := range bundle.Modules {
		if m != nil && !m.IsExternal && m.Program != nil {
			byPath[filepath.Clean(m.Path)] = m
		}
	}
	reached := map[string]bool{}
	var walk func(path string)
	walk = func(path string) {
		path = filepath.Clean(path)
		if reached[path] {
			return
		}
		reached[path] = true
		m := byPath[path]
		if m == nil {
			return
		}
		for _, spec := range m.Imports {
			if r := bundle.Resolver.Resolve(m.Path, spec); r != "" {
				walk(r)
			}
		}
	}
	for _, cb := range boundaries {
		walk(cb.SourcePath)
	}
	if !serverPage {
		walk(entry)
	}
	kept := make(map[string]string, len(bundle.DynImportFiles))
	for src, url := range bundle.DynImportFiles {
		ok := false
		sites := bundle.DynImportSites[src]
		for _, site := range sites {
			if reached[filepath.Clean(site)] {
				ok = true
				break
			}
		}
		if ok {
			kept[src] = url
		}
	}
	bundle.DynImportFiles = kept
}
