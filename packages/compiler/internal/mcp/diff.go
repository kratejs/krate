package mcp

import (
	"fmt"
	"os"
	"strings"
)

// unifiedDiff renders a minimal unified-style diff between old and new text.
// It is intentionally line-oriented and self-contained (no diff dependency).
// An empty old text represents a new file.
func unifiedDiff(path, oldText, newText string) string {
	if oldText == newText {
		return "(no changes)\n"
	}
	oldLines := splitLines(oldText)
	newLines := splitLines(newText)

	// Trim common prefix/suffix for a compact hunk.
	prefix := 0
	for prefix < len(oldLines) && prefix < len(newLines) && oldLines[prefix] == newLines[prefix] {
		prefix++
	}
	suffix := 0
	for suffix < len(oldLines)-prefix && suffix < len(newLines)-prefix &&
		oldLines[len(oldLines)-1-suffix] == newLines[len(newLines)-1-suffix] {
		suffix++
	}

	oldHunk := oldLines[prefix : len(oldLines)-suffix]
	newHunk := newLines[prefix : len(newLines)-suffix]

	var b strings.Builder
	if oldText == "" {
		fmt.Fprintf(&b, "--- /dev/null\n+++ %s\n", path)
	} else {
		fmt.Fprintf(&b, "--- a/%s\n+++ b/%s\n", path, path)
	}
	oldStart := prefix + 1
	newStart := prefix + 1
	fmt.Fprintf(&b, "@@ -%d,%d +%d,%d @@\n", oldStart, len(oldHunk), newStart, len(newHunk))
	for _, l := range oldHunk {
		b.WriteString("-" + l + "\n")
	}
	for _, l := range newHunk {
		b.WriteString("+" + l + "\n")
	}
	return b.String()
}

func splitLines(s string) []string {
	if s == "" {
		return nil
	}
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.TrimSuffix(s, "\n")
	return strings.Split(s, "\n")
}

// captureStdout runs fn with os.Stdout redirected to a pipe and returns the
// captured text. It is used around build/check calls because the compiler
// prints progress to stdout, which would otherwise corrupt the JSON-RPC stream.
func captureStdout(fn func() error) (string, error) {
	orig := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		// Cannot redirect; run directly rather than failing the operation.
		return "", fn()
	}
	os.Stdout = w

	done := make(chan string, 1)
	go func() {
		var b strings.Builder
		buf := make([]byte, 32*1024)
		for {
			n, rerr := r.Read(buf)
			if n > 0 {
				b.Write(buf[:n])
			}
			if rerr != nil {
				break
			}
		}
		done <- b.String()
	}()

	runErr := fn()
	_ = w.Close()
	os.Stdout = orig
	out := <-done
	_ = r.Close()
	return out, runErr
}
