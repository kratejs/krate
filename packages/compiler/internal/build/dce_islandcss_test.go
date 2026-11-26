package build

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/bundler"
)

// pruneCSSFor prunes text as a page would: scoped is every class in the
// sheet, used the classes the page references.
func pruneCSSFor(text string, used ...string) string {
	scoped := map[string]bool{}
	for _, m := range selectorClassRe.FindAllStringSubmatch(text, -1) {
		scoped[m[1]] = true
	}
	u := map[string]bool{}
	for _, c := range used {
		u[c] = true
	}
	return pruneModuleCSS(text, scoped, u)
}

func TestPruneKeepsBaseAndPseudoRulesTogether(t *testing.T) {
	t.Parallel()
	sheet := ".close_h{color:red}\n.close_h:hover{color:blue}\n.close_h:focus-visible,.close_h:active{color:green}\n.dead_h{color:gray}\n.dead_h::before{content:\"\"}"
	got := pruneCSSFor(sheet, "close_h")
	for _, want := range []string{".close_h{", ".close_h:hover{", ".close_h:focus-visible,.close_h:active{"} {
		if !strings.Contains(got, want) {
			t.Errorf("used class lost %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "dead_h") {
		t.Errorf("unused class kept some rule:\n%s", got)
	}
	// An unused class drops its pseudo rules along with the base rule.
	if got := pruneCSSFor(sheet); strings.Contains(got, "close_h:hover") || strings.Contains(got, ".close_h{") {
		t.Errorf("unused class kept a rule:\n%s", got)
	}
}

func TestPruneCompoundAndGroupedSelectors(t *testing.T) {
	t.Parallel()
	sheet := ".a_h .b_h{x:1}\n.a_h:not(.b_h){x:2}\n.c_h,.d_h{x:3}\n.e_h.global-x{x:4}\n@media (min-width:1px){.f_h{x:5}.f_h:hover{x:6}.g_h{x:7}}\n@media (min-width:2px){.h_h{x:8}}"
	got := pruneCSSFor(sheet, "a_h", "d_h", "g_h")
	for _, want := range []string{".a_h .b_h{", ".a_h:not(.b_h){", ".c_h,.d_h{", ".g_h{x:7}"} {
		if !strings.Contains(got, want) {
			t.Errorf("lost %q:\n%s", want, got)
		}
	}
	for _, gone := range []string{"f_h", "h_h", "@media (min-width:2px)"} {
		if strings.Contains(got, gone) {
			t.Errorf("kept %q:\n%s", gone, got)
		}
	}
	// A class outside the module mappings is never pruned.
	if got := pruneModuleCSS(".e_h.global-x{x:4}", map[string]bool{"e_h": true}, map[string]bool{}); !strings.Contains(got, "global-x") {
		t.Errorf("rule with a global class was pruned: %q", got)
	}
}

func TestPageCSSAfterDCEClientClassesKeptWhole(t *testing.T) {
	t.Parallel()
	b, _ := scProject(t, nil)
	sheet := ".panel_h{x:1}\n.close_h{x:2}\n.close_h:hover{x:3}\n.unused_h{x:4}"
	modules := map[string]*bundler.CSSModuleInfo{"/m.module.css": {Mappings: map[string]string{"panel": "panel_h", "close": "close_h", "unused": "unused_h"}}}
	// Dynamic access (s[key]) means any class may be rendered: the island's
	// whole module is kept even though no class appears in the HTML or JS.
	all := map[string]bool{"panel_h": true, "close_h": true, "unused_h": true}
	got := b.pageCSSAfterDCE(RenderSSG, sheet, modules, all, "<p>static</p>")
	if strings.Count(got, "{") != 4 {
		t.Errorf("client module classes were pruned:\n%s", got)
	}
	// A module only static components use is still pruned.
	got = b.pageCSSAfterDCE(RenderSSG, sheet, modules, nil, `<p class="panel_h">static</p>`)
	if !strings.Contains(got, ".panel_h{") || strings.Contains(got, "close_h") || strings.Contains(got, "unused_h") {
		t.Errorf("static-only module was not pruned class by class:\n%s", got)
	}
}

// An island that renders a panel only after interaction keeps every rule of
// its CSS modules, while a module used by build-time components alone is
// still pruned.
func TestIslandCSSModuleRulesKept(t *testing.T) {
	t.Parallel()
	if _, err := exec.LookPath("node"); err != nil {
		requireE2E(t, "node is not installed")
	}
	repo, err := repoRootPath()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(repo, "packages", "runtime", "dist", "krate-runtime.js")); err != nil {
		requireE2E(t, "the runtime package is not built: %v", err)
	}
	root := t.TempDir()
	cfg := setupIslandProject(t, root, true)
	e2eEnv(t)
	if err := os.MkdirAll(filepath.Join(root, "node_modules", "@krate"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := linkRuntimePackage(t, filepath.Join(root, "node_modules", "@krate")); err != nil {
		t.Skipf("cannot link the runtime package: %v", err)
	}
	writeProjectFile(t, root, "src/components/finder.module.css", ".panel{padding:7px}\n.close{color:#090807}\n.close:hover{color:#c80000}\n.spare{color:#050505}\n")
	writeProjectFile(t, root, "src/components/Finder.tsx", `import { useState } from 'react';
import s from './finder.module.css';
export default function Finder() {
	const [open, setOpen] = useState(false);
	return <div><button onClick={() => setOpen(true)}>open</button>{open && <div class={s.panel}><button class={s.close} onClick={() => setOpen(false)}>x</button></div>}</div>;
}
`)
	writeProjectFile(t, root, "src/components/stat.module.css", ".shown{color:#010101}\n.hidden{color:#020202}\n.hidden:hover{color:#030303}\n")
	writeProjectFile(t, root, "src/components/Stat.tsx", `import s from './stat.module.css';
export function Stat() { return <p class={s.shown}>static</p>; }
`)
	writeProjectFile(t, root, "src/pages/index.tsx", `import { Frame } from 'fake-lib';
import Finder from '../components/Finder';
import { Stat } from '../components/Stat';
export default function Page() {
	return <main><Frame><Finder /></Frame><Stat /></main>;
}
`)
	out, err := captureBuild(New(root, cfg))
	if err != nil {
		t.Fatalf("BuildAll: %v\n%s", err, out)
	}
	sheets, _ := filepath.Glob(filepath.Join(cfg.OutDir, "*.css"))
	var all strings.Builder
	for _, s := range sheets {
		all.WriteString(readBuilt(t, s))
	}
	text := all.String()
	for _, want := range []string{".panel_", ".close_", "color:#c80000", "color:#050505", ".shown_"} {
		if !strings.Contains(text, want) {
			t.Errorf("page CSS lost %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, ".hidden_") {
		t.Errorf("a class only static components could use was kept:\n%s", text)
	}
}
