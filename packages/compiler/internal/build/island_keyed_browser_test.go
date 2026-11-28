package build

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// islandKeyedFiles is an admin review list: a server page renders one keyed
// "use client" card per row. A card hides itself at once (optimistic), then
// calls a server action that deletes its row and refreshes the page; "hide
// me" hides a card without the server knowing. The deleted rows live in a
// cookie, so the page and the action agree without a database.
var islandKeyedFiles = map[string]string{
	"src/actions/rows.ts": `"use server";
import { cookies } from "@krate/runtime/server";
export async function deleteRow(id: string) {
  const jar = await cookies();
  const gone = (jar.get("gone")?.value ?? "").split(".").filter(Boolean);
  jar.set("gone", [...gone, id].join("."));
  return true;
}
`,
	"src/components/row-card.tsx": `"use client";
import { useState } from "react";
import { useRouter } from "@krate/runtime";
import { deleteRow } from "../actions/rows";
export function RowCard({ id }: { id: string }) {
  const router = useRouter();
  const [hidden, setHidden] = useState(false);
  if (hidden) return null;
  return (
    <article class="card" data-id={id}>
      {id}
      <button class="del" onClick={async () => { setHidden(true); if (await deleteRow(id)) router.refresh(); else setHidden(false); }}>delete</button>
      <button class="hide" onClick={() => setHidden(true)}>hide me</button>
    </article>
  );
}
`,
	"src/pages/rows.tsx": `import { cookies } from "@krate/runtime/server";
import { RowCard } from "../components/row-card";
export default async function Rows() {
  const gone = ((await cookies()).get("gone")?.value ?? "").split(".");
  const rows = ["a", "b", "c"].filter((r) => !gone.includes(r));
  return <main><p id="count">{rows.length}</p><section id="list">{rows.map((r) => <RowCard key={r} id={r} />)}</section></main>;
}`,
	// A compiled page with state gives the site its runtime chunk (the router).
	"src/pages/index.tsx": `import { useState } from "react";
export default function Home() {
  const [n, setN] = useState(0);
  return <button id="count" onClick={() => setN(n + 1)}>{n}</button>;
}`,
}

// TestBrowserKeyedIslandsKeepStateAcrossRefresh deletes rows of the list
// above, each followed by router.refresh(), and checks after every step that
// the cards shown are the server's rows minus the ones hidden by their own
// state: a refresh pairs the cards by key, so a card never takes the state of
// the row that was before it (the optimistic "hidden" of a deleted row).
func TestBrowserKeyedIslandsKeepStateAcrossRefresh(t *testing.T) {
	t.Parallel()
	browser := strings.ToLower(strings.TrimSpace(os.Getenv("KRATE_BROWSER")))
	if browser == "" {
		t.Skip("set KRATE_BROWSER=chromium|firefox|webkit to run browser checks")
	}
	if !canStopInProcessServer {
		t.Skip("in-process server needs a unix host")
	}
	ensureRuntimeDist(t)
	if err := exec.Command("node", "-e", "require.resolve('playwright')").Run(); err != nil {
		requireE2E(t, "playwright is not installed (run pnpm install)")
	}
	b, root := scProject(t, islandKeyedFiles)
	withPreact(t, root)
	linkRuntimeDist(t, root)
	if err := linkRuntimePackage(t, filepath.Join(root, "node_modules", "@krate")); err != nil {
		t.Fatal(err)
	}
	if out, err := captureBuild(b); err != nil {
		t.Fatalf("BuildAll: %v\n%s", err, out)
	}
	base, err := startKrateServer(t, root, b.Cfg)
	if err != nil {
		t.Fatalf("serve: %v", err)
	}
	script, err := filepath.Abs(filepath.Join("testdata", "island_keyed.mjs"))
	if err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command("node", script, base, browser).CombinedOutput()
	if err != nil {
		if strings.Contains(string(out), "Executable doesn't exist") || strings.Contains(string(out), "browserType.launch") {
			requireE2E(t, "%s browser is not installed: %s", browser, out)
		}
		t.Fatalf("browser script failed: %v\n%s", err, out)
	}
	type view struct {
		Count string
		Shown []string
	}
	var res struct {
		Steps  map[string]view
		Order  []string
		Errors []string
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if err := json.Unmarshal([]byte(lines[len(lines)-1]), &res); err != nil {
		t.Fatalf("parsing browser output: %v\n%s", err, out)
	}
	wants := map[string]view{
		"load": {"3", []string{"a", "b", "c"}},
		// c hides itself: only its own state changes.
		"hide-c": {"3", []string{"a", "b"}},
		// Deleting the first row: b keeps its own (visible) state, c stays hidden.
		"delete-a": {"2", []string{"b"}},
		// Deleting b leaves c, still hidden by its own state.
		"delete-b": {"1", nil},
		// A reload shows c again (its state is gone, the server still has it).
		"reload": {"1", []string{"c"}},
		// Deleting the last row empties the list because the server says so.
		"delete-c": {"0", nil},
	}
	for _, name := range res.Order {
		w, ok := wants[name]
		if !ok {
			continue
		}
		v := res.Steps[name]
		if v.Count != w.Count || strings.Join(v.Shown, ",") != strings.Join(w.Shown, ",") {
			t.Errorf("%s: %s rows, cards %v; want %s rows, cards %v", name, v.Count, v.Shown, w.Count, w.Shown)
		}
	}
	if len(res.Order) != len(wants) {
		t.Errorf("steps run: %v\n%s", res.Order, out)
	}
	if len(res.Errors) > 0 {
		t.Errorf("page errors: %v", res.Errors)
	}
}
