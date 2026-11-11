package updater

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// maxExtract bounds the bytes one archive may expand to.
const maxExtract = 8 << 30

// extractArchive unpacks the .zip, .tar.gz or .tgz at src into dst through
// fsys. Entries that escape dst, special files and over-long archives are
// rejected.
func extractArchive(fsys FS, src, dst string) error {
	if err := fsys.MkdirAll(dst, 0o755); err != nil {
		return err
	}
	lower := strings.ToLower(src)
	switch {
	case strings.HasSuffix(lower, ".zip"):
		return extractZip(fsys, src, dst)
	case strings.HasSuffix(lower, ".tar.gz"), strings.HasSuffix(lower, ".tgz"):
		return extractTarGz(fsys, src, dst)
	}
	return fmt.Errorf("updater: unsupported artifact %q (want .zip or .tar.gz)", filepath.Base(src))
}

func isArchive(name string) bool {
	l := strings.ToLower(name)
	return strings.HasSuffix(l, ".zip") || strings.HasSuffix(l, ".tar.gz") || strings.HasSuffix(l, ".tgz")
}

// safeJoin joins an archive entry name under root and rejects escapes.
func safeJoin(root, name string) (string, error) {
	name = strings.ReplaceAll(name, "\\", "/")
	clean := path.Clean("/" + name)
	if clean == "/" {
		return "", nil
	}
	if strings.Contains(name, "\x00") {
		return "", fmt.Errorf("updater: bad archive entry %q", name)
	}
	for _, seg := range strings.Split(name, "/") {
		if seg == ".." {
			return "", fmt.Errorf("updater: archive entry %q escapes the install directory", name)
		}
	}
	return filepath.Join(root, filepath.FromSlash(clean[1:])), nil
}

// linkOK reports whether a symlink at entry pointing at target stays inside
// the archive root.
func linkOK(entry, target string) bool {
	target = strings.ReplaceAll(target, "\\", "/")
	if path.IsAbs(target) || filepath.IsAbs(target) || target == "" {
		return false
	}
	depth := 0
	for _, seg := range strings.Split(path.Dir(path.Clean("/"+entry)), "/") {
		if seg != "" {
			depth++
		}
	}
	for _, seg := range strings.Split(target, "/") {
		switch seg {
		case "", ".":
		case "..":
			depth--
			if depth < 0 {
				return false
			}
		default:
			depth++
		}
	}
	return true
}

type writer struct {
	fs    FS
	root  string
	total int64
}

func (w *writer) dir(name string, mode fs.FileMode) error {
	p, err := safeJoin(w.root, name)
	if err != nil || p == "" {
		return err
	}
	return w.fs.MkdirAll(p, mode.Perm()|0o700)
}

func (w *writer) file(name string, mode fs.FileMode, r io.Reader) error {
	p, err := safeJoin(w.root, name)
	if err != nil {
		return err
	}
	if p == "" {
		return fmt.Errorf("updater: bad archive entry %q", name)
	}
	if err := w.fs.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	f, err := w.fs.Create(p, mode.Perm()|0o200)
	if err != nil {
		return err
	}
	n, err := io.Copy(f, io.LimitReader(r, maxExtract-w.total+1))
	w.total += n
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return err
	}
	if w.total > maxExtract {
		return errors.New("updater: archive expands to too many bytes")
	}
	return w.fs.Chmod(p, mode.Perm())
}

func (w *writer) symlink(name, target string) error {
	p, err := safeJoin(w.root, name)
	if err != nil || p == "" {
		return err
	}
	if !linkOK(name, target) {
		return fmt.Errorf("updater: archive symlink %q -> %q leaves the install directory", name, target)
	}
	if err := w.fs.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	return w.fs.Symlink(target, p)
}

func extractZip(fsys FS, src, dst string) error {
	zr, err := zip.OpenReader(src)
	if err != nil {
		return fmt.Errorf("updater: opening the artifact: %w", err)
	}
	defer zr.Close()
	w := &writer{fs: fsys, root: dst}
	for _, f := range zr.File {
		mode := f.Mode()
		switch {
		case mode.IsDir():
			err = w.dir(f.Name, mode)
		case mode&fs.ModeSymlink != 0:
			var rc io.ReadCloser
			if rc, err = f.Open(); err == nil {
				var b []byte
				b, err = io.ReadAll(io.LimitReader(rc, 4096))
				rc.Close()
				if err == nil {
					err = w.symlink(f.Name, string(b))
				}
			}
		case mode.IsRegular():
			var rc io.ReadCloser
			if rc, err = f.Open(); err == nil {
				err = w.file(f.Name, mode, rc)
				rc.Close()
			}
		default:
			err = fmt.Errorf("updater: archive entry %q is a special file", f.Name)
		}
		if err != nil {
			return err
		}
	}
	return nil
}

func extractTarGz(fsys FS, src, dst string) error {
	f, err := os.Open(src)
	if err != nil {
		return err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return fmt.Errorf("updater: opening the artifact: %w", err)
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	w := &writer{fs: fsys, root: dst}
	for {
		h, err := tr.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return fmt.Errorf("updater: reading the artifact: %w", err)
		}
		switch h.Typeflag {
		case tar.TypeDir:
			err = w.dir(h.Name, fs.FileMode(h.Mode))
		case tar.TypeReg:
			err = w.file(h.Name, fs.FileMode(h.Mode), tr)
		case tar.TypeSymlink:
			err = w.symlink(h.Name, h.Linkname)
		case tar.TypeXGlobalHeader, tar.TypeXHeader:
		default:
			err = fmt.Errorf("updater: archive entry %q is a special file", h.Name)
		}
		if err != nil {
			return err
		}
	}
}
