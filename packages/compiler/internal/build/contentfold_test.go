package build

import (
	"reflect"
	"testing"
)

func TestFoldChainJSONMemoized(t *testing.T) {
	resetContentFoldCache()

	js := `[{"slug":"a","n":1},{"slug":"b","n":2}]`
	v1, ok1 := foldChainJSON(js)
	if !ok1 {
		t.Fatal("expected first fold to succeed")
	}
	v2, ok2 := foldChainJSON(js)
	if !ok2 {
		t.Fatal("expected cached fold to succeed")
	}
	if !reflect.DeepEqual(v1, v2) {
		t.Fatalf("cached value differs: %v vs %v", v1, v2)
	}

	contentFoldCache.mu.Lock()
	n := len(contentFoldCache.m)
	contentFoldCache.mu.Unlock()
	if n != 1 {
		t.Fatalf("cache entries = %d, want 1 (identical chains must share one eval)", n)
	}

	// A failing chain is cached as a negative result (no re-evaluation).
	if _, ok := foldChainJSON("this is not ( valid js"); ok {
		t.Fatal("expected fold of invalid JS to fail")
	}
	contentFoldCache.mu.Lock()
	n = len(contentFoldCache.m)
	contentFoldCache.mu.Unlock()
	if n != 2 {
		t.Fatalf("cache entries = %d, want 2", n)
	}
}

// The cached value is shared read-only, so each call must still produce its own
// mutable AST nodes.
func TestFoldChainJSONFreshASTPerCall(t *testing.T) {
	resetContentFoldCache()
	js := `[{"slug":"a"}]`
	v1, ok := foldChainJSON(js)
	if !ok {
		t.Fatal("fold failed")
	}
	a1 := arrayASTFromJSON(v1)
	a2 := arrayASTFromJSON(v1)
	if a1 == a2 {
		t.Fatal("expected distinct AST nodes per call")
	}
}
