package bundler

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/astprint"
	"github.com/kratejs/krate/packages/compiler/internal/diag"
)

func actionsContain(t *testing.T, out string, wants ...string) {
	t.Helper()
	for _, w := range wants {
		if !strings.Contains(out, w) {
			t.Fatalf("missing %q in:\n%s", w, out)
		}
	}
}

func TestLowerUseActionState(t *testing.T) {
	out := rewriteSrc(t, `import { useActionState } from "react";
export default function Page() {
  const [count, formAction, isPending] = useActionState(increment, 0, "/count");
  return <form action={formAction}><p>{count} {isPending ? "..." : ""}</p></form>;
}`)
	actionsContain(t, out,
		`[count, __krate_as_set_count] = createSignal(0)`,
		`[isPending, __krate_as_setp_count] = createSignal(false)`,
		`formAction = __krateActionState(increment, () => count(), __krate_as_set_count, __krate_as_setp_count, 0, "/count")`,
		`{count()}`,
		`isPending() ?`,
	)
	if strings.Contains(out, "@krate/runtime/react") {
		t.Fatalf("lowered hook must not import the compat runtime:\n%s", out)
	}
}

func TestLowerUseActionStateWithoutPendingBinding(t *testing.T) {
	out := rewriteSrc(t, `import { useActionState } from "react";
function F() {
  const [msg, act] = useActionState(async (prev, fd) => prev + "!", "hi");
  return <form action={act}>{msg}</form>;
}`)
	actionsContain(t, out, `[__krate_as_pending_msg, __krate_as_setp_msg] = createSignal(false)`, `act = __krateActionState(`)
}

func TestLowerUseOptimistic(t *testing.T) {
	out := rewriteSrc(t, `import { useOptimistic, useState } from "react";
function List() {
  const [items, setItems] = useState([]);
  const [shown, addShown] = useOptimistic(items, (s, n) => [...s, n]);
  return <ul>{shown.length}</ul>;
}`)
	actionsContain(t, out,
		`[shown, __krate_opt_set_shown] = createSignal(items())`,
		`addShown = __krateOptimistic(() => items(), __krate_opt_set_shown, (s, n) =>`,
		`shown().length`,
	)
}

func TestLowerUseFormStatusDestructured(t *testing.T) {
	out := rewriteSrc(t, `import { useFormStatus } from "react-dom";
function Submit() {
  const { pending, data: formData } = useFormStatus();
  return <button disabled={pending}>{formData ? "x" : "y"}</button>;
}`)
	actionsContain(t, out,
		`[pending, __krate_fs_pending_pending] = createSignal(false)`,
		`[formData, __krate_fs_data_formData] = createSignal(null)`,
		`__krate_fs_ref_1 = (el) => __krateFormStatusMount(el, { pending: __krate_fs_pending_pending, data: __krate_fs_data_formData })`,
		`ref={__krate_fs_ref_1}`,
		`disabled={pending()}`,
	)
}

func TestLowerUseFormStatusObjectAndComposedRef(t *testing.T) {
	out := rewriteSrc(t, `import * as ReactDOM from "react-dom";
function Status(props) {
  const status = ReactDOM.useFormStatus();
  return <span ref={props.r}>{status.pending ? "busy" : "idle"}</span>;
}`)
	actionsContain(t, out,
		`[status, __krate_fs_set_status] = createSignal({ pending: false, data: null, method: null, action: null })`,
		`status().pending`,
		`__krateAssignRef(props.r, el)`,
	)
}

func TestLowerUseFormStatusWarnsWithoutElement(t *testing.T) {
	prog := parseProg(t, `import { useFormStatus } from "react-dom";
function Submit() {
  const { pending } = useFormStatus();
  return <Button disabled={pending} />;
}`)
	diags := RewriteReactDiag(prog, "page.tsx", "")
	if len(diags) != 1 || diags[0].Code != diag.CodeFormStatusNoElement || !diags[0].IsWarning() {
		t.Fatalf("expected one KR7002 warning, got %+v", diags)
	}
	if diags[0].Line != 3 {
		t.Fatalf("warning should point at the hook call, got line %d", diags[0].Line)
	}
}

func TestRequestFormResetFromReactDOM(t *testing.T) {
	out := rewriteSrc(t, `import { requestFormReset } from "react-dom";
function F() { return <button onClick={(e) => requestFormReset(e.target.form)}>r</button>; }`)
	actionsContain(t, out, `requestFormReset(e.target.form)`)
	if strings.Contains(out, "react-dom") {
		t.Fatalf("react-dom import should be removed:\n%s", out)
	}
}

func TestInlineUseServerIsCodedError(t *testing.T) {
	root := t.TempDir()
	page := filepath.Join(root, "src", "pages", "index.tsx")
	writeActionFile(t, page, `export default function Page() {
  async function save(fd) {
    "use server";
    return 1;
  }
  return <form action={save}><button>Save</button></form>;
}`)
	_, err := New(root).Bundle(page)
	var d diag.Diagnostic
	if !errors.As(err, &d) {
		t.Fatalf("expected a diagnostic, got %v", err)
	}
	if d.Code != diag.CodeInlineUseServer || d.Line != 3 || !strings.Contains(d.Hint, `"use server"`) {
		t.Fatalf("unexpected diagnostic: %+v", d)
	}
}

func TestInlineUseServerInArrowAttr(t *testing.T) {
	prog := parseProg(t, `export default function Page() {
  return <form action={async (fd) => { "use server"; }}><button>Save</button></form>;
}`)
	if _, ok := findInlineUseServer(prog.Body); !ok {
		t.Fatalf("inline arrow directive not found:\n%s", astprint.Print(prog))
	}
	clean := parseProg(t, `export default function Page() { const s = "use server"; return <p>{s}</p>; }`)
	if _, ok := findInlineUseServer(clean.Body); ok {
		t.Fatal("a string value is not a directive")
	}
}

func TestMissingActionExportIsCoded(t *testing.T) {
	root := t.TempDir()
	actionPath := filepath.Join(root, "src", "actions.ts")
	pagePath := filepath.Join(root, "src", "pages", "index.tsx")
	writeActionFile(t, actionPath, `"use server"; export async function save() {}`)
	writeActionFile(t, pagePath, `import { missing } from "../actions";
export default function Page() { return <div>{missing}</div>; }`)
	b := New(root)
	b.SetActionModules(map[string]map[string]string{filepath.Clean(actionPath): {"save": "a1"}})
	_, err := b.Bundle(pagePath)
	var d diag.Diagnostic
	if !errors.As(err, &d) || d.Code != diag.CodeActionImportMissing || d.Line != 1 {
		t.Fatalf("expected KR7006 at line 1, got %v", err)
	}
}
