package build

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"html"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Serving segment pages: the shared lookup state plus the responders used by
// the static handler, the dynamic-route handler and the SSR page path.

// segmentIndex caches the manifest's segment map and each route's source
// directory. It reloads when manifest.json changes (a dev rebuild rewrites it).
type segmentIndex struct {
	absOut string

	mu      sync.Mutex
	modTime time.Time
	size    int64
	entries []SegmentEntry
	segDirs map[string]string // normalized route pattern -> segDir
}

func newSegmentIndex(absOut string) *segmentIndex {
	return &segmentIndex{absOut: absOut}
}

func (s *segmentIndex) load() ([]SegmentEntry, map[string]string) {
	path := ResolveManifestPath(s.absOut, "manifest.json")
	fi, err := os.Stat(path)
	s.mu.Lock()
	defer s.mu.Unlock()
	if err != nil {
		s.entries, s.segDirs, s.modTime, s.size = nil, nil, time.Time{}, 0
		return nil, nil
	}
	if s.segDirs != nil && fi.ModTime().Equal(s.modTime) && fi.Size() == s.size {
		return s.entries, s.segDirs
	}
	s.modTime, s.size = fi.ModTime(), fi.Size()
	s.entries, s.segDirs = nil, map[string]string{}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, s.segDirs
	}
	var m Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, s.segDirs
	}
	s.entries = m.Segments
	for _, p := range m.Pages {
		s.segDirs[normalizeRoutePattern(p.Route)] = p.SegDir
	}
	return s.entries, s.segDirs
}

// forURL finds the segment page for a URL that matched no route. pattern, when
// set, is the dynamic route the URL matched but rejected (a closed route with
// an unknown param); the route's own directory then decides.
func (s *segmentIndex) forURL(kind SegmentKind, urlPath, pattern string) (SegmentMatch, bool) {
	entries, segDirs := s.load()
	if len(entries) == 0 {
		return SegmentMatch{}, false
	}
	if pattern != "" {
		if dir, ok := segDirs[normalizeRoutePattern(pattern)]; ok {
			return LookupSegmentPage(entries, kind, urlPath, true, dir)
		}
	}
	return LookupSegmentPage(entries, kind, urlPath, false, "")
}

// forRoute finds the segment page for a request that matched a route.
func (s *segmentIndex) forRoute(kind SegmentKind, urlPath, segDir string) (SegmentMatch, bool) {
	entries, _ := s.load()
	if len(entries) == 0 {
		return SegmentMatch{}, false
	}
	return LookupSegmentPage(entries, kind, urlPath, true, segDir)
}

// readPage reads a matched segment page, refusing paths outside the output dir.
func (s *segmentIndex) readPage(m SegmentMatch) ([]byte, bool) {
	clean := filepath.Clean(filepath.FromSlash(m.File))
	if filepath.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return nil, false
	}
	data, err := os.ReadFile(filepath.Join(s.absOut, clean))
	if err != nil {
		return nil, false
	}
	return data, true
}

// serverError is what an error page receives as its error prop for a failed
// request.
type serverError struct {
	Name    string `json:"name"`
	Message string `json:"message"`
	Digest  string `json:"digest"`
	Stack   string `json:"stack,omitempty"`
}

// errorDigest is the short stable id that ties the sanitized error a visitor
// sees to the full error in the server log. The renderer computes the same
// hash (ssr-core.ts errorDigest) so both sides agree.
func errorDigest(message, stack string) string {
	sum := sha256.Sum256([]byte(message + "\n" + stack))
	return hex.EncodeToString(sum[:])[:10]
}

// newServerError builds the error prop. Outside dev the message is replaced by
// a generic one and the stack is dropped; the digest is kept.
func newServerError(message, stack, digest string, dev bool) serverError {
	if digest == "" {
		digest = errorDigest(message, stack)
	}
	if !dev {
		return serverError{Name: "Error", Message: "Internal Server Error", Digest: digest}
	}
	if message == "" {
		message = "Internal Server Error"
	}
	return serverError{Name: "Error", Message: message, Digest: digest, Stack: stack}
}

// fillServerError turns a built error page into the response for one failed
// request: the placeholders the static variant carries are replaced with the
// escaped error, and the error is injected as a JSON data block for error
// components that read it on the client (a data block is not script, so a
// strict CSP does not touch it).
func fillServerError(page string, e serverError) string {
	for sentinel, val := range map[string]string{
		errSentinelName:    e.Name,
		errSentinelMessage: e.Message,
		errSentinelDigest:  e.Digest,
		errSentinelStack:   e.Stack,
	} {
		page = strings.ReplaceAll(page, sentinel, html.EscapeString(val))
	}
	data, err := json.Marshal(e)
	if err != nil {
		return page
	}
	block := `<script type="application/json" id="__krate_error">` + escapeJSONForScript(string(data)) + `</script>`
	if strings.Contains(page, "</head>") {
		return strings.Replace(page, "</head>", block+"</head>", 1)
	}
	return block + page
}

// respondSegment writes a segment page with the given status. It returns false
// when no page matched, so the caller can fall back to its default response.
func respondSegment(w http.ResponseWriter, s *segmentIndex, m SegmentMatch, status int, srvErr *serverError) bool {
	data, ok := s.readPage(m)
	if !ok {
		return false
	}
	page := string(data)
	if len(m.Params) > 0 {
		page = applyDynamicRouteParams(page, m.Params)
	}
	if srvErr != nil {
		page = fillServerError(page, *srvErr)
	}
	h := w.Header()
	h.Set("Content-Type", "text/html; charset=utf-8")
	h.Del("Content-Length")
	h.Del("Content-Encoding")
	h.Del("Etag")
	h.Del("Last-Modified")
	h.Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_, _ = w.Write([]byte(page))
	return true
}
