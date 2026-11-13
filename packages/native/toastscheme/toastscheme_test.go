package toastscheme

import (
	"strings"
	"testing"
)

func TestSchemeIsStableAndValid(t *testing.T) {
	a := Scheme("com.acme.app")
	if a != Scheme("COM.ACME.APP") {
		t.Error("the scheme must not depend on case")
	}
	if a == Scheme("com.acme.other") {
		t.Error("different apps must get different schemes")
	}
	if !IsScheme(a) || IsScheme("https") || IsScheme(prefix+"zzzzzzzzzzzz") {
		t.Errorf("IsScheme is wrong for %q", a)
	}
	if a[0] < 'a' || a[0] > 'z' {
		t.Errorf("scheme %q must start with a letter", a)
	}
	for _, r := range a {
		if !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-') {
			t.Errorf("scheme %q has the character %q", a, r)
		}
	}
}

func TestURIRoundTrip(t *testing.T) {
	s := Scheme("x.y")
	for _, c := range [][2]string{{"t1", ""}, {"t1", "ok"}, {"id with space&=?", "a/b"}, {"", ""}} {
		raw := URI(s, c[0], c[1])
		id, act, ok := ParseURI(s, raw)
		if !ok || id != c[0] || act != c[1] {
			t.Errorf("%q: got %q %q %v", raw, id, act, ok)
		}
		// Windows appends a slash to the authority of some launches.
		id, act, ok = ParseURI(s, strings.Replace(raw, "activate?", "activate/?", 1))
		if !ok || id != c[0] || act != c[1] {
			t.Errorf("%q with slash: got %q %q %v", raw, id, act, ok)
		}
	}
	for _, bad := range []string{"", "https://x", s + "://activate", s + "://activate?id=1", Scheme("other") + "://activate?krate=1&id=1"} {
		if _, _, ok := ParseURI(s, bad); ok {
			t.Errorf("%q must not parse", bad)
		}
	}
}

func TestFindArg(t *testing.T) {
	s := Scheme("a.b")
	id, act, ok := FindArg(s, []string{"--flag", "file.txt", URI(s, "n", "go")})
	if !ok || id != "n" || act != "go" {
		t.Errorf("FindArg = %q %q %v", id, act, ok)
	}
	if _, _, ok := FindArg(s, []string{"--flag"}); ok {
		t.Error("no activation argument")
	}
}

func TestRegistryValues(t *testing.T) {
	s := Scheme("a.b")
	vals := RegistryValues(s, "My App", `C:\Apps\My App\app.exe`)
	got := map[string]string{}
	for _, v := range vals {
		got[v.Key+"|"+v.Name] = v.Value
	}
	cmd := got[`Software\Classes\`+s+`\shell\open\command|`]
	if cmd != `"C:\Apps\My App\app.exe" "%1"` {
		t.Errorf("command = %q", cmd)
	}
	if _, ok := got[`Software\Classes\`+s+`|URL Protocol`]; !ok {
		t.Error("the URL Protocol marker is missing")
	}
}
