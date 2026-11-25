package renderer

import (
	"strings"
	"testing"
)

func TestDevComponentEncoding(t *testing.T) {
	t.Parallel()
	c := DevComponent{Key: "_l1_c0.2", Name: "Like Button", Tier: "client", File: "/p/src/com ponents/A,b.tsx", Line: 12, Parent: "x/y#1", Island: "", Region: "region-c0"}
	enc := EncodeDevComponent(c)
	if strings.ContainsAny(enc, " \"'<>&-!%") {
		t.Errorf("token %q is not safe in an attribute or a comment", enc)
	}
	got, ok := DecodeDevComponent(enc)
	if !ok || got != c {
		t.Errorf("round trip = %+v, %v", got, ok)
	}
	for _, bad := range []string{"", "a,b", "k,*zz,a,b,1,,,", ",n,t,f,1,,,"} {
		if _, ok := DecodeDevComponent(bad); ok {
			t.Errorf("decoded %q", bad)
		}
	}
}

func TestMarkDevRoots(t *testing.T) {
	t.Parallel()
	for _, c := range []struct{ name, in, want string }{
		{"single root", `<div class="a"><span>x</span></div>`, `<div class="a" data-kd="T"><span>x</span></div>`},
		{"several roots and text", `hi <p>a</p><!--k:1-->t<!--/k:1--><p>b</p>`, `hi <p data-kd="T">a</p><!--k:1-->t<!--/k:1--><p data-kd="T">b</p>`},
		{"void and self-closing", `<img src="a.png"><br/><svg viewBox="0 0 1 1"><path d="M0"/></svg>`, `<img src="a.png" data-kd="T"><br data-kd="T"/><svg viewBox="0 0 1 1" data-kd="T"><path d="M0"/></svg>`},
		{"quoted >", `<a title="x > y" href="/">l</a>`, `<a title="x > y" href="/" data-kd="T">l</a>`},
		{"raw text content", `<style>p>a{}</style><div>x</div>`, `<style>p>a{}</style><div data-kd="T">x</div>`},
		{"script skipped", `<script>if(a<b){document.write("<p>")}</script><p>z</p>`, `<script>if(a<b){document.write("<p>")}</script><p data-kd="T">z</p>`},
		{"island placeholder left exact", `<div data-krate-island="i1" style="display:contents"></div>`, `<div data-krate-island="i1" style="display:contents"></div>`},
		{"template left alone", `<template data-kc="3"><p>x</p></template>`, `<template data-kc="3"><p>x</p></template>`},
		{"comments only", `<!--__children__-->`, `<!--__children__-->`},
		{"text only", `just text`, `just text`},
		{"not a tag", `a < b <p>c</p>`, `a < b <p data-kd="T">c</p>`},
	} {
		if got := MarkDevRoots(c.in, "T"); got != c.want {
			t.Errorf("%s:\n got %s\nwant %s", c.name, got, c.want)
		}
	}
	// An enclosing component goes first.
	inner := MarkDevRoots(`<main>x</main>`, "IN")
	if got := MarkDevRoots(inner, "OUT"); got != `<main data-kd="OUT IN">x</main>` {
		t.Errorf("nested = %s", got)
	}
}
