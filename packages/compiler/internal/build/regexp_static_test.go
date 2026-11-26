package build

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestStaticRegexHighlighterBuild(t *testing.T) {
	t.Parallel()
	b, root := scProject(t, map[string]string{
		"src/components/snippet.tsx": `import type { ReactNode } from "react";
type Mode = "js" | "sh";
const RULES: Record<Mode, RegExp> = { js: /\b(const|let)\b/g, sh: /\$\w+/g };
function renderLine(mode: Mode, text: string): ReactNode[] {
  const out: ReactNode[] = [];
  let pos = 0;
  for (const m of text.matchAll(RULES[mode])) {
    const idx = m.index ?? 0;
    if (idx > pos) out.push(text.slice(pos, idx));
    out.push(<span key={idx} class="tok">{m[0]}</span>);
    pos = idx + m[0].length;
  }
  if (pos < text.length) out.push(text.slice(pos));
  return out;
}
export function Snippet({ mode, lines }: { mode: Mode; lines: string[] }) {
  return <pre>{lines.map((line) => <div>{renderLine(mode, line)}</div>)}</pre>;
}
`,
		"src/pages/regex.tsx": `import { Snippet } from "../components/snippet";
const LABEL_RE = /(?<tag>[a-z]+)=(?<val>\d+)/;
export default function RegexPage() {
  const matched = "count=42".match(LABEL_RE);
  const parts = "one,  two, three".split(/,\s*/);
  const replaced = "a-b-c".replace(/-/g, "_");
  return (
    <main>
      <Snippet mode="js" lines={["const a = 1", "let b = a"]} />
      <p>{matched?.groups?.tag}:{matched?.groups?.val}|{parts.join("+")}|{replaced}</p>
    </main>
  );
}
`,
	})
	if err := b.BuildAll(); err != nil {
		t.Fatalf("BuildAll: %v", err)
	}
	htmlBytes, err := os.ReadFile(filepath.Join(root, "dist", "regex", "index.html"))
	if err != nil {
		t.Fatal(err)
	}
	html := string(htmlBytes)
	for _, want := range []string{
		`<pre><div><span class=tok>const</span> a = 1</div><div><span class=tok>let</span> b = a</div></pre>`,
		`<p>count:42|one+two+three|a_b_c</p>`,
	} {
		if !strings.Contains(html, want) {
			t.Fatalf("missing %q in built HTML:\n%s", want, html)
		}
	}
	if strings.Contains(html, "<script type=module") || strings.Contains(html, `<script type="module"`) {
		t.Fatalf("expected zero client JS for static regex page, got:\n%s", html)
	}
}
