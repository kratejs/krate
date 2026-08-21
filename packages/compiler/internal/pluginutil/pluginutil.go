// Package pluginutil provides shared utilities for krate's built-in plugins
// (file walking, HTML template building, path helpers). Community plugins are
// written in JavaScript and do not use this package.
package pluginutil

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// HTMLTemplate provides a simple, fast HTML template builder.
// Unlike text/template or html/template, it uses a builder pattern
// that avoids reflection for maximum speed.
type HTMLTemplate struct {
	b strings.Builder
}

// NewHTML creates a new HTML template builder.
func NewHTML() *HTMLTemplate { return &HTMLTemplate{} }

// W writes a string to the template.
func (h *HTMLTemplate) W(s string) { h.b.WriteString(s) }

// Wf writes a formatted string to the template.
func (h *HTMLTemplate) Wf(format string, args ...interface{}) { fmt.Fprintf(&h.b, format, args...) }

// Esc writes a string with HTML escaping to the template.
func (h *HTMLTemplate) Esc(s string) {
	for _, r := range s {
		switch r {
		case '&':
			h.b.WriteString("&amp;")
		case '<':
			h.b.WriteString("&lt;")
		case '>':
			h.b.WriteString("&gt;")
		case '"':
			h.b.WriteString("&quot;")
		case '\'':
			h.b.WriteString("&#39;")
		default:
			h.b.WriteRune(r)
		}
	}
}

// String returns the built template string.
func (h *HTMLTemplate) String() string { return h.b.String() }

// Reset clears the template builder for reuse.
func (h *HTMLTemplate) Reset() { h.b.Reset() }

// File is a single output file produced by a plugin.
type File struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

// FileWriter is a helper for collecting multiple files.
type FileWriter struct {
	files []File
}

// NewFileWriter creates a FileWriter.
func NewFileWriter() *FileWriter {
	return &FileWriter{}
}

// WriteFile adds a file. The path is relative to the output directory.
func (fw *FileWriter) WriteFile(path, content string) {
	fw.files = append(fw.files, File{Path: path, Content: content})
}

// Files returns the accumulated files.
func (fw *FileWriter) Files() []File { return fw.files }

// ResolvePath creates any needed directories and returns the absolute path.
func ResolvePath(outputDir, relPath string) string {
	return filepath.Join(outputDir, strings.TrimLeft(relPath, "/\\"))
}

// WalkMD walks a directory and calls fn for each .md and .mdx file found.
// dirPath is the absolute path, relPath is relative to the base.
func WalkMD(root string, fn func(absPath, relPath string) error) error {
	return filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		ext := strings.ToLower(filepath.Ext(path))
		if ext != ".md" && ext != ".mdx" {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		return fn(path, filepath.ToSlash(rel))
	})
}
