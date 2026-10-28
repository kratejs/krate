package build

import (
	"fmt"
	"strconv"

	"github.com/kratejs/krate/packages/compiler/ast"
)

// RenderMode defines how a page is rendered.
type RenderMode int

const (
	// RenderSSG - pre-rendered at build time (default, current behavior).
	RenderSSG RenderMode = iota
	// RenderSSR - rendered on every request via Node.js runtime.
	RenderSSR
	// RenderISR - pre-rendered at build, revalidated in background after `revalidate` seconds.
	RenderISR
	// RenderStreaming - SSR with Suspense-based streaming via HTTP chunked encoding.
	RenderStreaming
)

// defaultISRRevalidate is the fallback revalidation interval when a page opts
// into ISR without an explicit `revalidate` seconds value.
const defaultISRRevalidate = 60

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
	Route      string     `json:"route"`                // URL path (e.g. "/about")
	Source     string     `json:"source"`               // source file relative to project root
	Mode       RenderMode `json:"mode"`                 // ssg, ssr, isr, streaming
	Revalidate int        `json:"revalidate,omitempty"` // ISR revalidation interval in seconds
	// DynamicParams is false when a dynamic route must NOT render params other
	// than those returned by generateStaticParams (unknown params 404). True by
	// default; set via `export const dynamicParams = false` or static output mode.
	DynamicParams bool `json:"dynamicParams"`
	// StaticOnly marks a dynamic route whose set of valid params is closed, so
	// the build should not emit a `[param]` fallback template.
	StaticOnly bool `json:"staticOnly,omitempty"`
}

// pageConfig holds the merged page-level render config extracted from
// `export const config = { ... }` plus sibling const exports.
type pageConfig struct {
	streaming  bool
	ssr        bool
	isr        bool
	revalidate int
	// lang overrides the document <html lang> for this page (e.g. docs locales).
	lang string
	// dynamicParams is tri-state: nil = unset (inherit global), otherwise the
	// page-level override.
	dynamicParams *bool

	// Next.js-style segment exports: `export const revalidate = N | false`
	// and `export const dynamic = "..."`.
	hasRevalidateExport bool
	revalidateExport    int  // seconds; 0 means request-time (ssr)
	revalidateForever   bool // `revalidate = false`: cache indefinitely (static)
	dynamic             string
	dynamicPos          ast.Pos
	// issues are unsupported segment-export values, reported as warnings.
	issues []pageConfigIssue
}

// pageConfigIssue is an unsupported page config value found while parsing.
type pageConfigIssue struct {
	pos     ast.Pos
	message string
	hint    string
}

// detectRenderMode inspects a page's AST to determine its rendering mode.
// Returns the mode and revalidation interval.
// Precedence: explicit `isr` > `ssr` > `streaming` (config or <Suspense>).
// ISR/SSR were previously unreachable - nothing in the build ever produced
// them - so wiring the config keys here is what makes them real.
func detectRenderMode(prog *ast.Program) (RenderMode, int) {
	cfg := parsePageConfig(prog)
	if mode, reval, ok := explicitRenderMode(cfg); ok {
		return mode, reval
	}

	// Using <Suspense> implies a streaming boundary - resolved fallbacks are
	// swapped in at request time, so such pages cannot be statically baked.
	if usesSuspense(prog) {
		return RenderStreaming, 0
	}

	return RenderSSG, 0
}

// explicitRenderMode resolves the render mode a page asks for explicitly.
// The Krate `config` object wins; otherwise the Next.js-style segment exports
// apply: `dynamic = "force-dynamic"` or `revalidate = 0` render per request,
// `revalidate = N` is ISR, and `dynamic = "force-static"`/`"error"` or
// `revalidate = false` pin the page static. ok is false when the page states
// nothing and the mode is inferred (Suspense, data-cache usage, default SSG).
func explicitRenderMode(cfg pageConfig) (RenderMode, int, bool) {
	switch {
	case cfg.isr:
		if cfg.revalidate <= 0 {
			cfg.revalidate = defaultISRRevalidate
		}
		return RenderISR, cfg.revalidate, true
	case cfg.ssr:
		return RenderSSR, 0, true
	case cfg.streaming:
		return RenderStreaming, 0, true
	}
	if cfg.dynamic == "force-dynamic" {
		return RenderSSR, 0, true
	}
	if cfg.hasRevalidateExport {
		switch {
		case cfg.revalidateForever:
			return RenderSSG, 0, true
		case cfg.revalidateExport == 0:
			if cfg.dynamic == "force-static" || cfg.dynamic == "error" {
				return RenderSSG, 0, true
			}
			return RenderSSR, 0, true
		default:
			return RenderISR, cfg.revalidateExport, true
		}
	}
	if cfg.dynamic == "force-static" || cfg.dynamic == "error" {
		return RenderSSG, 0, true
	}
	return RenderSSG, 0, false
}

// detectPageLang returns a page-level `export const lang` (or `config.lang`)
// override for the document <html lang>, or "" when unset.
func detectPageLang(prog *ast.Program) string {
	return parsePageConfig(prog).lang
}

// parsePageConfig reads the page-level render config from
// `export const config = { ... }` plus sibling `export const dynamicParams`.
func parsePageConfig(prog *ast.Program) pageConfig {
	var cfg pageConfig
	for _, stmt := range prog.Body {
		// Assignment form: `generateStaticParams.dynamicParams = false`.
		if es, ok := stmt.(*ast.ExprStmt); ok {
			if bin, ok := es.Expression.(*ast.BinaryExpr); ok && bin.Op == "=" {
				if mem, ok := bin.Left.(*ast.MemberExpr); ok {
					if prop, ok := mem.Property.(*ast.Identifier); ok && prop.Name == "dynamicParams" {
						if id, ok := mem.Object.(*ast.Identifier); ok && id.Name == "generateStaticParams" {
							if b, ok := boolLiteral(bin.Right); ok {
								v := b
								cfg.dynamicParams = &v
							}
						}
					}
				}
			}
		}
		exp, ok := stmt.(*ast.ExportStmt)
		if !ok {
			continue
		}
		// Sibling exports: `export const dynamicParams = false`.
		if vs, ok := exp.Declaration.(*ast.VarStmt); ok {
			for _, d := range vs.Decls {
				if d.Init == nil {
					continue
				}
				switch d.Name {
				case "dynamicParams":
					if b, ok := boolLiteral(d.Init); ok {
						v := b
						cfg.dynamicParams = &v
					}
				case "lang":
					if lit, ok := d.Init.(*ast.Literal); ok && lit.Kind == ast.StringLit {
						cfg.lang = lit.Value
					}
				case "revalidate":
					parseRevalidateExport(&cfg, d.Init, exp.Position)
				case "dynamic":
					parseDynamicExport(&cfg, d.Init, exp.Position)
				}
			}
		}
		vs, ok := exp.Declaration.(*ast.VarStmt)
		if !ok {
			continue
		}
		for _, d := range vs.Decls {
			if d.Name != "config" || d.Init == nil {
				continue
			}
			obj, ok := d.Init.(*ast.ObjectExpr)
			if !ok {
				continue
			}
			for _, prop := range obj.Properties {
				switch prop.Key {
				case "streaming":
					cfg.streaming = boolPropTrue(prop.Value)
				case "ssr":
					cfg.ssr = boolPropTrue(prop.Value)
				case "isr":
					cfg.isr = boolPropTrue(prop.Value)
				case "revalidate":
					if lit, ok := prop.Value.(*ast.Literal); ok && lit.Kind == ast.NumberLit {
						if n, err := strconv.Atoi(lit.Value); err == nil {
							cfg.revalidate = n
						}
					}
				case "dynamicParams":
					if b, ok := boolLiteral(prop.Value); ok {
						v := b
						cfg.dynamicParams = &v
					}
				case "lang":
					if lit, ok := prop.Value.(*ast.Literal); ok && lit.Kind == ast.StringLit {
						cfg.lang = lit.Value
					}
				}
			}
		}
	}
	return cfg
}

// parseRevalidateExport reads `export const revalidate = N | false`. N must
// be a constant number of seconds (simple arithmetic such as 60 * 60 folds).
func parseRevalidateExport(cfg *pageConfig, init ast.Expr, pos ast.Pos) {
	if b, ok := boolLiteral(init); ok {
		if b {
			cfg.issues = append(cfg.issues, pageConfigIssue{pos: pos,
				message: "`export const revalidate = true` is not a valid revalidate value",
				hint:    "use a number of seconds (ISR), 0 (render per request) or false (cache indefinitely)"})
			return
		}
		cfg.hasRevalidateExport = true
		cfg.revalidateForever = true
		return
	}
	if id, ok := init.(*ast.Identifier); ok && id.Name == "Infinity" {
		cfg.hasRevalidateExport = true
		cfg.revalidateForever = true
		return
	}
	n, ok := constNumber(init)
	if !ok || n < 0 {
		cfg.issues = append(cfg.issues, pageConfigIssue{pos: pos,
			message: "`export const revalidate` must be a constant number of seconds or false; the value is ignored and the page renders statically",
			hint:    "write a literal such as `export const revalidate = 60`, `0` for per-request rendering, or `false` to cache indefinitely"})
		return
	}
	cfg.hasRevalidateExport = true
	cfg.revalidateExport = int(n)
}

// parseDynamicExport reads `export const dynamic = "auto" | "force-dynamic" |
// "force-static" | "error"`.
func parseDynamicExport(cfg *pageConfig, init ast.Expr, pos ast.Pos) {
	lit, ok := init.(*ast.Literal)
	if ok && lit.Kind == ast.StringLit {
		switch lit.Value {
		case "auto", "force-dynamic", "force-static", "error":
			cfg.dynamic = lit.Value
			cfg.dynamicPos = pos
			return
		}
	}
	val := "a non-literal value"
	if ok {
		val = fmt.Sprintf("%q", lit.Value)
	}
	cfg.issues = append(cfg.issues, pageConfigIssue{pos: pos,
		message: fmt.Sprintf("`export const dynamic` has unsupported %s; it is ignored", val),
		hint:    `use "auto", "force-dynamic" (render per request), "force-static" or "error" (static)`})
}

// constNumber folds a constant numeric expression (literals combined with
// + - * / and parentheses). It returns false for anything else.
func constNumber(e ast.Expr) (float64, bool) {
	switch v := e.(type) {
	case *ast.Literal:
		if v.Kind != ast.NumberLit {
			return 0, false
		}
		n, err := strconv.ParseFloat(v.Value, 64)
		if err != nil {
			return 0, false
		}
		return n, true
	case *ast.ParenExpr:
		return constNumber(v.Expr)
	case *ast.BinaryExpr:
		l, ok := constNumber(v.Left)
		if !ok {
			return 0, false
		}
		r, ok := constNumber(v.Right)
		if !ok {
			return 0, false
		}
		switch v.Op {
		case "+":
			return l + r, true
		case "-":
			return l - r, true
		case "*":
			return l * r, true
		case "/":
			if r == 0 {
				return 0, false
			}
			return l / r, true
		}
	}
	return 0, false
}

// boolLiteral returns the boolean value of a bool literal expression.
func boolLiteral(v ast.Expr) (bool, bool) {
	lit, ok := v.(*ast.Literal)
	if !ok || lit.Kind != ast.BoolLit {
		return false, false
	}
	return lit.Value == "true", true
}

func boolPropTrue(v ast.Expr) bool {
	lit, ok := v.(*ast.Literal)
	return ok && lit.Kind == ast.BoolLit && lit.Value == "true"
}

// dynamicParamsAllowed reports whether a page (typically a dynamic route) may
// render params outside those returned by generateStaticParams.
// Resolution: a page-level `export const dynamicParams` always wins; otherwise
// the global static output mode (`output: "static"`) closes the set, and SSG
// is the default (dynamic fallback allowed). Request-time modes (ssr/isr/
// streaming) are inherently dynamic and default to allowing params.
func dynamicParamsAllowed(prog *ast.Program, staticMode bool) bool {
	cfg := parsePageConfig(prog)
	if cfg.dynamicParams != nil {
		return *cfg.dynamicParams
	}
	return !staticMode
}

// usesSuspense reports whether the page AST contains a <Suspense> JSX element
// anywhere (nested inside functions, conditionals, arrays, etc.). This is an
// AST-based check - a prior string scan (`strings.Contains(source, "<Suspense")`)
// could misfire on comments and string literals.
func usesSuspense(prog *ast.Program) bool {
	found := false
	walkProgramExprs(prog, func(e ast.Expr) bool {
		if el, ok := e.(*ast.JSXElement); ok && el.Opening != nil {
			if el.Opening.Name == "Suspense" || el.Opening.Name == "suspense" {
				found = true
				return false
			}
		}
		return true
	})
	return found
}
