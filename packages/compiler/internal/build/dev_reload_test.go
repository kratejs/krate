package build

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/kratejs/krate/packages/compiler/internal/config"
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
