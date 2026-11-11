package updater

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// trashName is the directory next to the installed files that holds what an
// update replaced. It is removed after a successful update and again by
// CleanupStale at the next start (on Windows the running executable cannot
// be deleted while it runs).
const trashName = ".krate-old"

// Apply replaces the installed files with the verified download.
//
//	Linux, Windows  the entries of the artifact replace the entries of the
//	                install directory (the directory of the executable); the
//	                executable itself is renamed out of the way first, which
//	                works while it runs.
//	macOS           the whole .app bundle is replaced by renaming the
//	                directories.
//	AppImage        the .AppImage file is replaced.
//
// If a step fails, what was moved is restored.
func (u *Update) Apply() error {
	if u.path == "" {
		return ErrNotDownloaded
	}
	exe, err := u.cfg.executable()
	if err != nil {
		return err
	}
	fsys := u.cfg.fs()
	switch {
	case goos(&u.cfg) == "darwin":
		bundle := bundleOf(exe)
		if bundle == "" {
			return fmt.Errorf("updater: %s is not inside a .app bundle; a macOS update replaces the whole bundle", exe)
		}
		return applyBundle(fsys, u.path, bundle, u.Version)
	case goos(&u.cfg) == "linux" && u.cfg.appImage() != "":
		return applyFile(fsys, u.path, u.cfg.appImage(), u.Version)
	}
	return applyTree(fsys, u.path, filepath.Dir(exe), filepath.Base(exe), u.Version)
}

func (c *Config) executable() (string, error) {
	if c.Exe != "" {
		return c.Exe, nil
	}
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	if r, err := filepath.EvalSymlinks(exe); err == nil {
		exe = r
	}
	return exe, nil
}

func (c *Config) appImage() string {
	if c.AppImage != "" {
		return c.AppImage
	}
	return os.Getenv("APPIMAGE")
}

// bundleOf returns the .app directory that contains exe, or "".
func bundleOf(exe string) string {
	p := filepath.Clean(exe)
	for {
		if strings.HasSuffix(p, ".app") {
			return p
		}
		parent := filepath.Dir(p)
		if parent == p {
			return ""
		}
		p = parent
	}
}

type move struct{ from, to string }

// applyTree moves each top-level entry of the artifact into dir. The files
// they replace go to dir/.krate-old.
func applyTree(fsys FS, artifact, dir, exeName, version string) error {
	if !isArchive(artifact) {
		return fmt.Errorf("updater: %q is not an archive", filepath.Base(artifact))
	}
	stage := filepath.Join(dir, ".krate-new-"+safeVersion(version))
	trash := filepath.Join(dir, trashName)
	// Leftovers from an earlier run; a real failure shows in the next steps.
	_ = fsys.RemoveAll(stage)
	if err := extractArchive(fsys, artifact, stage); err != nil {
		_ = fsys.RemoveAll(stage)
		return err
	}
	defer func() { _ = fsys.RemoveAll(stage) }()

	entries, err := fsys.ReadDir(stage)
	if err != nil {
		return err
	}
	// A single top-level directory that holds the executable is a wrapper
	// (name-1.2.0/...); step into it.
	if len(entries) == 1 && entries[0].IsDir() && entries[0].Name() != "app" {
		if _, err := fsys.Stat(filepath.Join(stage, entries[0].Name(), exeName)); err == nil {
			stage = filepath.Join(stage, entries[0].Name())
			if entries, err = fsys.ReadDir(stage); err != nil {
				return err
			}
		}
	}
	hasExe := false
	for _, e := range entries {
		if e.Name() == exeName {
			hasExe = true
		}
	}
	if !hasExe {
		return fmt.Errorf("updater: the update does not contain %s", exeName)
	}
	_ = fsys.RemoveAll(trash)
	if err := fsys.MkdirAll(trash, 0o755); err != nil {
		return fmt.Errorf("updater: %s is not writable: %w (an app installed by a package manager updates through it)", dir, err)
	}

	var trashed, placed []move
	rollback := func() {
		for i := len(placed) - 1; i >= 0; i-- {
			_ = fsys.RemoveAll(placed[i].to)
		}
		for i := len(trashed) - 1; i >= 0; i-- {
			_ = fsys.Rename(trashed[i].to, trashed[i].from)
		}
	}
	for _, e := range entries {
		dst := filepath.Join(dir, e.Name())
		if _, err := fsys.Lstat(dst); err == nil {
			t := filepath.Join(trash, e.Name())
			if err := fsys.Rename(dst, t); err != nil {
				rollback()
				return fmt.Errorf("updater: moving %s aside: %w", e.Name(), err)
			}
			trashed = append(trashed, move{from: dst, to: t})
		}
		if err := fsys.Rename(filepath.Join(stage, e.Name()), dst); err != nil {
			rollback()
			return fmt.Errorf("updater: installing %s: %w", e.Name(), err)
		}
		placed = append(placed, move{to: dst})
	}
	// Best effort: a running executable cannot be deleted on Windows.
	_ = fsys.RemoveAll(trash)
	return nil
}

// applyBundle replaces a .app directory.
func applyBundle(fsys FS, artifact, bundle, version string) error {
	if !isArchive(artifact) {
		return fmt.Errorf("updater: %q is not an archive", filepath.Base(artifact))
	}
	parent := filepath.Dir(bundle)
	name := filepath.Base(bundle)
	stage := filepath.Join(parent, "."+name+".new-"+safeVersion(version))
	old := filepath.Join(parent, "."+name+".old")
	_ = fsys.RemoveAll(stage)
	if err := extractArchive(fsys, artifact, stage); err != nil {
		_ = fsys.RemoveAll(stage)
		return err
	}
	defer func() { _ = fsys.RemoveAll(stage) }()
	next := filepath.Join(stage, name)
	if st, err := fsys.Stat(next); err != nil || !st.IsDir() {
		entries, _ := fsys.ReadDir(stage)
		next = ""
		for _, e := range entries {
			if e.IsDir() && strings.HasSuffix(e.Name(), ".app") {
				if next != "" {
					return errors.New("updater: the update contains more than one .app bundle")
				}
				next = filepath.Join(stage, e.Name())
			}
		}
		if next == "" {
			return fmt.Errorf("updater: the update does not contain %s", name)
		}
	}
	_ = fsys.RemoveAll(old)
	if err := fsys.Rename(bundle, old); err != nil {
		return fmt.Errorf("updater: moving %s aside: %w (is %s writable?)", name, err, parent)
	}
	if err := fsys.Rename(next, bundle); err != nil {
		_ = fsys.Rename(old, bundle)
		return fmt.Errorf("updater: installing %s: %w", name, err)
	}
	_ = fsys.RemoveAll(old)
	return nil
}

// applyFile replaces one file (an AppImage) with the artifact, which is the
// file itself or an archive holding exactly one file.
func applyFile(fsys FS, artifact, target, version string) error {
	src := artifact
	if isArchive(artifact) {
		stage := target + ".new-" + safeVersion(version)
		_ = fsys.RemoveAll(stage)
		if err := extractArchive(fsys, artifact, stage); err != nil {
			_ = fsys.RemoveAll(stage)
			return err
		}
		defer func() { _ = fsys.RemoveAll(stage) }()
		entries, err := fsys.ReadDir(stage)
		if err != nil {
			return err
		}
		if len(entries) != 1 || entries[0].IsDir() {
			return errors.New("updater: an AppImage update must hold exactly one file")
		}
		src = filepath.Join(stage, entries[0].Name())
	}
	mode := fs.FileMode(0o755)
	if st, err := fsys.Stat(target); err == nil {
		mode = st.Mode().Perm()
	}
	tmp := target + ".new"
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := fsys.Create(tmp, mode|0o200)
	if err != nil {
		return fmt.Errorf("updater: writing next to %s: %w", target, err)
	}
	_, err = io.Copy(out, in)
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = fsys.Chmod(tmp, mode)
	}
	if err == nil {
		err = fsys.Rename(tmp, target)
	}
	if err != nil {
		_ = fsys.RemoveAll(tmp)
		return fmt.Errorf("updater: replacing %s: %w", target, err)
	}
	return nil
}

func safeVersion(v string) string { return unsafeName.ReplaceAllString(v, "_") }

// CleanupStale removes what an earlier update left behind next to the
// executable (see trashName). Call it once at start-up; errors are ignored.
func CleanupStale(exe string) {
	if exe == "" {
		var err error
		if exe, err = os.Executable(); err != nil {
			return
		}
	}
	dir := filepath.Dir(exe)
	os.RemoveAll(filepath.Join(dir, trashName))
	if b := bundleOf(exe); b != "" {
		os.RemoveAll(filepath.Join(filepath.Dir(b), "."+filepath.Base(b)+".old"))
	}
	if entries, err := os.ReadDir(dir); err == nil {
		for _, e := range entries {
			if strings.HasPrefix(e.Name(), ".krate-new-") {
				os.RemoveAll(filepath.Join(dir, e.Name()))
			}
		}
	}
}

// Relaunch starts the current executable again with the same arguments and
// returns without waiting. The caller quits afterwards. Under an AppImage the
// AppImage file is started, not the extracted executable.
func Relaunch() error {
	exe := os.Getenv("APPIMAGE")
	if exe == "" {
		var err error
		if exe, err = os.Executable(); err != nil {
			return err
		}
	}
	cmd := exec.Command(exe, os.Args[1:]...)
	cmd.Stdin = nil
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	detach(cmd)
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("updater: relaunching: %w", err)
	}
	go func() { _ = cmd.Wait() }()
	return nil
}
