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

func TestQuickJSModernSyntax(t *testing.T) {
	rt, err := New()
	if err != nil {
		t.Fatal(err)
	}
	defer rt.Close()

	// Optional chaining / nullish coalescing.
	if v, err := rt.Execute(`String({a:{b:5}}?.a?.b ?? 'x')`); err != nil {
		t.Errorf("optional chaining: %v", err)
	} else if s, _ := v.(string); s != "5" {
		t.Errorf("optional chaining = %q, want 5", s)
	}

	// async/await via the microtask queue.
	if _, err := rt.Execute(`globalThis.__pv = -1; (async () => { globalThis.__pv = (await Promise.resolve(41)) + 1; })();`); err != nil {
		t.Fatalf("async setup: %v", err)
	}
	rt.DrainJobs()
	if v, err := rt.Execute(`String(globalThis.__pv)`); err != nil {
		t.Errorf("async read: %v", err)
	} else if s, _ := v.(string); s != "42" {
		t.Errorf("async/await result = %q, want 42", s)
	}
}
