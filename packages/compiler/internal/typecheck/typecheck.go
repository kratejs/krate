// Package typecheck runs the project's TypeScript compiler for `krate
// typecheck`. tsc reports tier decorators (`@server function Stats() {}`) as
// errors, since they are valid only to Krate, so those are removed from its
// output. Everything else, including a misspelled decorator, is kept.
package typecheck

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

var (
	diagRe = regexp.MustCompile(`^(.+)\((\d+),(\d+)\): error TS(\d+): (.*)$`)
	tierRe = regexp.MustCompile(`^[ \t]*@(server|static|runtime|client)(?:[ \t]|\r?$)`)
)

// isTierDiagnostic reports whether a tsc error comes from a tier decorator:
// "Decorators are not valid here", `static` being a reserved word, or "Cannot
// find name" for the decorator's own name.
func isTierDiagnostic(code, message, line string, col int) bool {
	m := tierRe.FindStringSubmatch(line)
	if m == nil {
		return false
	}
	start := strings.IndexByte(line, '@') + 1
	if col != start && col != start+1 {
		return false
	}
	switch code {
	case "1206", "1212", "1213", "1214":
		return true
	case "2304":
		return strings.Contains(message, "'"+m[1]+"'")
	}
	return false
}

// Filter drops the tier decorator errors from tsc's plain output (`--pretty
// false`) and returns the rest, with the number of errors left and dropped.
// read returns a file's contents.
func Filter(output string, read func(path string) (string, error)) (kept string, remaining, dropped int) {
	files := map[string][]string{}
	lineOf := func(path string, n int) string {
		lines, ok := files[path]
		if !ok {
			if src, err := read(path); err == nil {
				lines = strings.Split(src, "\n")
			}
			files[path] = lines
		}
		if n >= 1 && n <= len(lines) {
			return lines[n-1]
		}
		return ""
	}
	var b strings.Builder
	skipping := false
	for _, line := range strings.SplitAfter(output, "\n") {
		// Continuation lines of an error are indented.
		if strings.HasPrefix(line, " ") {
			if !skipping {
				b.WriteString(line)
			}
			continue
		}
		skipping = false
		trimmed := strings.TrimRight(line, "\r\n")
		if m := diagRe.FindStringSubmatch(trimmed); m != nil {
			n, _ := strconv.Atoi(m[2])
			col, _ := strconv.Atoi(m[3])
			if isTierDiagnostic(m[4], m[5], lineOf(m[1], n), col) {
				dropped++
				skipping = true
				continue
			}
		}
		if strings.Contains(trimmed, "error TS") {
			remaining++
		}
		b.WriteString(line)
	}
	return b.String(), remaining, dropped
}

// FindTSC returns the TypeScript compiler installed for root, looking in
// node_modules of root and its parents.
func FindTSC(root string) string {
	for dir := root; ; dir = filepath.Dir(dir) {
		p := filepath.Join(dir, "node_modules", "typescript", "bin", "tsc")
		if _, err := os.Stat(p); err == nil {
			return p
		}
		if filepath.Dir(dir) == dir {
			return ""
		}
	}
}

// ErrNoTSC means the project has no TypeScript compiler installed.
var ErrNoTSC = errors.New("typescript is not installed in this project")

// Run type-checks the project at root. It returns tsc's filtered output and
// whether the project is free of errors.
func Run(root string) (string, bool, error) {
	tsc := FindTSC(root)
	if tsc == "" {
		return "", false, ErrNoTSC
	}
	cmd := exec.Command("node", tsc, "--noEmit", "--pretty", "false")
	cmd.Dir = root
	out, err := cmd.CombinedOutput()
	var exit *exec.ExitError
	if err != nil && !errors.As(err, &exit) {
		return "", false, err
	}
	kept, remaining, dropped := Filter(string(out), func(path string) (string, error) {
		if !filepath.IsAbs(path) {
			path = filepath.Join(root, path)
		}
		data, err := os.ReadFile(path)
		return string(data), err
	})
	if err == nil {
		return kept, true, nil
	}
	return kept, remaining == 0 && dropped > 0, nil
}
