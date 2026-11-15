package build

import (
	"strings"
	"testing"
)

func TestBogusHintURL(t *testing.T) {
	for tag, want := range map[string]bool{
		`<link rel="preload" href="/x.wasm" as="fetch">`:            false,
		`<script type="module" src="/m.js" async></script>`:         false,
		`<link rel="preload" href="/img/undefined.png" as="image">`: true,
		`<link rel="preload" href="/a/[object Object]" as="fetch">`: true,
		`<link rel="preload" href="/p/NaN" as="fetch">`:             true,
		`<link rel="preload" as="fetch">`:                           true,
	} {
		if got := bogusHintURL(tag); got != want {
			t.Errorf("bogusHintURL(%s) = %v, want %v", tag, got, want)
		}
	}
}

// TestInjectIslandSSRHints: hints repeated across islands reach the head
// once, and an island whose render gave only head content keeps its plain
// placeholder.
func TestInjectIslandSSRHints(t *testing.T) {
	hint := `<link rel="preload" href="/x.wasm" as="fetch">`
	page := `<div data-krate-island="island-1" style="display:contents"></div><div data-krate-island="island-2" style="display:contents"></div>`
	html, head := injectIslandSSR(page, map[string]string{
		"island-1": "<p>a</p>" + realHintsHead([]string{hint}),
		"island-2": realHintsHead([]string{hint, `<link rel="preconnect" href="https://a.example">`}),
	})
	if head != hint+`<link rel="preconnect" href="https://a.example">` {
		t.Errorf("head = %s", head)
	}
	want := `<div data-krate-island="island-1" data-krate-island-ssr style="display:contents"><p>a</p></div><div data-krate-island="island-2" style="display:contents"></div>`
	if html != want {
		t.Errorf("html =\n%s\nwant\n%s", html, want)
	}
}

func TestRequestHeadMarkers(t *testing.T) {
	if got := requestHeadMarkers("<p>a</p>"); got != "<p>a</p>" {
		t.Errorf("markup without head changed: %s", got)
	}
	got := requestHeadMarkers(`<p>a</p><krate-head><link rel="preload" href="/x" as="fetch"></krate-head>`)
	if got != `<p>a</p><!--krate-head--><link rel="preload" href="/x" as="fetch"><!--/krate-head-->` {
		t.Errorf("requestHeadMarkers = %s", got)
	}
}

// TestRealBoundaryStubHints: an export with build-time hints records them
// when it renders; one without keeps the plain fallback.
func TestRealBoundaryStubHints(t *testing.T) {
	cb := ClientBoundary{ModuleID: "m", ChunkURL: "/chunks/c.js", Exports: []string{"default", "Other"}, Real: true,
		Hints: map[string][]string{"default": {`<link rel="preload" href="/x.wasm" as="fetch">`}}}
	stub := realBoundaryStub(cb)
	if strings.Count(stub, `import { krateHintTags as __krateHintTags } from "@krate/runtime/hints";`) != 1 {
		t.Errorf("stub must import krateHintTags once:\n%s", stub)
	}
	if !strings.Contains(stub, `__krateHintTags(["`+"\\u003c"+`link rel=\"preload\" href=\"/x.wasm\" as=\"fetch\"`+"\\u003e"+`"])`) {
		t.Errorf("default export must record its hint:\n%s", stub)
	}
	if !strings.Contains(stub, `function Other() { return null; }, __fallback, true, true)`) {
		t.Errorf("Other must keep the plain fallback:\n%s", stub)
	}
}
