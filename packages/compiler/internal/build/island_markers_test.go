package build

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// islandMarkerFiles: client components with an element root, a fragment root
// that inserts a node at the top level, a text root, and a toggle whose server
// children hold another island with a fragment root, all as direct children
// of a server <div> styled with a child combinator (Tailwind's space-y-*).
var islandMarkerFiles = map[string]string{
	"src/components/parts.tsx": `"use client";
import { useState } from "react";
export function Card() {
  const [n, setN] = useState(0);
  return <section id="card" onClick={() => setN(n + 1)}>card {n}</section>;
}
export function Multi() {
  const [b, setB] = useState(false);
  return <><p id="a">a</p>{b ? <p id="b">b</p> : null}<button id="toggle-b" onClick={() => setB(!b)}>toggle</button></>;
}
export function Text() {
  const [n, setN] = useState(0);
  return <>{"text " + n}<button id="txt-btn" onClick={() => setN(n + 1)}>+</button></>;
}
export function Outer({ children }: { children: any }) {
  const [show, setShow] = useState(true);
  return <><button id="outer-toggle" onClick={() => setShow(!show)}>outer</button>{show ? <div id="wrap">{children}</div> : null}</>;
}
export function Inner() {
  const [n, setN] = useState(0);
  return <><button id="inner-a" onClick={() => setN(n + 1)}>{"inner " + n}</button>{n > 0 ? <i id="inner-extra">{"extra " + n}</i> : null}</>;
}`,
	"src/pages/index.tsx": `import { Card, Multi, Text, Outer, Inner } from "../components/parts";
async function Server() { await Promise.resolve(); return <Inner />; }
export default async function Home() {
  await Promise.resolve();
  return <main>
    <style>{"#stack > :not(:last-child) { margin-bottom: 24px }"}</style>
    <div id="stack"><Card /><Multi /><span id="txt"><Text /></span><Outer><Server /></Outer><footer id="end">end</footer></div>
  </main>;
}`,
}

// TestIslandMarkersLeaveNoElement: the server puts a Preact island between
// two comments, with its props in the first, and no element: the parent's
// children are the component's root nodes.
func TestIslandMarkersLeaveNoElement(t *testing.T) {
	ensureRuntimeDist(t)
	b, root := scProject(t, islandMarkerFiles)
	withPreact(t, root)
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
	status, _, html := getBody(t, base+"/")
	if status != 200 {
		t.Fatalf("status %d:\n%s", status, html)
	}
	if strings.Contains(html, "<krate-island") || strings.Contains(html, "data-krate-props") {
		t.Errorf("a Preact island must not render an element or a props script:\n%s", html)
	}
	if !regexp.MustCompile(`<div id="stack"><!--krate-island:\{[^>]*"e":"Card"[^>]*--><section id="card">`).MatchString(html) {
		t.Errorf("the card must be a direct child of the stack, after its island comment:\n%s", html)
	}
	if n, m := strings.Count(html, "<!--krate-island:"), strings.Count(html, "<!--/krate-island-->"); n != 5 || m != 5 {
		t.Errorf("want 5 opening and 5 closing island comments, got %d and %d:\n%s", n, m, html)
	}
}

func TestBrowserIslandMarkers(t *testing.T) {
	browser := strings.ToLower(strings.TrimSpace(os.Getenv("KRATE_BROWSER")))
	if browser == "" {
		t.Skip("set KRATE_BROWSER=chromium|firefox|webkit to run the island markers browser test")
	}
	if !canStopInProcessServer {
		t.Skip("in-process server needs a unix host")
	}
	ensureRuntimeDist(t)
	if err := exec.Command("node", "-e", "require.resolve('playwright')").Run(); err != nil {
		requireE2E(t, "playwright is not installed (run pnpm install)")
	}
	b, root := scProject(t, islandMarkerFiles)
	withPreact(t, root)
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
	script, err := filepath.Abs(filepath.Join("testdata", "browser_island_markers.mjs"))
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
		Hosts                         int
		CardParent, CardMargin        string
		Order0, Order1, Order2, Text  string
		Inner1, Inner2, Inner3, After string
		Hidden                        bool
		Errors                        []string
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if err := json.Unmarshal([]byte(lines[len(lines)-1]), &res); err != nil {
		t.Fatalf("parsing browser output: %v\n%s", err, out)
	}
	want := map[string][2]string{
		"card parent":         {res.CardParent, "stack"},
		"card margin":         {res.CardMargin, "24px"},
		"order":               {res.Order0, "card,a,toggle-b,txt,outer-toggle,wrap,end"},
		"order with b":        {res.Order1, "card,a,b,toggle-b,txt,outer-toggle,wrap,end"},
		"order without b":     {res.Order2, "card,a,toggle-b,txt,outer-toggle,wrap,end"},
		"text root":           {res.Text, "text 1+"},
		"nested, added node":  {res.Inner1, "inner-a:inner 1,inner-extra:extra 1"},
		"nested, shown again": {res.Inner2, "inner-a:inner 1,inner-extra:extra 1"},
		"nested, updated":     {res.Inner3, "inner-a:inner 2,inner-extra:extra 2"},
		"last child":          {res.After, "end"},
	}
	for name, w := range want {
		if w[0] != w[1] {
			t.Errorf("%s: %q, want %q", name, w[0], w[1])
		}
	}
	if res.Hosts != 0 || !res.Hidden || len(res.Errors) > 0 {
		t.Errorf("unexpected browser result: %+v", res)
	}
}
