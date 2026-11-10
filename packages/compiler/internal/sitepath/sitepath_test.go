package sitepath

import "testing"

func TestJoin(t *testing.T) {
	cases := []struct{ base, in, want string }{
		{"", "/a", "/a"},
		{"/repo", "/a", "/repo/a"},
		{"/repo", "/", "/repo/"},
		{"/repo", "/a?x=1#y", "/repo/a?x=1#y"},
		{"/repo", "//cdn.example.com/a", "//cdn.example.com/a"},
		{"/repo", "https://example.com/a", "https://example.com/a"},
		{"/repo", "rel/path", "rel/path"},
		{"/repo", "#top", "#top"},
		{"/repo", "?q=1", "?q=1"},
		{"/repo", "", ""},
		{"/docs", "/docs/intro", "/docs/docs/intro"},
	}
	for _, c := range cases {
		if got := Join(c.base, c.in); got != c.want {
			t.Errorf("Join(%q, %q) = %q, want %q", c.base, c.in, got, c.want)
		}
	}
}
