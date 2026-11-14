package caps

import (
	"testing"
)

func TestSafeFileName(t *testing.T) {
	for in, want := range map[string]string{
		"../../etc/passwd": "_.._etc_passwd",
		"..":               "download",
		"":                 "download",
		"a/b.txt":          "a_b.txt",
		"C:\\x.txt":        "C__x.txt",
		"  .hidden":        "hidden",
		"ok name.pdf":      "ok name.pdf",
		"ctl\x01\x7fz":     "ctlz",
	} {
		if got := SafeFileName(in); got != want {
			t.Errorf("SafeFileName(%q) = %q, want %q", in, got, want)
		}
	}
	long := make([]byte, 0, 400)
	for len(long) < 400 {
		long = append(long, "é"...)
	}
	if got := SafeFileName(string(long)); len(got) > 200 || len(got)%2 != 0 {
		t.Fatalf("long name: %d bytes", len(got))
	}
}
