package escape

import "testing"

func TestCommentEncoding(t *testing.T) {
	cases := map[string]string{
		"r0.c1":        "r0.c1",
		"a-->b":        "a--%3Eb",
		"<!--x--!>":    "%3C!--x--!%3E",
		"50%":          "50%25",
		"k\x00\x1f":    "k%00",
		"item-1:key-2": "item-1:key-2",
	}
	for in, want := range cases {
		if got := Comment(in); got != want {
			t.Errorf("Comment(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestTextEscaping(t *testing.T) {
	if got := Text(`<a href="x">it's & done</a>`); got != `&lt;a href="x"&gt;it's &amp; done&lt;/a&gt;` {
		t.Errorf("Text = %s", got)
	}
}
