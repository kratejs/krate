// Package pluginapi implements the paired capability surface Workstream B
// exposes to both JavaScript and Go plugins: resolving/reading files against the
// project root and node_modules, and writing static assets into the project
// root. Every function anchors paths to the project root and rejects traversal.
// The JavaScript capability host functions (internal/plugin) and the public Go
// SDK (pluginsdk) both delegate here so the security guards live in one place.
package pluginapi

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/kratejs/krate/packages/compiler/internal/resolver"
)

// WithinRoot reports whether path p is the project root itself or lives inside
// it. Both paths are cleaned first; the result is case-sensitive, matching the
// underlying filesystem semantics.
func WithinRoot(root, p string) bool {
	root = filepath.Clean(root)
	p = filepath.Clean(p)
	if p == root {
		return true
	}
	rel, err := filepath.Rel(root, p)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel)
}

// SafeJoin joins rel onto root and verifies the result is inside root both
// lexically and after resolving symlinks. The final path component may not
// exist yet (writes), so symlinks are resolved on the deepest existing
// ancestor - this catches a symlinked directory inside the project that points
// outside it before a file is written through it.
// It returns the absolute path to use for the operation.
func SafeJoin(root, rel string) (string, error) {
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return "", fmt.Errorf("resolving root: %w", err)
	}
	joined := filepath.Clean(filepath.Join(rootAbs, filepath.FromSlash(rel)))
	if !WithinRoot(rootAbs, joined) {
		return "", fmt.Errorf("path %q escapes the project root", rel)
	}

	// Resolve symlinks on the deepest existing ancestor of the target and
	// re-check containment. This prevents a symlinked directory within the
	// root from redirecting a write outside it.
	probe := joined
	for {
		resolved, rerr := filepath.EvalSymlinks(probe)
		if rerr == nil {
			if !WithinRootEval(rootAbs, resolved) {
				return "", fmt.Errorf("path %q escapes the project root via a symlink", rel)
			}
			break
		}
		parent := filepath.Dir(probe)
		if parent == probe {
			break
		}
		probe = parent
	}
	return joined, nil
}

// WithinRootEval is WithinRoot but also resolves symlinks on both root and p so
// a symlinked root (e.g. /tmp -> /private/tmp on macOS) does not cause a false
// negative. Non-existent paths are compared lexically.
func WithinRootEval(root, p string) bool {
	if r, err := filepath.EvalSymlinks(root); err == nil {
		root = r
	}
	if r, err := filepath.EvalSymlinks(p); err == nil {
		p = r
	}
	return WithinRoot(root, p)
}

// isPathLike reports whether spec is an explicit relative path ("./", "../")
// rather than a bare package specifier.
func isPathLike(spec string) bool {
	return strings.HasPrefix(spec, "./") || strings.HasPrefix(spec, "../") ||
		strings.HasPrefix(spec, ".\\") || strings.HasPrefix(spec, "..\\")
}

// Resolve turns a plugin file specifier into an absolute path.
//   - Absolute paths are cleaned and must live under root.
//   - Explicit relative paths ("./", "../") resolve against root and must stay
//     inside it.
//
// - Bare specifiers resolve through node_modules (walking up from root) -
// including subpaths like "pkg/styles/x.css" and "@scope/pkg/src/y" - and
//
//	otherwise fall back to a root-relative file.
//
// The resolved path must exist. Traversal outside root is rejected.
func Resolve(root, spec string) (string, error) {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return "", fmt.Errorf("empty path")
	}

	if filepath.IsAbs(spec) {
		p := filepath.Clean(spec)
		if !WithinRoot(root, p) {
			return "", fmt.Errorf("path %q resolves outside the project root", spec)
		}
		if !resolver.Exists(p) {
			return "", fmt.Errorf("file not found: %s", spec)
		}
		return p, nil
	}

	if isPathLike(spec) {
		p := filepath.Clean(filepath.Join(root, spec))
		if !WithinRoot(root, p) {
			return "", fmt.Errorf("path %q escapes the project root", spec)
		}
		if !resolver.Exists(p) {
			return "", fmt.Errorf("file not found: %s", spec)
		}
		return p, nil
	}

	if p := resolver.NodeModuleDir(root, spec); p != "" {
		return p, nil
	}

	p := filepath.Clean(filepath.Join(root, spec))
	if WithinRoot(root, p) && resolver.Exists(p) {
		return p, nil
	}
	return "", fmt.Errorf("could not resolve %q", spec)
}

// ReadFile resolves spec (see Resolve) and returns its contents as a string.
func ReadFile(root, spec string) (string, error) {
	p, err := Resolve(root, spec)
	if err != nil {
		return "", err
	}
	data, err := os.ReadFile(p)
	if err != nil {
		return "", fmt.Errorf("reading %s: %w", spec, err)
	}
	return string(data), nil
}

// WriteFileToRoot writes content to rel, interpreted relative to the project
// root (e.g. "public/logo.svg"), creating parent directories as needed. A path
// that escapes root is rejected.
func WriteFileToRoot(root, rel string, content []byte) error {
	if strings.TrimSpace(rel) == "" {
		return fmt.Errorf("empty path")
	}
	p := filepath.Clean(filepath.Join(root, rel))
	if !WithinRoot(root, p) {
		return fmt.Errorf("path %q escapes the project root", rel)
	}
	if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
		return fmt.Errorf("creating directory for %s: %w", rel, err)
	}
	if err := os.WriteFile(p, content, 0644); err != nil {
		return fmt.Errorf("writing %s: %w", rel, err)
	}
	return nil
}
