package fsutil

import (
	"path/filepath"
	"testing"
)

func TestWithinDir(t *testing.T) {
	base := t.TempDir()
	cases := []struct {
		name string
		p    string
		want bool
	}{
		{"base", base, true},
		{"child", filepath.Join(base, "a", "b"), true},
		{"parent", filepath.Dir(base), false},
		{"escape", filepath.Join(base, "..", "escape"), false},
		{"sibling", base + "-sibling", false},
	}
	for _, c := range cases {
		if got := WithinDir(base, c.p); got != c.want {
			t.Errorf("%s: WithinDir(%q, %q) = %v, want %v", c.name, base, c.p, got, c.want)
		}
	}
}
