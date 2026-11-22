package build

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/kratejs/krate/packages/compiler/internal/css"
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
	fromConfigFile := file != ""
	line, col := 1, 1
	if !fromConfigFile {
		file, line, col = b.findTailwindDirective(found)
	}
	hint := "set `tailwind: { enabled: true }` in krate.config.ts, or remove the Tailwind config and directives if you do not use it"
	switch {
	case b.Cfg.Tailwind.EnabledSet:
		hint = "krate.config.ts sets `tailwind.enabled: false`; remove it (or set it to true) to use Tailwind, or remove the Tailwind directives"
	case !fromConfigFile && file != "" && b.DevMode:
		// A project stylesheet with a directive turns Tailwind on when the
		// builder starts, so this one was added while the dev server ran.
		hint = "restart the dev server to turn Tailwind on, or set `tailwind: { enabled: true }` in krate.config.ts"
	}
	b.printWarningsOnce([]diag.Diagnostic{{
		File:     file,
		Line:     line,
		Col:      col,
		Severity: diag.SeverityWarning,
		Code:     diag.CodeTailwindDisabled,
		Message:  fmt.Sprintf("%s found but Tailwind is not enabled, so no Tailwind CSS (preflight or utilities) is generated", found),
		Hint:     hint,
	}})
}

// findTailwindDirective returns the project CSS file, line and column of the
// first occurrence of the directive text, so the warning points at it. It
// returns an empty file when no stylesheet under the project holds it.
func (b *Builder) findTailwindDirective(found string) (file string, line, col int) {
	walkProjectCSS(b.Root, b.Cfg.OutDir, func(path, text string) bool {
		at := strings.Index(text, found)
		if at < 0 {
			return false
		}
		before := text[:at]
		file = b.relPath(path)
		line = strings.Count(before, "\n") + 1
		col = len(before) - strings.LastIndexByte(before, '\n')
		return true
	})
	if file == "" {
		return "", 0, 0
	}
	return file, line, col
}

// projectUsesTailwind reports whether a stylesheet of the project holds a
// Tailwind directive (`@import "tailwindcss"` or `@tailwind`).
func projectUsesTailwind(root, outDir string) bool {
	used := false
	walkProjectCSS(root, outDir, func(_, text string) bool {
		used = css.TailwindDirectiveUse(text) != ""
		return used
	})
	return used
}

// walkProjectCSS calls fn with each .css file under root, outside
// node_modules, .git, .krate and outDir, until fn returns true.
func walkProjectCSS(root, outDir string, fn func(path, text string) bool) {
	skipDirs := map[string]bool{"node_modules": true, ".git": true, ".krate": true}
	if outDir != "" && !filepath.IsAbs(outDir) {
		outDir, _ = filepath.Abs(outDir)
	}
	done := false
	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if done {
			return filepath.SkipAll
		}
		if err != nil {
			return filepath.SkipDir
		}
		if d.IsDir() {
			if path != root && (skipDirs[d.Name()] || path == outDir) {
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
		done = fn(path, string(data))
		return nil
	})
}
