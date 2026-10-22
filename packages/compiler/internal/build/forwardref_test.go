package build

import (
	"strings"
	"testing"
)

// TestReactForwardRefProp checks that a ref passed to a forwardRef component
// reaches the intrinsic element the component renders. The ref must never leak
// into the HTML and must bind through the props registry at hydration.
func TestReactForwardRefProp(t *testing.T) {
	page := `
		import { forwardRef, useRef } from 'react';
		const Input = forwardRef((props: any, ref: any) => <input ref={ref} {...props} />);
		export default function Page() {
			const ref = useRef(null);
			return <div><Input ref={ref} /></div>;
		}
	`
	html, js := buildReactPage(t, page)
	if strings.Contains(html, "<input ref") {
		t.Fatalf("ref leaked as an HTML attribute:\n%s", html)
	}
	if !strings.Contains(html, "data-k=") {
		t.Fatalf("expected the ref target to be marked for hydration:\n%s", html)
	}
	if !strings.Contains(js, "__krate_props[") {
		t.Fatalf("expected the forwarded ref to be passed through props:\n%s", js)
	}
	if !strings.Contains(js, "()=>props.ref,v=>{props.ref=v;}") {
		t.Fatalf("expected an adaptive ref binding:\n%s", js)
	}
}
