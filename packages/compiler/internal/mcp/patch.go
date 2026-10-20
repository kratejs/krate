package mcp

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

var hunkHeaderRe = regexp.MustCompile(`^@@ -([0-9]+)(?:,([0-9]+))? \+([0-9]+)(?:,([0-9]+))? @@`)

type patchHunk struct {
	OldStart int
	OldCount int
	NewStart int
	NewCount int
	Lines    []string // prefixed lines: " ", "-", "+"
}

type patchFile struct {
	OldPath string
	NewPath string
	Hunks   []patchHunk
}

func (p patchFile) deleteOnly() bool { return p.NewPath == "" }
func (p patchFile) createOnly() bool { return p.OldPath == "" }
func (p patchFile) rename() bool {
	return p.OldPath != "" && p.NewPath != "" && p.OldPath != p.NewPath
}

// parseUnifiedPatch parses the common unified-diff form used by git and
// apply_patch. It deliberately supports only exact patches: headers may carry
// timestamps, but hunk context is never fuzz-matched.
func parseUnifiedPatch(text string) ([]patchFile, error) {
	lines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	var files []patchFile
	var current *patchFile
	var hunk *patchHunk

	flushHunk := func() {
		if hunk != nil && current != nil {
			current.Hunks = append(current.Hunks, *hunk)
			hunk = nil
		}
	}
	flushFile := func() {
		flushHunk()
		if current != nil {
			if current.OldPath == "" && current.NewPath == "" {
				current = nil
				return
			}
			files = append(files, *current)
			current = nil
		}
	}

	for _, line := range lines {
		switch {
		case strings.HasPrefix(line, "diff --git "):
			flushFile()
		case strings.HasPrefix(line, "--- "):
			flushFile()
			current = &patchFile{OldPath: patchHeaderPath(line[4:])}
			hunk = nil
		case strings.HasPrefix(line, "+++ "):
			if current == nil {
				return nil, fmt.Errorf("patch has a +++ header without a --- header")
			}
			current.NewPath = patchHeaderPath(line[4:])
		case strings.HasPrefix(line, "@@ "):
			if current == nil {
				return nil, fmt.Errorf("patch has a hunk without file headers")
			}
			flushHunk()
			m := hunkHeaderRe.FindStringSubmatch(line)
			if m == nil {
				return nil, fmt.Errorf("invalid hunk header: %q", line)
			}
			h := &patchHunk{OldStart: atoiDefault(m[1], 0), OldCount: atoiDefault(m[2], 1), NewStart: atoiDefault(m[3], 0), NewCount: atoiDefault(m[4], 1)}
			hunk = h
		case hunk != nil:
			if line == "" || strings.HasPrefix(line, `\ No newline at end of file`) || strings.HasPrefix(line, "index ") || strings.HasPrefix(line, "new file mode ") || strings.HasPrefix(line, "deleted file mode ") {
				continue
			}
			if line[0] != ' ' && line[0] != '+' && line[0] != '-' {
				return nil, fmt.Errorf("invalid hunk line (expected space, +, or -): %q", line)
			}
			hunk.Lines = append(hunk.Lines, line)
		}
	}
	flushFile()
	if len(files) == 0 {
		return nil, fmt.Errorf("patch contains no file changes")
	}
	return files, nil
}

func patchHeaderPath(raw string) string {
	raw = strings.TrimSpace(raw)
	if i := strings.IndexByte(raw, '\t'); i >= 0 {
		raw = raw[:i]
	}
	raw = strings.TrimSpace(raw)
	if raw == "/dev/null" || raw == "" {
		return ""
	}
	raw = strings.ReplaceAll(raw, "\\", "/")
	if strings.HasPrefix(raw, "a/") || strings.HasPrefix(raw, "b/") {
		raw = raw[2:]
	}
	return strings.TrimPrefix(raw, "./")
}

func atoiDefault(s string, def int) int {
	if s == "" {
		return def
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return def
	}
	return n
}

// applyFilePatch applies a parsed file patch to original text. It requires every
// context/removal line to match exactly.
func applyFilePatch(original string, p patchFile) (string, error) {
	if p.createOnly() && original != "" {
		return "", fmt.Errorf("patch creates %s but the file already exists", p.NewPath)
	}
	if p.deleteOnly() && original == "" {
		return "", fmt.Errorf("patch deletes %s but the file is empty or missing", p.OldPath)
	}
	if p.rename() && original == "" {
		return "", fmt.Errorf("patch renames %s but the source is missing", p.OldPath)
	}

	hadTrailing := strings.HasSuffix(original, "\n")
	oldLines := splitLines(original)
	var out []string
	cursor := 0

	for _, h := range p.Hunks {
		start := h.OldStart
		if start > 0 {
			start--
		}
		if start < cursor || start > len(oldLines) {
			return "", fmt.Errorf("hunk starts at line %d outside the file", h.OldStart)
		}
		out = append(out, oldLines[cursor:start]...)
		cursor = start
		oldSeen, newSeen := 0, 0
		for _, line := range h.Lines {
			switch line[0] {
			case ' ':
				if cursor >= len(oldLines) || oldLines[cursor] != line[1:] {
					return "", fmt.Errorf("context mismatch at line %d", cursor+1)
				}
				out = append(out, line[1:])
				cursor++
				oldSeen++
				newSeen++
			case '-':
				if cursor >= len(oldLines) || oldLines[cursor] != line[1:] {
					return "", fmt.Errorf("removal mismatch at line %d", cursor+1)
				}
				cursor++
				oldSeen++
			case '+':
				out = append(out, line[1:])
				newSeen++
			}
		}
		if oldSeen != h.OldCount || newSeen != h.NewCount {
			return "", fmt.Errorf("hunk line counts do not match the header (old %d/%d, new %d/%d)", oldSeen, h.OldCount, newSeen, h.NewCount)
		}
	}
	out = append(out, oldLines[cursor:]...)

	result := strings.Join(out, "\n")
	if len(out) > 0 && (hadTrailing || p.createOnly()) {
		result += "\n"
	}
	return result, nil
}
