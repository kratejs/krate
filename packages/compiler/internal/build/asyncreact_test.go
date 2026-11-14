package build

import (
	"encoding/json"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// linkReactPackages makes the examples' real lucide-react and radix-ui
// packages (and the helpers shadcn components use) resolvable from a test
// project, skipping when the examples are not installed.
func linkReactPackages(t *testing.T, root string) {
	t.Helper()
	repo, err := repoRootPath()
	if err != nil {
		t.Fatal(err)
	}
	src := filepath.Join(repo, "examples", "node_modules")
	if _, err := os.Stat(filepath.Join(src, "lucide-react")); err != nil {
		requireE2E(t, "examples dependencies not installed: %v", err)
		t.Skip("examples dependencies not installed")
	}
	for _, name := range []string{"lucide-react", "radix-ui"} {
		if err := os.Symlink(filepath.Join(src, name), filepath.Join(root, "node_modules", name)); err != nil {
			t.Skipf("cannot link %s: %v", name, err)
		}
	}
}

// shadcnCheckbox is a shadcn-style wrapper: a plain module (no "use client")
// that renders a Radix primitive.
const shadcnCheckbox = `import * as React from "react"
import { Checkbox as CheckboxPrimitive } from "radix-ui"

function Checkbox({ ...props }: React.ComponentProps<typeof CheckboxPrimitive.Root>) {
  return (
    <CheckboxPrimitive.Root data-slot="checkbox" {...props}>
      <CheckboxPrimitive.Indicator>v</CheckboxPrimitive.Indicator>
    </CheckboxPrimitive.Root>
  )
}
export { Checkbox }
`

// asyncReactProject is a project whose async page renders compiled React
// components: an icon (stateless), a Radix checkbox, a Radix dialog (compound)
// and the global Link.
func asyncReactProject(t *testing.T) (*Builder, string) {
	t.Helper()
	b, root := scProject(t, map[string]string{
		"src/components/checkbox.tsx": shadcnCheckbox,
		"src/components/icons.ts":     `export { Check } from "lucide-react";`,
		"src/components/switch.tsx": `import * as Radix from "radix-ui";
export function Switch(props: any) {
  return <Radix.Switch.Root {...props}><Radix.Switch.Thumb /></Radix.Switch.Root>;
}`,
		"src/pages/index.tsx": `import { Loader2 } from "lucide-react";
import { Dialog } from "radix-ui";
import { Checkbox } from "../components/checkbox";
import { Check } from "../components/icons";
import { Switch } from "../components/switch";

export default async function Home() {
  const who = await Promise.resolve("ada");
  return (
    <main>
      <p id="who">hello {who}</p>
      <Loader2 id="spinner" />
      <Checkbox id="cb" />
      <Check id="check" />
      <Switch id="sw" />
      <Dialog.Root>
        <Dialog.Trigger id="open">Open</Dialog.Trigger>
        <Dialog.Portal>
          <Dialog.Content>
            <Dialog.Title>Hello dialog</Dialog.Title>
            <Dialog.Description>Dialog body</Dialog.Description>
            <Dialog.Close id="close">Close</Dialog.Close>
          </Dialog.Content>
        </Dialog.Portal>
      </Dialog.Root>
      <Link href="/other">other</Link>
    </main>
  );
}`,
	})
	withPreact(t, root)
	linkReactPackages(t, root)
	e2eEnv(t)
	if err := linkRuntimePackage(t, filepath.Join(root, "node_modules", "@krate")); err != nil {
		t.Fatal(err)
	}
	return b, root
}

func getBody(t *testing.T, url string) (int, http.Header, string) {
	t.Helper()
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := client.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, resp.Header, string(data)
}

// TestAsyncPageRendersCompiledReactComponents is the KR8013 case of the gap
// report: an async page uses a lucide-react icon, a shadcn-style Radix
// checkbox and Radix dialog parts, and the global Link. The icon is plain
// HTML, the stateful components are islands rendered by the server first.
func TestAsyncPageRendersCompiledReactComponents(t *testing.T) {
	b, root := asyncReactProject(t)
	if err := b.BuildAll(); err != nil {
		t.Fatalf("BuildAll: %v", err)
	}
	base, err := startKrateServer(t, root, b.Cfg)
	if err != nil {
		t.Fatalf("serve: %v", err)
	}
	status, _, html := getBody(t, base+"/")
	if status != 200 {
		t.Fatalf("status %d:\n%s", status, html)
	}
	if strings.Contains(html, "[object Object]") {
		t.Fatalf("a compiled component rendered as a tag name:\n%s", html)
	}
	for _, want := range []string{
		`hello ada`,
		`<svg `, `id="spinner"`, `lucide-loader-circle`,
		`role="checkbox"`, `data-slot="checkbox"`, `role="switch"`, `id="check"`,
		`<a href="/other" data-krate-link data-prefetch>other</a>`,
		`id="open"`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("response is missing %q:\n%s", want, html)
		}
	}
	// The icon is not inside an island: it needs no JavaScript.
	if i := strings.Index(html, `id="spinner"`); i < 0 || strings.LastIndex(html[:i], "<krate-island") > strings.LastIndex(html[:i], "</krate-island>") {
		t.Errorf("the stateless icon must not be wrapped in an island:\n%s", html)
	}
	// The checkbox, the switch and the dialog are islands, and the dialog
	// parts share one.
	if n := strings.Count(html, "<krate-island"); n != 3 {
		t.Errorf("expected 3 islands (checkbox, switch, dialog), got %d:\n%s", n, html)
	}
}

// TestCompiledReactComponentsBrowser hydrates the page above in a browser:
// the Radix checkbox toggles, the dialog opens through its trigger (the
// trigger and the content share the dialog's context) and closes again.
func TestCompiledReactComponentsBrowser(t *testing.T) {
	browser := strings.ToLower(strings.TrimSpace(os.Getenv("KRATE_BROWSER")))
	if browser == "" {
		t.Skip("set KRATE_BROWSER=chromium|firefox|webkit to run the compiled island browser test")
	}
	if err := exec.Command("node", "-e", "require.resolve('playwright')").Run(); err != nil {
		requireE2E(t, "playwright is not installed (run pnpm install)")
	}
	b, root := asyncReactProject(t)
	if err := b.BuildAll(); err != nil {
		t.Fatalf("BuildAll: %v", err)
	}
	base, err := startKrateServer(t, root, b.Cfg)
	if err != nil {
		t.Fatalf("serve: %v", err)
	}
	script, err := filepath.Abs(filepath.Join("testdata", "compiled_island_click.mjs"))
	if err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command("node", script, base+"/", browser).CombinedOutput()
	if err != nil {
		if strings.Contains(string(out), "Executable doesn't exist") {
			requireE2E(t, "%s browser is not installed: %s", browser, out)
		}
		t.Fatalf("browser script failed: %v\n%s", err, out)
	}
	var res struct {
		Before, After, Title string
		IconInIsland         bool
		IconCount            int
		Errors               []string
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if err := json.Unmarshal([]byte(lines[len(lines)-1]), &res); err != nil {
		t.Fatalf("parsing browser output: %v\n%s", err, out)
	}
	if res.Before != "false" || res.After != "true" || res.Title != "Hello dialog" || res.IconInIsland || res.IconCount != 2 || len(res.Errors) > 0 {
		t.Fatalf("unexpected browser result: %+v", res)
	}
}

// TestAsyncLayouts covers R3: an async layout awaits data and renders its
// children for an async page and a synchronous one; a nested async layout
// gates its subtree with redirect(); pages outside stay static.
func TestAsyncLayouts(t *testing.T) {
	b, root := scProject(t, map[string]string{
		"src/pages/app/_layout.tsx": `import { Loader2 } from "lucide-react";
export default async function Layout({ children }: any) {
  const user = await Promise.resolve({ name: "ada" });
  return <div class="shell"><nav>hello {user.name} <Link href="/about">about</Link> <Loader2 /></nav>{children}</div>;
}`,
		"src/pages/app/index.tsx": `export default function Page() { return <main>app home</main>; }`,
		"src/pages/app/data.tsx":  `export default async function Page() { await Promise.resolve(); return <main>app data</main>; }`,
		"src/pages/app/admin/_layout.tsx": `import { redirect } from "@krate/runtime/server";
export default async function Layout({ children }: any) {
  const role = await Promise.resolve("guest");
  if (role !== "admin") redirect("/app");
  return <section>{children}</section>;
}`,
		"src/pages/app/admin/index.tsx": `export default function Page() { return <main>admin</main>; }`,
		"src/pages/about.tsx":           `export default function Page() { return <main id="about">about page</main>; }`,
	})
	withPreact(t, root)
	linkReactPackages(t, root)
	e2eEnv(t)
	if err := linkRuntimePackage(t, filepath.Join(root, "node_modules", "@krate")); err != nil {
		t.Fatal(err)
	}
	if err := b.BuildAll(); err != nil {
		t.Fatalf("BuildAll: %v", err)
	}
	m := readManifest(t, root)
	if p, ok := manifestPage(m, "/about"); !ok || p.Mode != RenderSSG {
		t.Errorf("a page outside the async layout must stay static, got %+v (found %v)", p, ok)
	}
	if static, err := os.ReadFile(filepath.Join(root, "dist", "about", "index.html")); err != nil || !strings.Contains(string(static), "about page") {
		t.Errorf("the static page must be baked into its file: %v", err)
	}
	base, err := startKrateServer(t, root, b.Cfg)
	if err != nil {
		t.Fatalf("serve: %v", err)
	}
	for _, path := range []string{"/app/", "/app/data/"} {
		status, _, html := getBody(t, base+path)
		if status != 200 || !strings.Contains(html, `<nav>hello ada <a href="/about" data-krate-link data-prefetch>about</a> <svg `) {
			t.Errorf("%s: status %d, the async layout did not render around the page:\n%s", path, status, html)
		}
	}
	if _, _, html := getBody(t, base+"/app/"); !strings.Contains(html, "app home") {
		t.Errorf("the layout dropped its children:\n%s", html)
	}
	status, header, html := getBody(t, base+"/app/admin/")
	if status != 302 && status != 307 || header.Get("Location") != "/app" {
		t.Errorf("the nested layout must redirect a guest: status %d location %q\n%s", status, header.Get("Location"), html)
	}
	if strings.Contains(html, "admin") {
		t.Errorf("the gated page must not render:\n%s", html)
	}
}

// TestStatelessCompiledComponentsShipNoIslandJS: a page whose compiled
// components are all stateless (an icon) loads no island JavaScript, and an
// async child component of a static page renders a compiled component too.
func TestStatelessCompiledComponentsShipNoIslandJS(t *testing.T) {
	b, root := scProject(t, map[string]string{
		"src/components/UserNav.tsx": `import { Loader2 } from "lucide-react";
export async function UserNav() {
  const name = await Promise.resolve("ada");
  return <nav id="nav">{name} <Loader2 /></nav>;
}`,
		"src/pages/index.tsx": `import { UserNav } from "../components/UserNav";
export default function Home() { return <main><h1>static home</h1><UserNav /></main>; }`,
		"src/pages/only.tsx": `import { Loader2 } from "lucide-react";
export default async function Only() { await Promise.resolve(); return <main><Loader2 id="only" /></main>; }`,
	})
	withPreact(t, root)
	linkReactPackages(t, root)
	e2eEnv(t)
	if err := linkRuntimePackage(t, filepath.Join(root, "node_modules", "@krate")); err != nil {
		t.Fatal(err)
	}
	if err := b.BuildAll(); err != nil {
		t.Fatalf("BuildAll: %v", err)
	}
	base, err := startKrateServer(t, root, b.Cfg)
	if err != nil {
		t.Fatalf("serve: %v", err)
	}
	_, _, only := getBody(t, base+"/only/")
	if !strings.Contains(only, `id="only"`) || !strings.Contains(only, "<svg ") {
		t.Errorf("the icon did not render:\n%s", only)
	}
	if strings.Contains(only, "krate-island") || strings.Contains(only, "island-loader") {
		t.Errorf("a stateless icon must ship no island code:\n%s", only)
	}
	_, _, home := getBody(t, base+"/")
	if !strings.Contains(home, "static home") || !strings.Contains(home, `<nav id="nav">ada <svg `) {
		t.Errorf("the async child of a static page did not render its icon:\n%s", home)
	}
}

// TestErrorBoundaryUnderAsyncLayout: a segment's error.tsx below an async
// layout wraps the page per request. A page that renders is the normal page
// inside the layout; a page that throws renders the boundary's UI inside the
// layout (status 500, no-store) and ships the boundary's client JS.
func TestErrorBoundaryUnderAsyncLayout(t *testing.T) {
	b, root := scProject(t, map[string]string{
		"src/pages/(app)/_layout.tsx": `export default async function Layout({ children }: any) {
  const user = await Promise.resolve({ name: "ada" });
  return <div class="shell"><nav>hello {user.name}</nav>{children}</div>;
}`,
		"src/pages/(app)/dashboard/error.tsx": `'use client';
export default function Error({ error, reset }: any) {
  return <div><p>Something went wrong</p><button onClick={() => reset()}>Try again</button></div>;
}`,
		"src/pages/(app)/dashboard/index.tsx": `export default function Page() { return <main>dashboard ok</main>; }`,
		"src/pages/(app)/dashboard/boom.tsx":  `export default async function Page() { await Promise.resolve(); throw new Error("db down"); }`,
	})
	withPreact(t, root)
	linkReactPackages(t, root)
	e2eEnv(t)
	if err := linkRuntimePackage(t, filepath.Join(root, "node_modules", "@krate")); err != nil {
		t.Fatal(err)
	}
	if err := b.BuildAll(); err != nil {
		t.Fatalf("BuildAll: %v", err)
	}
	base, err := startKrateServer(t, root, b.Cfg)
	if err != nil {
		t.Fatalf("serve: %v", err)
	}
	status, _, html := getBody(t, base+"/dashboard/")
	if status != 200 || !strings.Contains(html, "<nav>hello ada</nav>") || !strings.Contains(html, "dashboard ok") || strings.Contains(html, "Something went wrong") {
		t.Errorf("the page must render normally inside the layout: status %d\n%s", status, html)
	}
	status, header, html := getBody(t, base+"/dashboard/boom/")
	if status != 500 || header.Get("Cache-Control") != "no-store" {
		t.Errorf("a caught error must answer 500 with no-store, got %d %q", status, header.Get("Cache-Control"))
	}
	if !strings.Contains(html, "<nav>hello ada</nav>") || !strings.Contains(html, "Something went wrong") {
		t.Errorf("a throwing page must render the boundary inside the layout: status %d\n%s", status, html)
	}
	if strings.Contains(html, "db down") {
		t.Errorf("the error message must not leak into the page:\n%s", html)
	}
	m := regexp.MustCompile(`data-src="([^"]+)"`).FindStringSubmatch(html)
	if m == nil || !strings.Contains(html, "&quot;digest&quot;") {
		t.Fatalf("the boundary must hydrate as an island with a sanitized error:\n%s", html)
	}
	if status, _, js := getBody(t, base+m[1]); status != 200 || !strings.Contains(js, "location.reload()") {
		t.Errorf("boundary island chunk %s: status %d\n%s", m[1], status, js)
	}
}

// TestAsyncLayoutAndPageShareAPackageBoundary: an async layout and the pages
// under it import different icons from lucide-react, some of them used as
// values (a prop, an array, a member read) instead of JSX tags. The page's server bundle
// carries one package boundary per tree for that specifier; they must be
// served as one module, or the build fails with `No matching export in
// "krate-pkg-boundary:lucide-react"` for the icons of whichever tree lost.
func TestAsyncLayoutAndPageShareAPackageBoundary(t *testing.T) {
	b, root := scProject(t, map[string]string{
		"src/pages/_layout.tsx": `import { Download } from "lucide-react";
export default async function Layout({ children }: any) {
  await Promise.resolve();
  return <div><Download id="dl" />{children}</div>;
}`,
		"src/pages/index.tsx": `import { ArrowLeft, Rocket } from "lucide-react";
export default function Home() { return <main><ArrowLeft id="back" /><Rocket id="rocket" /></main>; }`,
		"src/pages/other.tsx": `import { Download, Heart } from "lucide-react";
export default function Other() { return <main><Heart id="heart" /><Download id="dl2" /></main>; }`,
		"src/pages/values.tsx": `import { Download, Rocket, Star } from "lucide-react";
import type { LucideIcon } from "lucide-react";
const items: { icon: LucideIcon }[] = [{ icon: Rocket }];
const label = Star.displayName;
function Slot(props: { icon: LucideIcon }) { return <props.icon id="slot" />; }
export default function Values() {
  const item = items[0];
  return <main><item.icon id="item" /><Slot icon={Download} /><i id="label">{String(label)}</i></main>;
}`,
	})
	withPreact(t, root)
	linkReactPackages(t, root)
	e2eEnv(t)
	if err := linkRuntimePackage(t, filepath.Join(root, "node_modules", "@krate")); err != nil {
		t.Fatal(err)
	}
	if err := b.BuildAll(); err != nil {
		t.Fatalf("BuildAll: %v", err)
	}
	base, err := startKrateServer(t, root, b.Cfg)
	if err != nil {
		t.Fatalf("serve: %v", err)
	}
	for path, ids := range map[string][]string{"/": {"dl", "back", "rocket"}, "/other/": {"dl", "dl2", "heart"}, "/values/": {"dl", "item", "slot"}} {
		_, _, html := getBody(t, base+path)
		for _, id := range ids {
			if !regexp.MustCompile(`<svg [^>]*id="` + id + `"`).MatchString(html) {
				t.Errorf("%s: icon %q did not render:\n%s", path, id, html)
			}
		}
		if strings.Contains(html, "krate-island") {
			t.Errorf("%s: stateless icons must not become islands:\n%s", path, html)
		}
	}
}
