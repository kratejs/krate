package build

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kratejs/krate/packages/compiler/internal/diag"
)

func TestDevHubBroadcastToAllSubscribers(t *testing.T) {
	h := NewDevHub()
	a, cancelA := h.Subscribe()
	defer cancelA()
	b, cancelB := h.Subscribe()
	defer cancelB()

	h.Publish(DevEvent{Type: "reload", Routes: []string{"/x"}})

	for name, ch := range map[string]<-chan DevEvent{"a": a, "b": b} {
		select {
		case ev := <-ch:
			if len(ev.Routes) != 1 || ev.Routes[0] != "/x" {
				t.Fatalf("%s: unexpected event %+v", name, ev)
			}
			if !ev.BuildOK {
				t.Fatalf("%s: BuildOK should be true without diagnostics", name)
			}
		case <-time.After(time.Second):
			t.Fatalf("%s: did not receive broadcast", name)
		}
	}
}

func TestDevHubCurrentCatchUp(t *testing.T) {
	h := NewDevHub()
	h.Publish(DevEvent{Type: "reload", Diagnostics: []diag.Diagnostic{{File: "a.tsx", Line: 2, Message: "boom"}}})
	cur, ok := h.Current()
	if !ok || len(cur.Diagnostics) != 1 || cur.BuildOK {
		t.Fatalf("unexpected current event: %+v ok=%v", cur, ok)
	}
}

func TestDiagnosticsFromError(t *testing.T) {
	d := diag.New("a.tsx", 3, 5, "boom", "fix it", "line")
	err := &renderError{page: "p", summary: "render failed", errs: []error{d, fmt.Errorf("plain")}}
	got := DiagnosticsFromError(err)
	if len(got) != 2 {
		t.Fatalf("expected 2 diagnostics, got %d: %+v", len(got), got)
	}
	if got[0].Message != "boom" || got[0].Line != 3 || got[0].Hint != "fix it" {
		t.Errorf("structured diagnostic lost: %+v", got[0])
	}
	if got[1].Message != "plain" {
		t.Errorf("plain error not preserved: %+v", got[1])
	}

	multi := &multiError{summary: "s", errs: []error{d}}
	if got := DiagnosticsFromError(multi); len(got) != 1 || got[0].File != "a.tsx" {
		t.Errorf("multiError not unwrapped: %+v", got)
	}
}

func TestDiagnosticsFromFlattenedError(t *testing.T) {
	// BuildAll used to flatten errors to a single string, losing the location.
	// The fallback parser must recover file:line:col from the text.
	msg := "build failed: 1 error(s):\n" +
		"  C:\\p\\examples\\src\\pages\\edge-cases.tsx: bundling page " +
		"C:\\p\\examples\\src\\pages\\edge-cases.tsx: " +
		"C:\\p\\examples\\src\\pages\\edge-cases.tsx:417:23: expected ], got token(118)"
	got := DiagnosticsFromError(fmt.Errorf("%s", msg))
	if len(got) != 1 {
		t.Fatalf("expected 1 diagnostic, got %d: %+v", len(got), got)
	}
	if got[0].File != "C:\\p\\examples\\src\\pages\\edge-cases.tsx" || got[0].Line != 417 || got[0].Col != 23 {
		t.Errorf("location not recovered: %+v", got[0])
	}

	unix := DiagnosticsFromError(fmt.Errorf("src/pages/a.tsx:12:5: boom"))
	if len(unix) != 1 || unix[0].File != "src/pages/a.tsx" || unix[0].Line != 12 {
		t.Errorf("unix location not recovered: %+v", unix)
	}
}

func TestSafeProjectPath(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "a.tsx"), []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, ok := safeProjectPath(root, "a.tsx"); !ok {
		t.Error("valid relative path rejected")
	}
	// Diagnostics carry absolute paths; those inside the root must be accepted.
	absWithin := filepath.Join(root, "a.tsx")
	if _, ok := safeProjectPath(root, absWithin); !ok {
		t.Error("absolute path inside root rejected")
	}
	if _, ok := safeProjectPath(root, "../escape.tsx"); ok {
		t.Error("path escaping the root accepted")
	}
	// An absolute path outside the root must be rejected.
	outside := filepath.Join(t.TempDir(), "other.tsx")
	if err := os.WriteFile(outside, []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, ok := safeProjectPath(root, outside); ok {
		t.Error("absolute path outside root accepted")
	}
	if _, ok := safeProjectPath(root, ""); ok {
		t.Error("empty path accepted")
	}
	if _, ok := safeProjectPath(root, "missing.tsx"); ok {
		t.Error("missing file accepted")
	}
	if _, ok := safeProjectPath(root, "."); ok {
		t.Error("directory path accepted")
	}
}

func TestWriteDevChunkFallback(t *testing.T) {
	dir := t.TempDir()
	rel := writeDevChunk(dir, dir)
	if rel != "chunks/krate-dev.js" {
		t.Fatalf("unexpected dev chunk path: %q", rel)
	}
	data, err := os.ReadFile(filepath.Join(dir, rel))
	if err != nil {
		t.Fatalf("dev chunk not written: %v", err)
	}
	if !strings.Contains(string(data), "__krate/hotreload") {
		t.Errorf("dev chunk missing reload wiring:\n%s", data)
	}
}

func TestGenerateHTMLInjectsDevBundle(t *testing.T) {
	dev := generateHTML("<div></div>", "", "", "", nil, "", "", "/", true, `{"sse":"/__krate/hotreload"}`, "", "")
	if !strings.Contains(dev, "__KRATE_DEV__") {
		t.Errorf("dev HTML missing bootstrap:\n%s", dev)
	}
	if !strings.Contains(dev, "chunks/krate-dev.js") {
		t.Errorf("dev HTML missing dev bundle script:\n%s", dev)
	}
	prod := generateHTML("<div></div>", "", "", "", nil, "", "", "/", false, "", "", "")
	if strings.Contains(prod, "krate-dev.js") || strings.Contains(prod, "__KRATE_DEV__") {
		t.Errorf("production HTML must not include dev tooling:\n%s", prod)
	}
}
