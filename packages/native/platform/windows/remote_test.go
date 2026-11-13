package windows

import (
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/native/platform/caps"
)

func TestProfileName(t *testing.T) {
	a, _ := caps.ParsePartition("persist:shop")
	b, _ := caps.ParsePartition("shop")
	if profileName(a) == profileName(b) || profileName(a) != "kr-p-shop" || profileName(b) != "kr-m-shop" {
		t.Fatalf("%q %q", profileName(a), profileName(b))
	}
	long, _ := caps.ParsePartition("persist:" + strings.Repeat("x", 64))
	n := profileName(long)
	if len(n) > 64 || !strings.HasPrefix(n, "kr-p-") {
		t.Fatalf("long name %q", n)
	}
	long2, _ := caps.ParsePartition("persist:" + strings.Repeat("x", 63) + "y")
	if profileName(long2) == n {
		t.Fatal("two long names collided")
	}
	d, _ := caps.ParsePartition("")
	if profileName(d) != "kr-p-default" {
		t.Fatalf("default %q", profileName(d))
	}
}

func TestCertErrorNames(t *testing.T) {
	for st, want := range map[int32]string{1: "mismatch", 2: "expired", 4: "revoked", 5: "untrusted", 99: "unknown"} {
		if got := certErrorNames(st); len(got) != 1 || got[0] != want {
			t.Errorf("status %d: %v", st, got)
		}
	}
}

func TestParseChallenge(t *testing.T) {
	cases := []struct{ in, scheme, realm string }{
		{`Basic realm="vault"`, "basic", "vault"},
		{`Digest realm="a b", nonce="x"`, "digest", "a b"},
		{`Basic realm=plain, charset=UTF-8`, "basic", "plain"},
		{`Basic`, "basic", ""},
		{``, "other", ""},
	}
	for _, c := range cases {
		if s, r := parseChallenge(c.in); s != c.scheme || r != c.realm {
			t.Errorf("%q = %q, %q", c.in, s, r)
		}
	}
}

func TestFrameNavAllowed(t *testing.T) {
	p, err := caps.RemoteOptions{URL: "https://a.test/"}.Resolve()
	if err != nil {
		t.Fatal(err)
	}
	for uri, want := range map[string]bool{
		"https://a.test/x": true, "https://b.test/": false, "about:srcdoc": true, "data:text/html,x": true,
		"blob:https://a.test/u": true, "krate://app/": false, "https://app.localhost/": false, "file:///x": false,
	} {
		if got := frameNavAllowed(p, uri); got != want {
			t.Errorf("%q = %v", uri, got)
		}
	}
}
