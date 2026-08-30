package build

import (
	"strings"

	"krate-compiler/internal/ast"
)

// RenderMode defines how a page is rendered.
type RenderMode int

const (
	// RenderSSG — pre-rendered at build time (default, current behavior).
	RenderSSG RenderMode = iota
	// RenderSSR — rendered on every request via Node.js runtime.
	RenderSSR
	// RenderISR — pre-rendered at build, revalidated in background after `revalidate` seconds.
	RenderISR
	// RenderStreaming — SSR with Suspense-based streaming via HTTP chunked encoding.
	RenderStreaming
)

func (m RenderMode) String() string {
	switch m {
	case RenderSSR:
		return "ssr"
	case RenderISR:
		return "isr"
	case RenderStreaming:
		return "streaming"
	default:
		return "ssg"
	}
}

// PageMeta holds per-page rendering metadata extracted at build time.
type PageMeta struct {
	Route     string     `json:"route"`               // URL path (e.g. "/about")
	Source    string     `json:"source"`              // source file relative to project root
	Mode      RenderMode `json:"mode"`                // ssg, ssr, isr, streaming
	Revalidate int       `json:"revalidate,omitempty"` // ISR revalidation interval in seconds
}

// detectRenderMode inspects a page's AST and source to determine its rendering
// mode. Returns the mode and revalidation interval.
func detectRenderMode(prog *ast.Program, source string) (RenderMode, int) {
	// Explicit opt-in: export const config = { streaming: true }
	for _, stmt := range prog.Body {
		if exp, ok := stmt.(*ast.ExportStmt); ok {
			// Detect: export const config = { streaming: true }
			if vs, ok := exp.Declaration.(*ast.VarStmt); ok {
				for _, d := range vs.Decls {
					if d.Name == "config" && d.Init != nil {
						if obj, ok := d.Init.(*ast.ObjectExpr); ok {
							for _, prop := range obj.Properties {
								if prop.Key == "streaming" {
									if lit, ok := prop.Value.(*ast.Literal); ok && lit.Kind == ast.BoolLit && lit.Value == "true" {
										return RenderStreaming, 0
									}
								}
							}
						}
					}
				}
			}
		}
	}

	// Using <Suspense> implies a streaming boundary — resolved fallbacks are
	// swapped in at request time, so such pages cannot be statically baked.
	if hasSuspenseBoundaries(source) {
		return RenderStreaming, 0
	}

	return RenderSSG, 0
}

// hasSuspenseBoundaries checks if the page's source contains <Suspense> components.
// This is a source-level check (string scan) since the AST may not have been fully
// parsed when we need this info for streaming detection.
func hasSuspenseBoundaries(source string) bool {
	return strings.Contains(source, "<Suspense") || strings.Contains(source, "<suspense")
}
