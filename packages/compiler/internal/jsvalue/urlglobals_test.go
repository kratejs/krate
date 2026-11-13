package jsvalue

import "testing"

func TestURLAndURIEncoding(t *testing.T) {
	cases := []struct{ src, want string }{
		{`new URL("/pricing", "https://example.com").toString()`, `'https://example.com/pricing'`},
		{`new URL("https://a.test/x?y=1#z").pathname`, `'/x'`},
		{`new URL("https://a.test/x?y=1").searchParams.get("y")`, `'1'`},
		{`new URL("https://a.test").href`, `'https://a.test/'`},
		{`encodeURIComponent("a b/c&d")`, `'a%20b%2Fc%26d'`},
		{`encodeURI("a b/c?d")`, `'a%20b/c?d'`},
		{`decodeURIComponent("a%20b")`, `'a b'`},
	}
	for _, tc := range cases {
		got := evalSrc(t, tc.src)
		if src, ok := JSSource(got); !ok || src != tc.want {
			t.Errorf("%s = %v (%v), want %s", tc.src, src, ok, tc.want)
		}
	}
	if v := evalSrc(t, `new URL("not a url")`); !v.IsUnknown() {
		t.Errorf("an invalid URL must stay unknown, got %v", v)
	}
}
