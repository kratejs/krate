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
	"sort"
	"strings"
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
// from its bundled content once everything is written, rewrites the files that
// name it, and gives a content-hashed file whose text that changed (a page
// script that imports an entry) a new hash too, so every name the HTML points
// at changes with what it serves.

// islandEntryFileRe matches an entry as islandEntryName names it.
var islandEntryFileRe = regexp.MustCompile(`^` + islandEntryPrefix + `[0-9a-f]{12}\.js$`)

// buildHashedNameRe splits a name the build content-hashed with hashContent:
// stem, hash, extension.
var buildHashedNameRe = regexp.MustCompile(`^(.+)\.([0-9a-z]{6})\.(js|mjs|css)$`)

// renameTextExts are the outputs that can name an island entry or a page
// script.
var renameTextExts = map[string]bool{".html": true, ".htm": true, ".js": true, ".mjs": true, ".cjs": true, ".json": true, ".map": true}

// contentHashIslandEntries renames the island entries in outDir/chunks to
// content-hashed names and updates every file under outDir that names them
// (public files excepted, see collectHashedAssets). It returns the renames
// (old base name to new) for applyOutputRenames.
func contentHashIslandEntries(outDir string, public map[string]bool) (map[string]string, error) {
	chunks := filepath.Join(outDir, "chunks")
	entries, err := os.ReadDir(chunks)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	renames := map[string]string{}
	// final holds the files this pass named; their text may still change (a
	// source map's name) but their name stays.
	final := map[string]bool{}
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !islandEntryFileRe.MatchString(name) {
			continue
		}
		data, err := os.ReadFile(filepath.Join(chunks, name))
		if err != nil {
			return nil, err
		}
		sum := sha256.Sum256(data)
		to := strings.TrimSuffix(name, ".js") + "." + hex.EncodeToString(sum[:])[:10] + ".js"
		if err := moveOutput(filepath.Join(chunks, name), filepath.Join(chunks, to), data); err != nil {
			return nil, err
		}
		renames[name] = to
		final[filepath.Join(chunks, to)] = true
	}
	if len(renames) == 0 {
		return nil, nil
	}
	// Each round rewrites the files naming what the last round renamed; a
	// content-hashed file among them is renamed in turn. Page scripts are named
	// only by documents and server outputs, so this ends after two rounds.
	pending := renames
	for round := 0; len(pending) > 0; round++ {
		if round == 8 {
			return renames, fmt.Errorf("renaming content-hashed outputs did not settle")
		}
		next := map[string]string{}
		err := rewriteOutputs(outDir, public, pending, func(path string, data []byte) error {
			base := filepath.Base(path)
			m := buildHashedNameRe.FindStringSubmatch(base)
			if final[path] || isUnderKrateDir(outDir, path) {
				return replaceOutput(path, data)
			}
			if m == nil {
				// Rewriting a content-hashed file in place would keep its name
				// with new content: the bug this pass exists to prevent.
				if rel, err := filepath.Rel(outDir, path); err == nil && isHashedAsset("/"+filepath.ToSlash(rel)) {
					return fmt.Errorf("%s names a renamed file, but its name is not one the build hashes", rel)
				}
				return replaceOutput(path, data)
			}
			to := m[1] + "." + hashContent(data) + "." + m[3]
			if to == base {
				return replaceOutput(path, data)
			}
			dest := filepath.Join(filepath.Dir(path), to)
			if err := moveOutput(path, dest, data); err != nil {
				return err
			}
			final[dest] = true
			next[base] = to
			return nil
		})
		if err != nil {
			return renames, err
		}
		for from, to := range next {
			renames[from] = to
		}
		pending = next
	}
	return renames, nil
}

// applyOutputRenames rewrites the names in renames in the files under dir
// written after contentHashIslandEntries (the build manifest).
func applyOutputRenames(dir string, renames map[string]string) error {
	if len(renames) == 0 {
		return nil
	}
	return rewriteOutputs(dir, nil, renames, replaceOutput)
}

// rewriteOutputs calls write with the new text of each text file under dir
// that names one of renames (a public file never).
func rewriteOutputs(dir string, public map[string]bool, renames map[string]string, write func(path string, data []byte) error) error {
	froms := make([]string, 0, len(renames))
	for from := range renames {
		froms = append(froms, from)
	}
	// Longer names first, so one name inside another is replaced whole.
	sort.Slice(froms, func(i, j int) bool {
		return len(froms[i]) > len(froms[j]) || len(froms[i]) == len(froms[j]) && froms[i] < froms[j]
	})
	pairs := make([]string, 0, 2*len(froms))
	needles := make([][]byte, 0, len(froms))
	for _, from := range froms {
		pairs = append(pairs, from, renames[from])
		needles = append(needles, []byte(from))
	}
	replacer := strings.NewReplacer(pairs...)
	var paths []string
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !renameTextExts[strings.ToLower(filepath.Ext(p))] {
			return nil
		}
		if rel, err := filepath.Rel(dir, p); err == nil && public[filepath.ToSlash(rel)] {
			return nil
		}
		paths = append(paths, p)
		return nil
	})
	if err != nil {
		return err
	}
	for _, p := range paths {
		data, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		hit := false
		for _, n := range needles {
			if bytes.Contains(data, n) {
				hit = true
				break
			}
		}
		if !hit {
			continue
		}
		if err := write(p, []byte(replacer.Replace(string(data)))); err != nil {
			return err
		}
	}
	return nil
}

// replaceOutput replaces a file's content.
func replaceOutput(path string, data []byte) error {
	return os.WriteFile(path, data, 0644)
}

// moveOutput writes data at to and removes from with its precompressed
// siblings; a source map next to it moves with it.
func moveOutput(from, to string, data []byte) error {
	if err := os.WriteFile(to, data, 0644); err != nil {
		return err
	}
	if err := os.Remove(from); err != nil && !os.IsNotExist(err) {
		return err
	}
	for _, ext := range []string{".br", ".gz"} {
		_ = os.Remove(from + ext)
		_ = os.Remove(from + ".map" + ext)
	}
	if m, err := os.ReadFile(from + ".map"); err == nil {
		if err := os.WriteFile(to+".map", m, 0644); err != nil {
			return err
		}
		_ = os.Remove(from + ".map")
	}
	return nil
}

// buildIDPlaceholder stands for the build id in the documents and server
// outputs until stampBuildID replaces it.
const buildIDPlaceholder = "__krate_build_id__"

// stampBuildID writes the build id, a digest of the names of the files the
// build content-hashed, where the documents and server outputs hold
// buildIDPlaceholder. Two builds that serve the same files share it. A
// document of another build carries a different one, and the SPA router then
// loads it with a full navigation (as Next.js does when its build id
// changes): swapping it in would load that build's islands and page scripts
// next to the modules already running (a layout island kept mounted), with a
// second copy of each module they share, such as a React context.
func stampBuildID(outDir string, public map[string]bool, hashed []string) error {
	sum := sha256.Sum256([]byte(strings.Join(hashed, "\n")))
	return rewriteOutputs(outDir, public, map[string]string{buildIDPlaceholder: hex.EncodeToString(sum[:])[:16]}, replaceOutput)
}
