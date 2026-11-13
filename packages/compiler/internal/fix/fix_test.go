package fix

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/diag"
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

func TestFixSafeDiagnosticCodemods(t *testing.T) {
	src := `export default function Page() {
  return (
    <div>
      <input value={v} defaultValue="x" onclick={go} />
      <input value = {v} defaultValue = "spaced" />
      <textarea value={v} defaultValue={fallback} />
      <input type="checkbox" checked={c} defaultChecked />
      <div contentEditable onChange={edit} />
      <div contentEditable onChange={edit} onInput={input} />
      <input value={undefined} defaultValue="keep" />
      <Field value={v} defaultValue="keep" onChange={edit} />
    </div>
  );
}
`
	out, changes, err := fixSource(src)
	if err != nil {
		t.Fatal(err)
	}
	if len(changes) != 6 {
		t.Fatalf("changes = %+v\n%s", changes, out)
	}
	if strings.Contains(out, `value={v} defaultValue="x"`) ||
		strings.Contains(out, `value = {v} defaultValue = "spaced"`) ||
		strings.Contains(out, "spaced") ||
		strings.Contains(out, `value={v} defaultValue={fallback}`) ||
		strings.Contains(out, "checked={c} defaultChecked") {
		t.Errorf("ignored form defaults were not removed:\n%s", out)
	}
	if !strings.Contains(out, "onClick={go}") {
		t.Errorf("lowercase handler was not fixed:\n%s", out)
	}
	if strings.Count(out, "contentEditable onInput={edit}") != 1 {
		t.Errorf("contentEditable handler was not repaired once:\n%s", out)
	}
	if !strings.Contains(out, "onChange={edit} onInput={input}") {
		t.Errorf("conflicting contentEditable handler must be left alone:\n%s", out)
	}
	if !strings.Contains(out, `value={undefined} defaultValue="keep"`) {
		t.Errorf("defaultValue must stay when value is undefined:\n%s", out)
	}
	if !strings.Contains(out, `<Field value={v} defaultValue="keep" onChange={edit} />`) {
		t.Errorf("component props must not be rewritten:\n%s", out)
	}
	out2, changes2, err := fixSource(out)
	if err != nil {
		t.Fatal(err)
	}
	if out2 != out || len(changes2) != 0 {
		t.Fatalf("not idempotent: %+v\n%s", changes2, out2)
	}
}

func TestPlanWithCodesFiltersCodemods(t *testing.T) {
	root := t.TempDir()
	file := filepath.Join(root, "src", "page.tsx")
	if err := os.MkdirAll(filepath.Dir(file), 0755); err != nil {
		t.Fatal(err)
	}
	src := `export default function Page() { return <input value={v} defaultValue="x" onclick={go} />; }`
	if err := os.WriteFile(file, []byte(src), 0644); err != nil {
		t.Fatal(err)
	}
	files, changes, _, err := PlanWithCodes(root, []string{"KR5005"})
	if err != nil {
		t.Fatal(err)
	}
	if len(changes) != 1 || changes[0].Code != diag.CodeValueAndDefault {
		t.Fatalf("changes = %+v", changes)
	}
	out := files["src/page.tsx"]
	if !strings.Contains(out, "onclick={go}") {
		t.Errorf("KR5002 was applied despite the filter:\n%s", out)
	}
	if strings.Contains(out, "defaultValue") {
		t.Errorf("KR5005 was not applied:\n%s", out)
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
