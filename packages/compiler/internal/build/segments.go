package build

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/kratejs/krate/packages/compiler/internal/diag"
	"github.com/kratejs/krate/packages/compiler/internal/fsutil"
)

// Per-segment not-found and error pages.
//
// In any directory under the pages root:
//
//	404.tsx or not-found.tsx   the segment's not-found page (status 404)
//	error.tsx or 500.tsx       the segment's error page (status 500)
//	forbidden.tsx or 403.tsx   the page for forbidden() (status 403)
//	unauthorized.tsx or 401.tsx   the page for unauthorized() (status 401)
//
// They are not routes. Each is built like a page inside its directory's
// layouts and written to <dir>/404.html or <dir>/500.html in the output; the
// manifest records them in Segments so the servers can pick the nearest one
// for a request (LookupSegmentPage). Route groups (name) do not appear in the
// URL, so a segment page inside a group is only chosen for routes that live in
// the group.

// SegmentKind says which role a segment page plays.
type SegmentKind string

const (
	SegmentNotFound     SegmentKind = "notFound"
	SegmentError        SegmentKind = "error"
	SegmentForbidden    SegmentKind = "forbidden"
	SegmentUnauthorized SegmentKind = "unauthorized"
)

// segmentBaseKinds maps a file base name (no extension) to its role.
var segmentBaseKinds = map[string]SegmentKind{
	"404":       SegmentNotFound,
	"not-found": SegmentNotFound,
	"500":       SegmentError,
	"error":     SegmentError,

	"403":          SegmentForbidden,
	"forbidden":    SegmentForbidden,
	"401":          SegmentUnauthorized,
	"unauthorized": SegmentUnauthorized,
}

// segmentKindOfBase returns the role of a page base name.
func segmentKindOfBase(base string) (SegmentKind, bool) {
	k, ok := segmentBaseKinds[base]
	return k, ok
}

// segmentKindOfFile returns the role of a page file path.
func segmentKindOfFile(path string) (SegmentKind, bool) {
	return segmentKindOfBase(strings.TrimSuffix(filepath.Base(path), filepath.Ext(path)))
}

// isSegmentPageBase reports whether a page base name is a segment page.
func isSegmentPageBase(base string) bool {
	_, ok := segmentKindOfBase(base)
	return ok
}

// htmlName is the output file name of a segment page.
func (k SegmentKind) htmlName() string {
	return strconv.Itoa(k.status()) + ".html"
}

// status is the HTTP status the kind answers with.
func (k SegmentKind) status() int {
	switch k {
	case SegmentError:
		return http.StatusInternalServerError
	case SegmentForbidden:
		return http.StatusForbidden
	case SegmentUnauthorized:
		return http.StatusUnauthorized
	}
	return http.StatusNotFound
}

// assetDirName is the directory (next to the segment HTML) that holds the
// page's hydration script, so it never collides with the index page's.
func (k SegmentKind) assetDirName() string {
	return "__" + strconv.Itoa(k.status())
}

// segmentDirKey returns the directory of a segment page relative to pagesDir,
// slash-separated with route groups kept ("" for the root).
func segmentDirKey(page, pagesDir string) string {
	rel, err := filepath.Rel(pagesDir, filepath.Dir(page))
	if err != nil || rel == "." || strings.HasPrefix(rel, "..") {
		return ""
	}
	return filepath.ToSlash(rel)
}

// segmentURLDir is the URL pattern of a directory key: route groups removed.
func segmentURLDir(key string) string {
	if key == "" {
		return ""
	}
	var out []string
	for _, seg := range strings.Split(key, "/") {
		if !isRouteGroup(seg) {
			out = append(out, seg)
		}
	}
	return strings.Join(out, "/")
}

// segmentHasGroup reports whether a directory key lives in a route group.
func segmentHasGroup(key string) bool {
	for _, seg := range strings.Split(key, "/") {
		if isRouteGroup(seg) {
			return true
		}
	}
	return false
}

// segmentHTMLRel is the output path (relative to the output dir, slash
// separated) of a segment page: "404.html", "blog/404.html". Route groups stay
// in the path so two groups never write the same file.
func segmentHTMLRel(page, pagesDir string) string {
	kind, _ := segmentKindOfFile(page)
	key := segmentDirKey(page, pagesDir)
	if key == "" {
		return kind.htmlName()
	}
	return key + "/" + kind.htmlName()
}

// segmentAssetOut is the OutName of a segment page: the directory its
// hydration script and chunks are written to.
func segmentAssetOut(page, pagesDir string) string {
	kind, _ := segmentKindOfFile(page)
	key := segmentDirKey(page, pagesDir)
	if key == "" {
		return kind.assetDirName()
	}
	return key + "/" + kind.assetDirName()
}

// SegmentEntry is one directory's segment pages, as recorded in the manifest.
type SegmentEntry struct {
	// Key is the directory relative to the pages root with route groups kept
	// ("" for the root, "(shop)/blog" inside a group).
	Key string `json:"key"`
	// Dir is the URL pattern of the directory: groups removed, dynamic
	// segments as [x] / [...x] / [[...x]] ("" for the root).
	Dir string `json:"dir"`
	// Group is true when the directory lives inside a route group.
	Group bool `json:"group,omitempty"`
	// NotFound and Error are the output paths (relative to the output dir) of
	// the directory's pages; empty when the directory has none.
	NotFound     string `json:"notFound,omitempty"`
	Error        string `json:"error,omitempty"`
	Forbidden    string `json:"forbidden,omitempty"`
	Unauthorized string `json:"unauthorized,omitempty"`
}

// page returns the output path for a kind.
func (e SegmentEntry) page(kind SegmentKind) string {
	switch kind {
	case SegmentError:
		return e.Error
	case SegmentForbidden:
		return e.Forbidden
	case SegmentUnauthorized:
		return e.Unauthorized
	}
	return e.NotFound
}

// setPage records the output path of a kind.
func (e *SegmentEntry) setPage(kind SegmentKind, rel string) {
	switch kind {
	case SegmentError:
		e.Error = rel
	case SegmentForbidden:
		e.Forbidden = rel
	case SegmentUnauthorized:
		e.Unauthorized = rel
	default:
		e.NotFound = rel
	}
}

// pages lists the output paths of every page the entry holds.
func (e SegmentEntry) pages() []string {
	var out []string
	for _, p := range []string{e.NotFound, e.Error, e.Forbidden, e.Unauthorized} {
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

// SegmentMatch is the result of LookupSegmentPage.
type SegmentMatch struct {
	// File is the page's path relative to the output dir ("blog/404.html").
	File string
	// Dir is the matched entry's directory key.
	Key string
	// Params holds the values of dynamic segments in the entry's directory
	// when it was matched against a URL ("" for the other lookup mode).
	Params map[string]string
}

// LookupSegmentPage returns the nearest segment page of a kind.
//
// For a request that matched a route, segDir is the directory of the route's
// source file relative to the pages root (groups kept, "" for the root) and
// matched is true: the lookup walks up those directories, so a page's group
// scopes its pages like it scopes layouts. For a request that matched no route
// (matched false) the lookup uses urlPath: the entry whose URL directory is the
// longest prefix of the path wins (static beats [param] beats catch-all), and
// entries inside route groups are skipped because no route tells which group
// the URL belongs to. The root entry is the global fallback in both modes.
//
// The function is pure data in, data out so the serverless handler can port it.
func LookupSegmentPage(entries []SegmentEntry, kind SegmentKind, urlPath string, matched bool, segDir string) (SegmentMatch, bool) {
	if matched {
		byKey := make(map[string]SegmentEntry, len(entries))
		for _, e := range entries {
			byKey[e.Key] = e
		}
		dir := segDir
		for {
			if e, ok := byKey[dir]; ok && e.page(kind) != "" {
				return SegmentMatch{File: e.page(kind), Key: e.Key}, true
			}
			if dir == "" {
				return SegmentMatch{}, false
			}
			if i := strings.LastIndex(dir, "/"); i >= 0 {
				dir = dir[:i]
			} else {
				dir = ""
			}
		}
	}
	var best *SegmentEntry
	var bestParams map[string]string
	for i := range entries {
		e := &entries[i]
		if e.page(kind) == "" || e.Group {
			continue
		}
		params, ok := matchRoutePrefix(urlPath, e.Dir)
		if !ok {
			continue
		}
		if best == nil || compareRouteSpecificity(e.Dir, best.Dir) < 0 {
			best, bestParams = e, params
		}
	}
	if best == nil {
		return SegmentMatch{}, false
	}
	return SegmentMatch{File: best.page(kind), Key: best.Key, Params: bestParams}, true
}

// matchRoutePrefix reports whether pattern matches the leading segments of
// urlPath. The root pattern ("") matches every path. A catch-all as the last
// segment consumes the rest of the path.
func matchRoutePrefix(urlPath, pattern string) (map[string]string, bool) {
	segs := parseRoutePattern(pattern)
	if len(segs) == 0 {
		return map[string]string{}, true
	}
	trimmed := strings.Trim(urlPath, "/")
	var parts []string
	if trimmed != "" {
		parts = strings.Split(trimmed, "/")
	}
	last := segs[len(segs)-1]
	if last.kind == segCatchAll || last.kind == segOptionalCatchAll {
		return matchRoutePattern(urlPath, pattern)
	}
	if len(parts) < len(segs) {
		return nil, false
	}
	return matchRoutePattern("/"+strings.Join(parts[:len(segs)], "/"), pattern)
}

// segmentFile is one discovered segment page source.
type segmentFile struct {
	Path string
	Kind SegmentKind
	Key  string
}

// collectSegmentFiles walks pagesDir for segment page sources (including those
// in places findPages skips, such as slots) in a stable order.
func collectSegmentFiles(pagesDir string) []segmentFile {
	var out []segmentFile
	_ = fsutil.WalkExt(pagesDir, pageExts, nil, func(path string, _ os.FileInfo) error {
		if kind, ok := segmentKindOfFile(path); ok {
			out = append(out, segmentFile{Path: path, Kind: kind, Key: segmentDirKey(path, pagesDir)})
		}
		return nil
	})
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out
}

// validateSegmentPages reports misplaced or conflicting segment pages: two
// files for one role in a directory (404.tsx and not-found.tsx), two
// directories that resolve to the same URL directory through route groups, and
// pages inside @slot or intercepting directories.
func validateSegmentPages(pagesDir string) error {
	files := collectSegmentFiles(pagesDir)
	type slot struct {
		kind   SegmentKind
		urlDir string
	}
	byRole := map[slot][]string{}
	for _, f := range files {
		rel, err := filepath.Rel(pagesDir, f.Path)
		if err != nil {
			continue
		}
		relSlash := filepath.ToSlash(rel)
		base := filepath.Base(f.Path)
		if pathHasSlot(relSlash) {
			return diag.Diagnostic{
				File:    f.Path,
				Line:    1,
				Col:     1,
				Message: base + " is a segment page and cannot live inside a parallel-route slot",
				Hint:    "move it next to the slot's parent directory; a slot renders inside its parent's layout, which already has the parent's " + f.Kind.label() + " page",
			}.WithCode(diag.CodeSegmentMisplaced)
		}
		for _, seg := range strings.Split(filepath.ToSlash(filepath.Dir(rel)), "/") {
			if _, _, ok := interceptLevels(seg); ok {
				return diag.Diagnostic{
					File:    f.Path,
					Line:    1,
					Col:     1,
					Message: base + " is a segment page and cannot live inside an intercepting route directory (" + seg + ")",
					Hint:    "move it to the parent route directory",
				}.WithCode(diag.CodeSegmentMisplaced)
			}
		}
		k := slot{f.Kind, segmentURLDir(f.Key)}
		byRole[k] = append(byRole[k], relSlash)
	}
	var errs []string
	for k, list := range byRole {
		if len(list) > 1 {
			sort.Strings(list)
			errs = append(errs, fmt.Sprintf("%s all define the %s page of %s", strings.Join(list, ", "), k.kind.label(), segmentDirLabel(k.urlDir)))
		}
	}
	if len(errs) > 0 {
		sort.Strings(errs)
		return diag.Diagnostic{
			File:    pagesDir,
			Message: "conflicting segment pages: " + strings.Join(errs, "; "),
			Hint:    "keep one file per role in a directory (404.tsx or not-found.tsx, error.tsx or 500.tsx, 403.tsx or forbidden.tsx, 401.tsx or unauthorized.tsx); route groups do not appear in the URL, so groups cannot hold separate pages for the same directory",
		}.WithCode(diag.CodeSegmentConflict)
	}
	return nil
}

func (k SegmentKind) label() string {
	switch k {
	case SegmentError:
		return "error"
	case SegmentForbidden:
		return "forbidden"
	case SegmentUnauthorized:
		return "unauthorized"
	}
	return "not-found"
}

func segmentDirLabel(urlDir string) string {
	if urlDir == "" {
		return "the site root"
	}
	return "/" + urlDir
}

// segmentEntries builds the manifest's segment map from the pages that were
// built. The result is sorted by key so manifest.json is deterministic.
func segmentEntries(pages []string, pagesDir string) []SegmentEntry {
	byKey := map[string]*SegmentEntry{}
	for _, p := range pages {
		kind, ok := segmentKindOfFile(p)
		if !ok || pathHasSlot(relOrSelf(pagesDir, p)) {
			continue
		}
		key := segmentDirKey(p, pagesDir)
		e := byKey[key]
		if e == nil {
			e = &SegmentEntry{Key: key, Dir: segmentURLDir(key), Group: segmentHasGroup(key)}
			byKey[key] = e
		}
		e.setPage(kind, segmentHTMLRel(p, pagesDir))
	}
	out := make([]SegmentEntry, 0, len(byKey))
	for _, e := range byKey {
		out = append(out, *e)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out
}

func relOrSelf(base, p string) string {
	if rel, err := filepath.Rel(base, p); err == nil {
		return rel
	}
	return p
}

// routeSegDir is the directory of a page source (project-relative or absolute)
// relative to the pages root, groups kept: the segDir LookupSegmentPage walks.
func routeSegDir(root, pagesDir, source string) string {
	abs := source
	if !filepath.IsAbs(abs) {
		abs = filepath.Join(root, filepath.FromSlash(source))
	}
	return segmentDirKey(abs, pagesDir)
}
