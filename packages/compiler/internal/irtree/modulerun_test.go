package irtree

import (
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/jsvalue"
)

func runOf(t *testing.T, src string) *ModuleRun {
	t.Helper()
	return RunModule(parseModule(t, src), nil)
}

// A module whose top-level code cannot change a binding needs no run, so its
// initializer folds stay as they are.
func TestRunModuleIsNilWhenNothingChanges(t *testing.T) {
	for _, src := range []string{
		"const a = [1, 2];\nconst b = a.map((x) => x * 2);",
		"const cfg = { a: 1 };\nwindow.__cfg = cfg;",
		"const cfg = { a: 1 };\nconsole.log(cfg);",
		"let hits = 0;\ndocument.addEventListener('click', () => { hits++; });",
		"const a = [3, 1];\nfunction sorted() { return [...a].sort(); }\nconst s = sorted();",
	} {
		if run := runOf(t, src); run != nil {
			t.Errorf("%q: run = %+v, want nil", src, run)
		}
	}
}

func TestRunModuleValuesAndBlocked(t *testing.T) {
	run := runOf(t, `
		const squares = [];
		for (let i = 0; i < 3; i++) squares.push(i * i);
		const total = squares.length;
		const loose = [];
		loose.push(window.x);
		const after = loose.length;
		const plain = 5;
	`)
	if run == nil {
		t.Fatal("no run")
	}
	if v := run.Values["squares"]; v.ToString() != "0,1,4" {
		t.Errorf("squares = %q", v.ToString())
	}
	if v := run.Values["total"]; v.ToString() != "3" {
		t.Errorf("total = %q", v.ToString())
	}
	if _, ok := run.Blocked["loose"]; !ok {
		t.Errorf("loose should be blocked: %+v", run)
	}
	if _, ok := run.Values["loose"]; ok {
		t.Error("loose must not have a value")
	}
	if _, ok := run.Values["after"]; ok {
		t.Error("a value read from a blocked binding must not be known")
	}
	if v := run.Values["plain"]; v.ToString() != "5" {
		t.Errorf("plain = %q", v.ToString())
	}
}

func TestTypedModuleConstsUseTheRun(t *testing.T) {
	prog := parseModule(t, "const squares = [];\nfor (let i = 0; i < 3; i++) squares.push(i * i);\nconst bad = [];\nbad.push(window.x);")
	inits := collectModuleConstInits(prog)
	got := typedModuleConsts(inits, nil, RunModule(prog, nil))
	if v := got["squares"]; v.ToString() != "0,1,4" {
		t.Errorf("squares = %q, want the value the loop built", v.ToString())
	}
	if _, ok := got["bad"]; ok {
		t.Error("a blocked binding must not fold to its initializer")
	}
	// Without the run the initializers fold, which is the bug.
	plain := typedModuleConsts(inits, nil, nil)
	if v, ok := plain["squares"]; !ok || v.ToString() != "" {
		t.Errorf("baseline fold of squares = %v", v)
	}
	_ = jsvalue.Undefined
}
