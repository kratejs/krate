package build

import (
	"regexp"
	"strings"
	"testing"
)

func TestSuspenseUseRendersFallbackAndTemplatesContent(t *testing.T) {
	html, js, err := buildKrateProject(t, map[string]string{"src/pages/index.tsx": `
		import { Suspense, use, useEffect } from 'react';
		const userPromise = new Promise((r) => setTimeout(() => r({ name: 'Ada' }), 10));
		function User() {
			const user = use(userPromise);
			useEffect(() => { document.title = user.name; }, [user]);
			return <p id="user">{user.name}</p>;
		}
		export default function Page() {
			return <main><Suspense fallback={<p id="fb">loading</p>}><User /></Suspense></main>;
		}
	`})
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	m := regexp.MustCompile(`<!--suspense:([^>]+)--><!--k:s([^>]+)--><p id="?fb"?>loading</p><!--/k:s[^>]+--><!--/suspense:[^>]+-->`).FindStringSubmatch(html)
	if m == nil || m[1] != m[2] {
		t.Fatalf("expected the fallback inside the boundary slot:\n%s", html)
	}
	id := m[1]
	if !regexp.MustCompile(`<template data-kc="?s` + regexp.QuoteMeta(id) + `"? data-kb="?c"?><p id="?user"?><!--k:`).MatchString(html) {
		t.Fatalf("expected the content in a hoisted template:\n%s", html)
	}
	if !strings.Contains(js, `__krateBoundary("`+id+`",function(){`) {
		t.Fatalf("expected the component to run inside its boundary:\n%s", js)
	}
	// Effects of the content wait for the reveal: the component's owner is
	// declared inside the boundary's content branch, initially hidden.
	if !strings.Contains(js, `[["s`+id+`","c",0]]`) {
		t.Fatalf("expected the owner to sit in the boundary's content branch:\n%s", js)
	}
	if !strings.Contains(js, "__krateUse(()=>__krateRead(userPromise))") || strings.Count(js, "__krateUse(") != 1 {
		t.Fatalf("expected one use() declaration:\n%s", js)
	}
}
