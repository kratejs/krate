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

// useIDFiles is a server-rendered app whose islands all call useId(): a form
// field, and a menu whose trigger, controlled element and portaled content
// each take an id. The layout renders a field after the page, so the number
// of islands in front of it differs from page to page.
var useIDFiles = map[string]string{
	"src/components/Field.tsx": `"use client";
import { useId, useState } from "react";
export default function Field({ label }: { label: string }) {
  const id = useId();
  const [v, setV] = useState("");
  return <div class="field"><label htmlFor={id}>{label}</label><input id={id} value={v} onInput={(e: any) => setV(e.target.value)} /></div>;
}`,
	"src/components/Menu.tsx": `"use client";
import { useId, useState } from "react";
import { createPortal } from "react-dom";
function Content({ owner }: { owner: string }) {
  const id = useId();
  return <div id={"content-" + id} role="menu" aria-labelledby={owner}>items</div>;
}
export default function Menu({ name }: { name: string }) {
  const id = useId();
  const [open, setOpen] = useState(false);
  return (
    <div class="menu" data-name={name}>
      <button id={"trigger-" + id} aria-controls={"content-" + id} onClick={() => setOpen(!open)}>{name}</button>
      {open && createPortal(<Content owner={"trigger-" + id} />, document.body)}
    </div>
  );
}`,
	"src/pages/s/_layout.tsx": `import Field from "../../components/Field";
export default async function Layout({ children }: { children: any }) {
  await Promise.resolve();
  return <div id="shell">{children}<Field label="footer" /></div>;
}`,
	"src/pages/s/a.tsx": `import Menu from "../../components/Menu";
import Field from "../../components/Field";
export default async function A() {
  await Promise.resolve();
  return <main id="page-a"><Menu name="one" /><Menu name="two" /><Field label="a" /><a id="to-b" data-krate-link href="/s/b/">b</a></main>;
}`,
	"src/pages/s/b.tsx": `import Menu from "../../components/Menu";
import Field from "../../components/Field";
export default async function B() {
  await Promise.resolve();
  return <main id="page-b"><Field label="b1" /><Field label="b2" /><Menu name="three" /><a id="to-a" data-krate-link href="/s/a/">a</a></main>;
}`,
	"src/pages/index.tsx": `import { useState } from "react";
export default function Home() {
  const [n, setN] = useState(0);
  return <button id="count" onClick={() => setN(n + 1)}>{n}</button>;
}`,
}

func serveUseIDApp(t *testing.T) string {
	t.Helper()
	ensureRuntimeDist(t)
	if _, err := exec.LookPath("node"); err != nil {
		requireE2E(t, "node is not installed")
	}
	b, root := scProject(t, useIDFiles)
	withPreact(t, root)
	linkRuntimeDist(t, root)
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
	return base
}

func idsOf(html string) []string {
	var out []string
	for _, m := range regexp.MustCompile(`\sid="([^"]+)"`).FindAllStringSubmatch(html, -1) {
		out = append(out, m[1])
	}
	return out
}

// TestUseIDIsUniqueAcrossIslands: every island numbers useId() from 0, so the
// server render gave two islands on one page the same ids (P0-0 ...). Each
// island now carries a prefix of its own, in its marker and in its ids.
func TestUseIDIsUniqueAcrossIslands(t *testing.T) {
	t.Parallel()
	base := serveUseIDApp(t)
	resp, err := http.Get(base + "/s/a/")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	html := string(body)
	if resp.StatusCode != 200 {
		t.Fatalf("status %d:\n%s", resp.StatusCode, html)
	}
	var trig []string
	seen := map[string]bool{}
	for _, id := range idsOf(html) {
		if seen[id] {
			t.Errorf("id %q repeats in the server render:\n%s", id, html)
		}
		seen[id] = true
		if strings.HasPrefix(id, "trigger-") {
			trig = append(trig, id)
		}
	}
	if len(trig) != 2 {
		t.Fatalf("expected two menu triggers, got %v:\n%s", trig, html)
	}
	// Four islands, four prefixes in the markers.
	prefixes := map[string]bool{}
	for _, m := range regexp.MustCompile(`<!--krate-island:(.*?)-->`).FindAllStringSubmatch(html, -1) {
		var isl struct{ I string }
		if err := json.Unmarshal([]byte(m[1]), &isl); err != nil || isl.I == "" {
			t.Fatalf("island marker without an id prefix: %s", m[1])
		}
		prefixes[isl.I] = true
	}
	if len(prefixes) != 4 {
		t.Errorf("expected 4 distinct island prefixes, got %v", prefixes)
	}
	// A second request numbers the same way: the ids are stable per page.
	resp, err = http.Get(base + "/s/a/")
	if err != nil {
		t.Fatal(err)
	}
	again, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if strings.Join(idsOf(string(again)), ",") != strings.Join(idsOf(html), ",") {
		t.Errorf("ids differ between two renders of one page:\n%v\n%v", idsOf(html), idsOf(string(again)))
	}
}

// TestUseIDUniqueAfterHydrationAndNavigation hydrates the page in a browser,
// opens the portaled menu content (an id taken after hydration), navigates to
// another page through the router (the layout's field is a kept island) and
// checks that no id repeats at any step, the ids hydration kept are the
// server's, and nothing was logged.
func TestUseIDUniqueAfterHydrationAndNavigation(t *testing.T) {
	t.Parallel()
	browser := strings.ToLower(strings.TrimSpace(os.Getenv("KRATE_BROWSER")))
	if browser == "" {
		t.Skip("set KRATE_BROWSER=chromium|firefox|webkit to run the useId browser test")
	}
	if !canStopInProcessServer {
		t.Skip("in-process server needs a unix host")
	}
	if err := exec.Command("node", "-e", "require.resolve('playwright')").Run(); err != nil {
		requireE2E(t, "playwright is not installed (run pnpm install)")
	}
	base := serveUseIDApp(t)
	script, err := filepath.Abs(filepath.Join("testdata", "client_useid.mjs"))
	if err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command("node", script, base, browser).CombinedOutput()
	if err != nil {
		if strings.Contains(string(out), "Executable doesn't exist") {
			requireE2E(t, "%s browser is not installed: %s", browser, out)
		}
		t.Fatalf("browser script failed: %v\n%s", err, out)
	}
	var res struct {
		Server, Hydrated, Opened, Navigated, Back []string
		Dups                                      map[string][]string
		Errors                                    []string
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if err := json.Unmarshal([]byte(lines[len(lines)-1]), &res); err != nil {
		t.Fatalf("parsing browser output: %v\n%s", err, out)
	}
	if len(res.Dups) > 0 || len(res.Errors) > 0 {
		t.Fatalf("duplicate ids or console output: %+v", res)
	}
	if strings.Join(res.Server, ",") != strings.Join(res.Hydrated, ",") {
		t.Errorf("hydration changed the ids:\nserver   %v\nhydrated %v", res.Server, res.Hydrated)
	}
	if len(res.Opened) != len(res.Hydrated)+1 {
		t.Errorf("opening the menu should add one id (its portaled content): %v -> %v", res.Hydrated, res.Opened)
	}
	if len(res.Navigated) == 0 || len(res.Back) == 0 {
		t.Errorf("navigation produced no ids: %+v", res)
	}
}
