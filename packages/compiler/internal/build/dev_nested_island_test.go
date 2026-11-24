package build

import (
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
)

// devIslandChunkOf returns the island chunk URLs a page references and the
// text of those chunks (the entries only, not shared chunks).
func devIslandChunksOf(t *testing.T, d *devRun, page string) (urls []string, code string) {
	t.Helper()
	_, body := getText(t, d.base+page)
	seen := map[string]bool{}
	queue := devIslandChunkURL.FindAllString(body, -1)
	chunkRef := regexp.MustCompile(`(?:/chunks/|\./)((?:island|isl|kn)[-_A-Za-z0-9.]*\.js)`)
	for len(queue) > 0 {
		u := queue[0]
		queue = queue[1:]
		if seen[u] {
			continue
		}
		seen[u] = true
		urls = append(urls, u)
		c, text := getText(t, d.base+u)
		if c != 200 {
			t.Fatalf("%s answered %d", u, c)
		}
		code += text
		for _, m := range chunkRef.FindAllStringSubmatch(text, -1) {
			queue = append(queue, "/chunks/"+m[1])
		}
	}
	return urls, code
}

// An island that renders another client component from another file: editing
// the inner component must rebuild the island's chunk, not only the server
// HTML, or the page hydrates old code over new markup.
func TestDevEditingANestedClientComponentRebuildsTheIslandChunk(t *testing.T) {
	const (
		innerFile  = "src/components/inner.tsx"
		helperFile = "src/lib/helper.ts"
		deepFile   = "src/components/deep.tsx"
	)
	inner := func(useClient bool, label string) string {
		head := ""
		if useClient {
			head = `"use client";` + "\n"
		}
		return head + `import { useState } from "react";
import { helper } from "../lib/helper";
import { Deep } from "./deep";
export function Inner() {
  const [n] = useState(0);
  return <span data-probe="` + label + `">{helper()}{n}<Deep /></span>;
}
`
	}
	helper := func(label string) string { return "export function helper() { return \"" + label + "\"; }\n" }
	deep := func(useClient bool, label string) string {
		head := ""
		if useClient {
			head = `"use client";` + "\n"
		}
		return head + `import { useState } from "react";
export function Deep() { const [n] = useState(0); return <i data-deep="` + label + `">{n}</i>; }
`
	}
	variants := []struct {
		name     string
		innerUse bool
		deepUse  bool
		file     string
		edit     func(label string) string
		old      string
	}{
		{"inner without use client", false, false, innerFile, func(l string) string { return inner(false, l) }, "inner-a"},
		{"inner with use client", true, false, innerFile, func(l string) string { return inner(true, l) }, "inner-a"},
		{"helper imported only by the inner", true, false, helperFile, helper, "helper-a"},
		{"three levels", true, true, deepFile, func(l string) string { return deep(true, l) }, "deep-a"},
		{"three levels, innermost without use client", true, false, deepFile, func(l string) string { return deep(false, l) }, "deep-a"},
	}
	for _, v := range variants {
		t.Run(v.name, func(t *testing.T) {
			files := map[string]string{
				"src/pages/cad/_layout.tsx": `import { Provider } from "../../components/provider";
import { Frame } from "../../components/frame";
export default async function Layout({ children }: any) {
  await Promise.resolve();
  return <Provider><Frame>{children}</Frame></Provider>;
}
`,
				"src/components/provider.tsx": `"use client";
import { createContext, useContext } from "react";
const C = createContext("look");
export function Provider({ children }: any) { return <C.Provider value="look">{children}</C.Provider>; }
export function useLook() { return useContext(C); }
`,
				"src/components/frame.tsx": `"use client";
import { Inner } from "./inner";
import { useLook } from "./provider";
export function Frame({ children }: any) { const look = useLook(); return <div data-look={look}><Inner />{children}</div>; }
`,
				innerFile:               inner(v.innerUse, "inner-a"),
				helperFile:              helper("helper-a"),
				deepFile:                deep(v.deepUse, "deep-a"),
				"src/pages/cad/one.tsx": "export default async function P() { return <main>page one</main>; }\n",
				"src/pages/cad/two.tsx": "export default async function P() { return <main>page two</main>; }\n",
				"src/pages/index.tsx":   "export default async function P() { return <main>home</main>; }\n",
			}
			d := startDevRun(t, files)
			d.waitBody(t, "/cad/one/", v.old)
			urls0, code0 := devIslandChunksOf(t, d, "/cad/one/")
			if !strings.Contains(code0, v.old) {
				t.Fatalf("no island chunk with the first version: %v", urls0)
			}
			// The first edit after the server starts is picked up by other
			// means (a page registers its islands again); the ones after it
			// need the kept chunk checks to notice the file changed.
			base := strings.TrimSuffix(v.old, "a")
			prev := v.old
			for _, label := range []string{base + "b", base + "c", base + "d"} {
				d.write(t, v.file, v.edit(label))
				d.waitBody(t, "/cad/one/", label)
				d.waitBody(t, "/cad/two/", label)
				var urls []string
				var code string
				waitUntilTrue(t, 15*time.Second, "the island chunk to hold "+label, func() bool {
					urls, code = devIslandChunksOf(t, d, "/cad/one/")
					return strings.Contains(code, label)
				})
				if strings.Contains(code, prev) {
					t.Fatalf("the island chunk still holds %q after the edit to %q (chunks %v)", prev, label, urls)
				}
				prev = label
			}
		})
	}
}

// The report this guards: a layout renders a client island (the app's frame)
// whose module also holds the nested client components it renders. Edits to
// that module after the first one did not reach the island chunk: its check
// build was kept with no file stamped (a batched build's metafile has no
// inputs), so it was reused for good, nothing registered the island again, and
// the chunk was never written. The page then hydrated old code over the new
// server markup.
func TestDevEditingTheIslandModuleWithNestedComponentsRebuildsItsChunk(t *testing.T) {
	frame := func(label string) string {
		return `"use client";
import { useState } from "react";
import { usePathname } from "@krate/runtime";
import { useCad } from "./provider";
import { TooltipProvider } from "../ui/tooltip";

function FloatingHeader({ children }: any) {
  return <header data-probe="` + label + `"><div className="bar">{children}</div></header>;
}
function TopBar() {
  const { look } = useCad();
  const path = usePathname();
  const [n] = useState(0);
  return <FloatingHeader>{look} {path} {n}</FloatingHeader>;
}
export function CadFrame({ children }: any) {
  return <TooltipProvider><TopBar /><main>{children}</main></TooltipProvider>;
}
`
	}
	const frameFile = "src/components/cad/top-bar.tsx"
	d := startDevRun(t, map[string]string{
		"src/pages/cad/_layout.tsx": `import { CadProvider } from "../../components/cad/provider";
import { CadFrame } from "../../components/cad/top-bar";
export default async function Layout({ children }: any) {
  const a = await Promise.resolve(1);
  return <CadProvider initial={a}><CadFrame>{children}</CadFrame></CadProvider>;
}
`,
		"src/components/cad/provider.tsx": `"use client";
import { createContext, useContext } from "react";
const C = createContext<any>({ look: "x" });
export function CadProvider({ children }: any) { return <C.Provider value={{ look: "x" }}>{children}</C.Provider>; }
export function useCad() { return useContext(C); }
`,
		"src/components/ui/tooltip.tsx": `"use client";
export function TooltipProvider({ children }: any) { return <div>{children}</div>; }
`,
		frameFile:               frame("probe-a"),
		"src/pages/cad/one.tsx": "export default async function P() { return <main>page one</main>; }\n",
		"src/pages/cad/two.tsx": "export default async function P() { return <main>page two</main>; }\n",
		"src/pages/index.tsx":   "export default async function P() { return <main>home</main>; }\n",
	})
	d.waitBody(t, "/cad/one/", "probe-a")
	prev := "probe-a"
	for _, label := range []string{"probe-b", "probe-c", "probe-d"} {
		d.write(t, frameFile, frame(label))
		d.waitBody(t, "/cad/one/", label)
		var urls []string
		var code string
		waitUntilTrue(t, 15*time.Second, "the island chunk to hold "+label, func() bool {
			urls, code = devIslandChunksOf(t, d, "/cad/one/")
			return strings.Contains(code, label)
		})
		if strings.Contains(code, prev) {
			t.Fatalf("the island chunk still holds %q after the edit to %q (chunks %v)", prev, label, urls)
		}
		prev = label
	}
}

// A batched build's metafile lists outputs only, so the files an entry loaded
// come from the batch's import graph, entry by entry.
func TestBatchEntryInputsFollowEachEntrysImports(t *testing.T) {
	cwd := filepath.Join(string(filepath.Separator), "proj")
	meta := `{"inputs":{
	  ".krate-batch-0/a.js":{"bytes":1,"imports":[{"path":"src/a.tsx","kind":"import-statement"},{"path":"react","kind":"import-statement","external":true}]},
	  ".krate-batch-1/b.js":{"bytes":1,"imports":[{"path":"src/b.tsx","kind":"import-statement"}]},
	  "src/a.tsx":{"bytes":1,"imports":[{"path":"src/shared.ts","kind":"import-statement"},{"path":"src/dyn.ts","kind":"dynamic-import"}]},
	  "src/b.tsx":{"bytes":1,"imports":[{"path":"src/shared.ts","kind":"import-statement"}]},
	  "src/shared.ts":{"bytes":1,"imports":[{"path":"krate-island-css:/proj/src/x.css","kind":"import-statement"}]},
	  "src/dyn.ts":{"bytes":1,"imports":[]},
	  "krate-island-css:/proj/src/x.css":{"bytes":1,"imports":[]}
	}}`
	got := batchEntryInputs(cwd, meta, map[string]int{".krate-batch-0/a.js": 0, ".krate-batch-1/b.js": 1})
	p := func(rel string) string { return filepath.Join(cwd, filepath.FromSlash(rel)) }
	want := map[int][]string{
		0: {p("src/a.tsx"), p("src/dyn.ts"), p("src/shared.ts"), p("src/x.css")},
		1: {p("src/b.tsx"), p("src/shared.ts"), p("src/x.css")},
	}
	for i, w := range want {
		if strings.Join(got[i], "\n") != strings.Join(w, "\n") {
			t.Fatalf("entry %d inputs:\n%s\nwant:\n%s", i, strings.Join(got[i], "\n"), strings.Join(w, "\n"))
		}
	}
}

// A kept client chunk with no recorded input is never reused.
func TestClientChunkWithoutStampsIsNotReusable(t *testing.T) {
	b := &Builder{DevMode: true}
	r := clientChunkReq{key: "a.tsx", resolveDir: "d", entry: "e"}
	info := clientChunkInfo{sig: clientChunkSig(r), stamps: stampSet{}}
	if b.clientChunkReusable(info, r) {
		t.Fatal("a chunk with no stamped file was reused")
	}
}
