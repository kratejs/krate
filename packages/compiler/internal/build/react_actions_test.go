package build

import (
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/config"
	"github.com/kratejs/krate/packages/compiler/internal/diag"
)

// buildActionsFixture builds a temp project with one "use server" module and
// the given page, returning the page HTML and its hydration JS.
func buildActionsFixture(t *testing.T, page string) (string, string) {
	t.Helper()
	root := t.TempDir()
	writeActionTestFile(t, filepath.Join(root, "src", "actions", "todos.ts"), `"use server";
export async function addTodo(prev: string[], fd: FormData) { return [...prev, String(fd.get("title"))]; }
export async function save(fd: FormData) { return { ok: true }; }
`)
	writeActionTestFile(t, filepath.Join(root, "src", "pages", "index.tsx"), page)
	cfg := config.Default()
	cfg.PagesDir = filepath.Join(root, "src", "pages")
	cfg.OutDir = filepath.Join(root, "dist")
	cfg.Minify = false
	b := New(root, cfg)
	if err := b.prepareActions(); err != nil {
		t.Fatal(err)
	}
	if err := b.BuildAll(); err != nil {
		t.Fatal(err)
	}
	html, err := os.ReadFile(filepath.Join(cfg.OutDir, "index.html"))
	if err != nil {
		t.Fatal(err)
	}
	var js strings.Builder
	matches, _ := filepath.Glob(filepath.Join(cfg.OutDir, "index.*.js"))
	for _, m := range matches {
		data, _ := os.ReadFile(m)
		js.Write(data)
	}
	return string(html), js.String()
}

func TestReactActionFormsCompile(t *testing.T) {
	html, js := buildActionsFixture(t, `import { useActionState, useOptimistic } from "react";
import { useFormStatus } from "react-dom";
import { addTodo, save } from "../actions/todos";

function Submit() {
  const { pending } = useFormStatus();
  return <button type="submit" disabled={pending}>{pending ? "Saving" : "Save"}</button>;
}

export default function Page() {
  const [todos, formAction, isPending] = useActionState(addTodo, ["first"], "/todos");
  const [shown, addShown] = useOptimistic(todos, (s, t) => [...s, t]);
  const local = async (fd: FormData) => { addShown(String(fd.get("title"))); };
  return <main>
    <form action={formAction} data-testid="state"><input name="title" /><Submit /></form>
    <form action={local} data-testid="local"><button>Go</button></form>
    <form data-testid="plain" action={save}><button>Save</button></form>
    <form data-testid="fa"><button formAction={async () => {}}>FA</button></form>
    <p>{shown.length} {isPending ? "pending" : "idle"}</p>
  </main>;
}
`)
	// useActionState around a server action keeps a native POST target with
	// the initial state as the leading argument and the permalink as return.
	stateForm := regexp.MustCompile(`<form[^>]*data-testid="state"[^>]*>`).FindString(html)
	for _, want := range []string{`action="/__krate/action/`, `__krate_args=%5B%5B%22first%22%5D%5D`, `__krate_return=%2Ftodos`, `data-krate-action`, `method="post"`} {
		if !strings.Contains(stateForm, want) {
			t.Fatalf("state form missing %q: %s\n%s", want, stateForm, html)
		}
	}
	// A client function action has no native target.
	localForm := regexp.MustCompile(`<form[^>]*data-testid="local"[^>]*>`).FindString(html)
	if strings.Contains(localForm, "action=") || strings.Contains(localForm, "data-krate-action") {
		t.Fatalf("client function action must not render an action attribute: %s", localForm)
	}
	plain := regexp.MustCompile(`<form[^>]*data-testid="plain"[^>]*>`).FindString(html)
	if !strings.Contains(plain, `action="/__krate/action/`) || strings.Contains(plain, "__krate_args") {
		t.Fatalf("plain server action form: %s", plain)
	}
	if strings.Contains(html, `formaction=`) {
		t.Fatalf("client formAction must not render: %s", html)
	}
	if !strings.Contains(html, ">1<") {
		t.Fatalf("optimistic initial length should render 1:\n%s", html)
	}
	for _, want := range []string{
		"__krateActionState(addTodo",
		"__krateOptimistic(",
		"__krateBindFormAction(el,function(){return (formAction);})",
		"__krateBindFormAction(el,function(){return (local);})",
		"__krateFormStatusMount(el",
		"createSignal(todos())",
	} {
		if !strings.Contains(js, want) {
			t.Fatalf("hydration JS missing %q:\n%s", want, js)
		}
	}
	// Slot ids stay unique after a component child (Submit) inside a form.
	ids := regexp.MustCompile(`data-k="(k:[^"]+)"`).FindAllStringSubmatch(html, -1)
	seen := map[string]bool{}
	for _, m := range ids {
		if seen[m[1]] {
			t.Fatalf("duplicate slot id %s:\n%s", m[1], html)
		}
		seen[m[1]] = true
	}
}

func TestActionStateInitialNotStaticWarns(t *testing.T) {
	root := t.TempDir()
	writeActionTestFile(t, filepath.Join(root, "src", "actions", "a.ts"), `"use server";
export async function step(prev: number, fd: FormData) { return prev + 1; }
`)
	writeActionTestFile(t, filepath.Join(root, "src", "pages", "index.tsx"), `import { useActionState } from "react";
import { step } from "../actions/a";
export default function Page() {
  const [n, act] = useActionState(step, Date.now());
  return <form action={act}><p>{n}</p></form>;
}
`)
	cfg := config.Default()
	cfg.PagesDir = filepath.Join(root, "src", "pages")
	cfg.OutDir = filepath.Join(root, "dist")
	b := New(root, cfg)
	if err := b.prepareActions(); err != nil {
		t.Fatal(err)
	}
	stderr := captureStderr(t, func() {
		if err := b.BuildAll(); err != nil {
			t.Fatal(err)
		}
	})
	if !strings.Contains(stderr, "[KR7003]") {
		t.Fatalf("expected KR7003 warning, got:\n%s", stderr)
	}
	html, _ := os.ReadFile(filepath.Join(cfg.OutDir, "index.html"))
	if !strings.Contains(string(html), "__krate_args=%5Bnull%5D") {
		t.Fatalf("unknown initial state should fall back to null:\n%s", html)
	}
}

func captureStderr(t *testing.T, fn func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	old := os.Stderr
	os.Stderr = w
	done := make(chan string)
	go func() {
		var b strings.Builder
		buf := make([]byte, 4096)
		for {
			n, err := r.Read(buf)
			b.Write(buf[:n])
			if err != nil {
				break
			}
		}
		done <- b.String()
	}()
	defer func() { os.Stderr = old }()
	fn()
	_ = w.Close()
	os.Stderr = old
	return <-done
}

func TestParseActionExportsCodes(t *testing.T) {
	_, err := parseActionExports("\"use server\";\nexport const nope = 1;", "src/actions/bad.ts")
	var d diag.Diagnostic
	if !errors.As(err, &d) || d.Code != diag.CodeActionExportNotFunction || d.Line != 2 {
		t.Fatalf("expected KR7004 at line 2, got %v", err)
	}
	_, err = parseActionExports("\"use server\";\nexport { a } from './other';", "src/actions/re.ts")
	if !errors.As(err, &d) || d.Code != diag.CodeActionReexport {
		t.Fatalf("expected KR7005, got %v", err)
	}
}
