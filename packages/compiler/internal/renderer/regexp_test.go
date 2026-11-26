package renderer

import (
	"strings"
	"testing"
)

func TestRegExpMatchAllHighlighterBakedStatic(t *testing.T) {
	src := `
type Mode = "script" | "shell";
const TOKEN_RULES: Record<Mode, RegExp> = {
  script: /\b(const|let|return)\b/g,
  shell: /\$\w+/g,
};
function formatLine(mode: Mode, text: string) {
  const parts = [];
  let cursor = 0;
  for (const m of text.matchAll(TOKEN_RULES[mode])) {
    const pos = m.index ?? 0;
    if (pos > cursor) parts.push(text.slice(cursor, pos));
    parts.push(<span key={pos} class="tok">{m[0]}</span>);
    cursor = pos + m[0].length;
  }
  if (cursor < text.length) parts.push(text.slice(cursor));
  return parts;
}
function Snippet({ mode, rows }: { mode: Mode; rows: string[] }) {
  return <pre>{rows.map((row) => <div>{formatLine(mode, row)}</div>)}</pre>;
}
export default function Demo() {
  return <Snippet mode="script" rows={["const n = 1", "let total = n"]} />;
}
`
	result, hydrationJS := fullPipeline(t, src)
	if len(result.Errors) > 0 {
		t.Fatalf("unexpected errors: %v", result.Errors)
	}
	wantA := `<div><span class="tok">const</span> n = 1</div>`
	wantB := `<div><span class="tok">let</span> total = n</div>`
	if !strings.Contains(result.HTML, wantA) || !strings.Contains(result.HTML, wantB) {
		t.Fatalf("unexpected HTML:\n%s", result.HTML)
	}
	if strings.TrimSpace(hydrationJS) != "" {
		t.Fatalf("expected zero client JS for static regex component, got:\n%s", hydrationJS)
	}
}

func TestRegExpExecLoopOverModuleConst(t *testing.T) {
	src := `
const RULES = {
  vars: /\$\w+/g,
};
function extractVars(line: string) {
  const re = new RegExp(RULES.vars);
  const found: string[] = [];
  let m;
  while ((m = re.exec(line))) {
    found.push(m[0] + "@" + m.index);
  }
  return found.join(",");
}
function extractDirect(line: string) {
  RULES.vars.lastIndex = 0;
  const found: string[] = [];
  let m;
  while ((m = RULES.vars.exec(line))) {
    found.push(m[0]);
  }
  return found.join("|");
}
export default function Demo() {
  return <section><p>{extractVars("echo $HOME $USER")}</p><p>{extractDirect("echo $HOME $USER")}</p></section>;
}
`
	result, hydrationJS := fullPipeline(t, src)
	if len(result.Errors) > 0 {
		t.Fatalf("unexpected errors: %v", result.Errors)
	}
	if !strings.Contains(result.HTML, "<p>$HOME@5,$USER@11</p>") {
		t.Fatalf("missing new RegExp exec result:\n%s", result.HTML)
	}
	if !strings.Contains(result.HTML, "<p>$HOME|$USER</p>") {
		t.Fatalf("missing direct lastIndex exec result:\n%s", result.HTML)
	}
	if strings.TrimSpace(hydrationJS) != "" {
		t.Fatalf("expected zero client JS, got:\n%s", hydrationJS)
	}
}

func TestRegExpStringMethodsAndNamedGroups(t *testing.T) {
	src := `
const DATE_RE = /(?<yr>\d{4})-(?<mo>\d{2})-(?<dy>\d{2})/;
function summary(raw: string) {
  const ok = /\d+/.test(raw);
  const cleaned = raw.replace(/\s+/g, " ");
  const cols = "alpha,  beta, gamma".split(/,\s*/);
  const m = raw.match(DATE_RE);
  const swapped = raw.replace(/(\d{4})-(\d{2})/, (_all, y, mo) => mo + "/" + y);
  return [ok, cleaned, cols.join("|"), m ? m.groups.yr + "." + m.groups.mo + "." + m.groups.dy : "", swapped].join(" ; ");
}
export default function Demo() {
  return <p>{summary("date:   2026-11-26")}</p>;
}
`
	result, hydrationJS := fullPipeline(t, src)
	if len(result.Errors) > 0 {
		t.Fatalf("unexpected errors: %v", result.Errors)
	}
	want := "<p>true ; date: 2026-11-26 ; alpha|beta|gamma ; 2026.11.26 ; date:   11/2026-26</p>"
	if !strings.Contains(result.HTML, want) {
		t.Fatalf("got HTML:\n%s\nwant substring:\n%s", result.HTML, want)
	}
	if strings.TrimSpace(hydrationJS) != "" {
		t.Fatalf("expected zero client JS, got:\n%s", hydrationJS)
	}
}
