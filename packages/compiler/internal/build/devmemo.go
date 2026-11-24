package build

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// A dev rebuild repeats esbuild builds whose inputs the edit did not touch:
// the island chunks of the whole site, the client boundary checks, the server
// render modules of every client component. Those builds are kept across dev
// rebuilds together with a stamp (size and modification time) of every file
// they read, and a later rebuild reuses the output while all stamps still
// match. The stamps are taken after the build, so a file that changed while it
// ran is never taken as unchanged (see stampFiles).

type fileStamp struct {
	size  int64
	mtime int64
}

// stampSet is the state of the files a build read.
type stampSet map[string]fileStamp

// stampFiles stamps paths. A file modified at or after startedAt may have
// been read mid-edit, so its stamp never matches again. Paths that do not
// exist (the stand-ins of batched entries) are left out.
func stampFiles(paths []string, startedAt time.Time) stampSet {
	s := make(stampSet, len(paths))
	cutoff := startedAt.Add(-10 * time.Millisecond).UnixNano()
	for _, p := range paths {
		fi, err := os.Stat(p)
		if err != nil {
			// Not a file: the stand-in path of a batched entry.
			continue
		}
		st := fileStamp{size: fi.Size(), mtime: fi.ModTime().UnixNano()}
		if st.mtime >= cutoff {
			st = fileStamp{size: -1}
		}
		s[p] = st
	}
	return s
}

// fresh reports whether every file still has the stamp it had.
func (s stampSet) fresh() bool {
	if s == nil {
		return false
	}
	for p, st := range s {
		if st.size < 0 {
			return false
		}
		fi, err := os.Stat(p)
		if err != nil || fi.Size() != st.size || fi.ModTime().UnixNano() != st.mtime {
			return false
		}
	}
	return true
}

// metafileAllInputs lists every file a build loaded, including the ones tree
// shaking left out of the output: editing those can still change the output.
// Virtual modules (plugin namespaces) are not files and are left out; their
// content is part of the caller's signature.
func metafileAllInputs(root, metafile string) []string {
	if metafile == "" {
		return nil
	}
	var meta struct {
		Inputs map[string]json.RawMessage `json:"inputs"`
	}
	if json.Unmarshal([]byte(metafile), &meta) != nil {
		return nil
	}
	out := make([]string, 0, len(meta.Inputs))
	for in := range meta.Inputs {
		if p, ok := metafileInputPath(root, in); ok {
			out = append(out, p)
		}
	}
	return out
}

// metafileInputPath is the file a metafile input names, as an absolute path.
// A module of a plugin namespace is a file when the plugin serves one under
// its own namespace (the stylesheets of an island: "krate-island-css:/abs/x.css"),
// and is left out when the namespace names something generated.
func metafileInputPath(root, in string) (string, bool) {
	if ns, rest, ok := strings.Cut(in, ":"); ok {
		if len(ns) > 1 && filepath.IsAbs(filepath.FromSlash(rest)) {
			return filepath.Clean(filepath.FromSlash(rest)), true
		}
		return "", false
	}
	return filepath.Clean(filepath.Join(root, filepath.FromSlash(in))), true
}

// projectConfigStamp stamps the files a build's module resolution depends on
// beyond the modules it loaded.
func (b *Builder) projectConfigStamp() stampSet {
	var paths []string
	for _, name := range []string{"package.json", "tsconfig.json", "jsconfig.json", "krate.config.ts", "krate.config.js", "krate.config.json"} {
		p := filepath.Join(b.Root, name)
		if _, err := os.Stat(p); err == nil {
			paths = append(paths, p)
		}
	}
	return stampFiles(paths, b.buildStart)
}

// buildMemo is a kept build: its signature (the inputs that are not files) and
// the stamps of the files it read.
type buildMemo struct {
	sig    string
	stamps stampSet
	config stampSet
	// global and parts are what sig is made of, for builds that are kept by
	// part: the signature of what every part shares, and of each part by name.
	global string
	parts  map[string]string
}

func (m *buildMemo) valid(sig string) bool {
	return m != nil && m.sig == sig && m.stamps.fresh() && m.config.fresh()
}

// stampInputs stamps the files a build read. The files the build itself
// writes into the output directory (the shims it injects) are named by their
// content and rewritten only when it changes, so they are not inputs an edit
// can change.
func (b *Builder) stampInputs(paths []string) stampSet {
	kept := make([]string, 0, len(paths))
	for _, p := range paths {
		if !isWithin(p, b.Cfg.OutDir) {
			kept = append(kept, p)
		}
	}
	return stampFiles(kept, b.buildStart)
}
