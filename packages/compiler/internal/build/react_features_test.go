package build

import (
	"strings"
	"testing"
)

// TestReactChildrenMapSSR verifies a literal Children.map folds at build time,
// so the mapped children appear in the server-rendered HTML.
func TestReactChildrenMapSSR(t *testing.T) {
	page := `
		import { Children } from 'react';
		export default function Page() {
			return <ul>{Children.map(['a', 'b'], (item) => <li>{item}</li>)}</ul>;
		}
	`
	html, _, err := buildKrateProject(t, map[string]string{"src/pages/index.tsx": page})
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if !strings.Contains(html, "<li>a</li>") || !strings.Contains(html, "<li>b</li>") {
		t.Fatalf("Children.map did not render mapped children:\n%s", html)
	}
}

// TestReactChildrenCountSSR verifies Children.count folds to its literal value.
func TestReactChildrenCountSSR(t *testing.T) {
	page := `
		import { Children } from 'react';
		export default function Page() {
			return <p>{Children.count([<i />, <i />])}</p>;
		}
	`
	html, _, err := buildKrateProject(t, map[string]string{"src/pages/index.tsx": page})
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if !strings.Contains(html, ">2<") {
		t.Fatalf("Children.count did not fold to 2:\n%s", html)
	}
}

// TestReactCloneElementFold verifies cloneElement merges props at build time.
func TestReactCloneElementFold(t *testing.T) {
	page := `
		import { cloneElement } from 'react';
		export default function Page() {
			return <div>{cloneElement(<span class="a" />, { id: 'x' })}</div>;
		}
	`
	html, _, err := buildKrateProject(t, map[string]string{"src/pages/index.tsx": page})
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if !strings.Contains(html, `class="a"`) || !strings.Contains(html, `id="x"`) {
		t.Fatalf("cloneElement did not merge props:\n%s", html)
	}
}

// TestReactCreateRefBinds verifies createRef lowers to a {current:null} object
// and binds the element at hydration.
func TestReactCreateRefBinds(t *testing.T) {
	page := `
		import { createRef } from 'react';
		export default function Page() {
			const ref = createRef();
			return <button ref={ref}>x</button>;
		}
	`
	html, js, err := buildKrateProject(t, map[string]string{"src/pages/index.tsx": page})
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if strings.Contains(html, "<button ref") {
		t.Fatalf("ref leaked as an HTML attribute:\n%s", html)
	}
	if !strings.Contains(html, "<button data-k=") {
		t.Fatalf("expected a hydration marker on the ref target:\n%s", html)
	}
	if !strings.Contains(js, "var ref={current:null}") {
		t.Fatalf("createRef was not lowered:\n%s", js)
	}
	if !strings.Contains(js, "()=>ref,v=>{ref=v;}") {
		t.Fatalf("expected an adaptive ref binding:\n%s", js)
	}
}

// TestReactForwardRefCallback verifies a callback ref passed through forwardRef
// reaches the rendered element.
func TestReactForwardRefCallback(t *testing.T) {
	page := `
		import { forwardRef } from 'react';
		const Field = forwardRef((props: any, ref: any) => <input ref={ref} />);
		export default function Page() {
			const onRef = (el: any) => { console.log(el); };
			return <Field ref={onRef} />;
		}
	`
	html, js, err := buildKrateProject(t, map[string]string{"src/pages/index.tsx": page})
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if strings.Contains(html, "<input ref") {
		t.Fatalf("ref leaked as an HTML attribute:\n%s", html)
	}
	if !strings.Contains(js, "{ref:onRef}") || !strings.Contains(js, "()=>props.ref,v=>{props.ref=v;}") {
		t.Fatalf("expected a callback ref binding:\n%s", js)
	}
}

// TestReactSlotStaticChild verifies the IR builder merges <Slot> props onto a
// literal child element.
func TestReactSlotStaticChild(t *testing.T) {
	page := `
		import { Slot } from '@krate/runtime';
		export default function Page() {
			return (
				<Slot className="btn">
					<a className="extra" href="/x">Go</a>
				</Slot>
			);
		}
	`
	html, _, err := buildKrateProject(t, map[string]string{"src/pages/index.tsx": page})
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if !strings.Contains(html, `class="btn extra"`) || !strings.Contains(html, `href="/x"`) {
		t.Fatalf("Slot did not merge props onto its child:\n%s", html)
	}
}

// TestReactSlotAsChildComponent verifies the shadcn asChild pattern: a dynamic
// tag resolves to Slot and merges its props onto the call-site child.
func TestReactSlotAsChildComponent(t *testing.T) {
	page := `
		import { Slot } from '@krate/runtime';
		function Button({ asChild, children }: any) {
			const Comp = asChild ? Slot : 'button';
			return <Comp class="btn">{children}</Comp>;
		}
		export default function Page() {
			return <Button asChild><a href="/x">Go</a></Button>;
		}
	`
	html, _, err := buildKrateProject(t, map[string]string{"src/pages/index.tsx": page})
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if !strings.Contains(html, `<a href="/x" class="btn">Go</a>`) {
		t.Fatalf("asChild did not render its child with the Slot class:\n%s", html)
	}
}

// TestModuleConstEmittedToHydration verifies module-level constants used by
// client-only code are declared inside the component hydration scope.
func TestModuleConstEmittedToHydration(t *testing.T) {
	page := `
		import { createSignal, createMemo } from '@krate/runtime';
		const FRUITS = ['Apple', 'Apricot', 'Banana'];
		export default function Page() {
			const [q, setQ] = createSignal('a');
			const filtered = createMemo(() => FRUITS.filter((f) => f.toLowerCase().includes(q().toLowerCase())));
			return (
				<div>
					<button onClick={() => setQ('A')}>set</button>
					<ul>{filtered().map((f) => <li>{f}</li>)}</ul>
				</div>
			);
		}
	`
	_, js, err := buildKrateProject(t, map[string]string{"src/pages/index.tsx": page})
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if !strings.Contains(js, "var FRUITS=") {
		t.Fatalf("module const FRUITS was not declared in hydration JS:\n%s", js)
	}
}

// TestLocalCvaClassFolds verifies a component-local const built from
// cn(cva(...)) folds to a real class string instead of emitting the variable
// name (which produced class="classes" in the client path).
func TestLocalCvaClassFolds(t *testing.T) {
	files := map[string]string{
		"src/pages/index.tsx": `
			import { createSignal, cva, cn } from '@krate/runtime';
			const buttonVariants = cva('btn', {
				variants: { variant: { default: 'v-default', secondary: 'v-secondary' } },
				defaultVariants: { variant: 'default' },
			});
			function Button({ variant = 'default', children, ...props }: any) {
				const classes = cn(buttonVariants({ variant }), 'extra');
				return <button class={classes} onClick={props.onClick}>{children}</button>;
			}
			export default function Page() {
				const [n, setN] = createSignal(0);
				return <Button variant="default" onClick={() => setN(n() + 1)}>{n}</Button>;
			}
		`,
	}
	html, js, err := buildKrateProject(t, files)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if !strings.Contains(html, `class="btn v-default extra"`) {
		t.Fatalf("local cva class did not fold:\n%s", html)
	}
	if strings.Contains(html, "class=classes") || strings.Contains(js, "class=classes") {
		t.Fatalf("class variable leaked into output:\n%s\n%s", html, js)
	}
}
func TestReactUseMemoDepsBinding(t *testing.T) {
	page := `
		import { useMemo, useState } from 'react';
		export default function Page() {
			const [q, setQ] = useState('');
			const visible = useMemo(() => ['alpha', 'beta'].filter((item) => item.includes(q())), [q]);
			return (
				<div>
					<input value={q} onChange={(event: any) => setQ(event.target.value)} />
					<ul>{visible().map((item) => <li>{item}</li>)}</ul>
				</div>
			);
		}
	`
	_, js, err := buildKrateProject(t, map[string]string{"src/pages/index.tsx": page})
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if !strings.Contains(js, "createMemoDeps") {
		t.Fatalf("useMemo deps should lower to createMemoDeps:\n%s", js)
	}
	if !strings.Contains(js, "kbindList") {
		t.Fatalf("a derived memo list must emit a reactive binding:\n%s", js)
	}
}
