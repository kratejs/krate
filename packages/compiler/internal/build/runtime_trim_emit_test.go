package build

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/config"
	"github.com/kratejs/krate/packages/compiler/internal/renderer"
)

// loadFeatureManifest reads krate-features.json, or skips when the runtime
// package has not been built.
func loadFeatureManifest(t *testing.T) (string, runtimeFeatureManifest) {
	t.Helper()
	repoRoot, err := repoRootPath()
	if err != nil {
		t.Fatal(err)
	}
	raw := loadNamedBundleFromDisk(repoRoot, "krate-features.json")
	if raw == "" {
		requireE2E(t, "runtime dist not built")
		t.Skip("runtime dist not available")
	}
	var m runtimeFeatureManifest
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		t.Fatal(err)
	}
	return repoRoot, m
}

// optionalProbes are optional-feature names a bootstrap helper only tests for
// (window.__krateOnError && ...), so the helper works without the feature.
var optionalProbes = map[string]bool{
	"__krateOnError": true,
	"__krateCondPre": true, "__krateCondPost": true,
}

// TestBootstrapHelperFeatureDeps keeps helperFeatures complete. A bootstrap
// helper that calls an optional feature's export needs the feature shipped
// whenever page code calls the helper, but page code only names the helper:
// kbindList probes krateBindList, so before the table existed the list
// feature was dropped and keyed lists never updated in production builds.
func TestBootstrapHelperFeatureDeps(t *testing.T) {
	_, m := loadFeatureManifest(t)
	owner := map[string]string{}
	for _, name := range optionalRuntimeFeatures {
		for _, e := range m.Feature[name].Exports {
			owner[e] = name
		}
	}
	helpers := bootstrapHelperSources(renderer.HydrationBootstrapJS, renderer.CompatBootstrapJS)
	if len(helpers) < 10 {
		t.Fatalf("parsed only %d bootstrap helpers", len(helpers))
	}
	for helper, src := range helpers {
		declared := map[string]bool{}
		for _, f := range helperFeatures[helper] {
			declared[f] = true
		}
		for id := range identifierSet(src) {
			feature, ok := owner[id]
			if !ok || id == helper || optionalProbes[id] || declared[feature] {
				continue
			}
			t.Errorf("bootstrap helper %s uses %s from the %q feature: add %q to helperFeatures[%q] (or optionalProbes if it only probes)", helper, id, feature, feature, helper)
		}
	}
	for helper := range helperFeatures {
		if _, ok := helpers[helper]; !ok {
			t.Errorf("helperFeatures names %s, which no bootstrap script defines", helper)
		}
	}
}

// emittedCorpus covers the constructs whose generated code reaches into the
// runtime: custom hooks with arguments, keyed lists, conditionals, context,
// class components and error boundaries, server actions forms and portals.
var emittedCorpus = map[string]string{
	"src/pages/index.tsx": `import { useState } from "react";
function useCounter(init) { const [c, setC] = useState(init); return { c, inc: () => setC(c + 1) }; }
function Counter({ start }) { const { c, inc } = useCounter(start); return <button id="c" onClick={inc}>{c}</button>; }
export default function Page() {
	const [items, setItems] = useState([{ id: 1, t: "a" }, { id: 2, t: "b" }]);
	const [open, setOpen] = useState(false);
	return (<main>
		<Counter start={2} />
		<button id="rev" onClick={() => setItems([...items].reverse())}>rev</button>
		<ul>{items.map((i) => <li key={i.id}>{i.t}</li>)}</ul>
		<button id="t" onClick={() => setOpen(!open)}>t</button>
		{open ? <p>open</p> : <p>closed</p>}
	</main>);
}
`,
	"src/pages/ctx.tsx": `import { createContext, useContext, useState, Component } from "react";
const Theme = createContext("light");
function Leaf() { const t = useContext(Theme); return <i>{t}</i>; }
class Boom extends Component {
	state = { n: 0 };
	render() { return <button onClick={() => this.setState({ n: this.state.n + 1 })}>{this.state.n}</button>; }
}
export default function Page() {
	const [t, setT] = useState("dark");
	return <Theme.Provider value={t}><Leaf /><Boom /><button onClick={() => setT("x")}>x</button></Theme.Provider>;
}
`,
}

var identRe = regexp.MustCompile(`[A-Za-z_$][A-Za-z0-9_$]*`)

// TestTrimmedRuntimeDefinesEmittedNames builds a corpus with the production
// (trimmed) runtime and fails when emitted page code names a runtime export
// that the trimmed runtime does not define. Both C1 (a feature dropped
// because only a bootstrap helper used it) and C2 (krateParam missing from
// the core) were this one bug class: a passing build whose page JS calls a
// function the shipped runtime lacks.
func TestTrimmedRuntimeDefinesEmittedNames(t *testing.T) {
	repoRoot, m := loadFeatureManifest(t)
	if len(m.Full) == 0 {
		t.Skip("krate-features.json has no full export list (rebuild @krate/runtime)")
	}

	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "packages"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(repoRoot, "packages", "runtime"), filepath.Join(root, "packages", "runtime")); err != nil {
		t.Skipf("cannot link the runtime package: %v", err)
	}
	for rel, content := range emittedCorpus {
		p := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}
	cfg := config.Default()
	cfg.PagesDir = filepath.Join(root, "src", "pages")
	cfg.OutDir = filepath.Join(root, "dist")
	if err := New(root, cfg).BuildAll(); err != nil {
		t.Fatalf("BuildAll: %v", err)
	}

	var runtimeJS string
	var pageJS strings.Builder
	err := filepath.WalkDir(cfg.OutDir, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		data, rerr := os.ReadFile(path)
		if rerr != nil {
			return rerr
		}
		switch {
		case strings.HasPrefix(d.Name(), "runtime.") && strings.HasSuffix(d.Name(), ".js"):
			runtimeJS = string(data)
		case strings.HasSuffix(d.Name(), ".js"), strings.HasSuffix(d.Name(), ".html"):
			pageJS.Write(data)
			pageJS.WriteByte('\n')
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if runtimeJS == "" {
		t.Fatal("no runtime chunk was written")
	}
	if strings.Contains(runtimeJS, "longestIncreasingSubsequence") != strings.Contains(runtimeJS, "krateBindList:") {
		t.Fatal("test cannot tell how the runtime chunk lists its exports")
	}

	// The bundles list their exports as `name:()=>binding` in an export table.
	defined := func(name string) bool {
		return regexp.MustCompile(`[{,]`+regexp.QuoteMeta(name)+`:\(\)=>`).MatchString(runtimeJS) ||
			strings.Contains(runtimeJS, "window."+name+"=")
	}
	full := map[string]bool{}
	for _, n := range m.Full {
		full[n] = true
	}
	// Names the page code uses, directly or through the bootstrap helpers it
	// calls (only helpers that the page names).
	used := map[string]bool{}
	for _, id := range identRe.FindAllString(pageJS.String(), -1) {
		used[id] = true
	}
	for helper, src := range bootstrapHelperSources(renderer.HydrationBootstrapJS, renderer.CompatBootstrapJS) {
		if used[helper] {
			for _, id := range identRe.FindAllString(src, -1) {
				if !optionalProbes[id] {
					used[id] = true
				}
			}
		}
	}
	var missing []string
	for id := range used {
		if full[id] && !defined(id) {
			missing = append(missing, id)
		}
	}
	sort.Strings(missing)
	if len(missing) > 0 {
		t.Errorf("emitted code references runtime names the trimmed runtime does not define: %v", missing)
	}
	for _, must := range []string{"krateParam", "krateBindList"} {
		if !used[must] {
			t.Errorf("corpus no longer exercises %s", must)
		}
	}
}

// bootstrapHelperSources maps each helper the bootstrap scripts define on
// window (kbindList, cloneElement, ...) to its source line.
func bootstrapHelperSources(scripts ...string) map[string]string {
	out := make(map[string]string)
	for _, script := range scripts {
		for _, line := range strings.Split(script, "\n") {
			rest, ok := strings.CutPrefix(line, "window.")
			if !ok {
				continue
			}
			if name, _, found := strings.Cut(rest, "="); found && name != "" {
				out[name] = line
			}
		}
	}
	return out
}

// trimmedBrowserPage exercises, in one production page, what the trimmed
// runtime and the hydration emitter once dropped silently.
const trimmedBrowserPage = `import { useState } from "react";
import { createPortal } from "react-dom";
function useCounter(init) { const [c, setC] = useState(init); return { c, inc: () => setC(c + 1) }; }
function Counter({ start }) { const { c, inc } = useCounter(start); return <button id="hook" onClick={inc}>{c}</button>; }
function Fmt({ fmt }) { return <p id="fprop">{fmt("x")}</p>; }
const cfg = { label: "T", fmt: (s) => s.toUpperCase() };
function getInfo() { return { count: 3 }; }
function Destruct() {
  const { label, fmt } = cfg;
  const { count } = getInfo();
  const [n] = useState(0);
  return <p id="destruct">{label}{count + n}{fmt("y")}</p>;
}
export default function Page() {
  const [items, setItems] = useState([{ id: 1, t: "a" }, { id: 2, t: "b" }, { id: 3, t: "c" }]);
  const [user, setUser] = useState(null);
  const [open, setOpen] = useState(false);
  return (<main>
    <button id="rev" onClick={() => setItems([...items].reverse())}>rev</button>
    <button id="add" onClick={() => setItems([...items, { id: 9, t: "z" }])}>add</button>
    <ul id="list">{items.map((i) => <li key={i.id}>{i.t}</li>)}</ul>
    <Counter start={5} />
    <button id="login" onClick={() => setUser({ name: "Ann", tags: ["a", "b"] })}>in</button>
    <button id="logout" onClick={() => setUser(null)}>out</button>
    <div id="guard">{user && <p>{user.name} has {user.tags.length} tags</p>}</div>
    <Destruct />
    <Fmt fmt={(s) => s.toUpperCase()} />
    <button id="open" onClick={() => setOpen(!open)}>open</button>
    {open && createPortal(<div id="modal">hello</div>, document.body)}
  </main>);
}
`

// trimmedBrowserSuspensePage is the Suspense part: such a page streams, which
// keeps the whole runtime, so it cannot share a build with trimmedBrowserPage.
const trimmedBrowserSuspensePage = `import { useState, Suspense } from "react";
function A() { return <b>A</b>; }
function B() { return <b>B</b>; }
export default function Page() {
  const [n, setN] = useState(0);
  const [show, setShow] = useState(true);
  return (<main>
    <button id="inc" onClick={() => setN(n + 1)}>inc</button>
    <button id="tog" onClick={() => setShow(!show)}>tog</button>
    <Suspense fallback={<p>loading</p>}><p id="sus">count {n}</p></Suspense>
    <Suspense fallback={<p>loading</p>}><span id="pick">{show ? <A /> : <B />}</span></Suspense>
  </main>);
}
`

// TestTrimmedRuntimeBrowser builds trimmedBrowserPage for production and drives
// it in a real browser (KRATE_BROWSER=chromium|firefox|webkit, as for
// TestBrowserConformance).
func TestTrimmedRuntimeBrowser(t *testing.T) {
	browser := strings.ToLower(strings.TrimSpace(os.Getenv("KRATE_BROWSER")))
	if browser == "" {
		t.Skip("set KRATE_BROWSER=chromium|firefox|webkit to run browser checks")
	}
	if _, err := exec.LookPath("node"); err != nil {
		requireE2E(t, "node is not installed")
	}
	if err := exec.Command("node", "-e", "require.resolve('playwright')").Run(); err != nil {
		requireE2E(t, "playwright is not installed (run pnpm install)")
	}
	repoRoot, err := repoRootPath()
	if err != nil {
		t.Fatal(err)
	}
	for mode, page := range map[string]string{"main": trimmedBrowserPage, "suspense": trimmedBrowserSuspensePage} {
		t.Run(mode, func(t *testing.T) { runTrimmedBrowser(t, repoRoot, browser, mode, page) })
	}
}

func runTrimmedBrowser(t *testing.T, repoRoot, browser, mode, pageSrc string) {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "packages"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(repoRoot, "packages", "runtime"), filepath.Join(root, "packages", "runtime")); err != nil {
		t.Skipf("cannot link the runtime package: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(root, "src", "pages"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "src", "pages", "index.tsx"), []byte(pageSrc), 0644); err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()
	cfg.PagesDir = filepath.Join(root, "src", "pages")
	cfg.OutDir = filepath.Join(root, "dist")
	if err := New(root, cfg).BuildAll(); err != nil {
		t.Fatalf("BuildAll: %v", err)
	}
	script, err := filepath.Abs(filepath.Join("testdata", "trimmed_runtime_browser.mjs"))
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("node", script, cfg.OutDir, browser, mode)
	cmd.Env = append(os.Environ(), "NODE_PATH="+filepath.Join(repoRoot, "node_modules"))
	out, err := cmd.CombinedOutput()
	if err != nil {
		if strings.Contains(string(out), "Executable doesn't exist") || strings.Contains(string(out), "browserType.launch") {
			requireE2E(t, "%s browser is not installed: %s", browser, out)
		}
		t.Fatalf("trimmed runtime browser checks failed: %v\n%s", err, out)
	}
	t.Log(string(out))
}
