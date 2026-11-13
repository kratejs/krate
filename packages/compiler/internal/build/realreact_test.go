package build

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/config"
)

// realReactFixture installs react, react-dom and react-reconciler (from the
// react-oracle workspace package) and a tiny renderer built on
// react-reconciler, "fake-canvas", that draws <Rect> elements on a 2D canvas.
// It stands in for react-konva and react-three-fiber: both are libraries whose
// package.json depends on react-reconciler.
func realReactFixture(t *testing.T, root string) {
	t.Helper()
	repo, err := repoRootPath()
	if err != nil {
		t.Fatal(err)
	}
	realReactFixtureFrom(t, root, repo)
}

func realReactFixtureFrom(t *testing.T, root, repo string) {
	t.Helper()
	for _, name := range []string{"react", "react-dom", "react-reconciler"} {
		src, err := filepath.EvalSymlinks(filepath.Join(repo, "packages", "react-oracle", "node_modules", name))
		if err != nil {
			requireE2E(t, "%s is not installed for the real-React island tests (run pnpm install): %v", name, err)
		}
		dst := filepath.Join(root, "node_modules", name)
		if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(src, dst); err != nil {
			t.Skipf("cannot link %s: %v", name, err)
		}
	}
	writeProjectFile(t, root, "node_modules/fake-canvas/package.json",
		`{"name":"fake-canvas","version":"1.0.0","main":"index.js","dependencies":{"react-reconciler":"^0.33.0"},"peerDependencies":{"react":"^19.0.0"}}`)
	writeProjectFile(t, root, "node_modules/fake-canvas/index.js", fakeCanvasSource)
}

// fakeCanvasSource is a complete react-reconciler host config (mutation mode)
// that keeps a tree of plain objects and repaints a 2D canvas after each commit.
const fakeCanvasSource = `
import React from 'react';
import Reconciler from 'react-reconciler';
import { DefaultEventPriority } from 'react-reconciler/constants.js';

let priority = DefaultEventPriority;
const host = {
  supportsMutation: true, supportsPersistence: false, supportsHydration: false,
  isPrimaryRenderer: true, noTimeout: -1, scheduleTimeout: setTimeout, cancelTimeout: clearTimeout,
  supportsMicrotasks: true, scheduleMicrotask: queueMicrotask,
  createInstance(type, props) { return { type, props, children: [] }; },
  createTextInstance(text) { return { type: 'text', props: { text }, children: [] }; },
  appendInitialChild(parent, child) { parent.children.push(child); },
  appendChild(parent, child) { parent.children.push(child); },
  appendChildToContainer(container, child) { container.children.push(child); },
  insertBefore(parent, child, before) { parent.children.splice(parent.children.indexOf(before), 0, child); },
  insertInContainerBefore(container, child, before) { container.children.splice(container.children.indexOf(before), 0, child); },
  removeChild(parent, child) { parent.children.splice(parent.children.indexOf(child), 1); },
  removeChildFromContainer(container, child) { container.children.splice(container.children.indexOf(child), 1); },
  clearContainer(container) { container.children = []; },
  finalizeInitialChildren() { return false; },
  commitUpdate(instance, type, oldProps, newProps) { instance.props = newProps; },
  commitTextUpdate(instance, oldText, newText) { instance.props = { text: newText }; },
  shouldSetTextContent() { return false; },
  getRootHostContext() { return {}; },
  getChildHostContext(ctx) { return ctx; },
  getPublicInstance(instance) { return instance; },
  prepareForCommit() { return null; },
  resetAfterCommit(container) { container.draw(); },
  preparePortalMount() {}, detachDeletedInstance() {}, getInstanceFromNode() { return null; },
  beforeActiveInstanceBlur() {}, afterActiveInstanceBlur() {}, prepareScopeUpdate() {}, getInstanceFromScope() { return null; },
  setCurrentUpdatePriority(p) { priority = p; },
  getCurrentUpdatePriority() { return priority; },
  resolveUpdatePriority() { return priority || DefaultEventPriority; },
  trackSchedulerEvent() {}, resolveEventType() { return null; }, resolveEventTimeStamp() { return -1.1; },
  shouldAttemptEagerTransition() { return false; }, requestPostPaintCallback() {},
  maySuspendCommit() { return false; }, maySuspendCommitOnUpdate() { return false; }, maySuspendCommitInSyncRender() { return false; },
  preloadInstance() { return true; }, startSuspendingCommit() {}, suspendInstance() {}, waitForCommitToBeReady() { return null; },
  NotPendingTransition: null, HostTransitionContext: React.createContext(null), resetFormInstance() {},
  bindToConsole(method, args) { return Function.prototype.bind.apply(console[method], [console].concat(args)); },
};
const reconciler = Reconciler(host);

export const Rect = 'rect';

export function Canvas({ id, width, height, children }) {
  const ref = React.useRef(null);
  const root = React.useRef(null);
  React.useEffect(() => {
    const ctx = ref.current.getContext('2d');
    const container = { children: [], draw() {
      ctx.clearRect(0, 0, width, height);
      for (const c of this.children) if (c.type === 'rect') { ctx.fillStyle = c.props.fill; ctx.fillRect(c.props.x, c.props.y, c.props.w, c.props.h); }
    } };
    root.current = reconciler.createContainer(container, 0, null, false, null, '', console.error, console.error, console.error, null);
    return () => reconciler.updateContainer(null, root.current, null, null);
  }, []);
  React.useEffect(() => { reconciler.updateContainer(children, root.current, null, null); });
  return React.createElement('canvas', { id, ref, width, height });
}
`

// realReactSceneFiles is a site with one real-React island page (a module that
// renders through the reconciler library), one Preact island page and one page
// without islands.
func realReactSceneFiles(t *testing.T, root string) {
	t.Helper()
	writeProjectFile(t, root, "src/components/Scene.tsx", `
		import { useState } from 'react';
		import { Canvas, Rect } from 'fake-canvas';
		export default function Scene({ color }: { color: string }) {
			const [n, setN] = useState(1);
			return (
				<div>
					<button id="grow" onClick={() => setN(n + 1)}>grow {n}</button>
					<Canvas id="cv" width={60} height={20}><Rect x={n * 5} y={0} w={5} h={5} fill={color} /></Canvas>
				</div>
			);
		}
	`)
	writeProjectFile(t, root, "src/pages/index.tsx", `
		import { createSignal } from '@krate/runtime';
		import Scene from '../components/Scene';
		export default function Page() {
			const [color, setColor] = createSignal('rgb(255, 0, 0)');
			return (
				<main>
					<h1>scene</h1>
					<button id="recolor" onClick={() => setColor('rgb(0, 0, 255)')}>recolor</button>
					<Scene client:fallback="Loading scene" color={color()} />
				</main>
			);
		}
	`)
	writeProjectFile(t, root, "src/pages/second.tsx", `
		import Scene from '../components/Scene';
		export default function Page() { return <main><Scene color="rgb(0, 255, 0)" /></main>; }
	`)
	writeProjectFile(t, root, "src/pages/direct.tsx", `
		import { Canvas, Rect } from 'fake-canvas';
		export default function Page() { return <main><Canvas id="cv" width={20} height={20}><Rect x={0} y={0} w={5} h={5} fill="red" /></Canvas></main>; }
	`)
	writeProjectFile(t, root, "src/pages/badge.tsx", `
		import { Badge } from 'fake-lib';
		export default function Page() { return <main><Badge label="plain" /></main>; }
	`)
	writeProjectFile(t, root, "src/pages/plain.tsx", `
		export default function Page() { return <main><h1>no islands</h1></main>; }
	`)
}

func buildRealReactSite(t *testing.T) (*config.Config, string) {
	t.Helper()
	root := t.TempDir()
	cfg := setupIslandProject(t, root, true)
	realReactFixture(t, root)
	realReactSceneFiles(t, root)
	return cfg, root
}

// TestRealReactIsland: a module built on a React reconciler library runs on
// real React in a client-rendered island, its chunk is loaded only by the page
// that uses it, and the Preact alias is not applied inside it. A Preact island
// on another page keeps the alias.
func TestRealReactIsland(t *testing.T) {
	cfg, root := buildRealReactSite(t)
	out, err := captureOutput(func() error { return New(root, cfg).BuildAll() })
	if err != nil {
		t.Fatalf("BuildAll: %v\n%s", err, out)
	}
	if strings.Contains(out, "KR8") {
		t.Fatalf("unexpected diagnostics:\n%s", out)
	}
	page := func(name string) string { return readBuilt(t, filepath.Join(cfg.OutDir, name, "index.html")) }
	index := readBuilt(t, filepath.Join(cfg.OutDir, "index.html"))
	if !strings.Contains(index, "data-krate-island-ssr") || !strings.Contains(index, "Loading scene") {
		t.Fatalf("the island must carry its fallback markup:\n%s", index)
	}

	chunkRe := regexp.MustCompile(`chunks/(island_[0-9a-f]+|isr-[0-9a-z]+)\.js`)
	pageChunks := func(dir string) []string {
		var found []string
		files, _ := filepath.Glob(filepath.Join(cfg.OutDir, dir, "*.js"))
		for _, f := range files {
			found = append(found, chunkRe.FindAllString(readBuilt(t, f), -1)...)
		}
		return found
	}
	realEntry := pageChunks(".")
	if len(realEntry) != 1 {
		t.Fatalf("the index page must load exactly one island entry, got %v", realEntry)
	}
	entryCode := readBuilt(t, filepath.Join(cfg.OutDir, filepath.FromSlash(realEntry[0])))
	if strings.Contains(entryCode, "preact") {
		t.Fatalf("a real-React island must not import Preact:\n%.600s", entryCode)
	}
	// Two pages with real-React islands share one React chunk.
	sharedRe := regexp.MustCompile(`\./(isr-[0-9a-z]+\.js)`)
	shared := sharedRe.FindStringSubmatch(entryCode)
	if shared == nil {
		t.Fatalf("the real-React entry must import a shared React chunk:\n%.600s", entryCode)
	}
	second := pageChunks("second")
	if len(second) != 1 || second[0] == realEntry[0] {
		t.Fatalf("the second page must load its own island entry, got %v (index: %v)", second, realEntry)
	}
	if m := sharedRe.FindStringSubmatch(readBuilt(t, filepath.Join(cfg.OutDir, filepath.FromSlash(second[0])))); m == nil || m[1] != shared[1] {
		t.Fatalf("both pages must share one React chunk (%s), got %v", shared[1], m)
	}
	reactCode := entryCode
	allShared, _ := filepath.Glob(filepath.Join(cfg.OutDir, "chunks", "isr-*.js"))
	for _, f := range allShared {
		reactCode += readBuilt(t, f)
	}
	for _, want := range []string{"__CLIENT_INTERNALS_DO_NOT_USE_OR_WARN_USERS_THEY_CANNOT_UPGRADE", "createContainer"} {
		if !strings.Contains(reactCode, want) {
			t.Fatalf("real React and react-reconciler must be bundled (missing %s)", want)
		}
	}

	// A reconciler library used as a tag directly in a page is an island too.
	direct := pageChunks("direct")
	if len(direct) != 1 {
		t.Fatalf("direct page must load one island entry, got %v", direct)
	}
	if m := sharedRe.FindStringSubmatch(readBuilt(t, filepath.Join(cfg.OutDir, filepath.FromSlash(direct[0])))); m == nil {
		t.Fatalf("a page tag of a reconciler library must run on the shared React chunks, got %v", m)
	}

	// Pages without a real-React island load none of it.
	for _, name := range []string{"badge", "plain"} {
		html := page(name)
		if strings.Contains(html, "isr-") || strings.Contains(html, realEntry[0]) {
			t.Fatalf("%s must not reference the real-React chunks:\n%s", name, html)
		}
		for _, c := range pageChunks(name) {
			if c == realEntry[0] {
				t.Fatalf("%s must not load the real-React entry", name)
			}
		}
	}
	// The Preact island still resolves React to Preact.
	badge := pageChunks("badge")
	if len(badge) != 1 {
		t.Fatalf("badge page must load one island entry, got %v", badge)
	}
	preactCode := readBuilt(t, filepath.Join(cfg.OutDir, filepath.FromSlash(badge[0])))
	if !strings.Contains(preactCode, "preact") || strings.Contains(preactCode, "__CLIENT_INTERNALS_DO_NOT_USE") {
		t.Fatalf("the Preact island must stay on the alias:\n%.600s", preactCode)
	}
}

// TestRealReactConfigPackage: a package listed in react.islands runs on real
// React even when it does not depend on a reconciler.
func TestRealReactConfigPackage(t *testing.T) {
	root := t.TempDir()
	cfg := setupIslandProject(t, root, true)
	realReactFixture(t, root)
	writeProjectFile(t, root, "node_modules/fake-canvas/package.json",
		`{"name":"fake-canvas","version":"1.0.0","main":"index.js","peerDependencies":{"react":"^19.0.0"}}`)
	realReactSceneFiles(t, root)
	cfg.React.Islands = []string{"fake-canvas"}
	out, err := captureOutput(func() error { return New(root, cfg).BuildAll() })
	if err != nil {
		t.Fatalf("BuildAll: %v\n%s", err, out)
	}
	code := islandCode(t, cfg.OutDir)
	shared, _ := filepath.Glob(filepath.Join(cfg.OutDir, "chunks", "isr-*.js"))
	for _, f := range shared {
		code += readBuilt(t, f)
	}
	if !strings.Contains(code, "__CLIENT_INTERNALS_DO_NOT_USE") {
		t.Fatalf("react.islands must bundle real React (output:\n%s)", out)
	}
}

// TestReconcilerLibraryOnPreactIsland: a library that pulls in react-reconciler
// without declaring it (here through a wrapper package) cannot run on Preact;
// the build says how to fix it instead of shipping a broken island.
func TestReconcilerLibraryOnPreactIsland(t *testing.T) {
	root := t.TempDir()
	cfg := setupIslandProject(t, root, true)
	realReactFixture(t, root)
	writeProjectFile(t, root, "node_modules/wrap-canvas/package.json",
		`{"name":"wrap-canvas","version":"1.0.0","main":"index.js","peerDependencies":{"react":"^19.0.0"}}`)
	writeProjectFile(t, root, "node_modules/wrap-canvas/index.js", `
		import { Canvas } from 'fake-canvas';
		import { createElement } from 'react';
		export function Wrapped(props) { return createElement(Canvas, props); }
	`)
	writeProjectFile(t, root, "src/pages/index.tsx", `
		import { Wrapped } from 'wrap-canvas';
		export default function Page() { return <main><Wrapped id="cv" width={20} height={20} /></main>; }
	`)
	_, err := captureOutput(func() error { return New(root, cfg).BuildAll() })
	if err == nil || !strings.Contains(err.Error(), "react: { islands:") {
		t.Fatalf("want a build error that names react.islands, got %v", err)
	}
}
