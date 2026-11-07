package build

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const mergeFormSrc = `import { useState, useEffect } from "react";
function Form(props: { mode: string; hint?: string }) {
  const [text, setText] = useState("");
  useEffect(() => { console.log("mount"); }, []);
  return <form><h3>{props.mode}</h3><input id="in" value={text} onChange={(e) => setText(e.target.value)} /><em>{props.hint ?? "none"}</em></form>;
}
`

// buildMergeSrc builds a one-page project and returns its HTML, the
// hydration JS and the captured build output.
func buildMergeSrc(t *testing.T, src string) (html, js, out string) {
	t.Helper()
	var outDir string
	var err error
	out, _ = captureOutput(func() error {
		html, outDir, err = buildPageSrc(t, src)
		return err
	})
	if err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}
	files, _ := filepath.Glob(filepath.Join(outDir, "index.*.js"))
	for _, f := range files {
		data, rerr := os.ReadFile(f)
		if rerr != nil {
			t.Fatal(rerr)
		}
		js += string(data)
	}
	return html, js, stripANSI(out)
}

func TestBuildSameTypeBranchesOneInstance(t *testing.T) {
	html, js, out := buildMergeSrc(t, mergeFormSrc+`export default function Page() {
  const [isEdit, setEdit] = useState(false);
  return <div><button id="flip" onClick={() => setEdit((v) => !v)}>flip</button>{isEdit ? <Form mode="edit" hint="h" /> : <Form mode="new" />}</div>;
}`)
	if n := strings.Count(html, "<form"); n != 1 {
		t.Errorf("static HTML has %d forms, want the one instance:\n%s", n, html)
	}
	if strings.Contains(html, "<template") || strings.Contains(html, "edit") {
		t.Errorf("the other branch must not be baked into the page:\n%s", html)
	}
	if !strings.Contains(html, "new") {
		t.Errorf("the initial branch must be server-rendered:\n%s", html)
	}
	if n := strings.Count(js, "createEffectDeps("); n != 1 {
		t.Errorf("want one Form instance in the hydration code, got %d copies of its body:\n%s", n, js)
	}
	if !strings.Contains(js, `get mode(){return t()?"edit":"new"}`) {
		t.Errorf("mode must follow the condition through a live prop:\n%s", js)
	}
	if strings.Contains(out, "KR8360") {
		t.Errorf("a merged pair must not warn:\n%s", out)
	}
}

func TestBuildSameTypeBranchesStatelessKeepsBothBranches(t *testing.T) {
	html, _, out := buildMergeSrc(t, `import { useState } from "react";
function Badge(props: { label: string }) { return <b>{props.label}</b>; }
export default function Page() {
  const [on, setOn] = useState(false);
  return <div onClick={() => setOn(!on)}>{on ? <Badge label="yes" /> : <Badge label="no" />}</div>;
}`)
	if !strings.Contains(html, "yes") || !strings.Contains(html, "no") {
		t.Errorf("a stateless component keeps both branches:\n%s", html)
	}
	if strings.Contains(out, "KR8360") {
		t.Errorf("a stateless component must not warn:\n%s", out)
	}
}

func TestBuildSameTypeBranchesWarnsWhenItCannotMerge(t *testing.T) {
	_, _, out := buildMergeSrc(t, mergeFormSrc+`const a = { hint: "a" };
const b = { hint: "b" };
export default function Page() {
  const [isEdit, setEdit] = useState(false);
  return <div onClick={() => setEdit((v) => !v)}>{isEdit ? <Form mode="edit" {...a} /> : <Form mode="new" {...b} />}</div>;
}`)
	if !strings.Contains(out, "KR8360") || !strings.Contains(out, "<Form>") {
		t.Errorf("want a KR8360 warning naming <Form>:\n%s", out)
	}
}

func TestBuildDifferentKeysDoNotWarn(t *testing.T) {
	_, _, out := buildMergeSrc(t, mergeFormSrc+`const a = { hint: "a" };
const b = { hint: "b" };
export default function Page() {
  const [isEdit, setEdit] = useState(false);
  return <div onClick={() => setEdit((v) => !v)}>{isEdit ? <Form key="e" mode="edit" {...a} /> : <Form key="n" mode="new" {...b} />}</div>;
}`)
	if strings.Contains(out, "KR8360") {
		t.Errorf("different keys are an intentional remount and must not warn:\n%s", out)
	}
}
