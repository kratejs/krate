package bundler

import "testing"

func TestHasUseClientDirective(t *testing.T) {
	cases := map[string]bool{
		`"use client";
export default function C() {}`: true,
		`'use client'
import x from "y";`: true,
		"// header comment\n/* block */\n\"use client\";\n": true,
		"\"use strict\";\n\"use client\";\n":                true,
		"#!/usr/bin/env node\n'use client';\n":              true,
		`import x from "y";
"use client";`: false,
		`const s = "use client";`: false,
		`"use server";`:           false,
		``:                        false,
	}
	for src, want := range cases {
		if got := HasUseClientDirective(src); got != want {
			t.Errorf("HasUseClientDirective(%q) = %v, want %v", src, got, want)
		}
	}
}
