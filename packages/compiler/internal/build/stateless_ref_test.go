package build

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestStatelessRefInMappedWrapperAndDirectChild(t *testing.T) {
	t.Parallel()
	files := map[string]string{
		"src/components/track-panel.tsx": `"use client";
import { useRef, type ComponentPropsWithoutRef } from "react";
export function TrackPanel({ className = "", children, ...rest }: ComponentPropsWithoutRef<"div">) {
  const panelRef = useRef<HTMLDivElement>(null);
  return (
    <div
      ref={panelRef}
      className={` + "`panel ${className}`" + `}
      onPointerMove={(e) => {
        const el = panelRef.current;
        if (!el) return;
        el.style.setProperty("--pos", ` + "`${e.clientX}px`" + `);
      }}
      {...rest}
    >
      {children}
    </div>
  );
}
`,
		"src/pages/mapped.tsx": `import { TrackPanel } from "../components/track-panel";
const ROWS = [
  { id: "alpha", label: "Alpha" },
  { id: "beta", label: "Beta" },
  { id: "gamma", label: "Gamma" },
];
function Row({ entry }: { entry: { id: string; label: string } }) {
  return (
    <li>
      <TrackPanel className="row">
        <a href={` + "`/p/${entry.id}`" + `}>{entry.label}</a>
      </TrackPanel>
    </li>
  );
}
export default function MappedPage() {
  return <ul>{ROWS.map((entry) => <Row entry={entry} />)}</ul>;
}
`,
		"src/pages/direct.tsx": `import { TrackPanel } from "../components/track-panel";
export default function DirectPage() {
  return (
    <main>
      <TrackPanel className="solo">
        <a href="/p/solo">Solo</a>
      </TrackPanel>
    </main>
  );
}
`,
	}

	b, _ := scProject(t, files)
	b.Cfg.Minify = false
	out, err := captureBuild(b)
	if err != nil {
		t.Fatalf("BuildAll failed: %v\n%s", err, out)
	}
	if strings.Contains(out, "KR8011") {
		t.Fatalf("unexpected KR8011 warning:\n%s", out)
	}

	// Mapped page HTML must render real link hrefs and text statically.
	mappedHTMLBytes, err := os.ReadFile(filepath.Join(b.Cfg.OutDir, "mapped", "index.html"))
	if err != nil {
		t.Fatal(err)
	}
	mappedHTML := string(mappedHTMLBytes)
	for _, want := range []string{
		`class="panel row"`,
		`<a href="/p/alpha">Alpha</a>`,
		`<a href="/p/beta">Beta</a>`,
		`<a href="/p/gamma">Gamma</a>`,
	} {
		if !strings.Contains(mappedHTML, want) {
			t.Errorf("mapped HTML missing %q:\n%s", want, mappedHTML)
		}
	}
	if strings.Contains(mappedHTML, "data-kattr-href") {
		t.Errorf("mapped HTML should not contain unresolved data-kattr-href:\n%s", mappedHTML)
	}

	// Direct page HTML must also render statically.
	directHTMLBytes, err := os.ReadFile(filepath.Join(b.Cfg.OutDir, "direct", "index.html"))
	if err != nil {
		t.Fatal(err)
	}
	directHTML := string(directHTMLBytes)
	if !strings.Contains(directHTML, `class="panel solo"`) || !strings.Contains(directHTML, `<a href="/p/solo">Solo</a>`) {
		t.Errorf("direct HTML missing expected markup:\n%s", directHTML)
	}

	// Collect emitted page and shared scope JS.
	readJS := func(dir string) string {
		matches, _ := filepath.Glob(filepath.Join(dir, "*.js"))
		var sb strings.Builder
		for _, m := range matches {
			data, err := os.ReadFile(m)
			if err != nil {
				t.Fatal(err)
			}
			sb.Write(data)
			sb.WriteByte('\n')
		}
		return sb.String()
	}
	mappedJS := readJS(filepath.Join(b.Cfg.OutDir, "mapped"))
	directJS := readJS(filepath.Join(b.Cfg.OutDir, "direct"))
	sharedJS := readJS(filepath.Join(b.Cfg.OutDir, "chunks", "c"))
	allJS := mappedJS + directJS + sharedJS

	if !strings.Contains(allJS, "var panelRef=") {
		t.Fatalf("client JS must declare panelRef before kbindRef/onPointerMove:\nmapped:\n%s\ndirect:\n%s\nshared:\n%s", mappedJS, directJS, sharedJS)
	}
	if !strings.Contains(allJS, "kbindRef(") || !strings.Contains(allJS, `"onPointerMove"`) {
		t.Fatalf("client JS missing ref or pointermove binding:\n%s", allJS)
	}
	// Each mapped instance must receive its own ref object.
	if got := strings.Count(mappedJS, "{current:null}"); got < 3 && strings.Count(mappedJS, "{current: null}") < 3 {
		t.Errorf("expected 3 fresh ref objects in mapped page JS, got:\n%s\nshared:\n%s", mappedJS, sharedJS)
	}
	if got := strings.Count(directJS, "{current:null}"); got < 1 && strings.Count(directJS, "{current: null}") < 1 {
		t.Errorf("expected fresh ref object in direct page JS, got:\n%s\nshared:\n%s", directJS, sharedJS)
	}
}
