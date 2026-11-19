package build

import (
	"bytes"
	"hash/fnv"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"github.com/evanw/esbuild/pkg/api"
	"github.com/kratejs/krate/packages/compiler/internal/fsutil"
)

// Context across islands.
//
// In a server tree, a "use client" provider and the client components inside
// its server children are separate islands: the provider's children arrive as
// HTML and each nested island mounts on its own. Next.js renders them as one
// React tree, so the nested components see the provider's context. Krate
// carries the context across the island boundary instead:
//
//   - Every createContext call site in an island bundle is keyed at build
//     time (a hash of the module path and the call's index). The Preact and
//     real-React builds of a module key the same context alike; Radix creates
//     all its contexts at one call site and tells them apart by displayName.
//   - The HTML slot of a Preact island records the providers above it (the
//     legacy context map Preact passes to function components). A Preact
//     island nested in the slot mounts under the same providers, so it shares
//     their instances and updates. A real-React island re-provides its own
//     copy of each keyed context with the value the Preact tree holds,
//     following changes.
//   - On the server a boundary renders before its server children, so the
//     values in scope at each slot render the nested islands too.

// contextKeysSource is the module the keyed call sites and the island mount
// code share. It is injected into each island build, which gives every build (one
// renderer world in the browser, one server render module) its own registry.
const contextKeysSource = `var own = [];
function all() { return globalThis.__krateContexts || (globalThis.__krateContexts = []); }
function tag(c, k, v) { try { Object.defineProperty(c, k, { value: v, configurable: true }); } catch (e) {} }
export function __krateKeyContext(site, n, create) {
  return function () {
    var c = create.apply(this, arguments);
    if (c && (typeof c === "object" || typeof c === "function") && own.length < 4096) {
      tag(c, "__krateSite", site + ":" + n);
      tag(c, "__krateOwn", own);
      own.push(c);
      all().push(c);
    }
    return c;
  };
}
function keyOf(c) {
  return c && typeof c.__krateSite === "string" ? c.__krateSite + "|" + (typeof c.displayName === "string" ? c.displayName : "") : null;
}
function unique(list, k) {
  var hit = null;
  for (var i = 0; i < list.length; i++) {
    if (keyOf(list[i]) === k) {
      if (hit) return null;
      hit = list[i];
    }
  }
  return hit;
}
export function __krateOwnContext(k) { return unique(own, k); }
var byId = new Map();
function contextById(id) {
  if (byId.has(id)) return byId.get(id);
  var list = all();
  for (var i = 0; i < list.length; i++) {
    var c = list[i];
    for (var p in c) {
      if (c[p] === id) {
        byId.set(id, c);
        return c;
      }
    }
  }
  return null;
}
export function __krateForwarded(map) {
  var out = [];
  if (!map || typeof map !== "object") return out;
  for (var id in map) {
    var inst = map[id];
    var c = contextById(id);
    var k = keyOf(c);
    if (!k || !inst || !inst.props || unique(c.__krateOwn, k) !== c) continue;
    out.push({ k: k, c: c, p: inst });
  }
  return out;
}
export function __krateProvide(h, node, fwd) {
  for (var i = 0; fwd && i < fwd.length; i++) {
    var c = unique(own, fwd[i].k);
    if (c) node = h(c.Provider || c, { value: fwd[i].v }, node);
  }
  return node;
}
export function __krateScopeOf(el) {
  for (var n = el; n; n = n.parentNode) if (n.__krateScope) return n.__krateScope;
  return null;
}
export function __krateScopeSet(s, vals) {
  s.vals = vals;
  s.subs.slice().forEach(function (f) { f(); });
}
export function __krateScopePut(s, k, v) {
  for (var i = 0; i < s.vals.length; i++) {
    if (s.vals[i].k === k) {
      if (Object.is(s.vals[i].v, v)) return;
      var next = s.vals.slice();
      next[i] = { k: k, v: v };
      return __krateScopeSet(s, next);
    }
  }
  __krateScopeSet(s, s.vals.concat([{ k: k, v: v }]));
}
export function __krateScopeSubscribe(s) {
  return function (f) {
    s.subs.push(f);
    return function () { var i = s.subs.indexOf(f); if (i >= 0) s.subs.splice(i, 1); };
  };
}
`

// preactSlotJS renders an HTML slot in a Preact island chunk, with a watcher
// per forwarded context, which keeps the slot's scope current for real-React
// islands inside it. It also defines the root that nested Preact islands
// mount under.
//
// A slot puts its nodes straight into the parent the component renders it
// in, with no element around them, so child combinators (:has(> svg),
// space-y-*) match as they do in React. Preact cannot render foreign nodes,
// so the slot renders an empty text vnode per node and, once it is diffed
// (before paint), puts each node in place of its text node and points the
// vnode at it (Preact's mangled _dom, __e, and the ancestors' first-node
// pointers, as Preact's own updateParentDomPointers does). Preact then moves
// and removes the nodes as its own and never diffs inside them. Each node
// carries the slot's scope (__krateScope).
//
// A slot with an id (r) is already in the document: the server rendered its
// nodes between <!--krate-slot:id--> and <!--/krate-slot:id-->. Before
// hydrating, mount takes the nodes of each of the island's server slots out of
// the document (__krateServerSlots) and leaves an empty text node for each,
// which the slot's text vnodes hydrate, so hydration neither claims nor
// removes the server nodes. The slot that renders them takes them back, and a
// slot rendered again after a hide takes back the same nodes, so nested
// islands keep their state. When every copy is in use (the slot rendered once
// more than the server did), it gets a clone. A slot without an id parses
// the HTML its props carry.
//
// An island has no element of its own: it is the nodes between its
// <!--krate-island:...--> and <!--/krate-island--> comments. __krateRangeRoot
// is the parent Preact renders it into: it reads and inserts between the two
// comments of the real parent, so Preact never touches the parent's other
// children. A nested island among a slot's nodes can add or drop nodes of its
// own between its comments, so a slot refreshes those runs from the document
// when it lets go of its nodes (__krateSlotRefresh).
const preactSlotJS = `
import { useContext as __krateUseContext } from "preact/hooks";
import { Fragment as __KrateFragment, options as __krateOptions } from "preact";
var __krateSlotPool = null;
var __krateSlotOpen = /^krate-slot:([0-9a-z]+)$/;
function __krateIslandOpen(n) { return n.nodeType === 8 && n.data.indexOf("krate-island:") === 0; }
function __krateIslandClose(n) { return n.nodeType === 8 && n.data === "/krate-island"; }
function __krateRangeRoot(island) {
  var start = island.start, end = island.end;
  function parent() { return start.parentNode; }
  var root = {
    nodeType: 1,
    get parentNode() { return parent(); },
    get ownerDocument() { return parent().ownerDocument; },
    get namespaceURI() { return parent().namespaceURI; },
    get ownerSVGElement() { return parent().ownerSVGElement; },
    get firstChild() { var n = start.nextSibling; return n === end ? null : n; },
    get childNodes() { return island.nodes(); },
    contains: function (n) { return island.contains(n); },
    insertBefore: function (n, ref) {
      // Preact walks on past the last node it knows (over comments): a node
      // outside the island means the end of it.
      parent().insertBefore(n, ref && ref.parentNode === parent() && island.contains(ref) ? ref : end);
      return n;
    },
    appendChild: function (n) { parent().insertBefore(n, end); return n; },
    removeChild: function (n) { if (n.parentNode) n.parentNode.removeChild(n); return n; },
  };
  return root;
}
function __krateServerSlots(island) {
  var pool = {}, opens = [], depth = 0;
  function see(c) {
    if (__krateIslandOpen(c)) return void depth++;
    if (__krateIslandClose(c)) return void depth--;
    if (depth || !__krateSlotOpen.test(c.data)) return;
    var host = c.parentElement && c.parentElement.closest("krate-island");
    if (!host || !island.contains(host)) opens.push(c);
  }
  island.nodes().forEach(function (n) {
    if (n.nodeType === 8) return see(n);
    if (n.nodeType !== 1) return;
    var w = document.createTreeWalker(n, 128);
    for (var c = w.nextNode(); c; c = w.nextNode()) see(c);
  });
  opens.forEach(function (c) {
    if (!island.contains(c)) return;
    var id = __krateSlotOpen.exec(c.data)[1], close = "/krate-slot:" + id, nodes = [], n = c.nextSibling;
    for (; n && !(n.nodeType === 8 && n.data === close); n = n.nextSibling) nodes.push(n);
    if (!n) return;
    var parent = c.parentNode;
    nodes.forEach(function (x) { parent.replaceChild(document.createTextNode(""), x); });
    parent.removeChild(c);
    parent.removeChild(n);
    (pool[id] = pool[id] || []).push(nodes);
  });
  return pool;
}
function __krateSlotTake(p) {
  if (p.r == null) {
    var t = document.createElement("template");
    t.innerHTML = p.html || "";
    return Array.prototype.slice.call(t.content.childNodes);
  }
  var sets = p.pool || [];
  for (var i = 0; i < sets.length; i++) {
    if (!sets[i].used) {
      sets[i].used = true;
      return sets[i];
    }
  }
  return sets.length ? sets[0].map(function (n) { return n.cloneNode(true); }) : [];
}
function __krateSlotRefresh(set) {
  for (var i = 0; i < set.length; i++) {
    var s = set[i];
    if (!__krateIslandOpen(s) || !s.parentNode) continue;
    var cur = [], d = 0, n = s.nextSibling;
    for (; n; n = n.nextSibling) {
      if (__krateIslandOpen(n)) d++;
      else if (__krateIslandClose(n)) { if (!d) break; d--; }
      cur.push(n);
    }
    var j = n ? set.indexOf(n, i + 1) : -1;
    if (j < 0) continue;
    var old = set.slice(i + 1, j);
    // Preact removes the nodes it placed; the island's own additions go too.
    cur.forEach(function (x) { if (old.indexOf(x) < 0) x.parentNode.removeChild(x); });
    set.splice.apply(set, [i + 1, j - i - 1].concat(cur));
    i += cur.length + 1;
  }
}
function __krateSlotRelease(c) {
  if (c.__krateSet) {
    __krateSlotRefresh(c.__krateSet);
    c.__krateSet.used = false;
  }
  c.__krateSet = null;
}
function __krateSlotPlace(c) {
  var set = c.__krateSet, v = c.__v, frag = v && v.__k && v.__k[0], kids = frag && frag.__k, moved = false;
  if (!set || !kids) return;
  for (var i = 0; i < set.length; i++) {
    var t = kids[i], old = t && t.__e;
    if (!old || old === set[i] || !old.parentNode) continue;
    old.parentNode.replaceChild(set[i], old);
    t.__e = set[i];
    moved = true;
  }
  set.forEach(function (n) { n.__krateScope = c.__krateSlotScope; });
  for (v = frag; moved && v && typeof v.type === "function"; v = v.__) {
    var first = null;
    for (i = 0; v.__k && i < v.__k.length && !first; i++) first = v.__k[i] && v.__k[i].__e;
    v.__e = first || null;
    if (v.__c) v.__c.base = v.__e;
  }
}
// Placement runs from Preact's diffed hook (after the slot's children are in
// the DOM, before paint) and release from its unmount hook, not from a
// layout effect: a hook import would make every island chunk look stateful
// to the build (statefulHookRe).
var __krateSlotHooked = false;
function __krateSlotHooks() {
  if (__krateSlotHooked) return;
  __krateSlotHooked = true;
  var diffed = __krateOptions.diffed, unmount = __krateOptions.unmount;
  __krateOptions.diffed = function (v) {
    if (v.type === __KrateSlot && v.__c) __krateSlotPlace(v.__c);
    if (diffed) diffed(v);
  };
  __krateOptions.unmount = function (v) {
    if (v.type === __KrateSlot && v.__c) __krateSlotRelease(v.__c);
    if (unmount) unmount(v);
  };
}
function __krateSlot(h, html, r) {
  __krateSlotHooks();
  return h(__KrateSlot, { html: html, r: r, pool: r != null && __krateSlotPool ? __krateSlotPool[r] || [] : null, scope: { ctx: null, vals: [], subs: [] } });
}
function __KrateSlot(p, map) {
  var c = this, s = p.scope, f = __krateForwarded(map), keep = {};
  s.ctx = map;
  f.forEach(function (e) { keep[e.k] = 1; });
  var kept = s.vals.filter(function (e) { return keep[e.k]; });
  if (kept.length !== s.vals.length) __krateScopeSet(s, kept);
  var was = c.__krateFor;
  if (!was || was.r !== p.r || was.html !== p.html || was.pool !== p.pool) {
    __krateSlotRelease(c);
    c.__krateSet = __krateSlotTake(p);
    c.__krateFor = p;
    c.__krateGen = (c.__krateGen || 0) + 1;
  }
  c.__krateSlotScope = s;
  var texts = c.__krateSet.map(function () { return ""; });
  return [h(__KrateFragment, { key: "n" + c.__krateGen }, texts)].concat(f.map(function (e) { return h(__KrateWatch, { key: e.k, k: e.k, c: e.c, s: s }); }));
}
function __KrateWatch(p) {
  __krateScopePut(p.s, p.k, __krateUseContext(p.c));
  return null;
}
function __KrateScopeRoot(p) {
  var map = p.map;
  this.getChildContext = function () { return map; };
  return p.children;
}
function __krateInScope(el, node) {
  var s = __krateScopeOf(el);
  return s && s.ctx ? h(__KrateScopeRoot, { map: s.ctx }, node) : node;
}
`

// reactSlotJS is the HTML slot and scoped root of a real-React island chunk.
const reactSlotJS = `
function __krateSlot(h, html) { return h("krate-children", { style: { display: "contents" }, dangerouslySetInnerHTML: { __html: html || "" } }); }
function __KrateScopeRoot(p) {
  var s = p.s;
  var sub = React.useMemo(function () { return __krateScopeSubscribe(s); }, [s]);
  var vals = React.useSyncExternalStore(sub, function () { return s.vals; }, function () { return s.vals; });
  return __krateProvide(React.createElement, p.children, vals);
}
function __krateInScope(el, node) {
  var s = __krateScopeOf(el);
  return s ? React.createElement(__KrateScopeRoot, { s: s }, node) : node;
}
`

// ssrSlotJS is the HTML slot of a server render module: it records the
// forwarded context values at the slot's token for the server children that
// render into it, and leaves the slot's HTML (the token) in place with no
// element around it. Preact cannot render raw HTML without an element, so
// the slot renders a placeholder element that __krate_render swaps for the
// HTML (__krateRawHTML).
const ssrSlotJS = `
var __krateCapture = null;
var __krateRaw = null;
function __krateSlot(h, html) { return h(__KrateSlot, { html: html }); }
function __KrateSlot(p, map) {
  if (__krateCapture && /^<!--krate-slot:[0-9a-z]+-->$/.test(p.html)) {
    __krateCapture[p.html] = __krateForwarded(map).map(function (e) { return { k: e.k, v: e.p.props.value }; });
  }
  var r = __krateRaw || (__krateRaw = { id: Math.random().toString(36).slice(2), list: [] });
  r.list.push(p.html || "");
  return h("krate-raw", { "data-krate-raw": r.id + ":" + (r.list.length - 1) });
}
function __krateRawHTML(html) {
  var r = __krateRaw;
  if (!r) return html;
  return html.replace(new RegExp('<krate-raw data-krate-raw="' + r.id + ':(\\d+)"></krate-raw>', "g"), function (_, i) { return r.list[+i]; });
}
`

// contextKeysFile writes the injected registry module (once per content) and
// returns its path.
func contextKeysFile(dotKrate string) (string, error) {
	if err := os.MkdirAll(dotKrate, 0755); err != nil {
		return "", err
	}
	path := filepath.Join(dotKrate, "context-keys."+hashContent([]byte(contextKeysSource))+".mjs")
	if err := fsutil.WriteFileIfChanged(path, []byte(contextKeysSource), 0644); err != nil {
		return "", err
	}
	return path, nil
}

// withContextKeys makes a build key the createContext call sites of its
// modules: it wraps the OnLoad callbacks of plugins so their output is keyed,
// adds a last plugin that keys the files no other plugin loads, and injects
// the registry module.
func withContextKeys(opts *api.BuildOptions, root, dotKrate string) error {
	inject, err := contextKeysFile(dotKrate)
	if err != nil {
		return err
	}
	opts.Inject = append(opts.Inject, inject)
	wrapped := make([]api.Plugin, 0, len(opts.Plugins)+1)
	for _, p := range opts.Plugins {
		wrapped = append(wrapped, keyedPlugin(p, root))
	}
	opts.Plugins = append(wrapped, contextKeysPlugin(root))
	return nil
}

func keyedPlugin(p api.Plugin, root string) api.Plugin {
	setup := p.Setup
	p.Setup = func(pb api.PluginBuild) {
		onLoad := pb.OnLoad
		pb.OnLoad = func(o api.OnLoadOptions, cb func(api.OnLoadArgs) (api.OnLoadResult, error)) {
			onLoad(o, func(args api.OnLoadArgs) (api.OnLoadResult, error) {
				res, err := cb(args)
				if err == nil && res.Contents != nil && args.Namespace == "file" && !skipContextKeys(args.Path) {
					if out, ok := keyContextCalls(contextSite(root, args.Path), *res.Contents); ok {
						res.Contents = &out
					}
				}
				return res, err
			})
		}
		setup(pb)
	}
	return p
}

// contextKeysPlugin loads the script files that mention createContext and no
// earlier plugin claimed, keyed.
func contextKeysPlugin(root string) api.Plugin {
	return api.Plugin{
		Name: "krate-context-keys",
		Setup: func(pb api.PluginBuild) {
			pb.OnLoad(api.OnLoadOptions{Filter: `\.([cm]?[jt]sx?)$`, Namespace: "file"}, func(args api.OnLoadArgs) (api.OnLoadResult, error) {
				if skipContextKeys(args.Path) || !mentionsCreateContext(args.Path) {
					return api.OnLoadResult{}, nil
				}
				out, ok := keyedFile(root, args.Path)
				if !ok {
					return api.OnLoadResult{}, nil
				}
				return api.OnLoadResult{Contents: &out, Loader: loaderForExt(args.Path), ResolveDir: filepath.Dir(args.Path)}, nil
			})
		},
	}
}

type mentionEntry struct {
	mtimeNS, size int64
	hit           bool
}

type keyedEntry struct {
	mtimeNS, size int64
	out           string
	ok            bool
}

// keyedFiles holds each file's keyed source (keyContextCalls), by root and
// path, while the file keeps its modification time and size.
var keyedFiles sync.Map

func keyedFile(root, path string) (string, bool) {
	info, err := os.Stat(path)
	if err != nil {
		return "", false
	}
	key := root + "\x00" + path
	if v, hit := keyedFiles.Load(key); hit {
		if e := v.(keyedEntry); e.mtimeNS == info.ModTime().UnixNano() && e.size == info.Size() {
			return e.out, e.ok
		}
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", false
	}
	out, ok := keyContextCalls(contextSite(root, path), string(data))
	keyedFiles.Store(key, keyedEntry{mtimeNS: info.ModTime().UnixNano(), size: info.Size(), out: out, ok: ok})
	return out, ok
}

var createContextMentions sync.Map // mentionEntry by path

// mentionsCreateContext reports whether a file's source contains
// "createContext", caching the answer per path and modification time.
func mentionsCreateContext(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	if v, ok := createContextMentions.Load(path); ok {
		if e := v.(mentionEntry); e.mtimeNS == info.ModTime().UnixNano() && e.size == info.Size() {
			return e.hit
		}
	}
	data, err := os.ReadFile(path)
	hit := err == nil && bytes.Contains(data, []byte("createContext"))
	createContextMentions.Store(path, mentionEntry{mtimeNS: info.ModTime().UnixNano(), size: info.Size(), hit: hit})
	return hit
}

// skipContextKeys leaves the renderers' own sources alone.
func skipContextKeys(path string) bool {
	p := filepath.ToSlash(path)
	for _, pkg := range []string{"/node_modules/react/", "/node_modules/react-dom/", "/node_modules/preact/", "/node_modules/scheduler/", "/node_modules/react-reconciler/"} {
		if strings.Contains(p, pkg) {
			return true
		}
	}
	return false
}

// contextSite is the call-site prefix of a module: a number derived from its
// root-relative path, so no build-machine path reaches the browser.
func contextSite(root, path string) string {
	rel, err := filepath.Rel(canonicalPath(root), canonicalPath(path))
	if err != nil {
		rel = path
	}
	h := fnv.New32a()
	_, _ = h.Write([]byte(filepath.ToSlash(rel)))
	return strconv.FormatUint(uint64(h.Sum32()), 10)
}

// keyContextCalls rewrites each `createContext(...)` call (also as a member,
// `React.createContext(...)` or `(0, r.createContext)(...)`, and with type
// arguments) to `__krateKeyContext(site, n, createContext)(...)`. Strings,
// comments and declarations are left alone. The inserted text has no quotes
// and balanced parentheses, so a miss can never break the module's syntax.
func keyContextCalls(site, src string) (string, bool) {
	const name = "createContext"
	if !strings.Contains(src, name) {
		return src, false
	}
	var out strings.Builder
	last, n := 0, 0
	s := jsScanner{src: src}
	for s.next() {
		i := s.pos
		if !strings.HasPrefix(src[i:], name) || (i > 0 && isIdentByte(src[i-1])) || (i+len(name) < len(src) && isIdentByte(src[i+len(name)])) {
			s.pos++
			continue
		}
		end := i + len(name)
		s.pos = end
		start, ok := calleeStart(src, i)
		if !ok {
			continue
		}
		after := end
		if j := skipSpace(src, after); j < len(src) && src[j] == '<' {
			if k := skipTypeArgs(src, j); k > 0 {
				after = k
			}
		}
		call := skipSpace(src, after)
		if call < len(src) && src[call] == ')' && start > 0 && strings.HasSuffix(strings.TrimRight(src[:start], " \t\r\n"), ",") {
			call = skipSpace(src, call+1)
		}
		if call >= len(src) || src[call] != '(' {
			continue
		}
		close := matchParen(src, call)
		if close < 0 {
			continue
		}
		if j := skipSpace(src, close+1); j < len(src) && (src[j] == '{' || src[j] == ':' || strings.HasPrefix(src[j:], "=>")) {
			continue
		}
		out.WriteString(src[last:start])
		out.WriteString("__krateKeyContext(" + site + ", " + strconv.Itoa(n) + ", ")
		out.WriteString(src[start:after])
		out.WriteString(")")
		last = after
		n++
	}
	if n == 0 {
		return src, false
	}
	out.WriteString(src[last:])
	return out.String(), true
}

// calleeStart returns where the callee expression ending in the identifier at
// i begins: the identifier itself, or the start of a member chain before it.
// It reports false for a declaration (`function createContext`, a method).
func calleeStart(src string, i int) (int, bool) {
	j := i
	for {
		k := skipSpaceBack(src, j)
		if k > 0 && src[k-1] == '.' {
			k--
			if k > 0 && src[k-1] == '?' {
				k--
			}
			k = skipSpaceBack(src, k)
			s := k
			for s > 0 && isIdentByte(src[s-1]) {
				s--
			}
			if s == k {
				return 0, false
			}
			j = s
			continue
		}
		if j == i {
			word := k
			for word > 0 && isIdentByte(src[word-1]) {
				word--
			}
			switch src[word:k] {
			case "function", "get", "set", "static", "async", "class", "new", "interface", "type", "declare":
				return 0, false
			}
		}
		return j, true
	}
}

func isIdentByte(c byte) bool {
	return c == '_' || c == '$' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c >= 0x80
}

func skipSpace(src string, i int) int {
	for i < len(src) && (src[i] == ' ' || src[i] == '\t' || src[i] == '\n' || src[i] == '\r') {
		i++
	}
	return i
}

func skipSpaceBack(src string, i int) int {
	for i > 0 && (src[i-1] == ' ' || src[i-1] == '\t' || src[i-1] == '\n' || src[i-1] == '\r') {
		i--
	}
	return i
}

// skipTypeArgs returns the index after the `<...>` type arguments at i, or -1.
func skipTypeArgs(src string, i int) int {
	angle, nest := 0, 0
	s := jsScanner{src: src, pos: i}
	for s.next() && s.pos < i+4096 {
		k := s.pos
		s.pos++
		switch src[k] {
		case '{', '(', '[':
			nest++
		case '}', ')', ']':
			if nest--; nest < 0 {
				return -1
			}
		case ';':
			if nest == 0 {
				return -1
			}
		case '<':
			if nest == 0 {
				angle++
			}
		case '>':
			if nest > 0 || src[k-1] == '=' {
				continue
			}
			if angle--; angle == 0 {
				return k + 1
			}
		}
	}
	return -1
}

// matchParen returns the index of the parenthesis closing the one at open,
// skipping strings and comments, or -1.
func matchParen(src string, open int) int {
	s := jsScanner{src: src, pos: open}
	depth := 0
	for s.next() {
		switch src[s.pos] {
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				return s.pos
			}
		}
		s.pos++
	}
	return -1
}

// jsScanner steps through code outside string literals, template literals
// and comments. next moves pos to the next code byte.
type jsScanner struct {
	src string
	pos int
}

func (s *jsScanner) next() bool {
	src := s.src
	for s.pos < len(src) {
		c := src[s.pos]
		switch {
		case c == '/' && s.pos+1 < len(src) && src[s.pos+1] == '/':
			nl := strings.IndexByte(src[s.pos:], '\n')
			if nl < 0 {
				s.pos = len(src)
				return false
			}
			s.pos += nl + 1
		case c == '/' && s.pos+1 < len(src) && src[s.pos+1] == '*':
			end := strings.Index(src[s.pos+2:], "*/")
			if end < 0 {
				s.pos = len(src)
				return false
			}
			s.pos += end + 4
		case c == '"' || c == '\'' || c == '`':
			s.pos = skipQuoted(src, s.pos)
		default:
			return true
		}
	}
	return false
}

// skipQuoted returns the index after the string or template literal at i. A
// template's ${...} parts are skipped with it.
func skipQuoted(src string, i int) int {
	q := src[i]
	for k := i + 1; k < len(src); k++ {
		switch src[k] {
		case '\\':
			k++
		case q:
			return k + 1
		case '\n':
			if q != '`' {
				return k + 1
			}
		}
	}
	return len(src)
}
