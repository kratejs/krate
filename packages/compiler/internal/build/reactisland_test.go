package build

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// reactLibFixture adds small stand-ins for the npm React libraries that the
// island path has to run: a zustand-style store hook (default `import React`),
// a react-hook-form-style hook (namespace import), a CommonJS hook library
// (`require("react")`, as swr's shim does) and a styled-components-style
// factory. Each declares react as a peer dependency, as the real ones do.
func reactLibFixture(t *testing.T, root string) {
	t.Helper()
	lib := func(name, peer, src string) {
		dir := filepath.Join(root, "node_modules", name)
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatal(err)
		}
		pkg := `{"name":"` + name + `","version":"1.0.0","main":"index.js","peerDependencies":{"react":"*"}}`
		if peer == "" {
			pkg = `{"name":"` + name + `","version":"1.0.0","main":"index.js"}`
		}
		if err := os.WriteFile(filepath.Join(dir, "package.json"), []byte(pkg), 0644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "index.js"), []byte(src), 0644); err != nil {
			t.Fatal(err)
		}
	}
	lib("fake-store", "react", `
		import React from 'react';
		export function create(init) {
			const listeners = new Set();
			let state;
			const set = (p) => { state = { ...state, ...(typeof p === 'function' ? p(state) : p) }; listeners.forEach((l) => l()); };
			state = init(set);
			const subscribe = (cb) => { listeners.add(cb); return () => listeners.delete(cb); };
			return function useStore(sel) {
				return React.useSyncExternalStore(subscribe, () => sel(state), () => sel(state));
			};
		}
	`)
	lib("fake-form", "react", `
		import * as React from 'react';
		export function useForm() {
			const ref = React.useRef({ name: '' });
			const [, bump] = React.useState(0);
			return { value: () => ref.current.name, set: (v) => { ref.current.name = v; bump((n) => n + 1); } };
		}
	`)
	lib("fake-cjs", "react", `
		var React = require('react');
		exports.useGreeting = function (who) { return React.useMemo(function () { return 'hello ' + who; }, [who]); };
	`)
	lib("styled-components", "react", `
		import React from 'react';
		function make(tag) {
			return function (strings) {
				var css = strings.join('');
				return function Styled(props) {
					React.useEffect(function () {
						var s = document.createElement('style');
						s.setAttribute('data-fake-styled', '');
						s.textContent = '.fs-' + tag + '{' + css + '}';
						document.head.appendChild(s);
					}, []);
					return React.createElement(tag, { id: props.id, className: 'fs-' + tag }, props.children);
				};
			};
		}
		var styled = { div: make('div') };
		export default styled;
	`)
}

func readBuilt(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func writeProjectFile(t *testing.T, root, rel, src string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(src), 0644); err != nil {
		t.Fatal(err)
	}
}

// TestReactHookLibraryPageIsland: a page whose component calls a hook of an npm
// React library runs as a Preact island (rendered on the server, hydrated in
// place) instead of crashing in the browser.
func TestReactHookLibraryPageIsland(t *testing.T) {
	root := t.TempDir()
	cfg := setupIslandProject(t, root, true)
	reactLibFixture(t, root)
	writeProjectFile(t, root, "src/pages/index.tsx", `
		import { create } from 'fake-store';
		import { useState } from 'react';
		const useCount = create((set) => ({ n: 4, inc: () => set((s) => ({ n: s.n + 1 })) }));
		export default function Page() {
			const n = useCount((s) => s.n);
			const inc = useCount((s) => s.inc);
			const [label] = useState('clicks');
			return <button id="b" onClick={inc}>{label} {n}</button>;
		}
	`)
	out, err := captureOutput(func() error { return New(root, cfg).BuildAll() })
	if err != nil {
		t.Fatalf("BuildAll: %v\n%s", err, out)
	}
	html := readBuilt(t, filepath.Join(cfg.OutDir, "index.html"))
	if !strings.Contains(html, "data-krate-island-ssr") || !strings.Contains(html, "clicks 4") {
		t.Fatalf("page island was not server-rendered:\n%s", html)
	}
	if strings.Contains(out, "KR8008") || strings.Contains(out, "KR2002") {
		t.Fatalf("unexpected warnings:\n%s", out)
	}
	jsFiles, _ := filepath.Glob(filepath.Join(cfg.OutDir, "index.*.js"))
	for _, f := range jsFiles {
		if js := readBuilt(t, f); strings.Contains(js, "useSyncExternalStore") {
			t.Fatalf("the store library leaked into the page script:\n%.400s", js)
		}
	}
	chunk := islandCode(t, cfg.OutDir)
	if !strings.Contains(chunk, "preact/compat") || strings.Contains(chunk, "react.production") {
		t.Fatalf("island chunk must run on preact/compat:\n%.600s", chunk)
	}
	if !strings.Contains(chunk, "export") || !strings.Contains(chunk, "mount") {
		t.Fatalf("island chunk must export mount():\n%.600s", chunk)
	}
}

func mustGlobOne(t *testing.T, pattern string) string {
	t.Helper()
	m, err := filepath.Glob(pattern)
	if err != nil || len(m) != 1 {
		t.Fatalf("glob %s: want one match, got %v (%v)", pattern, m, err)
	}
	return m[0]
}
