package irtree_test

import (
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/irtree"
)

// childComponent returns the first component slot of the page root's tree.
func childComponent(t *testing.T, tree *irtree.ComponentTree, name string) *irtree.ComponentNode {
	t.Helper()
	var found *irtree.ComponentNode
	var walk func([]irtree.SlotNode)
	walk = func(nodes []irtree.SlotNode) {
		for _, n := range nodes {
			cs, ok := n.(*irtree.ComponentSlot)
			if !ok || cs.Component == nil {
				continue
			}
			if cs.Component.Name == name && found == nil {
				found = cs.Component
			}
			walk(cs.Component.Children)
		}
	}
	walk(tree.Root.Children)
	if found == nil {
		t.Fatalf("component %s not found", name)
	}
	return found
}

// dynamicSlot returns the initial text and signal names of the first text or
// expression slot of n.
func dynamicSlot(t *testing.T, n *irtree.ComponentNode) (string, []string) {
	t.Helper()
	for _, c := range n.Children {
		switch s := c.(type) {
		case *irtree.ExprSlot:
			return s.Initial, s.Signals
		case *irtree.TextSlot:
			return s.Initial, []string{s.Signal.Name}
		}
	}
	t.Fatalf("%s has no dynamic slot", n.Name)
	return "", nil
}

func TestStateHookGettersAreReactive(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want string
	}{
		{
			name: "object result",
			src: `import { createSignal } from "@krate/runtime";
function useToggle(initial = false) {
  const [on, setOn] = createSignal(initial);
  return { on, toggle: () => setOn(!on()) };
}
function Panel() {
  const { on, toggle } = useToggle();
  return <button onClick={toggle}>{on() ? "open" : "closed"}</button>;
}
export default function Page() { return <div><Panel /></div>; }`,
			want: "closed",
		},
		{
			name: "array result and an argument",
			src: `import { createSignal } from "@krate/runtime";
function useFlag(initial = false) {
  const [on, setOn] = createSignal(initial);
  return [on, () => setOn(!on())] as const;
}
function Panel() {
  const [on, toggle] = useFlag(true);
  return <button onClick={toggle}>{on() ? "open" : "closed"}</button>;
}
export default function Page() { return <div><Panel /></div>; }`,
			want: "open",
		},
		{
			name: "hook calling a hook",
			src: `import { createSignal } from "@krate/runtime";
function useToggle(initial = false) {
  const [on, setOn] = createSignal(initial);
  return { on, toggle: () => setOn(!on()) };
}
function usePanelState() {
  const t = useToggle(true);
  return t;
}
function Panel() {
  const { on, toggle } = usePanelState();
  return <button onClick={toggle}>{on() ? "open" : "closed"}</button>;
}
export default function Page() { return <div><Panel /></div>; }`,
			want: "open",
		},
		{
			name: "display only component",
			src: `import { createSignal, createEffect } from "@krate/runtime";
function useClock() {
  const [now, setNow] = createSignal("idle");
  createEffect(() => { setNow("ticking"); });
  return { now };
}
function Clock() {
  const { now } = useClock();
  return <p>{now()}</p>;
}
export default function Page() { return <div><Clock /></div>; }`,
			want: "idle",
		},
		{
			name: "argument the build cannot compute",
			src: `import { createSignal } from "@krate/runtime";
function useToggle(initial = false) {
  const [on, setOn] = createSignal(initial);
  return { on, toggle: () => setOn(!on()) };
}
function Panel(props) {
  const { on, toggle } = useToggle(props.start);
  return <button onClick={toggle}>{on() ? "open" : "closed"}</button>;
}
export default function Page() { return <div><Panel start={window.matchMedia("(min-width: 1px)").matches} /></div>; }`,
			want: "",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tree := annotateAndBuildReact(t, tc.src)
			name := "Panel"
			if strings.Contains(tc.src, "function Clock") {
				name = "Clock"
			}
			node := childComponent(t, tree, name)
			if node.Tier != irtree.TierClient || node.IsSSREval || !node.StateHooks {
				t.Fatalf("tier %v ssreval %v stateHooks %v, want a hydrating client component", node.Tier, node.IsSSREval, node.StateHooks)
			}
			initial, signals := dynamicSlot(t, node)
			if len(signals) != 1 {
				t.Fatalf("the slot reads %v, want the hook's getter as its one signal", signals)
			}
			if initial != tc.want {
				t.Fatalf("initial text %q, want %q", initial, tc.want)
			}
			vars := strings.Join(append(append([]string{}, node.PreSignalVars...), node.ExtraVars...), "\n")
			if !strings.Contains(vars, "createSignal(") || !strings.Contains(vars, "=use") {
				t.Fatalf("hydration scope lacks the hook and its call:\n%s", vars)
			}
		})
	}
}

// A hook whose getter is the only thing a component reads needs no handler to
// hydrate, and a plain stateless component is still flattened.
func TestStatelessComponentWithoutHookStaysStatic(t *testing.T) {
	src := `function Note(props) { return <p>{props.text}</p>; }
function noState(n) { return n + 1; }
function Label() { const n = noState(1); return <b>{n}</b>; }
export default function Page() { return <div><Note text="hi" /><Label /></div>; }`
	tree := annotateAndBuildReact(t, src)
	for _, name := range []string{"Note", "Label"} {
		node := childComponent(t, tree, name)
		if node.StateHooks || (node.Tier == irtree.TierClient && !node.IsSSREval) {
			t.Fatalf("%s: tier %v ssreval %v stateHooks %v, want a build-time component", name, node.Tier, node.IsSSREval, node.StateHooks)
		}
	}
}

func TestUseClientComponentTakesTheClientPath(t *testing.T) {
	src := `"use client";
function Wide() { return <p>{window.innerWidth > 0 ? "wide" : "none"}</p>; }
function Fixed() { return <p>fixed</p>; }
export default function Page() { return <div><Wide /><Fixed /></div>; }`
	tree := annotateAndBuildReact(t, src)
	wide := childComponent(t, tree, "Wide")
	if wide.Tier != irtree.TierClient || wide.IsSSREval {
		t.Fatalf("Wide: tier %v ssreval %v, want a client component", wide.Tier, wide.IsSSREval)
	}
	unresolved := false
	for _, c := range wide.Children {
		if e, ok := c.(*irtree.ExprSlot); ok && e.Unresolved {
			unresolved = true
		}
	}
	if !unresolved {
		t.Fatal("the window read must be left for the browser")
	}
	// What the build can compute ships no JavaScript.
	if fixed := childComponent(t, tree, "Fixed"); fixed.Tier != irtree.TierStatic {
		t.Fatalf("Fixed: tier %v, want static", fixed.Tier)
	}
}

func TestComponentWithoutDirectiveStaysBuildTime(t *testing.T) {
	src := `function Wide() { return <p>{window.innerWidth > 0 ? "wide" : "none"}</p>; }
export default function Page() { return <div><Wide /></div>; }`
	tree := annotateAndBuildReact(t, src)
	if wide := childComponent(t, tree, "Wide"); !wide.IsSSREval {
		t.Fatalf("Wide: tier %v ssreval %v, want a build-time component", wide.Tier, wide.IsSSREval)
	}
}
