package caps

import (
	"bytes"
	"fmt"
	"strconv"
	"strings"
	"unicode/utf16"
)

// RichClipboard is implemented by Apps that can put several representations
// of one value on the clipboard (text with HTML and RTF, as a browser does)
// and read or write a format by name.
//
// Format names are MIME types ("text/plain", "text/html", "text/rtf",
// "image/png"). Each platform maps them to its own names: UTIs on macOS
// ("public.html"), registered formats on Windows ("HTML Format"). A name the
// platform does not know is passed through as a native format name.
type RichClipboard interface {
	// WriteRich replaces the clipboard with every non-empty field of d, all
	// on one clipboard entry.
	WriteRich(d ClipboardData) error
	// ReadHTML returns the HTML fragment on the clipboard, or "".
	ReadHTML() (string, error)
	// ReadRTF returns the RTF document on the clipboard, or "".
	ReadRTF() (string, error)
	// ClipboardFormats lists the formats currently on the clipboard.
	ClipboardFormats() ([]string, error)
	// ReadFormat returns the bytes of one format, or nil.
	ReadFormat(name string) ([]byte, error)
	// WriteFormat replaces the clipboard with a single format.
	WriteFormat(name string, data []byte) error
}

// ClipboardData is a set of representations of one clipboard value.
type ClipboardData struct {
	Text string
	HTML string
	RTF  string
}

// Empty reports whether no representation is set.
func (d ClipboardData) Empty() bool { return d.Text == "" && d.HTML == "" && d.RTF == "" }

// Common clipboard format names.
const (
	FormatText = "text/plain"
	FormatHTML = "text/html"
	FormatRTF  = "text/rtf"
	FormatPNG  = "image/png"
	FormatURIs = "text/uri-list"
)

// formatNames maps a MIME type to the macOS UTI and the Windows registered
// format name.
var formatNames = []struct{ mime, uti, win string }{
	{FormatText, "public.utf8-plain-text", ""},
	{FormatHTML, "public.html", "HTML Format"},
	{FormatRTF, "public.rtf", "Rich Text Format"},
	{FormatPNG, "public.png", "PNG"},
	{FormatURIs, "public.file-url", ""},
}

// UTIForFormat returns the macOS UTI for a MIME type, or name unchanged.
func UTIForFormat(name string) string {
	for _, f := range formatNames {
		if f.mime == name {
			return f.uti
		}
	}
	return name
}

// FormatForUTI returns the MIME type for a UTI, or the UTI unchanged.
func FormatForUTI(uti string) string {
	if uti == "public.plain-text" {
		return FormatText
	}
	for _, f := range formatNames {
		if f.uti == uti {
			return f.mime
		}
	}
	return uti
}

// WinFormatForFormat returns the Windows registered format name for a MIME
// type, or name unchanged. It is "" for text/plain, which uses CF_UNICODETEXT.
func WinFormatForFormat(name string) string {
	for _, f := range formatNames {
		if f.mime == name {
			return f.win
		}
	}
	return name
}

// FormatForWinFormat returns the MIME type for a registered Windows format
// name, or the name unchanged.
func FormatForWinFormat(name string) string {
	for _, f := range formatNames {
		if f.win != "" && strings.EqualFold(f.win, name) {
			return f.mime
		}
	}
	return name
}

const (
	cfHTMLStart = "<!--StartFragment-->"
	cfHTMLEnd   = "<!--EndFragment-->"
)

// EncodeCFHTML wraps an HTML fragment (or a whole document) in the Windows
// "HTML Format" clipboard envelope: a header of byte offsets, then the
// document with StartFragment and EndFragment markers. The result is UTF-8
// and is not NUL terminated; the caller adds the terminator.
func EncodeCFHTML(htmlText, sourceURL string) []byte {
	doc := htmlText
	if !strings.Contains(doc, cfHTMLStart) {
		if i := indexFold(doc, "<body"); i >= 0 {
			// A whole document: put the markers inside <body>.
			j := strings.IndexByte(doc[i:], '>')
			k := lastIndexFold(doc, "</body")
			if j >= 0 && k > i+j {
				doc = doc[:i+j+1] + cfHTMLStart + doc[i+j+1:k] + cfHTMLEnd + doc[k:]
			} else {
				doc = "<html><body>" + cfHTMLStart + doc + cfHTMLEnd + "</body></html>"
			}
		} else {
			doc = "<html><body>\r\n" + cfHTMLStart + doc + cfHTMLEnd + "\r\n</body></html>"
		}
	}
	hdr := func(startHTML, endHTML, startFrag, endFrag int) string {
		s := "Version:0.9\r\n" +
			fmt.Sprintf("StartHTML:%010d\r\n", startHTML) +
			fmt.Sprintf("EndHTML:%010d\r\n", endHTML) +
			fmt.Sprintf("StartFragment:%010d\r\n", startFrag) +
			fmt.Sprintf("EndFragment:%010d\r\n", endFrag)
		if sourceURL != "" {
			s += "SourceURL:" + sourceURL + "\r\n"
		}
		return s
	}
	// The header length does not depend on the offsets (fixed width).
	n := len(hdr(0, 0, 0, 0))
	frag := strings.Index(doc, cfHTMLStart) + len(cfHTMLStart)
	fragEnd := strings.Index(doc, cfHTMLEnd)
	if fragEnd < frag {
		fragEnd = frag
	}
	var b bytes.Buffer
	b.WriteString(hdr(n, n+len(doc), n+frag, n+fragEnd))
	b.WriteString(doc)
	return b.Bytes()
}

// DecodeCFHTML extracts the HTML fragment from "HTML Format" clipboard data.
// It trusts the header offsets when they are in range and otherwise falls
// back to the fragment markers, then to the whole document, because some
// producers write wrong offsets. A trailing NUL is ignored.
func DecodeCFHTML(data []byte) string {
	data = bytes.TrimRight(data, "\x00")
	hdr := map[string]int{}
	pos := 0
	for pos < len(data) {
		end := bytes.IndexByte(data[pos:], '\n')
		var line []byte
		if end < 0 {
			line = data[pos:]
			pos = len(data)
		} else {
			line = data[pos : pos+end]
			pos += end + 1
		}
		line = bytes.TrimRight(line, "\r")
		k, v, ok := strings.Cut(string(line), ":")
		if !ok || strings.HasPrefix(k, "<") {
			break
		}
		if n, err := strconv.Atoi(strings.TrimSpace(v)); err == nil {
			hdr[k] = n
		}
	}
	inRange := func(a, b int) bool { return a >= 0 && b >= a && b <= len(data) }
	if s, e := hdr["StartFragment"], hdr["EndFragment"]; inRange(s, e) && e > 0 {
		return string(data[s:e])
	}
	s := string(data)
	if i := strings.Index(s, cfHTMLStart); i >= 0 {
		rest := s[i+len(cfHTMLStart):]
		if j := strings.Index(rest, cfHTMLEnd); j >= 0 {
			return rest[:j]
		}
		return rest
	}
	if a, b := hdr["StartHTML"], hdr["EndHTML"]; inRange(a, b) && b > 0 {
		return string(data[a:b])
	}
	if i := strings.Index(s, "<"); i >= 0 {
		return s[i:]
	}
	return ""
}

func indexFold(s, sub string) int {
	return strings.Index(strings.ToLower(s), strings.ToLower(sub))
}

func lastIndexFold(s, sub string) int {
	return strings.LastIndex(strings.ToLower(s), strings.ToLower(sub))
}

// DecodeClipboardText turns the bytes of a text clipboard format into a
// string. Producers differ: UTF-8 (with or without a BOM), or UTF-16 with a
// BOM or without one (recognized by the NUL bytes). A trailing NUL is dropped.
func DecodeClipboardText(b []byte) string {
	switch {
	case len(b) == 0:
		return ""
	case len(b) >= 2 && b[0] == 0xFF && b[1] == 0xFE:
		return decodeUTF16(b[2:], false)
	case len(b) >= 2 && b[0] == 0xFE && b[1] == 0xFF:
		return decodeUTF16(b[2:], true)
	case len(b) >= 3 && b[0] == 0xEF && b[1] == 0xBB && b[2] == 0xBF:
		b = b[3:]
	case len(b) >= 4 && len(b)%2 == 0 && b[1] == 0 && b[3] == 0 && b[0] != 0:
		return decodeUTF16(b, false)
	}
	return string(bytes.TrimRight(b, "\x00"))
}

func decodeUTF16(b []byte, bigEndian bool) string {
	u := make([]uint16, 0, len(b)/2)
	for i := 0; i+1 < len(b); i += 2 {
		if bigEndian {
			u = append(u, uint16(b[i])<<8|uint16(b[i+1]))
		} else {
			u = append(u, uint16(b[i+1])<<8|uint16(b[i]))
		}
	}
	for len(u) > 0 && u[len(u)-1] == 0 {
		u = u[:len(u)-1]
	}
	return string(utf16.Decode(u))
}
