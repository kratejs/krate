package fix

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFixLowercaseEvents(t *testing.T) {
	src := `export default function Page() {
  return (
    <div>
      <button onclick={go} onchange="native()" onClick={keep}>
        <span onmouseenter={hover} onclickcapture={capture}>go</span>
      </button>
      <Widget onsave={keep} />
    </div>
  );
}
`
	out, changes, err := fixSource(src)
	if err != nil {
		t.Fatal(err)
	}
	if len(changes) != 3 {
		t.Fatalf("changes = %+v", changes)
	}
	for _, want := range []string{"onClick={go}", "onMouseEnter={hover}", "onClickCapture={capture}"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q:\n%s", want, out)
		}
	}
	if !strings.Contains(out, `onchange="native()"`) {
		t.Error("string-valued onchange must not be rewritten")
	}
	if strings.Contains(out, "onclick={go}") || strings.Contains(out, "onmouseenter=") || strings.Contains(out, "onclickcapture=") {
		t.Errorf("lowercase handlers remain:\n%s", out)
	}
	if !strings.Contains(out, "onsave={keep}") {
		t.Errorf("component prop must not be rewritten:\n%s", out)
	}
	out2, changes2, err := fixSource(out)
	if err != nil {
		t.Fatal(err)
	}
	if out2 != out || len(changes2) != 0 {
		t.Fatalf("not idempotent: %+v\n%s", changes2, out2)
	}
}

func TestFixSkipsParseErrors(t *testing.T) {
	src := `export default function Page() { return <div onclick={go}>`
	out, changes, err := fixSource(src)
	if err == nil {
		t.Fatal("expected a parse error")
	}
	if out != src || len(changes) != 0 {
		t.Fatalf("parse error must not rewrite: %+v\n%s", changes, out)
	}
}

func TestRunDryRunAndApply(t *testing.T) {
	root := t.TempDir()
	file := filepath.Join(root, "src", "page.tsx")
	if err := os.MkdirAll(filepath.Dir(file), 0755); err != nil {
		t.Fatal(err)
	}
	src := `export default function Page() { return <button onclick={go}>go</button>; }`
	if err := os.WriteFile(file, []byte(src), 0644); err != nil {
		t.Fatal(err)
	}
	res, err := Run(root, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Changes) != 1 || res.Applied {
		t.Fatalf("dry run = %+v", res)
	}
	got, _ := os.ReadFile(file)
	if string(got) != src {
		t.Fatal("dry run wrote the file")
	}
	res, err = Run(root, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Changes) != 1 || !res.Applied {
		t.Fatalf("apply = %+v", res)
	}
	got, _ = os.ReadFile(file)
	if !strings.Contains(string(got), "onClick={go}") {
		t.Fatalf("apply did not rewrite: %s", got)
	}
	res, err = Run(root, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Changes) != 0 {
		t.Fatalf("second apply should be a no-op: %+v", res.Changes)
	}
}
