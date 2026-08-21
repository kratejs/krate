package bundler

import (
	"strings"
)

// Directive types for server components
const (
	DirectiveNone     = ""
	DirectiveServer   = "@server"
	DirectiveRuntime  = "@runtime"
	DirectiveStatic   = "@static"
)

// HasDirective checks if source code starts with a server/runtime directive.
// The directive must be in the first non-blank, non-comment line.
// Valid formats:
//   - // @server
//   - // @runtime
//   - // @static
//   - /* @server */
//   - /** @runtime */
func HasDirective(source string, directive string) bool {
	lines := strings.SplitN(source, "\n", 10)
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}

		// Line comment: // @server
		if strings.HasPrefix(trimmed, "//") {
			comment := strings.TrimSpace(strings.TrimPrefix(trimmed, "//"))
			if strings.EqualFold(comment, directive) {
				return true
			}
			// Also support: // @server <optional description>
			if strings.HasPrefix(strings.ToLower(comment), directive+" ") {
				return true
			}
			// Stop scanning on non-directive comments (only check first comment block)
			if strings.HasPrefix(comment, "@") && !strings.EqualFold(comment, directive) {
				continue
			}
			// If first non-empty line is a comment without directive, stop
			if !strings.HasPrefix(comment, "@") {
				return false
			}
			continue
		}

		// Block comment: /* @server */ or /** @runtime */
		if strings.HasPrefix(trimmed, "/*") {
			end := strings.Index(trimmed, "*/")
			if end > 0 {
				inner := strings.TrimSpace(trimmed[2:end])
				// Strip leading * from /** ... */
				inner = strings.TrimLeft(inner, "*")
				inner = strings.TrimSpace(inner)
				if strings.EqualFold(inner, directive) {
					return true
				}
				if strings.HasPrefix(strings.ToLower(inner), directive+" ") {
					return true
				}
			}
			// Multi-line block comment: only check first comment block
			if !strings.Contains(trimmed, "*/") {
				return false
			}
			continue
		}

		// First non-comment line: stop scanning
		break
	}
	return false
}

// HasServerDirective checks if source has the @server directive
func HasServerDirective(source string) bool {
	return HasDirective(source, DirectiveServer)
}

// HasRuntimeDirective checks if source has the @runtime directive
func HasRuntimeDirective(source string) bool {
	return HasDirective(source, DirectiveRuntime)
}

// HasStaticDirective checks if source has the @static directive
func HasStaticDirective(source string) bool {
	return HasDirective(source, DirectiveStatic)
}

// IsServerComponentFile checks if a filename matches the *.server.tsx convention
func IsServerComponentFile(path string) bool {
	return strings.HasSuffix(path, ".server.tsx") || strings.HasSuffix(path, ".server.ts") ||
		strings.HasSuffix(path, ".server.jsx") || strings.HasSuffix(path, ".server.js")
}

// IsRuntimeComponentFile checks if a filename matches the *.runtime.tsx convention
func IsRuntimeComponentFile(path string) bool {
	return strings.HasSuffix(path, ".runtime.tsx") || strings.HasSuffix(path, ".runtime.ts") ||
		strings.HasSuffix(path, ".runtime.jsx") || strings.HasSuffix(path, ".runtime.js")
}

// IsStaticComponentFile checks if a filename matches the *.static.tsx convention
func IsStaticComponentFile(path string) bool {
	return strings.HasSuffix(path, ".static.tsx") || strings.HasSuffix(path, ".static.ts") ||
		strings.HasSuffix(path, ".static.jsx") || strings.HasSuffix(path, ".static.js")
}

// ComponentClass classifies a source file for server component handling.
// The 4-tier system:
//
//	ComponentClassStatic  — compile-time only, no client JS, no hydration
//	ComponentClassClient  — default: SSR/SSG + client hydration
//	ComponentClassServer  — build-time server evaluation, HTML output only
//	ComponentClassRuntime — serve-time evaluation via QuickJS, streamed to client
type ComponentClass int

const (
	ComponentClassStatic  ComponentClass = -1 // compile-time only, no interactivity
	ComponentClassClient   ComponentClass = 0 // default: client component
	ComponentClassServer   ComponentClass = 1 // build-time server component (@server)
	ComponentClassRuntime  ComponentClass = 2 // runtime server component (@runtime)
)

// IsStatic returns true if the component is compile-time only.
func (cc ComponentClass) IsStatic() bool { return cc == ComponentClassStatic }

// IsClient returns true if the component needs client hydration.
func (cc ComponentClass) IsClient() bool { return cc == ComponentClassClient }

// IsServer returns true if the component is build-time server.
func (cc ComponentClass) IsServer() bool { return cc == ComponentClassServer }

// IsRuntime returns true if the component is serve-time runtime.
func (cc ComponentClass) IsRuntime() bool { return cc == ComponentClassRuntime }

// String returns a human-readable name for the component class.
func (cc ComponentClass) String() string {
	switch cc {
	case ComponentClassStatic:
		return "static"
	case ComponentClassServer:
		return "server"
	case ComponentClassRuntime:
		return "runtime"
	default:
		return "client"
	}
}

// ClassifyComponent determines if a file is a server/runtime/static component.
// Checks directive in source code first, then file convention, then config lists,
// then directory membership. Last fallback is ComponentClassClient.
//
// Priority order:
//  1. Directive in source code (@server, @runtime, @static)
//  2. File convention (*.server.tsx, *.runtime.tsx, *.static.tsx)
//  3. Config name/path list match (serverComponents, runtimeComponents)
//  4. Directory membership (serverDirs, runtimeDirs)
//  5. Default → ComponentClassClient
func ClassifyComponent(source string, filePath string, serverComponents []string, runtimeComponents []string, serverDirs []string, runtimeDirs []string) ComponentClass {
	// 1. Directive check (highest priority)
	if HasServerDirective(source) {
		return ComponentClassServer
	}
	if HasRuntimeDirective(source) {
		return ComponentClassRuntime
	}
	if HasStaticDirective(source) {
		return ComponentClassStatic
	}

	// 2. File convention
	if IsServerComponentFile(filePath) {
		return ComponentClassServer
	}
	if IsRuntimeComponentFile(filePath) {
		return ComponentClassRuntime
	}
	if IsStaticComponentFile(filePath) {
		return ComponentClassStatic
	}

	// 3. Config list (component name match)
	baseName := extractComponentName(filePath)
	for _, name := range serverComponents {
		if name == baseName || name == filePath {
			return ComponentClassServer
		}
	}
	for _, name := range runtimeComponents {
		if name == baseName || name == filePath {
			return ComponentClassRuntime
		}
	}

	// 4. Directory membership — check if the file lives under any configured
	//    server or runtime directory. Paths are matched by prefix.
	if isPathInDirs(filePath, serverDirs) {
		return ComponentClassServer
	}
	if isPathInDirs(filePath, runtimeDirs) {
		return ComponentClassRuntime
	}

	return ComponentClassClient
}

// isPathInDirs checks if a file path is contained within any of the given directories.
// Both absolute and relative paths are handled. Directories that don't end with
// a separator are treated as prefixes (e.g. "src/components" matches
// "src/components/Foo.tsx").
func isPathInDirs(filePath string, dirs []string) bool {
	if len(dirs) == 0 {
		return false
	}
	normalized := toForwardSlash(filePath)
	for _, dir := range dirs {
		d := toForwardSlash(dir)
		if !strings.HasSuffix(d, "/") {
			d += "/"
		}
		if strings.HasPrefix(normalized, d) {
			return true
		}
		// Also check if the file's directory (not file itself) is the dir
		parent := normalized
		if idx := strings.LastIndex(parent, "/"); idx >= 0 {
			parent = parent[:idx+1]
		}
		if parent == d || strings.HasPrefix(d, parent) && strings.HasPrefix(normalized, d[:len(d)-1]) {
			return true
		}
	}
	return false
}

// toForwardSlash converts backslashes to forward slashes for cross-platform path comparison.
func toForwardSlash(s string) string {
	return strings.ReplaceAll(s, "\\", "/")
}

// extractComponentName extracts the component name from a file path.
// e.g., "/src/components/DataTable.server.tsx" → "DataTable"
// e.g., "/src/components/DataTable.tsx" → "DataTable"
func extractComponentName(filePath string) string {
	base := filePath
	// Get the filename without extension
	if idx := strings.LastIndex(base, "/"); idx >= 0 {
		base = base[idx+1:]
	}
	if idx := strings.LastIndex(base, "\\"); idx >= 0 {
		base = base[idx+1:]
	}
	// Remove extension
	for _, ext := range []string{".tsx", ".ts", ".jsx", ".js"} {
		if strings.HasSuffix(base, ext) {
			base = strings.TrimSuffix(base, ext)
			break
		}
	}
	// Remove server/runtime/static suffix
	base = strings.TrimSuffix(base, ".server")
	base = strings.TrimSuffix(base, ".runtime")
	base = strings.TrimSuffix(base, ".static")
	return base
}

// HasStaticReactivity checks if source code contains reactive primitives
// (createSignal, createEffect, createMemo, createResource, onMount, event handlers).
// Used for automatic static tier detection — components without these are candidates
// for static rendering (no client hydration needed).
func HasStaticReactivity(source string) bool {
	indicators := []string{
		"createSignal",
		"createEffect",
		"createMemo",
		"createResource",
		"onMount",
		"onCleanup",
		"createContext",
	}
	for _, indicator := range indicators {
		if strings.Contains(source, indicator) {
			return true
		}
	}
	// Check for event handler patterns: onClick, onInput, onChange, etc.
	// These are camelCase props starting with "on" followed by uppercase
	for i := 0; i < len(source)-2; i++ {
		if source[i] == 'o' && source[i+1] == 'n' && i+2 < len(source) && source[i+2] >= 'A' && source[i+2] <= 'Z' {
			// Verify it's actually a prop assignment (preceded by = or { or , or tab/space)
			if i > 0 {
				prev := source[i-1]
				if prev == '=' || prev == '{' || prev == ',' || prev == '\t' || prev == ' ' || prev == '(' {
					return true
				}
			} else {
				return true
			}
		}
	}
	return false
}
