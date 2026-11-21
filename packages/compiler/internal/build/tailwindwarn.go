package build

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/kratejs/krate/packages/compiler/internal/diag"
)

// warnTailwindOff warns, once per build, that Tailwind input exists while
// Tailwind is not enabled. file is the config file when that was the trigger.
func (b *Builder) warnTailwindOff(file, found string) {
	const key = "tailwind-off"
	b.twMu.Lock()
	if b.twWarned == nil {
		b.twWarned = map[string]bool{}
	}
	seen := b.twWarned[key]
	b.twWarned[key] = true
	b.twMu.Unlock()
	if seen {
		return
	}
	line, col := 1, 1
	if file == "" {
		file, line, col = b.findTailwindDirective(found)
	}
	b.printWarningsOnce([]diag.Diagnostic{{
		File:     file,
		Line:     line,
		Col:      col,
		Severity: diag.SeverityWarning,
		Code:     diag.CodeTailwindDisabled,
		Message:  fmt.Sprintf("%s found but Tailwind is not enabled, so no utility CSS is generated and Tailwind directives are removed", found),
		Hint:     "set `tailwind: { enabled: true }` in krate.config.ts, or remove the Tailwind config and directives if you do not use it",
	}})
}

// findTailwindDirective returns the project CSS file, line and column of the
// first occurrence of the directive text, so the warning points at it. It
// returns an empty file when no stylesheet under the project holds it.
func (b *Builder) findTailwindDirective(found string) (file string, line, col int) {
	var skipDirs = map[string]bool{"node_modules": true, ".git": true, ".krate": true}
	outDir, _ := filepath.Abs(b.Cfg.OutDir)
	_ = filepath.WalkDir(b.Root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || file != "" {
			return filepath.SkipDir
		}
		if d.IsDir() {
			if skipDirs[d.Name()] || path == outDir {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".css") {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		if at := strings.Index(string(data), found); at >= 0 {
			text := string(data[:at])
			file = b.relPath(path)
			line = strings.Count(text, "\n") + 1
			col = len(text) - strings.LastIndexByte(text, '\n')
		}
		return nil
	})
	if file == "" {
		return "", 0, 0
	}
	return file, line, col
}
