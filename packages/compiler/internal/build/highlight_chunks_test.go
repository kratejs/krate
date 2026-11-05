package build

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/config"
)

// TestMarkdownChunkResolvesFences runs the language chunks the way a browser
// does (ES modules, one shared tokenizer) and checks a Markdown snippet's fence:
// highlighted when the fence's language is loaded, left plain (null) when chroma
// has that language but the page did not load it, and an unknown language is
// plain text as in chroma.
func TestMarkdownChunkResolvesFences(t *testing.T) {
	requireRuntimeDist(t)
	node, err := exec.LookPath("node")
	if err != nil {
		requireE2E(t, "node is not installed")
	}
	root := codeThemeProject(t, map[string]string{"index.tsx": "export default function P() { return <main/>; }\n"})
	cfg := config.Default()
	cfg.PagesDir = filepath.Join(root, "src", "pages")
	cfg.OutDir = filepath.Join(root, "dist")
	b := New(root, cfg)
	md, err := b.highlightLangChunk("markdown")
	if err != nil {
		t.Fatal(err)
	}
	ts, err := b.highlightLangChunk("TypeScript")
	if err != nil {
		t.Fatal(err)
	}
	script := filepath.Join(t.TempDir(), "run.mjs")
	src := `import { pathToFileURL } from "node:url";
const out = process.argv[2];
const mdMod = await import(pathToFileURL(out + "/" + process.argv[3]).href);
const doc = (lang) => "# T\n\n` + "```" + `" + lang + "\nconst a = 1\n` + "```" + `\n";
const res = {
  loaded: mdMod.highlight(doc("ts"), "md"),
};
await import(pathToFileURL(out + "/" + process.argv[4]).href);
res.withTs = mdMod.highlight(doc("ts"), "md");
res.unknown = mdMod.highlight(doc("notalanguage"), "md");
res.unloadedKnown = mdMod.highlight(doc("cobol"), "md");
console.log(JSON.stringify(res));
`
	if err := os.WriteFile(script, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command(node, script, cfg.OutDir, md, ts).CombinedOutput()
	if err != nil {
		t.Fatalf("node: %v\n%s", err, out)
	}
	s := string(out)
	if !strings.Contains(s, `"loaded":null`) {
		t.Errorf("a fence naming a language chroma has but the page did not load must leave the snippet plain:\n%s", s)
	}
	if !strings.Contains(s, `"unloadedKnown":null`) {
		t.Errorf("an unshipped but known language must decline:\n%s", s)
	}
	if !strings.Contains(s, `"withTs":"<span class=\"line\">`) || !strings.Contains(s, `class=\"kr\">const`) {
		t.Errorf("a loaded fence language was not highlighted:\n%.600s", s)
	}
	if !strings.Contains(s, `"unknown":"<span class=\"line\">`) {
		t.Errorf("an unknown fence language should highlight the rest of the document:\n%.600s", s)
	}
}
