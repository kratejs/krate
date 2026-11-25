package build

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/diag"
)

const lowercaseEventButton = `import { createSignal } from "@krate/runtime";
export default function Btn() {
  const [n, setN] = createSignal(0);
  return <button onclick={() => setN(n() + 1)}>x {n()}</button>;
}
`

func warningCodes(ds []diag.Diagnostic) string {
	var out []string
	for _, d := range ds {
		out = append(out, string(d.Code)+"@"+d.File)
	}
	return strings.Join(out, ",")
}

// A KR5002 warning reaches the tabs with build-end, once although two pages
// use the component, stays when an unrelated page is rebuilt and goes away
// when the component is fixed.
func TestDevWarningsReachTheOverlay(t *testing.T) {
	t.Parallel()
	root, _, hub, events, _ := startActivityWatch(t, map[string]string{
		"src/components/Btn.tsx": lowercaseEventButton,
		"src/pages/index.tsx": `import Btn from "../components/Btn";
export default function Home() { return <main><Btn /></main>; }
`,
		"src/pages/other.tsx": `import Btn from "../components/Btn";
export default function Other() { return <main><Btn /></main>; }
`,
		"src/pages/about.tsx": devPage("about"),
	})
	ev := nextEvent(t, events, "build-end")
	if got := warningCodes(ev.Warnings); got != "KR5002@src/components/Btn.tsx" {
		t.Fatalf("initial build-end warnings = %q", got)
	}
	if d := ev.Warnings[0]; !d.IsWarning() || d.Line != 4 || d.Col == 0 {
		t.Errorf("warning = %+v", d)
	}
	if got := warningCodes(hub.Warnings()); got != "KR5002@src/components/Btn.tsx" {
		t.Errorf("hub warnings = %q", got)
	}

	// Rebuilding an unrelated page keeps the warning of the others.
	if err := os.WriteFile(filepath.Join(root, "src", "pages", "about.tsx"), []byte(devPage("about 2")), 0o644); err != nil {
		t.Fatal(err)
	}
	nextEvent(t, events, "build-start")
	if ev := nextEvent(t, events, "build-end"); warningCodes(ev.Warnings) != "KR5002@src/components/Btn.tsx" {
		t.Errorf("after an unrelated rebuild: warnings = %q", warningCodes(ev.Warnings))
	}
	if ev := nextEvent(t, events, "reload"); warningCodes(ev.Warnings) != "KR5002@src/components/Btn.tsx" {
		t.Errorf("reload event warnings = %q", warningCodes(ev.Warnings))
	}

	// Fixing the component clears it.
	if err := os.WriteFile(filepath.Join(root, "src", "components", "Btn.tsx"), []byte(strings.Replace(lowercaseEventButton, "onclick", "onClick", 1)), 0o644); err != nil {
		t.Fatal(err)
	}
	nextEvent(t, events, "build-start")
	if ev := nextEvent(t, events, "build-end"); len(ev.Warnings) != 0 {
		t.Errorf("after the fix: warnings = %q", warningCodes(ev.Warnings))
	}
	if len(hub.Warnings()) != 0 {
		t.Errorf("hub still has warnings %q", warningCodes(hub.Warnings()))
	}
}

func TestDevWarningStoreDedupesAndCaps(t *testing.T) {
	root := t.TempDir()
	w := diag.Diagnostic{File: filepath.Join(root, "src", "a.tsx"), Line: 3, Col: 5, Message: "m", Code: "KR5002", Severity: diag.SeverityWarning}
	var s devWarningStore
	s.record([]*PageResult{
		{Page: "/p/a.tsx", Warnings: []diag.Diagnostic{w, {Message: "an error is not a warning"}}},
		{Page: "/p/b.tsx", Warnings: []diag.Diagnostic{w}},
	}, nil, true)
	got := s.list(root)
	if len(got) != 1 || got[0].File != "src/a.tsx" {
		t.Fatalf("list = %+v", got)
	}
	// A partial build that rebuilt a.tsx without warnings keeps b.tsx's.
	s.record(nil, []string{"/p/a.tsx"}, false)
	if len(s.list(root)) != 1 {
		t.Errorf("b.tsx's warning was dropped")
	}
	s.record(nil, []string{"/p/b.tsx"}, false)
	if len(s.list(root)) != 0 {
		t.Errorf("warnings remain after both pages were rebuilt clean")
	}
	var many []diag.Diagnostic
	for i := 0; i < devWarningCap+20; i++ {
		d := w
		d.Line = i + 1
		many = append(many, d)
	}
	s.record([]*PageResult{{Page: "/p/a.tsx", Warnings: many}}, nil, true)
	if n := len(s.list(root)); n != devWarningCap {
		t.Errorf("list has %d warnings, want the cap %d", n, devWarningCap)
	}
}

func TestBuildWarningsSSEFrames(t *testing.T) {
	ws := []diag.Diagnostic{{File: "src/a.tsx", Line: 1, Message: "m", Code: "KR5002", Severity: diag.SeverityWarning}}
	var buf bytes.Buffer
	writeBuildWarnings(&buf, ws)
	if got := buf.String(); !strings.HasPrefix(got, "event: build-warnings\ndata: {\"warnings\":[{") || !strings.Contains(got, `"severity":"warning"`) {
		t.Errorf("build-warnings frame = %q", got)
	}
	buf.Reset()
	writeBuildError(&buf, DevEvent{Diagnostics: []diag.Diagnostic{{Message: "boom"}}, Warnings: ws})
	if got := buf.String(); !strings.Contains(got, `"warnings":[{`) || !strings.Contains(got, `"boom"`) {
		t.Errorf("build-error frame = %q", got)
	}
	msg := string(NativeHMRMessage(DevEvent{Type: "build-end", Build: &BuildRecord{Kind: "partial"}, Warnings: ws}))
	if !strings.Contains(msg, `"warnings":[{`) {
		t.Errorf("native build-end = %s", msg)
	}
}
