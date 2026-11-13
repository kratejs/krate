package build

import (
	"reflect"
	"testing"
)

func TestRouteSpecificityOrder(t *testing.T) {
	patterns := []string{
		"[[...all]]",
		"docs/[...slug]",
		"docs/[id]",
		"docs/intro",
		"[lang]/docs",
		"docs/[id]/edit",
		"shop/[[...f]]",
		"shop/[...f]",
	}
	sortRoutePatterns(patterns)
	want := []string{
		"docs/intro",
		"docs/[id]/edit",
		"docs/[id]",
		"docs/[...slug]",
		"shop/[...f]",
		"shop/[[...f]]",
		"[lang]/docs",
		"[[...all]]",
	}
	if !reflect.DeepEqual(patterns, want) {
		t.Errorf("order = %v\nwant    %v", patterns, want)
	}
	// Deterministic for any input order.
	rev := append([]string{}, want...)
	for i, j := 0, len(rev)-1; i < j; i, j = i+1, j-1 {
		rev[i], rev[j] = rev[j], rev[i]
	}
	sortRoutePatterns(rev)
	if !reflect.DeepEqual(rev, want) {
		t.Errorf("order depends on input: %v", rev)
	}
}
