package main

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/kratejs/krate/packages/compiler/internal/build"
)

// doctorFix is a repair `krate doctor --fix` can apply.
type doctorFix struct {
	// Name is a short label, for example "gitignore".
	Name string
	// Problem says what is wrong; it is printed with and without --fix.
	Problem string
	// Apply repairs it and returns a one-line description of what it did.
	Apply func() (string, error)
}

// runDoctorFixes lists the fixes (dry run) or applies them. It returns how
// many failed to apply.
func runDoctorFixes(w io.Writer, fixes []doctorFix, apply bool) int {
	if len(fixes) == 0 {
		if apply {
			fmt.Fprintf(w, "\n  Nothing to fix.\n")
		}
		return 0
	}
	if !apply {
		fmt.Fprintf(w, "\n  %d issue(s) can be fixed automatically; run `krate doctor --fix`:\n", len(fixes))
		for _, f := range fixes {
			fmt.Fprintf(w, "    - %s: %s\n", f.Name, f.Problem)
		}
		return 0
	}
	fmt.Fprintf(w, "\n  Fixing %d issue(s):\n", len(fixes))
	failed := 0
	for _, f := range fixes {
		done, err := f.Apply()
		if err != nil {
			failed++
			fmt.Fprintf(w, "    %sx%s %s: %v\n", cRed, cReset, f.Name, err)
			continue
		}
		fmt.Fprintf(w, "    %s+%s %s: %s\n", cGreen, cReset, f.Name, done)
	}
	return failed
}

// versionMismatchFix aligns the project's @krate/* packages with the running
// binary when they are older. A newer installed package is not downgraded: the
// binary is what is out of date, and `krate upgrade --global` is the repair.
func versionMismatchFix(root string, deps []installedKratePackage, binary string, run func(string, upgradeCommand) error) *doctorFix {
	if binary == "dev" {
		return nil
	}
	var older []string
	for _, p := range deps {
		if p.Name != krateScope+"core" {
			continue
		}
		if c, ok := compareSemver(p.Version, binary); ok && c < 0 {
			older = append(older, p.Name)
		}
	}
	if len(older) == 0 {
		return nil
	}
	return &doctorFix{
		Name:    "versions",
		Problem: fmt.Sprintf("installed @krate/core is older than this binary (v%s)", binary),
		Apply: func() (string, error) {
			var out bytes.Buffer
			err := executeUpgrade(&out, upgradeOptions{
				Root:   root,
				Target: binary,
				PM:     packageManagerFor(root),
				Run:    run,
			})
			if err != nil {
				return "", err
			}
			return fmt.Sprintf("moved @krate packages to v%s", binary), nil
		},
	}
}

// gitignoreCovers reports whether the lines of a .gitignore already ignore
// the directory name (".krate", "dist"), in the spellings people use.
func gitignoreCovers(lines []string, dir string) bool {
	dir = strings.Trim(filepath.ToSlash(dir), "/")
	for _, l := range lines {
		l = strings.TrimSpace(l)
		switch strings.TrimPrefix(l, "/") {
		case dir, dir + "/", dir + "/*", dir + "/**":
			return true
		}
	}
	return false
}

// insideGitRepo reports whether root, or a parent directory, holds a .git entry.
func insideGitRepo(root string) bool {
	for dir := root; ; {
		if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
			return true
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return false
		}
		dir = parent
	}
}

// gitignoreMissing returns the generated directories (relative to root) that a
// project's .gitignore does not list. It returns nothing outside a git
// repository.
func gitignoreMissing(root string, dirs []string) []string {
	if !insideGitRepo(root) {
		return nil
	}
	var lines []string
	if data, err := os.ReadFile(filepath.Join(root, ".gitignore")); err == nil {
		lines = strings.Split(string(data), "\n")
	}
	var missing []string
	for _, d := range dirs {
		if !gitignoreCovers(lines, d) {
			missing = append(missing, d)
		}
	}
	return missing
}

// appendGitignore adds entries to root/.gitignore, creating it when needed.
func appendGitignore(root string, entries []string) error {
	path := filepath.Join(root, ".gitignore")
	existing, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	var b bytes.Buffer
	b.Write(existing)
	if len(existing) > 0 && !bytes.HasSuffix(existing, []byte("\n")) {
		b.WriteByte('\n')
	}
	if len(existing) > 0 {
		b.WriteByte('\n')
	}
	b.WriteString("# krate\n")
	for _, e := range entries {
		b.WriteString(strings.Trim(filepath.ToSlash(e), "/") + "/\n")
	}
	return os.WriteFile(path, b.Bytes(), 0o644)
}

// gitignoreFix builds the fix for generated directories missing from
// .gitignore, or nil when there are none.
func gitignoreFix(root string, dirs []string) *doctorFix {
	missing := gitignoreMissing(root, dirs)
	if len(missing) == 0 {
		return nil
	}
	return &doctorFix{
		Name:    "gitignore",
		Problem: ".gitignore does not list " + strings.Join(missing, ", "),
		Apply: func() (string, error) {
			if err := appendGitignore(root, missing); err != nil {
				return "", err
			}
			return "added " + strings.Join(missing, ", ") + " to .gitignore", nil
		},
	}
}

// staleCacheFix removes a page cache left by another compiler version.
func staleCacheFix(root string) *doctorFix {
	stale, why := build.CacheStale(root)
	if !stale {
		return nil
	}
	return &doctorFix{
		Name:    "cache",
		Problem: "the .krate cache is stale (" + why + ")",
		Apply: func() (string, error) {
			dir := filepath.Join(root, ".krate", "cache")
			size := dirSizeOf(dir)
			if err := os.RemoveAll(dir); err != nil {
				return "", err
			}
			return "removed .krate/cache (" + build.FormatBytes(size) + ")", nil
		},
	}
}

func dirSizeOf(dir string) int64 {
	var total int64
	_ = filepath.WalkDir(dir, func(_ string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			if info, err := d.Info(); err == nil {
				total += info.Size()
			}
		}
		return nil
	})
	return total
}

// outDirIgnoreName returns the output directory relative to root when it is a
// plain directory inside the project, else "".
func outDirIgnoreName(root, outDir string) string {
	rel, err := filepath.Rel(root, outDir)
	if err != nil || rel == "." || strings.HasPrefix(rel, "..") || filepath.IsAbs(rel) {
		return ""
	}
	return filepath.ToSlash(rel)
}
