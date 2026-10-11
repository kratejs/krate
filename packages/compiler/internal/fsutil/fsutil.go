// Package fsutil provides shared filesystem helpers used across the compiler.
package fsutil

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
)

// WriteFileIfChanged writes data to path only when the existing contents
// differ, creating the file when absent. It prevents no-op writes from
// re-triggering file watchers (the dev loop rebuilds on source-tree writes,
// including generated declaration files rewritten identically every build).
func WriteFileIfChanged(path string, data []byte, perm os.FileMode) error {
	if existing, err := os.ReadFile(path); err == nil && bytes.Equal(existing, data) {
		return nil
	}
	return WriteFileAtomic(path, data, perm)
}

// WriteFileAtomic writes data to path via a sibling temp file and an atomic
// rename, so a crash or concurrent reader never observes a half-written file.
// The parent directory is created only when missing, and an existing target's
// file mode is preserved.
func WriteFileAtomic(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	if fi, err := os.Stat(dir); err != nil || !fi.IsDir() {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	if fi, err := os.Stat(path); err == nil {
		perm = fi.Mode().Perm()
	}

	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".tmp*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return err
	}
	if err := tmp.Chmod(perm); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return err
	}
	if err := os.Rename(tmpName, path); err != nil {
		_ = os.Remove(tmpName)
		return err
	}
	return nil
}

// WithinDir reports whether target is base itself or lies inside it. Both paths
// are cleaned and compared lexically; a ".." that escapes is rejected. Used to
// keep plugin/compiler writes inside the output or project directory.
func WithinDir(base, target string) bool {
	baseAbs, err := filepath.Abs(base)
	if err != nil {
		return false
	}
	targetAbs, err := filepath.Abs(target)
	if err != nil {
		return false
	}
	rel, err := filepath.Rel(baseAbs, targetAbs)
	if err != nil {
		return false
	}
	if rel == "." {
		return true
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel)
}

// WalkExt walks root and invokes fn for every regular file whose extension is
// in exts. Directories named in skipDirs (base name match) are pruned. The first
// error returned by fn or the walker is returned.
func WalkExt(root string, exts map[string]bool, skipDirs map[string]bool, fn func(path string, info os.FileInfo) error) error {
	return filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			if skipDirs != nil && skipDirs[info.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		if exts != nil && !exts[strings.ToLower(filepath.Ext(path))] {
			return nil
		}
		return fn(path, info)
	})
}

// HasAnyExt reports whether path ends with one of the given extensions.
func HasAnyExt(path string, exts ...string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	for _, e := range exts {
		if ext == e {
			return true
		}
	}
	return false
}
