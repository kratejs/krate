package build

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"sync"
)

// An island entry is named from its source (islandEntryName) because pages
// and server bundles name it before it is bundled. Its content still changes
// when a component it imports changes, or when a shared chunk it imports gets
// a new content-hashed name, so the stable name was served revalidated (not
// immutable) and relied on every cache honoring that. A CDN that kept it for
// hours paired an old entry with the new build's other islands, and a module
// such as a React context existed twice on one page (Genesis: "`Tooltip` must
// be used within `TooltipProvider`" after a deploy).
//
// A production build therefore renames each entry to island_<id>.<hash>.js
// from its bundled content once everything is written, gives a content-hashed
// file that names a renamed one (a page script importing an entry) a new hash
// too, and rewrites the files naming them, so every name the HTML points at
// changes with what it serves. nameOutputsByContent does it in one pass: one
// read of each text output, names computed in dependency order (a file's hash
// covers the final names of the files it references), one write of each file
// that changes.

// islandEntryFileRe matches an entry as islandEntryName names it.
var islandEntryFileRe = regexp.MustCompile(`^` + islandEntryPrefix + `[0-9a-f]{12}\.js$`)

// buildHashedNameRe splits a name the build content-hashed with hashContent:
// stem, hash, extension.
var buildHashedNameRe = regexp.MustCompile(`^(.+)\.([0-9a-z]{6})\.(js|mjs|css)$`)

// renameTextExts are the outputs that can name an island entry or a page
// script.
var renameTextExts = map[string]bool{".html": true, ".htm": true, ".js": true, ".mjs": true, ".cjs": true, ".json": true, ".map": true}

// selfName stands for a file's own name in the content its name is hashed
// from.
const selfName = "\x00self"

// buildIDPlaceholder stands for the build id in the documents and server
// outputs until nameOutputsByContent replaces it.
const buildIDPlaceholder = "__krate_build_id__"

// outputNames is what nameOutputsByContent did.
type outputNames struct {
	// renames maps an old base name to the new one.
	renames map[string]string
	// hashed lists the content-hashed files under the final names, as
	// collectHashedAssets lists them.
	hashed []string
	// buildID is the digest of hashed written for buildIDPlaceholder.
	buildID string
	// read and written count the files read and written.
	read, written int
	replacer      *strings.Replacer
}

// nameOutputsByContent names the island entries in outDir/chunks by their
// content, renames the content-hashed files that name them (transitively),
// writes the build id into the files holding buildIDPlaceholder, and
// rewrites every file naming a renamed one. Public files are left alone and
// are never content-hashed (see collectHashedAssets).
//
// The build id identifies the build's documents: a digest of the names of the
// files it content-hashed, so two builds that serve the same files share it. A
// document of another build carries a different one, and the SPA router loads
// it with a full navigation (as Next.js does when its build id changes):
// swapping it in would load that build's islands and page scripts next to the
// modules already running (a layout island kept mounted), with a second copy
// of each module they share, such as a React context.
func nameOutputsByContent(outDir string, public map[string]bool) (*outputNames, error) {
	isPublic := func(rel string) bool { return public[rel] || public[strings.TrimSuffix(rel, ".map")] }
	// One walk lists every output.
	type output struct {
		path, rel   string
		text, krate bool
	}
	var all []output
	err := filepath.WalkDir(outDir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(outDir, p)
		if err != nil {
			return nil
		}
		rel = filepath.ToSlash(rel)
		if isPublic(rel) {
			return nil
		}
		all = append(all, output{path: p, rel: rel, text: renameTextExts[strings.ToLower(filepath.Ext(p))], krate: isUnderKrateDir(outDir, p)})
		return nil
	})
	if err != nil {
		return nil, err
	}

	// The files that can be renamed, by base name: the island entries and the
	// files the build content-hashed.
	chunks := filepath.Join(outDir, "chunks")
	candidates := map[string][]string{}
	entry := map[string]bool{}
	for _, o := range all {
		base := filepath.Base(o.path)
		switch {
		case filepath.Dir(o.path) == chunks && islandEntryFileRe.MatchString(base):
			entry[o.path] = true
			candidates[base] = append(candidates[base], o.path)
		case !o.krate && buildHashedNameRe.MatchString(base):
			candidates[base] = append(candidates[base], o.path)
		}
	}

	// One read of each text output: the candidates it names.
	type scanned struct {
		path, rel   string
		krate       bool
		data        []byte
		refs        []string
		occ         []nameAt
		placeholder bool
	}
	var texts []*scanned
	for _, o := range all {
		if o.text {
			texts = append(texts, &scanned{path: o.path, rel: o.rel, krate: o.krate})
		}
	}
	isCandidate := func(name string) bool { _, ok := candidates[name]; return ok }
	if err := parallel(len(texts), func(i int) error {
		s := texts[i]
		data, err := os.ReadFile(s.path)
		if err != nil {
			return err
		}
		self := filepath.Base(s.path)
		s.occ = namesIn(data, isCandidate)
		seen := map[string]bool{}
		for _, o := range s.occ {
			if o.name != self && !seen[o.name] {
				seen[o.name] = true
				s.refs = append(s.refs, o.name)
			}
		}
		s.occ = appendPlaceholders(s.occ, data)
		s.placeholder = hasPlaceholder(s.occ)
		if len(s.refs) > 0 || s.placeholder || entry[s.path] {
			s.data = data
		} else {
			s.occ = nil
		}
		return nil
	}); err != nil {
		return nil, err
	}
	res := &outputNames{renames: map[string]string{}, read: len(texts)}
	byPath := make(map[string]*scanned, len(texts))
	for _, s := range texts {
		byPath[s.path] = s
	}

	// The renamed files: the entries, then every content-hashed file that
	// names a renamed one.
	users := map[string][]*scanned{} // base name -> files naming it
	for _, s := range texts {
		for _, r := range s.refs {
			users[r] = append(users[r], s)
		}
	}
	renamed := map[string]bool{}
	var queue []string
	for p := range entry {
		renamed[p] = true
		queue = append(queue, p)
	}
	for len(queue) > 0 {
		p := queue[0]
		queue = queue[1:]
		for _, u := range users[filepath.Base(p)] {
			if renamed[u.path] || strings.HasSuffix(u.path, ".map") {
				// A source map moves with its file (below).
				continue
			}
			if !u.krate && buildHashedNameRe.MatchString(filepath.Base(u.path)) {
				renamed[u.path] = true
				queue = append(queue, u.path)
			} else if !u.krate && isHashedAsset("/"+u.rel) {
				// Rewriting it in place would keep its name with new
				// content: the bug this pass exists to prevent.
				return nil, fmt.Errorf("%s names %s, but its own name is not one the build hashes", u.rel, filepath.Base(p))
			}
		}
	}

	// Names in dependency order: a file is named from its content with the
	// final names of the renamed files it references.
	deps := map[string][]string{}
	pendingDeps := map[string]int{}
	dependents := map[string][]string{}
	for p := range renamed {
		s := byPath[p]
		if s == nil {
			return nil, fmt.Errorf("%s cannot be renamed: it is not a text output", p)
		}
		for _, r := range s.refs {
			for _, q := range candidates[r] {
				if renamed[q] {
					deps[p] = append(deps[p], r)
					pendingDeps[p]++
					dependents[q] = append(dependents[q], p)
				}
			}
		}
	}
	var ready []string
	for p := range renamed {
		if pendingDeps[p] == 0 {
			ready = append(ready, p)
		}
	}
	sort.Strings(ready)
	newName := map[string]string{} // path -> new base name
	for len(ready) > 0 {
		p := ready[0]
		ready = ready[1:]
		s := byPath[p]
		base := filepath.Base(p)
		// The final names of the files it references, and its own name (its
		// source map comment) masked: the hash cannot cover the name it is
		// computing.
		content := splice(s.data, s.occ, func(name string) (string, bool) {
			if name == base {
				return selfName, true
			}
			if name == buildIDPlaceholder {
				return "", false
			}
			to, ok := res.renames[name]
			return to, ok
		})
		var to string
		if entry[p] {
			sum := sha256.Sum256(content)
			to = strings.TrimSuffix(base, ".js") + "." + hex.EncodeToString(sum[:])[:10] + ".js"
		} else {
			m := buildHashedNameRe.FindStringSubmatch(base)
			to = m[1] + "." + hashContent(content) + "." + m[3]
		}
		if prev, ok := res.renames[base]; ok && prev != to {
			return nil, fmt.Errorf("two outputs named %s get different names (%s, %s)", base, prev, to)
		}
		res.renames[base] = to
		newName[p] = to
		for _, d := range dependents[p] {
			if pendingDeps[d]--; pendingDeps[d] == 0 {
				ready = append(ready, d)
			}
		}
	}
	if len(newName) != len(renamed) {
		var cycle []string
		for p := range renamed {
			if _, ok := newName[p]; !ok {
				cycle = append(cycle, filepath.Base(p))
			}
		}
		sort.Strings(cycle)
		return nil, fmt.Errorf("content-hashed outputs name each other in a cycle: %s", strings.Join(cycle, ", "))
	}

	// A source map moves with its file (its text names the file, so it is
	// rewritten too).
	for p, to := range newName {
		if m := byPath[p+".map"]; m != nil {
			newName[m.path] = to + ".map"
		}
	}

	// The content-hashed files under their final names, and the build id.
	for _, o := range all {
		if o.krate {
			continue
		}
		rel := o.rel
		if to, ok := newName[o.path]; ok {
			rel = pathJoinSlash(filepath.ToSlash(filepath.Dir(o.rel)), to)
		}
		if isHashedAsset("/" + rel) {
			res.hashed = append(res.hashed, "/"+rel)
		}
	}
	sort.Strings(res.hashed)
	sum := sha256.Sum256([]byte(strings.Join(res.hashed, "\n")))
	res.buildID = hex.EncodeToString(sum[:])[:16]

	// One write of each file that changes.
	pairs := []string{buildIDPlaceholder, res.buildID}
	for from, to := range res.renames {
		pairs = append(pairs, from, to)
	}
	res.replacer = strings.NewReplacer(pairs...)
	var writes []*scanned
	for _, s := range texts {
		if _, moved := newName[s.path]; moved || s.placeholder || namesRenamed(s.refs, res.renames) {
			writes = append(writes, s)
		}
	}
	res.written = len(writes)
	err = parallel(len(writes), func(i int) error {
		s := writes[i]
		if s.data == nil {
			// A source map that names nothing renamed still moves.
			data, err := os.ReadFile(s.path)
			if err != nil {
				return err
			}
			s.data = data
		}
		var data []byte
		if s.occ != nil {
			data = splice(s.data, s.occ, func(name string) (string, bool) {
				if name == buildIDPlaceholder {
					return res.buildID, true
				}
				to, ok := res.renames[name]
				return to, ok
			})
		} else {
			data = []byte(res.replacer.Replace(string(s.data)))
		}
		if to, ok := newName[s.path]; ok {
			return moveOutput(s.path, filepath.Join(filepath.Dir(s.path), to), data)
		}
		return replaceOutput(s.path, data)
	})
	return res, err
}

// patch applies the renames to files written after nameOutputsByContent (the
// build manifests). A missing file is skipped.
func (n *outputNames) patch(paths ...string) error {
	if n == nil || len(n.renames) == 0 {
		return nil
	}
	for _, p := range paths {
		data, err := os.ReadFile(p)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return err
		}
		if out := n.replacer.Replace(string(data)); out != string(data) {
			if err := replaceOutput(p, []byte(out)); err != nil {
				return err
			}
		}
	}
	return nil
}

func namesRenamed(refs []string, renames map[string]string) bool {
	for _, r := range refs {
		if _, ok := renames[r]; ok {
			return true
		}
	}
	return false
}

func pathJoinSlash(dir, base string) string {
	if dir == "." || dir == "" {
		return base
	}
	return dir + "/" + base
}

// isNameByte reports whether c can be part of an output file name.
func isNameByte(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_' || c == '-' || c == '.'
}

// nameAt is an occurrence of a name in a file's text.
type nameAt struct {
	start, end int
	name       string
}

// namesIn returns, in order, the occurrences of the file names ending in .js,
// .mjs or .css in data that isName accepts. A name is the longest run of name
// bytes before the extension, or a part of it after a dot ("a.index.x.js"
// names index.x.js).
func namesIn(data []byte, isName func(string) bool) []nameAt {
	var out []nameAt
	for i := 0; i < len(data); {
		k := bytes.IndexByte(data[i:], '.')
		if k < 0 {
			break
		}
		dot := i + k
		i = dot + 1
		var end int
		switch {
		case bytes.HasPrefix(data[dot:], []byte(".js")):
			end = dot + 3
		case bytes.HasPrefix(data[dot:], []byte(".mjs")), bytes.HasPrefix(data[dot:], []byte(".css")):
			end = dot + 4
		default:
			continue
		}
		if end < len(data) && isNameByte(data[end]) && data[end] != '.' {
			continue // .json, .jsx, .cssx...
		}
		start := dot
		for start > 0 && isNameByte(data[start-1]) {
			start--
		}
		for s := start; s < dot; s++ {
			if s != start && data[s-1] != '.' {
				continue
			}
			name := string(data[s:end])
			if isName(name) {
				out = append(out, nameAt{s, end, name})
				break
			}
		}
	}
	return out
}

// appendPlaceholders adds the occurrences of buildIDPlaceholder in data to occ,
// keeping occ in order.
func appendPlaceholders(occ []nameAt, data []byte) []nameAt {
	ph := []byte(buildIDPlaceholder)
	var found []nameAt
	for i := 0; ; {
		k := bytes.Index(data[i:], ph)
		if k < 0 {
			break
		}
		found = append(found, nameAt{i + k, i + k + len(ph), buildIDPlaceholder})
		i += k + len(ph)
	}
	if len(found) == 0 {
		return occ
	}
	occ = append(occ, found...)
	sort.Slice(occ, func(a, b int) bool { return occ[a].start < occ[b].start })
	return occ
}

func hasPlaceholder(occ []nameAt) bool {
	for _, o := range occ {
		if o.name == buildIDPlaceholder {
			return true
		}
	}
	return false
}

// splice returns data with each occurrence in occ replaced by what to returns
// for its name (occurrences it declines stay).
func splice(data []byte, occ []nameAt, to func(name string) (string, bool)) []byte {
	out := make([]byte, 0, len(data)+len(occ)*12)
	last := 0
	for _, o := range occ {
		if o.start < last {
			continue
		}
		r, ok := to(o.name)
		if !ok {
			continue
		}
		out = append(out, data[last:o.start]...)
		out = append(out, r...)
		last = o.end
	}
	return append(out, data[last:]...)
}

// parallel runs fn for 0..n-1 on GOMAXPROCS goroutines and returns the first
// error.
func parallel(n int, fn func(i int) error) error {
	workers := min(runtime.GOMAXPROCS(0), n)
	var (
		next    int
		mu      sync.Mutex
		wg      sync.WaitGroup
		errOnce sync.Once
		first   error
	)
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				mu.Lock()
				i := next
				next++
				mu.Unlock()
				if i >= n {
					return
				}
				if err := fn(i); err != nil {
					errOnce.Do(func() { first = err })
				}
			}
		}()
	}
	wg.Wait()
	return first
}

// replaceOutput replaces a file's content.
func replaceOutput(path string, data []byte) error {
	return os.WriteFile(path, data, 0644)
}

// moveOutput writes data at to and removes from with its precompressed
// siblings.
func moveOutput(from, to string, data []byte) error {
	if err := os.WriteFile(to, data, 0644); err != nil {
		return err
	}
	if err := os.Remove(from); err != nil && !os.IsNotExist(err) {
		return err
	}
	for _, ext := range []string{".br", ".gz"} {
		_ = os.Remove(from + ext)
	}
	return nil
}
