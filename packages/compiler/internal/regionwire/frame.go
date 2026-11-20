// Package regionwire is the binary frame format that the Go server and its SSR
// renderers (the Node sidecar and the generated Go host) use for region
// responses. HTML travels as raw bytes: there is no JSON string escaping on
// either side.
//
// Negotiation: the client sends "Accept: application/vnd.krate.frames". A
// server that understands it answers with that Content-Type and the frames
// below; any other server answers NDJSON as before, and the client picks the
// parser from the response Content-Type. Requests stay JSON.
//
// Wire format (version 1, bytes are big endian, uvarint is encoding/binary's):
//
//	stream = frame*
//	frame  = u32 n, body[n]
//	body   = u8 type, u8 flags, uvarint status, uvarint denied, uvarint count,
//	         str id, str kind, str cacheStatus, str redirect, str error,
//	         str digest, str stack, str title, str head, str html,
//	         [vary]               (present when flags has hasVary)
//	str    = uvarint len, bytes[len]
//	vary   = list headers, list cookies, list query, list params
//	list   = uvarint count, str[count]
//	type   = 1 region, 2 skip, 3 error, 4 end
//	flags  = 0x01 notFound, 0x02 noStore, 0x04 errorCaught, 0x08 fatal,
//	         0x10 hasVary, 0x20 varyFull
//
// A frame that stops before its n bytes, a body that does not parse exactly,
// or an unknown type is an error and ends the stream: the reader cannot
// resynchronise, so the caller treats it like a cut connection.
//
// This file depends on the standard library only: the Go host generator copies
// it into the generated render package.
package regionwire

import (
	"encoding/binary"
	"errors"
	"strings"
	"unicode/utf8"
)

// ContentType is the response Content-Type, and the Accept value that asks for it.
const ContentType = "application/vnd.krate.frames"

// Frame type codes.
const (
	typeRegion = 1
	typeSkip   = 2
	typeError  = 3
	typeEnd    = 4
)

const (
	flagNotFound    = 0x01
	flagNoStore     = 0x02
	flagErrorCaught = 0x04
	flagFatal       = 0x08
	flagHasVary     = 0x10
	flagVaryFull    = 0x20
)

// MaxFrame bounds one frame body, so a corrupt length cannot ask for gigabytes.
const MaxFrame = 1 << 30

// ErrCorrupt reports a frame that does not follow the format.
var ErrCorrupt = errors.New("regionwire: corrupt frame")

// VaryLists is what a Go-host render read of the request (see the host's Vary).
type VaryLists struct {
	Full    bool     `json:"full"`
	Headers []string `json:"headers"`
	Cookies []string `json:"cookies"`
	Query   []string `json:"query"`
	Params  []string `json:"params"`
}

// Frame is one region response. The JSON tags describe the NDJSON form that
// servers without the binary format still send.
type Frame struct {
	Type        string `json:"type"`
	ID          string `json:"id,omitempty"`
	Kind        string `json:"kind,omitempty"`
	Status      int    `json:"status,omitempty"`
	HTML        string `json:"html,omitempty"`
	Error       string `json:"error,omitempty"`
	Count       int    `json:"count,omitempty"`
	CacheStatus string `json:"cacheStatus,omitempty"`
	NotFound    bool   `json:"notFound,omitempty"`
	Redirect    string `json:"redirect,omitempty"`
	// Denied is 401 or 403 when the render called unauthorized() or forbidden().
	Denied int    `json:"denied,omitempty"`
	Title  string `json:"title,omitempty"`
	// Head is the <Head> tags (meta, link, script) the region rendered, to be
	// merged into the document head (see headmerge.go).
	Head string `json:"head,omitempty"`
	// NoStore is set when the render read request state (headers/cookies) or
	// otherwise opted out of caching. The page response must not be public.
	NoStore bool `json:"noStore,omitempty"`
	// ErrorCaught is set when an error boundary rendered its fallback in the
	// region: the response is a 500 that must never be cached.
	ErrorCaught bool `json:"errorCaught,omitempty"`
	// Fatal marks a failed render that has no usable baked content (an async
	// server component): the response must be an error, not the shell.
	Fatal bool `json:"fatal,omitempty"`
	// Digest and Stack describe a failed render: the renderer logs the full
	// error with the digest, and the server shows visitors only the digest.
	Digest string `json:"digest,omitempty"`
	Stack  string `json:"stack,omitempty"`
	// Vary is set by the Go host only.
	Vary *VaryLists `json:"vary,omitempty"`
}

func typeCode(t string) int {
	switch t {
	case "region":
		return typeRegion
	case "skip":
		return typeSkip
	case "error":
		return typeError
	case "end":
		return typeEnd
	}
	return 0
}

func typeName(c byte) string {
	switch c {
	case typeRegion:
		return "region"
	case typeSkip:
		return "skip"
	case typeError:
		return "error"
	case typeEnd:
		return "end"
	}
	return ""
}

// AppendFrame appends the encoded frame to dst. It fails only for a frame type
// the format does not know or a body over MaxFrame. Invalid UTF-8 in a string
// becomes U+FFFD per bad byte, as encoding/json does.
func AppendFrame(dst []byte, f *Frame) ([]byte, error) {
	code := typeCode(f.Type)
	if code == 0 {
		return dst, errors.New("regionwire: unknown frame type " + f.Type)
	}
	var flags byte
	if f.NotFound {
		flags |= flagNotFound
	}
	if f.NoStore {
		flags |= flagNoStore
	}
	if f.ErrorCaught {
		flags |= flagErrorCaught
	}
	if f.Fatal {
		flags |= flagFatal
	}
	if f.Vary != nil {
		flags |= flagHasVary
		if f.Vary.Full {
			flags |= flagVaryFull
		}
	}
	start := len(dst)
	dst = append(dst, 0, 0, 0, 0, byte(code), flags)
	dst = binary.AppendUvarint(dst, uint64(max(f.Status, 0)))
	dst = binary.AppendUvarint(dst, uint64(max(f.Denied, 0)))
	dst = binary.AppendUvarint(dst, uint64(max(f.Count, 0)))
	for _, s := range [...]string{f.ID, f.Kind, f.CacheStatus, f.Redirect, f.Error, f.Digest, f.Stack, f.Title, f.Head, f.HTML} {
		dst = appendStr(dst, s)
	}
	if f.Vary != nil {
		for _, l := range [...][]string{f.Vary.Headers, f.Vary.Cookies, f.Vary.Query, f.Vary.Params} {
			dst = binary.AppendUvarint(dst, uint64(len(l)))
			for _, s := range l {
				dst = appendStr(dst, s)
			}
		}
	}
	n := len(dst) - start - 4
	if n > MaxFrame {
		return dst[:start], errors.New("regionwire: frame too large")
	}
	binary.BigEndian.PutUint32(dst[start:], uint32(n))
	return dst, nil
}

func appendStr(dst []byte, s string) []byte {
	if !utf8.ValidString(s) {
		s = strings.Map(func(r rune) rune { return r }, s)
	}
	dst = binary.AppendUvarint(dst, uint64(len(s)))
	return append(dst, s...)
}
