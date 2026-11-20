package build

import (
	"bufio"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/kratejs/krate/packages/compiler/internal/regionwire"
)

// regionFrame is one frame of a renderer's region response, from either wire
// format (see internal/regionwire).
type regionFrame = regionwire.Frame

// goVaryFrame is the "vary" member of a Go host frame.
type goVaryFrame = regionwire.VaryLists

// nodeRegionsRequest is the JSON body POSTed to the Node sidecar's
// /__krate/regions.
type nodeRegionsRequest struct {
	Route   string
	URL     string
	Method  string
	Headers map[string]string
	Params  map[string]string
	Query   map[string]string
	Regions []map[string]string
}

// marshal writes the request as JSON by hand: it is a fixed shape of strings,
// and reflection over it is a measurable share of a hot request.
func (r nodeRegionsRequest) marshal() []byte {
	b := make([]byte, 0, 512)
	b = append(b, `{"route":`...)
	b = appendJSONString(b, r.Route)
	b = append(b, `,"url":`...)
	b = appendJSONString(b, r.URL)
	b = append(b, `,"method":`...)
	b = appendJSONString(b, r.Method)
	b = append(b, `,"headers":`...)
	b = appendStringMap(b, r.Headers)
	b = append(b, `,"params":`...)
	b = appendStringMap(b, r.Params)
	b = append(b, `,"query":`...)
	b = appendStringMap(b, r.Query)
	b = append(b, `,"regions":`...)
	if r.Regions == nil {
		b = append(b, "null"...)
	} else {
		b = append(b, '[')
		for i, m := range r.Regions {
			if i > 0 {
				b = append(b, ',')
			}
			b = appendStringMap(b, m)
		}
		b = append(b, ']')
	}
	return append(b, '}')
}

// marshalGoRegionsRequest is the JSON body POSTed to the Go host's
// /__krate/gorender, shaped like {"regions":[goRenderRegion...]}.
func marshalGoRegionsRequest(regions []goRenderRegion) []byte {
	b := make([]byte, 0, 256+len(regions)*128)
	b = append(b, `{"regions":`...)
	if regions == nil {
		b = append(b, "null"...)
	} else {
		b = append(b, '[')
		for i, r := range regions {
			if i > 0 {
				b = append(b, ',')
			}
			b = append(b, `{"id":`...)
			b = appendJSONString(b, r.ID)
			b = append(b, `,"name":`...)
			b = appendJSONString(b, r.Name)
			b = append(b, `,"props":`...)
			b = appendJSONString(b, r.Props)
			if r.Request != "" {
				b = append(b, `,"request":`...)
				b = appendJSONString(b, r.Request)
			}
			if r.Revalidate != 0 {
				b = append(b, `,"revalidate":`...)
				b = strconv.AppendInt(b, int64(r.Revalidate), 10)
			}
			b = append(b, '}')
		}
		b = append(b, ']')
	}
	return append(b, '}')
}

func appendStringMap(b []byte, m map[string]string) []byte {
	if m == nil {
		return append(b, "null"...)
	}
	b = append(b, '{')
	first := true
	for k, v := range m {
		if !first {
			b = append(b, ',')
		}
		first = false
		b = appendJSONString(b, k)
		b = append(b, ':')
		b = appendJSONString(b, v)
	}
	return append(b, '}')
}

const hexDigits = "0123456789abcdef"

// appendJSONString appends s as a JSON string. Like encoding/json it replaces
// invalid UTF-8 with U+FFFD and escapes U+2028 and U+2029; unlike it, it leaves
// <, > and & alone.
func appendJSONString(b []byte, s string) []byte {
	b = append(b, '"')
	start := 0
	for i := 0; i < len(s); {
		c := s[i]
		if c < utf8.RuneSelf {
			if c >= 0x20 && c != '"' && c != '\\' {
				i++
				continue
			}
			b = append(b, s[start:i]...)
			switch c {
			case '"', '\\':
				b = append(b, '\\', c)
			case '\n':
				b = append(b, '\\', 'n')
			case '\r':
				b = append(b, '\\', 'r')
			case '\t':
				b = append(b, '\\', 't')
			default:
				b = append(b, '\\', 'u', '0', '0', hexDigits[c>>4], hexDigits[c&0xf])
			}
			i++
			start = i
			continue
		}
		r, size := utf8.DecodeRuneInString(s[i:])
		if r == utf8.RuneError && size == 1 {
			b = append(b, s[start:i]...)
			b = append(b, `\ufffd`...)
			i += size
			start = i
			continue
		}
		if r == '\u2028' || r == '\u2029' {
			b = append(b, s[start:i]...)
			b = append(b, `\u202`...)
			b = append(b, hexDigits[r&0xf])
			i += size
			start = i
			continue
		}
		i += size
	}
	b = append(b, s[start:]...)
	return append(b, '"')
}

// acceptsFrames marks a request as one that understands the binary frames.
func acceptsFrames(req *http.Request) { req.Header.Set("Accept", regionwire.ContentType) }

// isFrameContentType reports whether a response Content-Type is the binary
// frame format.
func isFrameContentType(ct string) bool {
	n := len(regionwire.ContentType)
	if len(ct) < n || !strings.EqualFold(ct[:n], regionwire.ContentType) {
		return false
	}
	return len(ct) == n || ct[n] == ';' || ct[n] == ' '
}

// frameStream reads region frames from a response in whichever format the
// server answered: binary when its Content-Type says so, NDJSON otherwise
// (an older renderer).
type frameStream struct {
	bin *regionwire.Reader
	nd  *bufio.Reader
}

func newFrameStream(resp *http.Response) *frameStream {
	if isFrameContentType(resp.Header.Get("Content-Type")) {
		return &frameStream{bin: regionwire.NewReader(resp.Body)}
	}
	return &frameStream{nd: bufio.NewReaderSize(resp.Body, 256)}
}

// next returns the next frame. ok is false when err is set without a usable
// frame, or for an NDJSON line that is not valid JSON (skip it and call next
// again). err is non-nil once the stream has ended or broken; an NDJSON line
// that was cut before its newline can still come back as ok with that err.
func (s *frameStream) next() (f regionFrame, ok bool, err error) {
	if s.bin != nil {
		f, err = s.bin.Next()
		if errors.Is(err, io.EOF) {
			return f, false, err
		}
		return f, err == nil, err
	}
	line, err := s.nd.ReadBytes('\n')
	if len(line) > 0 && json.Unmarshal(line, &f) == nil {
		return f, true, err
	}
	return f, false, err
}

// goFrameReader returns a reader of the Go host's frames in whichever format
// it answered. The NDJSON reader stops at the first line that is not valid
// JSON, as the json.Decoder it replaces did.
func goFrameReader(resp *http.Response) func() (regionFrame, error) {
	if isFrameContentType(resp.Header.Get("Content-Type")) {
		r := regionwire.NewReader(resp.Body)
		return r.Next
	}
	dec := json.NewDecoder(resp.Body)
	return func() (f regionFrame, err error) {
		err = dec.Decode(&f)
		return f, err
	}
}
