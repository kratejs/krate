package build

import (
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/evanw/esbuild/pkg/api"
)

// writeFakeKrateKonva installs a stand-in @krate/konva whose Krate-source
// entry and Preact island build say which one was bundled. Like the real
// package it depends on no React reconciler.
func writeFakeKrateKonva(t *testing.T, root string) {
	t.Helper()
	writeProjectFile(t, root, "node_modules/@krate/konva/package.json",
		`{"name":"@krate/konva","version":"0.1.0","type":"module","exports":{".":"./src/index.ts","./island":"./src/island/index.js"},"peerDependencies":{"konva":">=9","preact":">=10.20.0"}}`)
	writeProjectFile(t, root, "node_modules/@krate/konva/src/index.ts", `export const which = "krate-source";`)
	writeProjectFile(t, root, "node_modules/@krate/konva/src/island/index.js", `
import { createElement } from 'preact';
export const which = "konva-island";
export function Stage(props) { return createElement('div', { id: props.id, 'data-stage': 'konva-island' }); }
export const Layer = 'Layer';
export const Rect = 'Rect';
`)
}

func konvaBundleWith(t *testing.T, root string, plugin api.Plugin) (string, []api.Message) {
	t.Helper()
	res := api.Build(api.BuildOptions{
		Stdin:   &api.StdinOptions{Contents: `import { which } from "@krate/konva"; console.log(which);`, ResolveDir: root, Loader: api.LoaderTS},
		Bundle:  true,
		Write:   false,
		Format:  api.FormatESModule,
		Plugins: []api.Plugin{plugin},
	})
	if len(res.OutputFiles) == 0 {
		return "", res.Errors
	}
	return string(res.OutputFiles[0].Contents), res.Errors
}

// A Preact island gets the island build of @krate/konva.
func TestKonvaIslandPluginResolvesIslandBuild(t *testing.T) {
	root := t.TempDir()
	writeFakeKrateKonva(t, root)
	writeProjectFile(t, root, "node_modules/preact/package.json", `{"name":"preact","main":"index.js"}`)
	writeProjectFile(t, root, "node_modules/preact/index.js", `export function createElement() {}`)
	out, errs := konvaBundleWith(t, root, konvaIslandPlugin())
	if len(errs) > 0 {
		t.Fatalf("build errors: %v", errs)
	}
	if !strings.Contains(out, `"konva-island"`) || strings.Contains(out, "krate-source") {
		t.Fatalf("island build not used:\n%s", out)
	}
}

// A real-React island runs @krate/konva as react-konva, and says what to
// install when react-konva is missing.
func TestKonvaRealReactPluginUsesReactKonva(t *testing.T) {
	root := t.TempDir()
	writeFakeKrateKonva(t, root)
	_, errs := konvaBundleWith(t, root, konvaRealReactPlugin())
	if len(errs) == 0 || !strings.Contains(errs[0].Text, "real-React island") || !strings.Contains(errs[0].Notes[0].Text, "install react-konva") {
		t.Fatalf("expected an actionable error without react-konva, got %+v", errs)
	}
	writeProjectFile(t, root, "node_modules/react-konva/package.json", `{"name":"react-konva","main":"index.js"}`)
	writeProjectFile(t, root, "node_modules/react-konva/index.js", `export const which = "react-konva";`)
	out, errs := konvaBundleWith(t, root, konvaRealReactPlugin())
	if len(errs) > 0 {
		t.Fatalf("build errors: %v", errs)
	}
	if !strings.Contains(out, `"react-konva"`) || strings.Contains(out, "krate-source") || strings.Contains(out, "konva-island") {
		t.Fatalf("react-konva not used:\n%s", out)
	}
}

// TestKonvaIslandKind: on a server-rendered page the island kind follows the
// import. A "use client" module that imports react-konva (built on react-reconciler) is a real-React island and
// its page loads the shared React chunks; a module that imports @krate/konva
// is a Preact island that loads the island build and no React; a @krate/konva
// module rendered inside a real-React island runs there as react-konva.
func TestKonvaIslandKind(t *testing.T) {
	root := t.TempDir()
	cfg := setupIslandProject(t, root, true)
	realReactFixture(t, root)
	writeFakeKrateKonva(t, root)
	// react-konva stands in as a reconciler library (it depends on
	// react-reconciler), rendering through fake-canvas.
	writeProjectFile(t, root, "node_modules/react-konva/package.json",
		`{"name":"react-konva","version":"19.3.2","main":"index.js","dependencies":{"react-reconciler":"0.34.0"},"peerDependencies":{"konva":">=9","react":"^19.3.0"}}`)
	writeProjectFile(t, root, "node_modules/react-konva/index.js", `
import { createElement } from 'react';
import { Canvas, Rect as FakeRect } from 'fake-canvas';
export const which = "react-konva";
export function Stage(props) { return createElement(Canvas, { id: props.id, width: 20, height: 20 }, props.children); }
export const Layer = 'Layer';
export const Rect = FakeRect;
`)
	writeProjectFile(t, root, "src/components/Editor.tsx", `'use client';
import { useState } from 'react';
import { Stage, Layer, Rect } from '@krate/konva';
export default function Editor() {
	const [x, setX] = useState(1);
	return <div><button id="move" onClick={() => setX(x + 1)}>move</button><Stage id="editor"><Layer><Rect x={x} y={0} width={5} height={5} fill="red" /></Layer></Stage></div>;
}
`)
	writeProjectFile(t, root, "src/components/Legacy.tsx", `'use client';
import { useState } from 'react';
import { Stage, Layer, Rect } from 'react-konva';
export default function Legacy() {
	const [x] = useState(1);
	return <Stage id="legacy"><Layer><Rect x={x} y={0} w={5} h={5} fill="red" /></Layer></Stage>;
}
`)
	writeProjectFile(t, root, "src/components/Shapes.tsx", `'use client';
import { Rect, which } from '@krate/konva';
export default function Shapes() { return <Rect x={0} y={0} w={5} h={5} fill={which === "react-konva" ? "blue" : "red"} />; }
`)
	writeProjectFile(t, root, "src/components/Mixed.tsx", `'use client';
import { useState } from 'react';
import { Stage, Layer } from 'react-konva';
import Shapes from './Shapes';
export default function Mixed() {
	const [n] = useState(1);
	return <Stage id="mixed"><Layer><Shapes key={n} /></Layer></Stage>;
}
`)
	writeProjectFile(t, root, "src/pages/editor.tsx", `import Editor from '../components/Editor';
export default async function Page() {
	const title = await Promise.resolve("editor");
	return <main><h1>{title}</h1><Editor /></main>;
}
`)
	writeProjectFile(t, root, "src/pages/legacy.tsx", `import Legacy from '../components/Legacy';
export default async function Page() {
	const title = await Promise.resolve("legacy");
	return <main><h1>{title}</h1><Legacy /></main>;
}
`)
	writeProjectFile(t, root, "src/pages/mixed.tsx", `import Mixed from '../components/Mixed';
export default async function Page() {
	const title = await Promise.resolve("mixed");
	return <main><h1>{title}</h1><Mixed /></main>;
}
`)
	if err := linkRuntimePackage(t, filepath.Join(root, "node_modules", "@krate")); err != nil {
		t.Skipf("cannot link the runtime package: %v", err)
	}
	e2eEnv(t)
	t.Chdir(root)
	out, err := captureOutput(func() error { return New(root, cfg).BuildAll() })
	if err != nil {
		t.Fatalf("BuildAll: %v\n%s", err, out)
	}

	if !canStopInProcessServer {
		t.Skip("cannot stop an in-process server on this platform")
	}
	base, err := startKrateServer(t, root, cfg)
	if err != nil {
		t.Fatalf("serve: %v", err)
	}
	chunkRe := regexp.MustCompile(`chunks/[\w.-]+\.js`)
	importRe := regexp.MustCompile(`["'(]\./([\w.-]+\.js)["')]`)
	// pageJS returns the code of every chunk the server-rendered page loads,
	// following the static and dynamic imports between chunks.
	pageJS := func(page string) (string, []string) {
		resp, err := http.Get(base + "/" + page)
		if err != nil {
			t.Fatalf("GET /%s: %v", page, err)
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("GET /%s: %d\n%s", page, resp.StatusCode, body)
		}
		var code strings.Builder
		var names []string
		seen := map[string]bool{}
		queue := chunkRe.FindAllString(string(body), -1)
		for len(queue) > 0 {
			rel := queue[0]
			queue = queue[1:]
			if seen[rel] {
				continue
			}
			seen[rel] = true
			data, err := os.ReadFile(filepath.Join(cfg.OutDir, filepath.FromSlash(rel)))
			if err != nil {
				continue
			}
			names = append(names, filepath.Base(rel))
			code.Write(data)
			queue = append(queue, chunkRe.FindAllString(string(data), -1)...)
			for _, m := range importRe.FindAllStringSubmatch(string(data), -1) {
				queue = append(queue, "chunks/"+m[1])
			}
		}
		return code.String(), names
	}
	hasReal := func(names []string) bool {
		for _, n := range names {
			if strings.HasPrefix(n, islandRealSharedStem) {
				return true
			}
		}
		return false
	}

	editor, editorChunks := pageJS("editor")
	if hasReal(editorChunks) || strings.Contains(editor, "__CLIENT_INTERNALS_DO_NOT_USE") {
		t.Fatalf("a @krate/konva module must be a Preact island without React, loaded %v", editorChunks)
	}
	if !strings.Contains(editor, "konva-island") || strings.Contains(editor, "krate-source") {
		t.Fatalf("the editor island must use the @krate/konva island build (chunks %v)", editorChunks)
	}

	legacy, legacyChunks := pageJS("legacy")
	if !hasReal(legacyChunks) || !strings.Contains(legacy, "__CLIENT_INTERNALS_DO_NOT_USE") {
		t.Fatalf("a react-konva module must be a real-React island, loaded %v", legacyChunks)
	}

	mixed, mixedChunks := pageJS("mixed")
	if !hasReal(mixedChunks) {
		t.Fatalf("the mixed page must be a real-React island, loaded %v", mixedChunks)
	}
	if strings.Contains(mixed, "konva-island") || strings.Contains(mixed, "krate-source") {
		t.Fatalf("@krate/konva inside a real-React island must run as react-konva (chunks %v)", mixedChunks)
	}
}
