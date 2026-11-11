// Package fs is scoped file access for UI and TypeScript host code.
//
// The scope is a set of roots: the app directories (sdkos.AllDirs) plus paths
// granted at run time with Grant, which the dialog SDK calls when the user
// picks a file or folder. Every path is cleaned, made absolute (a relative
// path means relative to AppDataDir), resolved through symlinks and then
// checked to lie inside a root. A granted directory covers its subtree; a
// granted file covers just that file.
//
// Go host code is trusted and should use the standard library directly. The
// functions here apply the scope too, so they are the right call when a Go
// function handles a path that came from the UI.
//
// A path that leaves the scope fails with code "permission_denied" and data
// {permission: "fs", reason: "outside_scope", path}. The check and the use are
// not atomic: another process that swaps a directory for a symlink between
// the two can still escape. The scope protects against UI mistakes and
// malicious pages, not against a hostile local process.
package fs

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/kratejs/krate/packages/native/krate"
	sdkos "github.com/kratejs/krate/packages/native/sdk/os"
)

// MaxReadSize is the largest file ReadFile and ReadText return.
const MaxReadSize = 64 << 20

// Scope is a set of allowed roots.
type Scope struct {
	base func() ([]string, error)

	mu     sync.RWMutex
	grants []string
}

// NewScope returns a Scope whose fixed roots come from base, called on every
// check (it may create the directories). The first root is what a relative
// path is relative to.
func NewScope(base func() ([]string, error)) *Scope { return &Scope{base: base} }

// Default is the scope used by the package functions and by Register.
var Default = NewScope(sdkos.AllDirs)

// Grant allows access to path: a file, or a directory with everything under
// it. The path must exist or have an existing parent directory. It is
// resolved through symlinks first, so granting a link grants its target.
func Grant(path string) error { return Default.Grant(path) }

// Revoke removes a grant made with Grant.
func Revoke(path string) { Default.Revoke(path) }

// Grants lists the granted roots.
func Grants() []string { return Default.Grants() }

// Grant is the method form of the package function.
func (s *Scope) Grant(path string) error {
	if path == "" {
		return errors.New("fs: Grant needs a path")
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	real, err := resolve(abs, true)
	if err != nil {
		return fmt.Errorf("fs: granting %s: %w", path, err)
	}
	if _, err := os.Lstat(real); err != nil {
		if st, perr := os.Stat(filepath.Dir(real)); perr != nil || !st.IsDir() {
			return fmt.Errorf("fs: granting %s: neither it nor its parent directory exists", path)
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, g := range s.grants {
		if g == real {
			return nil
		}
	}
	s.grants = append(s.grants, real)
	return nil
}

// Revoke removes the grant for path.
func (s *Scope) Revoke(path string) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return
	}
	real, err := resolve(abs, true)
	if err != nil {
		real = abs
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, g := range s.grants {
		if g == real {
			s.grants = append(s.grants[:i], s.grants[i+1:]...)
			return
		}
	}
}

// Grants lists the granted roots.
func (s *Scope) Grants() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return append([]string(nil), s.grants...)
}

// Roots returns the fixed roots followed by the grants, resolved.
func (s *Scope) Roots() ([]string, error) {
	var roots []string
	if s.base != nil {
		dirs, err := s.base()
		if err != nil {
			return nil, err
		}
		for _, d := range dirs {
			r, err := resolve(d, true)
			if err != nil {
				return nil, err
			}
			roots = append(roots, r)
		}
	}
	return append(roots, s.Grants()...), nil
}

// resolve returns p with symlinks resolved. When follow is false the last
// element is not resolved (so a symlink itself can be removed or renamed).
// Elements that do not exist yet are kept as they are, so new files can be
// checked before they are created.
func resolve(p string, follow bool) (string, error) {
	p = filepath.Clean(p)
	if !follow {
		dir, base := filepath.Split(p)
		if base == "" {
			return resolve(p, true)
		}
		rd, err := resolve(filepath.Clean(dir), true)
		if err != nil {
			return "", err
		}
		return filepath.Join(rd, base), nil
	}
	var tail []string
	cur := p
	for {
		real, err := filepath.EvalSymlinks(cur)
		if err == nil {
			for i := len(tail) - 1; i >= 0; i-- {
				real = filepath.Join(real, tail[i])
			}
			return real, nil
		}
		if !errors.Is(err, fs.ErrNotExist) {
			return "", err
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			return "", err
		}
		tail = append(tail, filepath.Base(cur))
		cur = parent
	}
}

func inside(root, p string) bool {
	if runtime.GOOS == "windows" {
		root, p = strings.ToLower(root), strings.ToLower(p)
	}
	rel, err := filepath.Rel(root, p)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel)
}

func samePath(a, b string) bool {
	if runtime.GOOS == "windows" {
		return strings.EqualFold(a, b)
	}
	return a == b
}

func denied(path string) error {
	e := krate.Errorf("permission_denied", "fs: %q is outside the folders this app may access", path).(*krate.Error)
	e.Data = map[string]string{"permission": "fs", "reason": "outside_scope", "path": path}
	return e
}

// Check resolves path (following symlinks, including the last element) and
// verifies it is inside the scope. It returns the real path to use.
func (s *Scope) Check(path string) (string, error) { return s.check(path, true) }

func (s *Scope) check(path string, follow bool) (string, error) {
	if path == "" || strings.ContainsRune(path, 0) {
		return "", krate.Errorf("bad_args", "fs: invalid path")
	}
	p := filepath.FromSlash(path)
	if !filepath.IsAbs(p) {
		var dirs []string
		if s.base != nil {
			var err error
			if dirs, err = s.base(); err != nil {
				return "", err
			}
		}
		if len(dirs) == 0 {
			return "", krate.Errorf("bad_args", "fs: %q is relative and the app has no data directory", path)
		}
		p = filepath.Join(dirs[0], p)
	}
	real, err := resolve(p, follow)
	if err != nil {
		return "", mapErr(err)
	}
	roots, err := s.Roots()
	if err != nil {
		return "", err
	}
	for _, r := range roots {
		if samePath(r, real) {
			return real, nil
		}
		if inside(r, real) {
			if st, err := os.Stat(r); err == nil && st.IsDir() {
				return real, nil
			}
		}
	}
	return "", denied(path)
}

// isRoot reports whether real is itself a root (which must not be removed).
func (s *Scope) isRoot(real string) bool {
	roots, _ := s.Roots()
	for _, r := range roots {
		if samePath(r, real) {
			return true
		}
	}
	return false
}

// mapErr gives common OS errors a stable code.
func mapErr(err error) error {
	var ke *krate.Error
	if errors.As(err, &ke) {
		return err
	}
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return krate.Errorf("not_found", "%w", err)
	case errors.Is(err, fs.ErrExist):
		return krate.Errorf("already_exists", "%w", err)
	case errors.Is(err, fs.ErrPermission):
		return krate.Errorf("access_denied", "%w", err)
	}
	return err
}

// ReadFile returns the contents of path.
func (s *Scope) ReadFile(path string) ([]byte, error) {
	real, err := s.Check(path)
	if err != nil {
		return nil, err
	}
	f, err := os.Open(real)
	if err != nil {
		return nil, mapErr(err)
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return nil, mapErr(err)
	}
	if st.IsDir() {
		return nil, krate.Errorf("is_directory", "fs: %q is a directory", path)
	}
	if st.Size() > MaxReadSize {
		return nil, krate.Errorf("too_large", "fs: %q is larger than %d bytes", path, MaxReadSize)
	}
	b := make([]byte, 0, st.Size())
	buf := make([]byte, 32<<10)
	for {
		n, err := f.Read(buf)
		b = append(b, buf[:n]...)
		if len(b) > MaxReadSize {
			return nil, krate.Errorf("too_large", "fs: %q is larger than %d bytes", path, MaxReadSize)
		}
		if err != nil {
			break
		}
	}
	return b, nil
}

// ReadText returns the contents of path as a string. Invalid UTF-8 is
// replaced by U+FFFD.
func (s *Scope) ReadText(path string) (string, error) {
	b, err := s.ReadFile(path)
	if err != nil {
		return "", err
	}
	if utf8.Valid(b) {
		return string(b), nil
	}
	return strings.ToValidUTF8(string(b), "�"), nil
}

// WriteFile writes data to path, creating or truncating it. The parent
// directory must exist.
func (s *Scope) WriteFile(path string, data []byte) error {
	real, err := s.Check(path)
	if err != nil {
		return err
	}
	return mapErr(os.WriteFile(real, data, 0o644))
}

// WriteText writes text to path.
func (s *Scope) WriteText(path, text string) error { return s.WriteFile(path, []byte(text)) }

// Entry is one directory entry.
type Entry struct {
	Name    string
	IsDir   bool
	Size    int64
	ModTime time.Time
}

// ReadDir lists a directory sorted by name.
func (s *Scope) ReadDir(path string) ([]Entry, error) {
	real, err := s.Check(path)
	if err != nil {
		return nil, err
	}
	des, err := os.ReadDir(real)
	if err != nil {
		return nil, mapErr(err)
	}
	out := make([]Entry, 0, len(des))
	for _, de := range des {
		e := Entry{Name: de.Name(), IsDir: de.IsDir()}
		if info, err := de.Info(); err == nil {
			e.Size, e.ModTime = info.Size(), info.ModTime()
		}
		out = append(out, e)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// Info describes a file.
type Info struct {
	Name    string
	Size    int64
	IsDir   bool
	ModTime time.Time
	// Mode holds the permission bits (0o755 and so on).
	Mode uint32
}

// Stat describes path (following symlinks).
func (s *Scope) Stat(path string) (Info, error) {
	real, err := s.Check(path)
	if err != nil {
		return Info{}, err
	}
	st, err := os.Stat(real)
	if err != nil {
		return Info{}, mapErr(err)
	}
	return Info{Name: st.Name(), Size: st.Size(), IsDir: st.IsDir(), ModTime: st.ModTime(), Mode: uint32(st.Mode().Perm())}, nil
}

// Exists reports whether path exists. A path outside the scope is an error,
// not false, so the answer never leaks what lies outside.
func (s *Scope) Exists(path string) (bool, error) {
	real, err := s.Check(path)
	if err != nil {
		return false, err
	}
	_, err = os.Stat(real)
	if err == nil {
		return true, nil
	}
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	return false, mapErr(err)
}

// Mkdir creates path and any missing parents.
func (s *Scope) Mkdir(path string) error {
	real, err := s.Check(path)
	if err != nil {
		return err
	}
	return mapErr(os.MkdirAll(real, 0o755))
}

// Remove deletes a file or an empty directory; with recursive it deletes a
// directory tree and ignores a missing path. A symlink is removed itself,
// never its target. A root cannot be removed.
func (s *Scope) Remove(path string, recursive bool) error {
	real, err := s.check(path, false)
	if err != nil {
		return err
	}
	if s.isRoot(real) {
		return krate.Errorf("bad_args", "fs: %q is one of the app's folders and cannot be removed", path)
	}
	if recursive {
		return mapErr(os.RemoveAll(real))
	}
	return mapErr(os.Remove(real))
}

// Rename moves from to to; both must be inside the scope.
func (s *Scope) Rename(from, to string) error {
	a, err := s.check(from, false)
	if err != nil {
		return err
	}
	b, err := s.check(to, false)
	if err != nil {
		return err
	}
	if s.isRoot(a) {
		return krate.Errorf("bad_args", "fs: %q is one of the app's folders and cannot be moved", from)
	}
	return mapErr(os.Rename(a, b))
}

// Package-level forms use Default.

// ReadFile returns the contents of path.
func ReadFile(path string) ([]byte, error) { return Default.ReadFile(path) }

// ReadText returns the contents of path as a string.
func ReadText(path string) (string, error) { return Default.ReadText(path) }

// WriteFile writes data to path.
func WriteFile(path string, data []byte) error { return Default.WriteFile(path, data) }

// WriteText writes text to path.
func WriteText(path, text string) error { return Default.WriteText(path, text) }

// ReadDir lists a directory.
func ReadDir(path string) ([]Entry, error) { return Default.ReadDir(path) }

// Stat describes path.
func Stat(path string) (Info, error) { return Default.Stat(path) }

// Exists reports whether path exists.
func Exists(path string) (bool, error) { return Default.Exists(path) }

// Mkdir creates path and any missing parents.
func Mkdir(path string) error { return Default.Mkdir(path) }

// Remove deletes path.
func Remove(path string, recursive bool) error { return Default.Remove(path, recursive) }

// Rename moves from to to.
func Rename(from, to string) error { return Default.Rename(from, to) }
