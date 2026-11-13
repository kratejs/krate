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
// If a step fails, what was moved is restored. With Config.StateDir set the
// replaced files are kept and the update is recorded as unconfirmed until
// MarkHealthy runs in the new version (see Boot).
func (u *Update) Apply() error {
	if u.path == "" && u.stage == "" {
		return ErrNotDownloaded
	}
	exe, err := u.cfg.executable()
	if err != nil {
		return err
	}
	fsys := u.cfg.fs()
	keep := u.cfg.StateDir != ""
	var prev *PrevInfo
	switch {
	case goos(&u.cfg) == "darwin":
		bundle := bundleOf(exe)
		if bundle == "" {
			return fmt.Errorf("updater: %s is not inside a .app bundle; a macOS update replaces the whole bundle", exe)
		}
		prev, err = u.installFrom(fsys, KindBundle, exe, bundle, keep)
	case goos(&u.cfg) == "linux" && u.cfg.appImage() != "":
		prev, err = u.installFrom(fsys, KindFile, exe, u.cfg.appImage(), keep)
	default:
		prev, err = u.installFrom(fsys, KindTree, exe, filepath.Dir(exe), keep)
	}
	if err != nil {
		return err
	}
	u.stage = ""
	if keep {
		if err := recordApplied(u.cfg, u.Version, prev); err != nil {
			// Without the record there is nothing to roll back to.
			discardPrevious(fsys, prev)
			u.cfg.logf("updater: could not record the update for rollback: %v", err)
		}
	}
	return nil
}

// installFrom installs the downloaded artifact (extracting it) or the tree a
// delta update staged. target is the install directory (KindTree), the bundle
// or the file.
func (u *Update) installFrom(fsys FS, kind, exe, target string, keep bool) (*PrevInfo, error) {
	stage := u.stage
	if stage == "" {
		if !isArchive(u.path) && kind != KindFile {
			return nil, fmt.Errorf("updater: %q is not an archive", filepath.Base(u.path))
		}
		stage = stagePath(kind, target, u.Version)
		if isArchive(u.path) {
			_ = fsys.RemoveAll(stage)
			if err := extractArchive(fsys, u.path, stage); err != nil {
				_ = fsys.RemoveAll(stage)
				return nil, err
			}
		}
	}
	if isArchive(u.path) || u.stage != "" {
		defer func() { _ = fsys.RemoveAll(stage) }()
	}
	var prev *PrevInfo
	var err error
	switch kind {
	case KindBundle:
		prev, err = installBundle(fsys, stage, target, keep)
	case KindFile:
		src := u.path
		if isArchive(u.path) || u.stage != "" {
			if src, err = singleFile(fsys, stage); err != nil {
				return nil, err
			}
		}
		prev, err = installFile(fsys, src, target, keep)
	default:
		prev, err = installTree(fsys, stage, target, filepath.Base(exe), keep)
	}
	if prev != nil {
		prev.Exe = exe
	}
	return prev, err
}

// stagePath is where an artifact is unpacked before it is installed; it is on
// the same file system as the files it replaces.
func stagePath(kind, target, version string) string {
	switch kind {
	case KindBundle:
		return filepath.Join(filepath.Dir(target), "."+filepath.Base(target)+".new-"+safeVersion(version))
	case KindFile:
		return target + ".new-" + safeVersion(version)
	}
	return filepath.Join(target, ".krate-new-"+safeVersion(version))
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

// PrevInfo records the files an update replaced, so a failed first start can
// put them back.
type PrevInfo struct {
	// Kind is KindTree, KindBundle or KindFile.
	Kind string `json:"kind"`
	// Target is the install directory, the bundle or the file.
	Target string `json:"target"`
	// Exe is the executable to start again after a rollback.
	Exe string `json:"exe"`
	// Dir holds the replaced files (tree), or is the replaced bundle or file.
	Dir string `json:"dir"`
	// Placed and Trashed are the top-level names a tree update installed and
	// the ones it moved into Dir.
	Placed  []string `json:"placed,omitempty"`
	Trashed []string `json:"trashed,omitempty"`
}

// prevName is the directory next to the installed files that keeps the
// replaced entries until the new version confirms a healthy start.
const prevName = ".krate-prev"

// installTree moves each top-level entry of the unpacked stage into dir. The
// files they replace go to dir/.krate-old, or dir/.krate-prev with keep.
func installTree(fsys FS, stage, dir, exeName string, keep bool) (*PrevInfo, error) {
	stage, entries, err := unwrapTree(fsys, stage, exeName)
	if err != nil {
		return nil, err
	}
	hasExe := false
	for _, e := range entries {
		if e.Name() == exeName {
			hasExe = true
		}
	}
	if !hasExe {
		return nil, fmt.Errorf("updater: the update does not contain %s", exeName)
	}
	trash := filepath.Join(dir, trashName)
	if keep {
		trash = filepath.Join(dir, prevName)
	}
	_ = fsys.RemoveAll(trash)
	if err := fsys.MkdirAll(trash, 0o755); err != nil {
		return nil, fmt.Errorf("updater: %s is not writable: %w (an app installed by a package manager updates through it)", dir, err)
	}

	var trashed, placed []move
	rollback := func() {
		for i := len(placed) - 1; i >= 0; i-- {
			_ = fsys.RemoveAll(placed[i].to)
		}
		for i := len(trashed) - 1; i >= 0; i-- {
			_ = fsys.Rename(trashed[i].to, trashed[i].from)
		}
		if keep {
			_ = fsys.RemoveAll(trash)
		}
	}
	prev := &PrevInfo{Kind: KindTree, Target: dir, Dir: trash}
	for _, e := range entries {
		dst := filepath.Join(dir, e.Name())
		if _, err := fsys.Lstat(dst); err == nil {
			t := filepath.Join(trash, e.Name())
			if err := fsys.Rename(dst, t); err != nil {
				rollback()
				return nil, fmt.Errorf("updater: moving %s aside: %w", e.Name(), err)
			}
			trashed = append(trashed, move{from: dst, to: t})
			prev.Trashed = append(prev.Trashed, e.Name())
		}
		if err := fsys.Rename(filepath.Join(stage, e.Name()), dst); err != nil {
			rollback()
			return nil, fmt.Errorf("updater: installing %s: %w", e.Name(), err)
		}
		placed = append(placed, move{to: dst})
		prev.Placed = append(prev.Placed, e.Name())
	}
	if !keep {
		// Best effort: a running executable cannot be deleted on Windows.
		_ = fsys.RemoveAll(trash)
	}
	return prev, nil
}

// installBundle replaces a .app directory with the one in the unpacked stage.
func installBundle(fsys FS, stage, bundle string, keep bool) (*PrevInfo, error) {
	parent := filepath.Dir(bundle)
	name := filepath.Base(bundle)
	next, err := findBundle(fsys, stage, name)
	if err != nil {
		return nil, err
	}
	old := filepath.Join(parent, "."+name+".old")
	if keep {
		old = filepath.Join(parent, "."+name+".prev")
	}
	_ = fsys.RemoveAll(old)
	if err := fsys.Rename(bundle, old); err != nil {
		return nil, fmt.Errorf("updater: moving %s aside: %w (is %s writable?)", name, err, parent)
	}
	if err := fsys.Rename(next, bundle); err != nil {
		_ = fsys.Rename(old, bundle)
		return nil, fmt.Errorf("updater: installing %s: %w", name, err)
	}
	if !keep {
		_ = fsys.RemoveAll(old)
	}
	return &PrevInfo{Kind: KindBundle, Target: bundle, Dir: old}, nil
}

// singleFile returns the one file in an unpacked stage.
func singleFile(fsys FS, stage string) (string, error) {
	entries, err := fsys.ReadDir(stage)
	if err != nil {
		return "", err
	}
	if len(entries) != 1 || entries[0].IsDir() {
		return "", errors.New("updater: an AppImage update must hold exactly one file")
	}
	return filepath.Join(stage, entries[0].Name()), nil
}

// installFile replaces one file (an AppImage) with src.
func installFile(fsys FS, src, target string, keep bool) (*PrevInfo, error) {
	mode := fs.FileMode(0o755)
	if st, err := fsys.Stat(target); err == nil {
		mode = st.Mode().Perm()
	}
	tmp := target + ".new"
	in, err := os.Open(src)
	if err != nil {
		return nil, err
	}
	defer in.Close()
	out, err := fsys.Create(tmp, mode|0o200)
	if err != nil {
		return nil, fmt.Errorf("updater: writing next to %s: %w", target, err)
	}
	_, err = io.Copy(out, in)
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = fsys.Chmod(tmp, mode)
	}
	prev := &PrevInfo{Kind: KindFile, Target: target, Dir: target + ".prev"}
	if err == nil && keep {
		_ = fsys.RemoveAll(prev.Dir)
		err = fsys.Rename(target, prev.Dir)
		if err == nil {
			if err = fsys.Rename(tmp, target); err != nil {
				_ = fsys.Rename(prev.Dir, target)
			}
		}
	} else if err == nil {
		err = fsys.Rename(tmp, target)
	}
	if err != nil {
		_ = fsys.RemoveAll(tmp)
		return nil, fmt.Errorf("updater: replacing %s: %w", target, err)
	}
	return prev, nil
}

// restorePrevious puts the files an update replaced back and moves the
// update's own files aside. It works on the real or an injected file system.
func restorePrevious(fsys FS, p *PrevInfo) error {
	var first error
	note := func(err error) {
		if first == nil && err != nil {
			first = err
		}
	}
	switch p.Kind {
	case KindTree:
		failed := filepath.Join(p.Target, ".krate-failed")
		note(fsys.MkdirAll(failed, 0o755))
		for i := len(p.Placed) - 1; i >= 0; i-- {
			cur := filepath.Join(p.Target, p.Placed[i])
			if _, err := fsys.Lstat(cur); err == nil {
				note(fsys.Rename(cur, filepath.Join(failed, p.Placed[i])))
			}
		}
		for _, name := range p.Trashed {
			note(fsys.Rename(filepath.Join(p.Dir, name), filepath.Join(p.Target, name)))
		}
		if first == nil {
			_ = fsys.RemoveAll(failed)
			_ = fsys.RemoveAll(p.Dir)
		}
	case KindBundle, KindFile:
		failed := p.Target + ".failed"
		_ = fsys.RemoveAll(failed)
		note(fsys.Rename(p.Target, failed))
		if first == nil {
			if err := fsys.Rename(p.Dir, p.Target); err != nil {
				note(err)
				_ = fsys.Rename(failed, p.Target)
			} else {
				_ = fsys.RemoveAll(failed)
			}
		}
	default:
		return fmt.Errorf("updater: unknown rollback kind %q", p.Kind)
	}
	return first
}

// discardPrevious deletes the kept files of a confirmed (or abandoned) update.
func discardPrevious(fsys FS, p *PrevInfo) {
	if p != nil && p.Dir != "" {
		_ = fsys.RemoveAll(p.Dir)
	}
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
	os.RemoveAll(filepath.Join(dir, ".krate-failed"))
	if b := bundleOf(exe); b != "" {
		os.RemoveAll(filepath.Join(filepath.Dir(b), "."+filepath.Base(b)+".old"))
		os.RemoveAll(b + ".failed")
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
	return RelaunchPath(exe)
}

// RelaunchPath is Relaunch for a given executable (after a rollback the
// running executable has been renamed, so its own path is not the one to
// start).
func RelaunchPath(exe string) error {
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
