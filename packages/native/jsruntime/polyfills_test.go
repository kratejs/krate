package jsruntime

import (
	"testing"
)

func TestURLRelativeResolution(t *testing.T) {
	rt, err := New()
	if err != nil {
		t.Fatal(err)
	}
	defer rt.Close()

	cases := []struct{ expr, want string }{
		{`String(new URL('./layout.tsx', 'file:///C:/proj/src/index.ts').href)`, "file:///C:/proj/src/layout.tsx"},
		{`String(new URL('sub/x', 'https://example.com/a/b/c').href)`, "https://example.com/a/b/sub/x"},
		{`String(new URL('/root', 'https://example.com/a/b').href)`, "https://example.com/root"},
		{`String(new URL('https://x.test/p?q=1#h').pathname)`, "/p"},
		{`String(new URL('https://x.test/p?q=1#h').search)`, "?q=1"},
	}
	for _, c := range cases {
		got, err := rt.Execute(c.expr)
		if err != nil {
			t.Errorf("%s: %v", c.expr, err)
			continue
		}
		if s, _ := got.(string); s != c.want {
			t.Errorf("%s = %q, want %q", c.expr, s, c.want)
		}
	}
}
