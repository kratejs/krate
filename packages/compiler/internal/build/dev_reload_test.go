package build

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/kratejs/krate/packages/compiler/internal/config"
	"github.com/kratejs/krate/packages/compiler/internal/diag"
)

// devRun is a project served the way `krate dev` serves it: the first build,
// the dev server with its renderer sidecar, and the file watcher, all in this
// process.
type devRun struct {
	root string
	base string
	b    *Builder
	hub  *DevHub
	errc <-chan error
	// watchErr receives Watch's result once it returns.
	watchErr <-chan error
	logs     func() string
}

// startDevRun builds files with the real runtime linked in and serves them
// with ServeDevContext and Watch. A failed first build does not stop it, as in
// runDevOnce.
func startDevRun(t *testing.T, files map[string]string, opts ...func(*config.Config)) *devRun {
	t.Helper()
	e2eEnv(t)
	b, root := scProject(t, files)
	if err := linkRuntimePackage(t, filepath.Join(root, "node_modules", "@krate")); err != nil {
		t.Fatal(err)
	}
	withPreact(t, root)
	captureProgress(t)
	logs := builderStderr(b)
	b.DevMode = true
	UseDevOutDir(root, b.Cfg)
	b.Cfg.DevServer.Port = freePort(t)
	for _, o := range opts {
		o(b.Cfg)
	}
	hub := NewDevHub()
	if err := b.BuildAll(); err != nil {
		hub.Publish(DevEvent{Type: "reload", Diagnostics: DiagnosticsFromError(err)})
	}
	cfg := b.Cfg
	cfg.Server.Host = "127.0.0.1"
	port := cfg.DevServer.Port

	ctx, stop := context.WithCancel(context.Background())
	b.SetContext(ctx)
	serveErr := serveInProcess(t, func(c context.Context) error {
		return ServeDevContext(c, root, cfg, hub, time.Now())
	})
	watchDone := make(chan struct{})
	watchErr := make(chan error, 1)
	go func() {
		defer close(watchDone)
		watchErr <- Watch(b, 30*time.Millisecond, hub)
	}()
	t.Cleanup(func() {
		stop()
		select {
		case <-watchDone:
		case <-time.After(30 * time.Second):
			t.Log("the watcher did not stop")
		}
	})
	base := fmt.Sprintf("http://127.0.0.1:%d", port)
	waitUntilTrue(t, 30*time.Second, "the dev server", func() bool {
		select {
		case err := <-serveErr:
			t.Fatalf("dev server exited: %v", err)
		default:
		}
		code, _ := getText(t, base+"/healthz")
		return code == 200
	})
	return &devRun{root: root, base: base, b: b, hub: hub, errc: serveErr, watchErr: watchErr, logs: logs}
}

func (d *devRun) write(t *testing.T, rel, src string) {
	t.Helper()
	writeProjectFile(t, d.root, rel, src)
}

func (d *devRun) remove(t *testing.T, rel string) {
	t.Helper()
	if err := os.Remove(filepath.Join(d.root, filepath.FromSlash(rel))); err != nil {
		t.Fatal(err)
	}
}

// waitBody polls a page until its body holds want.
func (d *devRun) waitBody(t *testing.T, path, want string) string {
	t.Helper()
	var body string
	waitUntilTrue(t, 40*time.Second, path+" to contain "+want, func() bool {
		var code int
		code, body = getText(t, d.base+path)
		return code == 200 && strings.Contains(body, want)
	})
	return body
}

// waitStatus polls a path until it answers status.
func (d *devRun) waitStatus(t *testing.T, path string, status int) {
	t.Helper()
	var code int
	waitUntilTrue(t, 40*time.Second, fmt.Sprintf("%s to answer %d", path, status), func() bool {
		code, _ = getText(t, d.base+path)
		return code == status
	})
}

var devIslandChunkURL = regexp.MustCompile(`/chunks/island_[A-Za-z0-9]+\.js`)

const devToggleIsland = `"use client";
import { useState } from "react";
export default function Toggle() {
  const [on, setOn] = useState(false);
  return <button onClick={() => setOn(!on)}>{on ? "on" : "off"}</button>;
}
`

const devSSRPage = `export const config = { ssr: true };
export default function Page({ params }: any) { return <h1>%s {params.id}</h1>; }
`

func devSSRPageSrc(label string) string { return strings.Replace(devSSRPage, "%s", label, 1) }

// Pages added while the dev server runs are served without a restart: a
// request-time dynamic page next to an existing one (the renderer must learn
// the new route), a page with a new "use client" island (its chunk must
// exist), and a renamed or deleted page.
func TestDevServesFilesAddedWhileRunning(t *testing.T) {
	d := startDevRun(t, map[string]string{
		"src/pages/index.tsx":       "export default function Home() { return <h1>home</h1>; }\n",
		"src/pages/admin/staff.tsx": "export const config = { ssr: true };\nexport default function Staff() { return <h1>staff list</h1>; }\n",
	})
	d.waitBody(t, "/admin/staff/", "staff list")

	// A dynamic request-time page.
	d.write(t, "src/pages/admin/staff/[id].tsx", devSSRPageSrc("member"))
	d.waitBody(t, "/admin/staff/7/", "member 7")
	if strings.Contains(d.logs(), "no such page") {
		t.Errorf("the renderer reported a missing page:\n%s", d.logs())
	}

	// A new island on a new page.
	d.write(t, "src/components/Toggle.tsx", devToggleIsland)
	d.write(t, "src/pages/toggle.tsx", `import Toggle from "../components/Toggle";
export default async function Page() { return <main><h1>toggle page</h1><Toggle /></main>; }
`)
	body := d.waitBody(t, "/toggle/", "toggle page")
	chunks := devIslandChunkURL.FindAllString(body, -1)
	if len(chunks) == 0 {
		t.Fatalf("the page references no island chunk:\n%s", body)
	}
	for _, c := range chunks {
		d.waitStatus(t, c, 200)
	}

	// A second island added to a page that already exists.
	d.write(t, "src/components/Other.tsx", strings.ReplaceAll(devToggleIsland, "Toggle", "Other"))
	d.write(t, "src/pages/toggle.tsx", `import Toggle from "../components/Toggle";
import Other from "../components/Other";
export default async function Page() { return <main><h1>toggle page two</h1><Toggle /><Other /></main>; }
`)
	body = d.waitBody(t, "/toggle/", "toggle page two")
	for _, c := range devIslandChunkURL.FindAllString(body, -1) {
		d.waitStatus(t, c, 200)
	}

	// Rename: the old route goes, the new one is served.
	d.remove(t, "src/pages/admin/staff/[id].tsx")
	d.write(t, "src/pages/admin/staff/[uid].tsx", strings.ReplaceAll(devSSRPageSrc("person"), "params.id", "params.uid"))
	d.waitBody(t, "/admin/staff/9/", "person 9")

	// Delete.
	d.remove(t, "src/pages/toggle.tsx")
	d.waitStatus(t, "/toggle/", 404)
	if code, _ := getText(t, d.base+"/admin/staff/"); code != 200 {
		t.Errorf("an unrelated page broke: %d", code)
	}
}

// A build error stops no one: when the first build fails the dev server stays
// up and shows the error, a good page is served beside the broken one, and
// the fix rebuilds without a restart.
func TestDevStaysUpWhenTheFirstBuildFails(t *testing.T) {
	d := startDevRun(t, map[string]string{
		"src/pages/index.tsx": "export default function Home() { return <h1>home</h1>; }\n",
		"src/pages/bad.tsx":   "export default function Bad() { const x = await Promise.resolve(1); return <h1>bad {x}</h1>; }\n",
	})
	select {
	case err := <-d.errc:
		t.Fatalf("a build error stopped the dev server: %v", err)
	case <-time.After(1500 * time.Millisecond):
	}
	if code, body := httpGet(t, d.base+"/bad/", "text/html"); code == 200 && !strings.Contains(body, "Build failed") && !strings.Contains(body, "async") {
		t.Errorf("the broken page should show the build error:\n%s", body)
	}
	snap := d.hub.Snapshot()
	if snap.BuildOK || len(snap.Diagnostics) == 0 {
		t.Fatalf("the error must reach the overlay: %+v", snap)
	}
	d.waitBody(t, "/", "home")

	d.write(t, "src/pages/bad.tsx", "export default async function Bad() { const x = await Promise.resolve(1); return <h1>fixed {x}</h1>; }\n")
	d.waitBody(t, "/bad/", "fixed 1")
	select {
	case err := <-d.errc:
		t.Fatalf("the dev server stopped: %v", err)
	default:
	}
	waitUntilTrue(t, 20*time.Second, "the build to be clean", func() bool { return d.hub.Snapshot().BuildOK })
}

// With no page building at all, the watcher must not give up (that ended the
// whole dev process): the error stays on screen and the fix is picked up.
func TestDevWatcherSurvivesAFirstBuildWithNoGoodPage(t *testing.T) {
	d := startDevRun(t, map[string]string{
		"src/pages/index.tsx": "export default function Home() { const x = await Promise.resolve(1); return <h1>home {x}</h1>; }\n",
	})
	select {
	case err := <-d.watchErr:
		t.Fatalf("the watcher returned after a failed first build: %v", err)
	case err := <-d.errc:
		t.Fatalf("the dev server stopped: %v", err)
	case <-time.After(1500 * time.Millisecond):
	}
	if code, body := httpGet(t, d.base+"/", "text/html"); !strings.Contains(body, "Build failed") {
		t.Errorf("the page should show the build error (%d):\n%s", code, body)
	}
	d.write(t, "src/pages/index.tsx", "export default function Home() { return <h1>home fixed</h1>; }\n")
	d.waitBody(t, "/", "home fixed")
	waitUntilTrue(t, 20*time.Second, "the build to be clean", func() bool { return d.hub.Snapshot().BuildOK })
}

// A client chunk that fails to build (KR8009: an island pulls in a Node-only
// module) leaves the route broken only until the import is fixed: the next
// rebuild serves the page again, with its island, without a restart. Both
// shapes of the failure are covered: an edit of a helper that was already
// part of the page, and a helper file created with the bad import.
func TestDevRouteRecoversAfterAFailedClientChunk(t *testing.T) {
	const editor = `"use client";
import { useState } from "react";
import { label } from "../lib/label";
export default function Editor() {
  const [n, setN] = useState(0);
  return <button onClick={() => setN(n + 1)}>{label} {n}</button>;
}
`
	page := func(title string) string {
		return `import Editor from "../components/Editor";
export default async function Site() { return <main><title>` + title + `</title><h1>site ` + title + `</h1><Editor /></main>; }
`
	}
	const bad = `import net from "node:net";
export const label = typeof net.createConnection;
`
	const good = "export const label = \"editor\";\n"

	for _, tc := range []struct {
		name         string
		helperBefore bool
	}{{"edited helper", true}, {"created helper", false}} {
		t.Run(tc.name, func(t *testing.T) {
			files := map[string]string{
				"src/pages/index.tsx":       "export default function Home() { return <h1>home</h1>; }\n",
				"src/pages/site.tsx":        page("one"),
				"src/components/Editor.tsx": editor,
			}
			if tc.helperBefore {
				files["src/lib/label.ts"] = good
			} else {
				files["src/components/Editor.tsx"] = strings.ReplaceAll(strings.ReplaceAll(editor, `import { label } from "../lib/label";`, ""), "{label}", "editor")
			}
			d := startDevRun(t, files)
			d.waitBody(t, "/site/", "site one")

			d.write(t, "src/lib/label.ts", bad)
			if !tc.helperBefore {
				d.write(t, "src/components/Editor.tsx", editor)
			}
			waitUntilTrue(t, 40*time.Second, "KR8009 to be reported", func() bool {
				for _, e := range d.hub.Snapshot().Diagnostics {
					if e.Code == diag.CodeIslandAsset {
						return true
					}
				}
				return false
			})

			// Any structural change while the chunk is broken rebuilds the whole
			// site, and the broken route is left out of the build manifests.
			d.write(t, "src/pages/other.tsx", "export default function Other() { return <h1>other</h1>; }\n")
			d.waitBody(t, "/other/", "other")

			d.write(t, "src/lib/label.ts", good)
			body := d.waitBody(t, "/site/", "site one")
			if strings.Contains(body, "Build failed") {
				t.Fatalf("the failed state was not cleared:\n%s", body)
			}
			waitUntilTrue(t, 20*time.Second, "the build to be clean", func() bool { return d.hub.Snapshot().BuildOK })
			// The route follows later edits too, and its island chunk exists.
			d.write(t, "src/pages/site.tsx", page("two"))
			body = d.waitBody(t, "/site/", "site two")
			chunks := devIslandChunkURL.FindAllString(body, -1)
			if len(chunks) == 0 {
				t.Fatalf("no island chunk on the page:\n%s", body)
			}
			for _, c := range chunks {
				d.waitStatus(t, c, 200)
			}
		})
	}
}

const devProviderSrc = `"use client";
import { createContext, useContext } from "react";
const Ctx = createContext<{ label: string } | null>(null);
export function Provider({ children }: any) {
  return <Ctx.Provider value={{ label: "LABEL" }}>{children}</Ctx.Provider>;
}
export function useLabel() {
  const v = useContext(Ctx);
  if (!v) throw new Error("useLabel needs the provider");
  return v.label;
}
`

// A layout renders a "use client" provider that owns a context, and each page
// has an island that reads it through a hook. Edits to the provider, to the
// layout and to an island must reach the server render of every page, and the
// islands must keep finding the layout's provider: the render modules of
// client components were cached by the renderer for good, so after a few
// reloads a module first loaded after an edit came from a newer build than the
// layout's provider and its hook saw no provider (the page failed with the
// app's own "needs the layout" guard).
func TestDevClientModulesStayOneGenerationAfterReloads(t *testing.T) {
	files := map[string]string{
		"src/pages/_layout.tsx": `import { Provider } from "../components/provider";
export default async function Layout({ children }: any) { return <Provider>{children}</Provider>; }
`,
		"src/components/provider.tsx": strings.ReplaceAll(devProviderSrc, "LABEL", "v1"),
	}
	pages := []string{"a", "b", "c"}
	for _, p := range pages {
		files["src/components/Show_"+p+".tsx"] = `"use client";
import { useLabel } from "./provider";
export default function Show() { return <b>` + p + `={useLabel()}</b>; }
`
		files["src/pages/"+p+".tsx"] = `import Show from "../components/Show_` + p + `";
export default async function P() { return <main><h1>page ` + p + `</h1><Show /></main>; }
`
	}
	d := startDevRun(t, files)
	d.waitBody(t, "/a/", "a=v1")

	check := func(label string) {
		t.Helper()
		for _, p := range pages {
			d.waitBody(t, "/"+p+"/", p+"="+label)
			if _, body := getText(t, d.base+"/"+p+"/"); strings.Contains(body, "needs the provider") {
				t.Fatalf("/%s lost the layout's provider:\n%s", p, body)
			}
		}
	}
	// Pages b and c are first served after the provider changed.
	d.write(t, "src/components/provider.tsx", strings.ReplaceAll(devProviderSrc, "LABEL", "v2"))
	check("v2")
	d.write(t, "src/pages/_layout.tsx", `import { Provider } from "../components/provider";
export default async function Layout({ children }: any) { return <div class="shell"><Provider>{children}</Provider></div>; }
`)
	d.waitBody(t, "/b/", `class="shell"`)
	check("v2")
	d.write(t, "src/components/provider.tsx", strings.ReplaceAll(devProviderSrc, "LABEL", "v3"))
	check("v3")

	// An island edited alone.
	d.write(t, "src/components/Show_c.tsx", `"use client";
import { useLabel } from "./provider";
export default function Show() { return <b>c=edited-{useLabel()}</b>; }
`)
	d.waitBody(t, "/c/", "c=edited-v3")
}

// A client component rendered by another one in the layout (a shell inside the
// provider) is found through the renderer's registry of client components.
// Each page bundle is reloaded on its own after a rebuild, so the registry
// held whichever page's copy was loaded last and a component of one page's
// graph met a provider of another's: the shell's hook saw no provider and
// every page failed until the server restarted.
func TestDevNestedClientComponentsKeepTheirOwnGeneration(t *testing.T) {
	const shell = `"use client";
import { useLabel } from "./provider";
export function Shell({ children }: any) { return <section>shell {useLabel()}{children}</section>; }
`
	files := map[string]string{
		"src/pages/_layout.tsx": `import { Provider } from "../components/provider";
import { Shell } from "../components/shell";
export default async function Layout({ children }: any) { return <Provider><Shell>{children}</Shell></Provider>; }
`,
		"src/components/provider.tsx": strings.ReplaceAll(devProviderSrc, "LABEL", "v1"),
		"src/components/shell.tsx":    shell,
	}
	pages := []string{"a", "b", "c"}
	for _, p := range pages {
		files["src/components/Show_"+p+".tsx"] = `"use client";
import { useLabel } from "./provider";
export default function Show() { return <b>` + p + `={useLabel()} one</b>; }
`
		files["src/pages/"+p+".tsx"] = `import Show from "../components/Show_` + p + `";
export default async function P() { return <main><h1>page ` + p + `</h1><Show /></main>; }
`
	}
	d := startDevRun(t, files)
	d.waitBody(t, "/a/", "a=v1 one")

	// Edits to an island rebuild its page only, so the three pages end up
	// loaded at different times.
	for i, p := range pages {
		d.write(t, "src/components/Show_"+p+".tsx", `"use client";
import { useLabel } from "./provider";
export default function Show() { return <b>`+p+`={useLabel()} edit`+strconv.Itoa(i)+`</b>; }
`)
		d.waitBody(t, "/"+p+"/", p+"=v1 edit"+strconv.Itoa(i))
	}
	for _, p := range pages {
		_, body := getText(t, d.base+"/"+p+"/")
		if strings.Contains(body, "needs the provider") || !strings.Contains(body, "shell v1") {
			t.Fatalf("/%s lost the layout's provider:\n%s", p, body)
		}
	}
}
