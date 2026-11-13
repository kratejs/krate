package caps

import (
	"fmt"
	"strconv"
	"strings"
	"testing"
)

func headerInt(t *testing.T, b []byte, key string) int {
	t.Helper()
	s := string(b)
	i := strings.Index(s, key+":")
	if i < 0 {
		t.Fatalf("no %s in header", key)
	}
	v := s[i+len(key)+1 : i+len(key)+11]
	n, err := strconv.Atoi(v)
	if err != nil {
		t.Fatal(err)
	}
	return n
}

func TestCFHTMLOffsets(t *testing.T) {
	for _, in := range []string{
		"<b>hi</b>",
		"héllo <i>wörld</i> \U0001F600",
		"",
		"<html><head></head><BODY class=x><p>doc</p></BODY></html>",
		"<p>a</p><!--StartFragment-->b<!--EndFragment--><p>c</p>",
	} {
		enc := EncodeCFHTML(in, "")
		if !strings.HasPrefix(string(enc), "Version:0.9\r\n") {
			t.Fatalf("header: %q", enc[:20])
		}
		sh, eh := headerInt(t, enc, "StartHTML"), headerInt(t, enc, "EndHTML")
		sf, ef := headerInt(t, enc, "StartFragment"), headerInt(t, enc, "EndFragment")
		if eh != len(enc) {
			t.Fatalf("%q: EndHTML %d, len %d", in, eh, len(enc))
		}
		if !strings.HasPrefix(string(enc[sh:]), "<html") && !strings.HasPrefix(string(enc[sh:]), "<p>a") {
			t.Fatalf("%q: StartHTML points at %q", in, enc[sh:sh+10])
		}
		if string(enc[sf-len(cfHTMLStart):sf]) != cfHTMLStart || string(enc[ef:ef+len(cfHTMLEnd)]) != cfHTMLEnd {
			t.Fatalf("%q: fragment offsets do not sit on the markers: %q ... %q", in, enc[sf-5:sf], enc[ef:ef+5])
		}
		want := in
		if strings.Contains(in, cfHTMLStart) {
			want = "b"
		} else if i := strings.Index(strings.ToLower(in), "<body"); i >= 0 {
			want = "<p>doc</p>"
		}
		if got := DecodeCFHTML(enc); got != want {
			t.Fatalf("round trip %q: got %q want %q", in, got, want)
		}
		if got := DecodeCFHTML(append(enc, 0)); got != want {
			t.Fatalf("NUL terminated: %q", got)
		}
	}
}

func TestCFHTMLSourceURL(t *testing.T) {
	enc := EncodeCFHTML("<b>x</b>", "https://example.com/a")
	if !strings.Contains(string(enc), "SourceURL:https://example.com/a\r\n") {
		t.Fatalf("no SourceURL: %q", enc)
	}
	sf, ef := headerInt(t, enc, "StartFragment"), headerInt(t, enc, "EndFragment")
	if string(enc[sf:ef]) != "<b>x</b>" {
		t.Fatalf("offsets shift with SourceURL: %q", enc[sf:ef])
	}
}

func TestDecodeCFHTMLFromOtherProducers(t *testing.T) {
	// Word-style: CRLF header, offsets exact.
	doc := "<html>\r\n<body>\r\n<!--StartFragment--><span>w</span><!--EndFragment-->\r\n</body>\r\n</html>"
	hdr := "Version:1.0\r\nStartHTML:0000000105\r\nEndHTML:0000000999\r\nStartFragment:0000000000\r\nEndFragment:0000000000\r\n"
	if got := DecodeCFHTML([]byte(hdr + doc)); got != "<span>w</span>" {
		t.Fatalf("zero fragment offsets, marker fallback: %q", got)
	}
	// Offsets out of range fall back to the markers.
	bad := "Version:0.9\r\nStartHTML:0000000000\r\nEndHTML:0000009999\r\nStartFragment:0000008888\r\nEndFragment:0000009999\r\n" + doc
	if got := DecodeCFHTML([]byte(bad)); got != "<span>w</span>" {
		t.Fatalf("bad offsets: %q", got)
	}
	// No markers: the StartHTML..EndHTML document.
	plain := "<p>x</p>"
	h := "Version:0.9\r\nStartHTML:%010d\r\nEndHTML:%010d\r\nStartFragment:0000000000\r\nEndFragment:0000000000\r\n"
	n := len(fmt.Sprintf(h, 0, 0))
	if got := DecodeCFHTML([]byte(fmt.Sprintf(h, n, n+len(plain)) + plain)); got != plain {
		t.Fatalf("no markers: %q", got)
	}
	// Bare HTML without a header.
	if got := DecodeCFHTML([]byte("<i>bare</i>")); got != "<i>bare</i>" {
		t.Fatalf("bare: %q", got)
	}
	if DecodeCFHTML(nil) != "" {
		t.Fatal("nil")
	}
}

func TestFormatNameMaps(t *testing.T) {
	cases := []struct{ mime, uti, win string }{
		{FormatHTML, "public.html", "HTML Format"},
		{FormatRTF, "public.rtf", "Rich Text Format"},
		{FormatPNG, "public.png", "PNG"},
	}
	for _, c := range cases {
		if UTIForFormat(c.mime) != c.uti || FormatForUTI(c.uti) != c.mime {
			t.Fatalf("uti map for %s", c.mime)
		}
		if WinFormatForFormat(c.mime) != c.win || FormatForWinFormat(strings.ToLower(c.win)) != c.mime {
			t.Fatalf("win map for %s", c.mime)
		}
	}
	if UTIForFormat("com.acme.x") != "com.acme.x" || FormatForUTI("com.acme.x") != "com.acme.x" {
		t.Fatal("unknown names pass through")
	}
	if FormatForUTI("public.plain-text") != FormatText {
		t.Fatal("plain-text alias")
	}
}

func TestDecodeClipboardText(t *testing.T) {
	cases := []struct {
		in   []byte
		want string
	}{
		{nil, ""},
		{[]byte("héllo"), "héllo"},
		{[]byte("\xef\xbb\xbfhi"), "hi"},
		{[]byte("a\x00"), "a"},
		{[]byte{0xFF, 0xFE, 'h', 0, 0xE9, 0, 0, 0}, "hé"},
		{[]byte{0xFE, 0xFF, 0, 'h', 0, 0xE9}, "hé"},
		{[]byte{'<', 0, 'b', 0, '>', 0}, "<b>"},
	}
	for _, c := range cases {
		if got := DecodeClipboardText(c.in); got != c.want {
			t.Fatalf("%v: got %q want %q", c.in, got, c.want)
		}
	}
}
