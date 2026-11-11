// Package goenv picks the environment for go commands run in a native app's
// Go module.
//
// A go.work in a parent directory (a developer's workspace, for example the
// Krate monorepo checkout) is active for every module below it. When it does
// not use the app's module, go resolves the app's packages in the workspace's
// modules and reports that the main module does not contain them. The app's
// own go.mod is then the one that counts, so its go commands run with
// GOWORK=off. A workspace that does use the app's module is left alone.
package goenv

import (
	"bufio"
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
)

var (
	mu    sync.Mutex
	cache = map[string]bool{}
)

// Env returns the environment for a go command run in root: os.Environ(),
// PWD set to root, and GOWORK=off when an active go.work does not use the
// module rooted at root.
//
// os/exec sets PWD itself only when Cmd.Env is nil. Without it the go command
// falls back to getcwd, which resolves symlinks (macOS's /var is
// /private/var), and the directories it reports no longer match the paths the
// caller built.
func Env(root string) []string {
	env := os.Environ()
	if abs, err := filepath.Abs(root); err == nil {
		env = append(env, "PWD="+abs)
	}
	if Excluded(root) {
		env = append(env, "GOWORK=off")
	}
	return env
}

// WithPWD returns env with PWD set to dir (absolute), for a go command whose
// caller chose its own environment; see Env.
func WithPWD(env []string, dir string) []string {
	if abs, err := filepath.Abs(dir); err == nil {
		dir = abs
	}
	return append(env[:len(env):len(env)], "PWD="+dir)
}

// Excluded reports whether an active go.work leaves out the module at root.
func Excluded(root string) bool {
	abs, err := filepath.Abs(root)
	if err != nil {
		abs = root
	}
	mu.Lock()
	defer mu.Unlock()
	if v, ok := cache[abs]; ok {
		return v
	}
	v := excluded(abs)
	cache[abs] = v
	return v
}

func excluded(root string) bool {
	cmd := exec.Command("go", "env", "GOWORK")
	cmd.Dir = root
	out, err := cmd.Output()
	if err != nil {
		return false
	}
	work := strings.TrimSpace(string(out))
	if work == "" || work == "off" {
		return false
	}
	data, err := os.ReadFile(work)
	if err != nil {
		return false
	}
	for _, dir := range Uses(data) {
		if !filepath.IsAbs(dir) {
			dir = filepath.Join(filepath.Dir(work), dir)
		}
		if sameDir(dir, root) {
			return false
		}
	}
	return true
}

// Uses returns the directories of the use directives of a go.work file, in
// both the single-line and the block form.
func Uses(data []byte) []string {
	var out []string
	block := false
	sc := bufio.NewScanner(bytes.NewReader(data))
	for sc.Scan() {
		line := sc.Text()
		if i := strings.Index(line, "//"); i >= 0 {
			line = line[:i]
		}
		line = strings.TrimSpace(line)
		switch {
		case line == "":
		case block && line == ")":
			block = false
		case block:
			out = append(out, unquote(line))
		case line == "use (" || line == "use(":
			block = true
		case strings.HasPrefix(line, "use ") || strings.HasPrefix(line, "use\t"):
			out = append(out, unquote(strings.TrimSpace(line[len("use"):])))
		}
	}
	return out
}

func unquote(s string) string {
	if len(s) >= 2 && (s[0] == '"' && s[len(s)-1] == '"' || s[0] == '`' && s[len(s)-1] == '`') {
		return s[1 : len(s)-1]
	}
	return s
}

func sameDir(a, b string) bool {
	if filepath.Clean(a) == filepath.Clean(b) {
		return true
	}
	sa, err1 := os.Stat(a)
	sb, err2 := os.Stat(b)
	return err1 == nil && err2 == nil && os.SameFile(sa, sb)
}
