package mcp

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/kratejs/krate/packages/compiler/internal/astprint"
)

// listIgnoreDirs are skipped by list_files to keep results useful (and to avoid
// walking huge dependency trees).
var listIgnoreDirs = map[string]bool{
	".git": true, "node_modules": true, "dist": true, "build": true,
	"out": true, ".next": true, ".cache": true, "coverage": true,
}

// maxListFiles caps list_files output so a huge tree cannot flood context.
const maxListFiles = 2000

// toolListFiles lists project files, honouring .gitignore-style ignores for the
// common heavy directories and the MCP policy's deny list.
func (s *Service) toolListFiles(ctx context.Context, args map[string]any) (ToolResult, *rpcError) {
	dir := argString(args, "dir")
	glob := argString(args, "glob")
	recursive := argBool(args, "recursive", true)
	includeDenied := argBool(args, "includeDenied", false)

	base := s.root
	if dir != "" {
		abs, _, err := s.resolveProjectPath(dir)
		if err != nil {
			return ErrorResult(err.Error()), nil
		}
		base = abs
	}

	var files []string
	truncated := false
	walkErr := filepath.WalkDir(base, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if p != base && listIgnoreDirs[d.Name()] {
				return filepath.SkipDir
			}
			if !recursive && p != base {
				return filepath.SkipDir
			}
			return nil
		}
		rel, relErr := filepath.Rel(s.root, p)
		if relErr != nil {
			return nil
		}
		relSlash := filepath.ToSlash(rel)
		if !includeDenied && s.policy != nil && s.policy.denied(relSlash) {
			return nil
		}
		if glob != "" && !globMatch(glob, relSlash) {
			return nil
		}
		if len(files) >= maxListFiles {
			truncated = true
			return filepath.SkipAll
		}
		files = append(files, relSlash)
		return nil
	})
	if walkErr != nil {
		return ErrorResult("listing files: " + walkErr.Error()), nil
	}
	sort.Strings(files)
	return JSONResult(map[string]any{
		"files":     files,
		"count":     len(files),
		"truncated": truncated,
	})
}

// toolReadFile reads a project file's text. Binary files and files denied by the
// policy are refused.
func (s *Service) toolReadFile(ctx context.Context, args map[string]any) (ToolResult, *rpcError) {
	target := argString(args, "path")
	if target == "" {
		return ErrorResult("provide path (project-relative)"), nil
	}
	abs, rel, err := s.resolveProjectPath(target)
	if err != nil {
		return ErrorResult(err.Error()), nil
	}
	if s.policy != nil && s.policy.denied(rel) {
		return ErrorResult("refusing to read " + rel + ": it " + s.policy.deniedReason(rel)), nil
	}
	data, readErr := os.ReadFile(abs)
	if readErr != nil {
		if os.IsNotExist(readErr) {
			return ErrorResult("file not found: " + rel), nil
		}
		return ErrorResult("reading " + rel + ": " + readErr.Error()), nil
	}
	if isBinary(data) {
		return JSONResult(map[string]any{
			"path": rel, "binary": true, "bytes": len(data),
			"message": "binary file; content not returned",
		})
	}
	const maxBytes = 512 * 1024
	content := string(data)
	truncated := false
	if len(content) > maxBytes {
		content = content[:maxBytes]
		truncated = true
	}
	return JSONResult(map[string]any{
		"path":      rel,
		"language":  languageForPath(rel),
		"bytes":     len(data),
		"truncated": truncated,
		"content":   content,
	})
}

// toolCreateFile creates a new file. It refuses to overwrite an existing file
// (use edit_page for edits), validates the write policy, and parse-gates
// editable source.
func (s *Service) toolCreateFile(ctx context.Context, args map[string]any) (ToolResult, *rpcError) {
	target := argString(args, "path")
	content := argString(args, "content")
	apply := argBool(args, "apply", false)
	if target == "" {
		return ErrorResult("provide path"), nil
	}
	abs, rel, err := s.resolveEditTarget(target)
	if err != nil {
		return ErrorResult(err.Error()), nil
	}
	if _, statErr := os.Stat(abs); statErr == nil {
		return ErrorResult("file already exists: " + rel + " (use edit_page to modify it)"), nil
	}
	if gate := parseGate(rel, content); gate != "" {
		return ErrorResult(gate), nil
	}
	diff := unifiedDiff(rel, "", content)
	if !apply {
		return TextResult("Dry run (would create " + rel + "; apply: true to write).\n\n" + diff), nil
	}
	if err := os.MkdirAll(filepath.Dir(abs), 0755); err != nil {
		return ErrorResult("creating the directory for " + rel + ": " + err.Error()), nil
	}
	if err := os.WriteFile(abs, []byte(content), 0644); err != nil {
		return ErrorResult("writing " + rel + ": " + err.Error()), nil
	}
	return TextResult("Created " + rel + "\n\n" + diff), nil
}

// toolDeleteFile deletes a project file.
func (s *Service) toolDeleteFile(ctx context.Context, args map[string]any) (ToolResult, *rpcError) {
	target := argString(args, "path")
	apply := argBool(args, "apply", false)
	if target == "" {
		return ErrorResult("provide path"), nil
	}
	abs, rel, err := s.resolveEditTarget(target)
	if err != nil {
		return ErrorResult(err.Error()), nil
	}
	info, statErr := os.Stat(abs)
	if statErr != nil {
		return ErrorResult("file not found: " + rel), nil
	}
	if info.IsDir() {
		return ErrorResult("refusing to delete a directory: " + rel + " (files only)"), nil
	}
	if !apply {
		return TextResult("Dry run (would delete " + rel + "; apply: true to delete)."), nil
	}
	if err := os.Remove(abs); err != nil {
		return ErrorResult("deleting " + rel + ": " + err.Error()), nil
	}
	return TextResult("Deleted " + rel), nil
}

// toolMoveFile moves or renames a project file.
func (s *Service) toolMoveFile(ctx context.Context, args map[string]any) (ToolResult, *rpcError) {
	from := argString(args, "from")
	to := argString(args, "to")
	apply := argBool(args, "apply", false)
	if from == "" || to == "" {
		return ErrorResult("provide from and to"), nil
	}
	fromAbs, fromRel, err := s.resolveEditTarget(from)
	if err != nil {
		return ErrorResult(err.Error()), nil
	}
	toAbs, toRel, err := s.resolveEditTarget(to)
	if err != nil {
		return ErrorResult(err.Error()), nil
	}
	if _, statErr := os.Stat(fromAbs); statErr != nil {
		return ErrorResult("source not found: " + fromRel), nil
	}
	if _, statErr := os.Stat(toAbs); statErr == nil {
		return ErrorResult("destination already exists: " + toRel), nil
	}
	if !apply {
		return TextResult("Dry run (would move " + fromRel + " -> " + toRel + "; apply: true to move)."), nil
	}
	if err := os.MkdirAll(filepath.Dir(toAbs), 0755); err != nil {
		return ErrorResult("creating the destination directory: " + err.Error()), nil
	}
	if err := os.Rename(fromAbs, toAbs); err != nil {
		return ErrorResult("moving " + fromRel + " -> " + toRel + ": " + err.Error()), nil
	}
	return TextResult("Moved " + fromRel + " -> " + toRel), nil
}

// createComponentKinds are the accepted component tiers for create_component.
var createComponentKinds = map[string]bool{"client": true, "static": true, "server": true}

// toolCreateComponent scaffolds a Krate component in the project's components
// directory (default src/components), optionally with a companion CSS file.
func (s *Service) toolCreateComponent(ctx context.Context, args map[string]any) (ToolResult, *rpcError) {
	name := argString(args, "name")
	kind := argString(args, "kind")
	dir := argString(args, "dir")
	withCss := argBool(args, "withCss", false)
	apply := argBool(args, "apply", false)
	if name == "" {
		return ErrorResult("provide name (PascalCase, e.g. StatusBadge)"), nil
	}
	if !isComponentName(name) {
		return ErrorResult("invalid component name " + strconvQuote(name) + ": use PascalCase letters/digits"), nil
	}
	if kind == "" {
		kind = "client"
	}
	if !createComponentKinds[kind] {
		return ErrorResult("kind must be one of: client, static, server"), nil
	}
	if dir == "" {
		dir = "src/components"
	}
	base := filepath.ToSlash(strings.TrimSuffix(dir, "/"))
	tsxRel := base + "/" + kebab(name) + "/" + kebab(name) + ".tsx"
	cssRel := base + "/" + kebab(name) + "/" + kebab(name) + ".css"

	tsxAbs, tsxRelNorm, err := s.resolveEditTarget(tsxRel)
	if err != nil {
		return ErrorResult(err.Error()), nil
	}
	if _, statErr := os.Stat(tsxAbs); statErr == nil {
		return ErrorResult("component already exists: " + tsxRelNorm), nil
	}
	tsxSrc := componentScaffold(name, kind, withCss, cssRel)
	if gate := parseGate(tsxRelNorm, tsxSrc); gate != "" {
		return ErrorResult(gate), nil
	}

	diff := unifiedDiff(tsxRelNorm, "", tsxSrc)
	if !apply {
		return TextResult("Dry run (would create " + tsxRelNorm + "; apply: true to write).\n\n" + diff), nil
	}
	if err := os.MkdirAll(filepath.Dir(tsxAbs), 0755); err != nil {
		return ErrorResult("creating the directory: " + err.Error()), nil
	}
	if err := os.WriteFile(tsxAbs, []byte(tsxSrc), 0644); err != nil {
		return ErrorResult("writing " + tsxRelNorm + ": " + err.Error()), nil
	}
	created := []string{tsxRelNorm}
	if withCss {
		cssAbs, cssRelNorm, err := s.resolveEditTarget(cssRel)
		if err == nil {
			if err := os.WriteFile(cssAbs, []byte(componentCSS(name)), 0644); err == nil {
				created = append(created, cssRelNorm)
			}
		}
	}
	return TextResult("Created " + strings.Join(created, ", ") + "\n\n" + diff), nil
}

// parseGate returns a non-empty message when an editable source file would fail
// to parse after the change.
func parseGate(rel, content string) string {
	if !isEditablePageExt(rel) {
		return ""
	}
	if _, perrs, _ := astprint.Parse(content); len(perrs) > 0 {
		msgs := make([]string, 0, len(perrs))
		for _, e := range perrs {
			msgs = append(msgs, e.Error())
		}
		return "refusing to write " + rel + ": it would not parse:\n" + strings.Join(msgs, "\n")
	}
	return ""
}

func isBinary(data []byte) bool {
	if len(data) == 0 {
		return false
	}
	probe := data
	if len(probe) > 8192 {
		probe = probe[:8192]
	}
	for _, b := range probe {
		if b == 0 {
			return true
		}
	}
	return !utf8.Valid(probe)
}

// languageForPath returns a display language for a file path extension.
func languageForPath(p string) string {
	switch strings.ToLower(filepath.Ext(p)) {
	case ".ts":
		return "typescript"
	case ".tsx":
		return "tsx"
	case ".js":
		return "javascript"
	case ".jsx":
		return "jsx"
	case ".css":
		return "css"
	case ".json":
		return "json"
	case ".md":
		return "markdown"
	case ".mdx":
		return "mdx"
	case ".html":
		return "html"
	case ".yaml", ".yml":
		return "yaml"
	case ".go":
		return "go"
	case ".svg":
		return "svg"
	}
	return "text"
}

// isComponentName reports whether name is a valid PascalCase component name.
func isComponentName(name string) bool {
	if name == "" {
		return false
	}
	for i, r := range name {
		if r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z' {
			continue
		}
		if (r >= '0' && r <= '9') && i > 0 {
			continue
		}
		return false
	}
	return true
}

// kebab converts PascalCase to kebab-case (StatusBadge -> status-badge).
func kebab(name string) string {
	var b strings.Builder
	for i, r := range name {
		if r >= 'A' && r <= 'Z' {
			if i > 0 {
				b.WriteByte('-')
			}
			b.WriteRune(r + ('a' - 'A'))
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

// componentScaffold renders a starter component source for the given tier.
func componentScaffold(name, kind string, withCss bool, cssRel string) string {
	var b strings.Builder
	cssBase := strings.TrimSuffix(filepath.Base(cssRel), ".css")
	className := "krate-" + kebab(name)
	if kind == "static" {
		fmt.Fprintf(&b, "// %s is a static component: it renders to HTML with no client JS.\n", name)
		if withCss {
			fmt.Fprintf(&b, "import './%s.css';\n\n", cssBase)
		}
		fmt.Fprintf(&b, "export function %s(props: { children?: any }) {\n", name)
		fmt.Fprintf(&b, "  return <div class=\"%s\">{props.children}</div>;\n}\n", className)
		fmt.Fprintf(&b, "\nexport default %s;\n", name)
		return b.String()
	}
	if kind == "server" {
		fmt.Fprintf(&b, "// %s is a server component: it runs on the server (SSR/ISR) and\n", name)
		fmt.Fprintf(&b, "// ships no client JavaScript.\n")
		if withCss {
			fmt.Fprintf(&b, "import './%s.css';\n\n", cssBase)
		}
		fmt.Fprintf(&b, "export function %s(props: { children?: any }) {\n", name)
		fmt.Fprintf(&b, "  return <section class=\"%s\">{props.children}</section>;\n}\n", className)
		fmt.Fprintf(&b, "\nexport default %s;\n", name)
		return b.String()
	}
	// client (default)
	fmt.Fprintf(&b, "import { createSignal } from '@krate/runtime';\n")
	if withCss {
		fmt.Fprintf(&b, "import './%s.css';\n", cssBase)
	}
	fmt.Fprintf(&b, "\nexport function %s(props: { children?: any }) {\n", name)
	fmt.Fprintf(&b, "  const [active, setActive] = createSignal(false);\n")
	fmt.Fprintf(&b, "  return (\n")
	fmt.Fprintf(&b, "    <div class=\"%s\" onClick={() => setActive(!active())}>\n", className)
	fmt.Fprintf(&b, "      {props.children}\n")
	fmt.Fprintf(&b, "    </div>\n")
	fmt.Fprintf(&b, "  );\n}\n")
	fmt.Fprintf(&b, "\nexport default %s;\n", name)
	return b.String()
}

func componentCSS(name string) string {
	return "." + "krate-" + kebab(name) + " {\n  /* styles */\n}\n"
}
