package build

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
)

// nativeExecutable returns the path of the running krate executable. Tests
// replace it to lay out a fake installation.
var nativeExecutable = os.Executable

// ShellEnv is the environment variable that names a shell binary.
const ShellEnv = "KRATE_SHELL"

// ShellFileName is the file name of the shell binary for an OS.
func ShellFileName(goos string) string {
	if goos == "windows" {
		return "krate-shell.exe"
	}
	return "krate-shell"
}

// FindPrebuiltShell looks for a shell binary that needs no build, in this
// order: the explicit path (--shell), $KRATE_SHELL, then a krate-shell[.exe]
// in the directory of the running krate executable after symlinks are
// resolved (the @krate/core-<platform> package puts it there). The last is
// only offered for the machine's own OS and architecture. It returns "" with
// no error when none of them applies; a path that was given but does not
// exist is an error. source says where the path came from.
func FindPrebuiltShell(override, goos, goarch string) (path, source string, err error) {
	if override != "" && override != "auto" {
		if !isFile(override) {
			return "", "", fmt.Errorf("the shell binary %s does not exist", override)
		}
		return override, "--shell", nil
	}
	if env := os.Getenv(ShellEnv); env != "" && env != "auto" {
		if !isFile(env) {
			return "", "", fmt.Errorf("$%s names %s, which does not exist", ShellEnv, env)
		}
		return env, "$" + ShellEnv, nil
	}
	if goos == runtime.GOOS && goarch == runtime.GOARCH {
		if p := siblingShell(goos); p != "" {
			return p, "next to the krate executable", nil
		}
	}
	return "", "", nil
}

// siblingShell returns the shell binary next to the running executable.
func siblingShell(goos string) string {
	exe, err := nativeExecutable()
	if err != nil {
		return ""
	}
	if real, err := filepath.EvalSymlinks(exe); err == nil {
		exe = real
	}
	p := filepath.Join(filepath.Dir(exe), ShellFileName(goos))
	if isFile(p) {
		return p
	}
	return ""
}

func isFile(p string) bool {
	st, err := os.Stat(p)
	return err == nil && st.Mode().IsRegular()
}

// errNoShell is the actionable error for a project with no shell binary.
func errNoShell(why string) error {
	msg := "no krate-shell binary found"
	if why != "" {
		msg += ": " + why
	}
	return errors.New(msg + ". It is looked for in --shell, $" + ShellEnv + ", next to the krate executable (the @krate/core-<platform> package ships it there) and, inside a Krate checkout, built from packages/native/cmd/krate-shell. Reinstall @krate/core, pass --shell <path>, or set " + ShellEnv)
}

// assembleShellDist lays out a runnable distributable for an app that runs
// under the prebuilt shell:
//
//	<outDir>/<os>-<arch>/<productName>[.exe]   the shell, renamed
//	<outDir>/<os>-<arch>/app/                  a copy of the app directory
//
// The shell finds app/ next to itself (and ../Resources/app inside a macOS
// bundle), and krate native package takes this directory as its payload. It
// returns the renamed shell.
func assembleShellDist(outDir, appDir, shellPath, productName, goos, goarch string) (string, error) {
	dir := filepath.Join(outDir, goos+"-"+goarch)
	if err := os.RemoveAll(dir); err != nil {
		return "", err
	}
	if err := copyNativeTree(appDir, filepath.Join(dir, "app")); err != nil {
		return "", err
	}
	exe := fileSafe(productName)
	if goos == "windows" {
		exe += ".exe"
	}
	dst := filepath.Join(dir, exe)
	if err := copyNativeFile(shellPath, dst); err != nil {
		return "", err
	}
	if err := os.Chmod(dst, 0o755); err != nil {
		return "", err
	}
	return dst, nil
}

// copyNativeTree copies a directory, keeping file modes and symlinks.
func copyNativeTree(src, dst string) error {
	return filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, p)
		to := filepath.Join(dst, rel)
		switch {
		case d.IsDir():
			return os.MkdirAll(to, 0o755)
		case d.Type()&fs.ModeSymlink != 0:
			target, err := os.Readlink(p)
			if err != nil {
				return err
			}
			return os.Symlink(target, to)
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		in, err := os.Open(p)
		if err != nil {
			return err
		}
		defer in.Close()
		out, err := os.OpenFile(to, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, info.Mode().Perm())
		if err != nil {
			return err
		}
		if _, err := io.Copy(out, in); err != nil {
			out.Close()
			return err
		}
		return out.Close()
	})
}
