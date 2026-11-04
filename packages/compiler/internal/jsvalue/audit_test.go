package jsvalue

import "testing"

func TestEvalAuditCases(t *testing.T) {
	str := func(v Value) string { return v.ToString() }
	cases := []struct{ src, want string }{
		{`({get v(){return 5}}).v`, "5"},
		{`({a: 2, get v(){return 5}}).v + 1`, "6"},
		{`"ß".toUpperCase()`, "SS"},
		{`"straße".toUpperCase()`, "STRASSE"},
		{`"ǆ".toUpperCase()`, "Ǆ"},
		{`"ŉ".toUpperCase()`, "ʼN"},
		{`"ﬁ".toUpperCase()`, "FI"},
		{`"İ".toLowerCase()`, "i̇"},
		{`"ΑΣ".toLowerCase()`, "ας"},
		{`"ΑΣ Β".toLowerCase()`, "ας β"},
		{`(255).toString(16)`, "ff"},
		{`(-255).toString(2)`, "-11111111"},
	}
	for _, tc := range cases {
		v := evalSrc(t, tc.src)
		if v.IsUnknown() || v.IsUnsupported() {
			t.Errorf("%s: unresolved %#v", tc.src, v)
			continue
		}
		if got := str(v); got != tc.want {
			t.Errorf("%s: got %q want %q", tc.src, got, tc.want)
		}
	}
	for _, src := range []string{
		`"a".toLocaleUpperCase()`, `"a".toLocaleLowerCase()`, `(1234.5).toLocaleString()`,
		`"a".localeCompare("b")`,
	} {
		if v := evalSrc(t, src); !v.IsUnknown() && !v.IsUnsupported() {
			t.Errorf("%s: locale-dependent call was folded to %#v", src, v)
		}
	}
}
