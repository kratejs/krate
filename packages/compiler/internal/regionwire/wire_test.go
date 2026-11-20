package regionwire

import (
	"bytes"
	"encoding/hex"
	"errors"
	"io"
	"reflect"
	"strings"
	"testing"
	"testing/iotest"
)

func full() Frame {
	return Frame{
		Type: "region", ID: "page", Kind: "page", Status: 404,
		HTML:        "<p class=\"a\">a & b < c > \"d\" \u2028 \U0001F600 \u00e9</p>",
		Error:       "boom",
		Count:       7,
		CacheStatus: "stale", NotFound: true, Redirect: "/login?next=/a&b=1", Denied: 403,
		Title: "T <b>", Head: `<meta name="x" content="y">`,
		NoStore: true, ErrorCaught: true, Fatal: true,
		Digest: "abc123", Stack: "at x\nat y",
		Vary: &VaryLists{Full: true, Headers: []string{"accept"}, Cookies: []string{"a", "b"}, Query: []string{"q"}},
	}
}

func encodeAll(t *testing.T, frames ...Frame) []byte {
	t.Helper()
	var out []byte
	for i := range frames {
		var err error
		if out, err = AppendFrame(out, &frames[i]); err != nil {
			t.Fatal(err)
		}
	}
	return out
}

func TestRoundTrip(t *testing.T) {
	frames := []Frame{
		full(),
		{Type: "region", ID: "a", Status: 200, HTML: "<i>x</i>", Vary: &VaryLists{}},
		{Type: "skip", ID: "b"},
		{Type: "error", ID: "c", Status: 500, Error: "x", Fatal: true},
		{Type: "end", Count: 3},
	}
	r := NewReader(bytes.NewReader(encodeAll(t, frames...)))
	for i, want := range frames {
		got, err := r.Next()
		if err != nil {
			t.Fatalf("frame %d: %v", i, err)
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("frame %d:\n got %#v\nwant %#v", i, got, want)
		}
	}
	if _, err := r.Next(); err != io.EOF {
		t.Fatalf("after the last frame: %v, want io.EOF", err)
	}
}

func TestHTMLIsNotEscaped(t *testing.T) {
	b := encodeAll(t, Frame{Type: "region", HTML: "<a href=\"x\">&amp;</a>"})
	if !bytes.Contains(b, []byte("<a href=\"x\">&amp;</a>")) {
		t.Fatalf("html is not raw in %q", b)
	}
}

func TestInvalidUTF8BecomesReplacement(t *testing.T) {
	b := encodeAll(t, Frame{Type: "region", HTML: "a\xffb\xc3"})
	f, err := NewReader(bytes.NewReader(b)).Next()
	if err != nil {
		t.Fatal(err)
	}
	if f.HTML != "a\uFFFDb\uFFFD" {
		t.Fatalf("html = %q", f.HTML)
	}
}

func TestLargeHTML(t *testing.T) {
	html := strings.Repeat("<td>\u00e9\u4e2d\U0001F600</td>", 600_000) // about 12 MB
	b := encodeAll(t, Frame{Type: "region", ID: "big", HTML: html}, Frame{Type: "end"})
	// A reader that returns a few bytes at a time must still reassemble it.
	r := NewReader(iotest.OneByteReader(bytes.NewReader(b[:200])))
	if _, err := r.Next(); err == nil {
		t.Fatal("a cut large frame decoded")
	}
	r = NewReader(bytes.NewReader(b))
	f, err := r.Next()
	if err != nil || f.HTML != html {
		t.Fatalf("large frame: err=%v equal=%v", err, f.HTML == html)
	}
	if f, err = r.Next(); err != nil || f.Type != "end" {
		t.Fatalf("frame after the large one: %v %v", f, err)
	}
}

func TestTruncated(t *testing.T) {
	b := encodeAll(t, full())
	for cut := 1; cut < len(b); cut++ {
		_, err := NewReader(bytes.NewReader(b[:cut])).Next()
		if !errors.Is(err, io.ErrUnexpectedEOF) {
			t.Fatalf("cut at %d of %d: err = %v, want ErrUnexpectedEOF", cut, len(b), err)
		}
	}
	if _, err := NewReader(bytes.NewReader(nil)).Next(); err != io.EOF {
		t.Fatalf("empty stream: %v, want io.EOF", err)
	}
}

func TestCorrupt(t *testing.T) {
	good := encodeAll(t, full())
	body := good[4:]
	cases := map[string][]byte{}
	mutate := func(name string, f func(b []byte) []byte) {
		cases[name] = f(append([]byte(nil), body...))
	}
	mutate("unknown type", func(b []byte) []byte { b[0] = 9; return b })
	mutate("unknown flag", func(b []byte) []byte { b[1] |= 0x80; return b })
	mutate("trailing bytes", func(b []byte) []byte { return append(b, 0) })
	mutate("short body", func(b []byte) []byte { return b[:len(b)-1] })
	mutate("string past the end", func(b []byte) []byte { b[7] = 0x7f; return b })
	mutate("overlong varint", func(b []byte) []byte {
		return append([]byte{b[0], b[1], 0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0x01}, b[3:]...)
	})
	for name, b := range cases {
		if _, err := Decode(b); err == nil {
			t.Errorf("%s: decoded without error", name)
		}
	}
	// A bad length prefix.
	for _, prefix := range [][]byte{{0xff, 0xff, 0xff, 0xff}, {0, 0, 0, 0}, {0, 0, 0, 1}} {
		_, err := NewReader(bytes.NewReader(append(append([]byte(nil), prefix...), body...))).Next()
		if !errors.Is(err, ErrCorrupt) {
			t.Errorf("prefix % x: err = %v, want ErrCorrupt", prefix, err)
		}
	}
	// A length that claims more than the stream holds is a cut stream.
	_, err := NewReader(bytes.NewReader([]byte{0x3f, 0xff, 0xff, 0xff, 1, 0})).Next()
	if !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Errorf("huge claimed length: err = %v, want ErrUnexpectedEOF", err)
	}
}

func TestUnknownTypeNotEncodable(t *testing.T) {
	if _, err := AppendFrame(nil, &Frame{Type: "nope"}); err == nil {
		t.Fatal("encoded an unknown frame type")
	}
}

// region-wire.test.ts checks the same bytes: both sides must produce and parse
// exactly these.
func TestGolden(t *testing.T) {
	const want = "000000190101c801000001610000000000000000083c623e263c2f623e"
	f := Frame{Type: "region", ID: "a", Status: 200, HTML: "<b>&</b>", NotFound: true}
	if got := hex.EncodeToString(encodeAll(t, f)); got != want {
		t.Fatalf("encoded %s, want %s", got, want)
	}
	raw, _ := hex.DecodeString(want)
	got, err := NewReader(bytes.NewReader(raw)).Next()
	if err != nil || !reflect.DeepEqual(got, f) {
		t.Fatalf("decoded %#v, %v", got, err)
	}
}

func TestHostSource(t *testing.T) {
	src := HostSource("render")
	if !strings.Contains(src, "\npackage render\n") || strings.Contains(src, "package regionwire") {
		t.Fatalf("package clause not rewritten:\n%s", src[:300])
	}
}
