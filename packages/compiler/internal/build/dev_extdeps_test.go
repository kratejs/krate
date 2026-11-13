package build

import (
	"path/filepath"
	"reflect"
	"testing"
)

func TestExternalDepDirs(t *testing.T) {
	base := t.TempDir()
	root := filepath.Join(base, "apps", "web")
	tokens := filepath.Join(base, "packages", "tokens")
	b := &Builder{Root: root, depGraph: map[string][]string{
		filepath.Join(root, "src", "a.css"):                   {"p"},
		filepath.Join(tokens, "tokens.css"):                   {"p"},
		filepath.Join(tokens, "more.css"):                     {"p"},
		filepath.Join(root, "node_modules", "x", "x.css"):     {"p"},
		filepath.Join(base, "node_modules", "y", "y.css"):     {"p"},
		filepath.Join(base, "packages", "other", "other.css"): {"p"},
	}}
	got := b.externalDepDirs()
	want := []string{filepath.Join(base, "packages", "other"), tokens}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("externalDepDirs = %v, want %v", got, want)
	}
}
