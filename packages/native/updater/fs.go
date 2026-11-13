package updater

import (
	"io"
	"io/fs"
	"os"
	"runtime"
)

// FS is the part of the file system Apply and extraction use. Tests inject
// an implementation that fails on demand or records the calls; the default
// is the real one.
type FS interface {
	Stat(name string) (fs.FileInfo, error)
	Lstat(name string) (fs.FileInfo, error)
	MkdirAll(path string, perm fs.FileMode) error
	Rename(oldpath, newpath string) error
	RemoveAll(path string) error
	Create(name string, perm fs.FileMode) (io.WriteCloser, error)
	Symlink(oldname, newname string) error
	Chmod(name string, mode fs.FileMode) error
	ReadDir(name string) ([]fs.DirEntry, error)
}

// OSFS is the real file system.
type OSFS struct{}

func (OSFS) Stat(n string) (fs.FileInfo, error)  { return os.Stat(n) }
func (OSFS) Lstat(n string) (fs.FileInfo, error) { return os.Lstat(n) }
func (OSFS) MkdirAll(p string, m fs.FileMode) error {
	return os.MkdirAll(p, m)
}
func (OSFS) Rename(a, b string) error  { return os.Rename(a, b) }
func (OSFS) RemoveAll(p string) error  { return os.RemoveAll(p) }
func (OSFS) Symlink(a, b string) error { return os.Symlink(a, b) }
func (OSFS) Chmod(n string, m fs.FileMode) error {
	return os.Chmod(n, m)
}
func (OSFS) ReadDir(n string) ([]fs.DirEntry, error) { return os.ReadDir(n) }
func (OSFS) Create(n string, m fs.FileMode) (io.WriteCloser, error) {
	return os.OpenFile(n, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, m)
}

func (c *Config) fs() FS {
	if c.FS != nil {
		return c.FS
	}
	return OSFS{}
}

func goos(c *Config) string {
	if c != nil && c.GOOS != "" {
		return c.GOOS
	}
	return runtime.GOOS
}

func goarch() string { return runtime.GOARCH }
