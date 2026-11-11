package pkg

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

// entry is one file, directory or symlink of a package tree.
type entry struct {
	Path string // slash separated, relative, no leading "./"
	Mode fs.FileMode
	Src  string // file on disk, or ""
	Data []byte // in-memory content when Src is ""
	Link string // symlink target
}

func (e *entry) isDir() bool     { return e.Mode&fs.ModeDir != 0 }
func (e *entry) isSymlink() bool { return e.Mode&fs.ModeSymlink != 0 }

func (e *entry) size() (int64, error) {
	if e.isDir() || e.isSymlink() {
		return 0, nil
	}
	if e.Src != "" {
		st, err := os.Stat(e.Src)
		if err != nil {
			return 0, err
		}
		return st.Size(), nil
	}
	return int64(len(e.Data)), nil
}

func (e *entry) open() (io.ReadCloser, error) {
	if e.Src != "" {
		return os.Open(e.Src)
	}
	return io.NopCloser(strings.NewReader(string(e.Data))), nil
}

// tree is a set of package entries keyed by path.
type tree struct {
	m map[string]*entry
	// follow makes addDisk copy what a symlink points to (Windows).
	follow bool
}

func newTree() *tree { return &tree{m: map[string]*entry{}} }

func cleanPath(p string) string {
	p = path.Clean("/" + strings.ReplaceAll(p, "\\", "/"))
	return strings.TrimPrefix(p, "/")
}

func (t *tree) put(e *entry) {
	e.Path = cleanPath(e.Path)
	if e.Path == "" {
		return
	}
	t.m[e.Path] = e
}

func (t *tree) addFile(p string, mode fs.FileMode, src string) {
	t.put(&entry{Path: p, Mode: mode.Perm(), Src: src})
}

func (t *tree) addData(p string, mode fs.FileMode, data []byte) {
	t.put(&entry{Path: p, Mode: mode.Perm(), Data: data})
}

func (t *tree) addSymlink(p, target string) {
	t.put(&entry{Path: p, Mode: fs.ModeSymlink | 0o777, Link: target})
}

func (t *tree) addDir(p string) {
	t.put(&entry{Path: p, Mode: fs.ModeDir | 0o755})
}

// addDisk copies a file or directory from disk to dest, keeping modes and
// symlinks.
func (t *tree) addDisk(dest, src string) error {
	stat := os.Lstat
	if t.follow {
		stat = os.Stat
	}
	st, err := stat(src)
	if err != nil {
		return err
	}
	switch {
	case st.Mode()&fs.ModeSymlink != 0:
		target, err := os.Readlink(src)
		if err != nil {
			return err
		}
		t.addSymlink(dest, target)
	case st.IsDir():
		t.addDir(dest)
		entries, err := os.ReadDir(src)
		if err != nil {
			return err
		}
		for _, e := range entries {
			if err := t.addDisk(path.Join(dest, e.Name()), filepath.Join(src, e.Name())); err != nil {
				return err
			}
		}
	case st.Mode().IsRegular():
		t.addFile(dest, st.Mode(), src)
	default:
		return fmt.Errorf("%s is not a regular file, directory or symlink", src)
	}
	return nil
}

// sorted returns the entries in path order with every parent directory
// present (as 0755 when not added explicitly).
func (t *tree) sorted() []*entry {
	all := map[string]*entry{}
	for p, e := range t.m {
		all[p] = e
		for d := path.Dir(p); d != "." && d != "/"; d = path.Dir(d) {
			if _, ok := all[d]; !ok {
				if ex, ok := t.m[d]; ok {
					all[d] = ex
				} else {
					all[d] = &entry{Path: d, Mode: fs.ModeDir | 0o755}
				}
			}
		}
	}
	out := make([]*entry, 0, len(all))
	for _, e := range all {
		out = append(out, e)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out
}

// buildTime is the modification time written into archives: SOURCE_DATE_EPOCH
// when set, so builds are reproducible, else the current time.
func buildTime() time.Time {
	if s := os.Getenv("SOURCE_DATE_EPOCH"); s != "" {
		if n, err := strconv.ParseInt(s, 10, 64); err == nil {
			return time.Unix(n, 0).UTC()
		}
	}
	return time.Now().UTC().Truncate(time.Second)
}

// writeZip writes the tree as a zip; every path gets prefix (may be "").
// Modes and symlinks are kept as Unix attributes.
func writeZip(w io.Writer, t *tree, prefix string) error {
	zw := zip.NewWriter(w)
	mt := buildTime()
	for _, e := range t.sorted() {
		name := path.Join(prefix, e.Path)
		h := &zip.FileHeader{Name: name, Modified: mt, Method: zip.Deflate}
		switch {
		case e.isDir():
			h.Name += "/"
			h.Method = zip.Store
			h.SetMode(e.Mode)
			if _, err := zw.CreateHeader(h); err != nil {
				return err
			}
		case e.isSymlink():
			h.Method = zip.Store
			h.SetMode(e.Mode)
			fw, err := zw.CreateHeader(h)
			if err != nil {
				return err
			}
			if _, err := io.WriteString(fw, e.Link); err != nil {
				return err
			}
		default:
			h.SetMode(e.Mode)
			fw, err := zw.CreateHeader(h)
			if err != nil {
				return err
			}
			if err := copyEntry(fw, e); err != nil {
				return err
			}
		}
	}
	return zw.Close()
}

func copyEntry(w io.Writer, e *entry) error {
	r, err := e.open()
	if err != nil {
		return err
	}
	defer r.Close()
	_, err = io.Copy(w, r)
	return err
}

// writeTar writes an uncompressed tar of the tree under prefix. dotSlash
// names entries "./path" as dpkg expects.
func writeTar(w io.Writer, t *tree, prefix string, dotSlash bool) error {
	tw := tar.NewWriter(w)
	mt := buildTime()
	if dotSlash {
		if err := tw.WriteHeader(&tar.Header{Name: "./", Typeflag: tar.TypeDir, Mode: 0o755, ModTime: mt, Format: tar.FormatGNU}); err != nil {
			return err
		}
	}
	for _, e := range t.sorted() {
		name := path.Join(prefix, e.Path)
		if dotSlash {
			name = "./" + name
		}
		h := &tar.Header{Name: name, ModTime: mt, Mode: int64(e.Mode.Perm()), Format: tar.FormatGNU}
		switch {
		case e.isDir():
			h.Typeflag = tar.TypeDir
			h.Name += "/"
			if e.Mode&fs.ModeSetgid != 0 {
				h.Mode |= 0o2000
			}
			if err := tw.WriteHeader(h); err != nil {
				return err
			}
		case e.isSymlink():
			h.Typeflag = tar.TypeSymlink
			h.Linkname = e.Link
			h.Mode = 0o777
			if err := tw.WriteHeader(h); err != nil {
				return err
			}
		default:
			n, err := e.size()
			if err != nil {
				return err
			}
			h.Typeflag = tar.TypeReg
			h.Size = n
			if err := tw.WriteHeader(h); err != nil {
				return err
			}
			if err := copyEntry(tw, e); err != nil {
				return err
			}
		}
	}
	return tw.Close()
}

// writeTarGz writes the tree as a .tar.gz with a reproducible gzip header.
func writeTarGz(w io.Writer, t *tree, prefix string, dotSlash bool) error {
	gz, err := gzip.NewWriterLevel(w, gzip.BestCompression)
	if err != nil {
		return err
	}
	gz.ModTime = time.Time{}
	if err := writeTar(gz, t, prefix, dotSlash); err != nil {
		return err
	}
	return gz.Close()
}

// writeDisk materialises the tree under root.
func writeDisk(root string, t *tree) error {
	for _, e := range t.sorted() {
		p := filepath.Join(root, filepath.FromSlash(e.Path))
		switch {
		case e.isDir():
			if err := os.MkdirAll(p, 0o755); err != nil {
				return err
			}
			if err := os.Chmod(p, e.Mode.Perm()|0o700); err != nil {
				return err
			}
		case e.isSymlink():
			if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
				return err
			}
			os.Remove(p)
			if err := os.Symlink(e.Link, p); err != nil {
				return err
			}
		default:
			if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
				return err
			}
			f, err := os.OpenFile(p, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, e.Mode.Perm()|0o600)
			if err != nil {
				return err
			}
			if err := copyEntry(f, e); err != nil {
				f.Close()
				return err
			}
			if err := f.Close(); err != nil {
				return err
			}
			if err := os.Chmod(p, e.Mode.Perm()); err != nil {
				return err
			}
		}
	}
	return nil
}

// createFile creates dir of path and the file, for the writers above.
func createFile(p string) (*os.File, error) {
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return nil, err
	}
	return os.Create(p)
}
