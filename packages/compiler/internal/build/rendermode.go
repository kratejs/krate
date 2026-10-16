package build

import (
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
	// dynamicParams is tri-state: nil = unset (inherit global), otherwise the
	// page-level override.
	dynamicParams *bool
}

// detectRenderMode inspects a page's AST to determine its rendering mode.
// Returns the mode and revalidation interval.
// Precedence: explicit `isr` > `ssr` > `streaming` (config or <Suspense>).
// ISR/SSR were previously unreachable - nothing in the build ever produced
// them - so wiring the config keys here is what makes them real.
func detectRenderMode(prog *ast.Program) (RenderMode, int) {
	cfg := parsePageConfig(prog)

	switch {
	case cfg.isr:
		if cfg.revalidate <= 0 {
			cfg.revalidate = defaultISRRevalidate
		}
		return RenderISR, cfg.revalidate
	case cfg.ssr:
		return RenderSSR, 0
	case cfg.streaming:
		return RenderStreaming, 0
	}

	// Using <Suspense> implies a streaming boundary - resolved fallbacks are
	// swapped in at request time, so such pages cannot be statically baked.
	if usesSuspense(prog) {
		return RenderStreaming, 0
	}

	return RenderSSG, 0
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
				if d.Name == "dynamicParams" && d.Init != nil {
					if b, ok := boolLiteral(d.Init); ok {
						v := b
						cfg.dynamicParams = &v
					}
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
				}
			}
		}
	}
	return cfg
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

	var walkStmt func([]ast.Stmt)
	var walkExpr func(ast.Expr)
	var walkJSXChild func(ast.JSXChild)

	walkExpr = func(e ast.Expr) {
		if found || e == nil {
			return
		}
		switch v := e.(type) {
		case *ast.CallExpr:
			walkExpr(v.Callee)
			for _, a := range v.Args {
				walkExpr(a)
			}
		case *ast.MemberExpr:
			walkExpr(v.Object)
			walkExpr(v.Property)
		case *ast.BinaryExpr:
			walkExpr(v.Left)
			walkExpr(v.Right)
		case *ast.UnaryExpr:
			walkExpr(v.Arg)
		case *ast.ConditionalExpr:
			walkExpr(v.Test)
			walkExpr(v.Consequent)
			walkExpr(v.Alternate)
		case *ast.TemplateExpr:
			for _, p := range v.Parts {
				walkExpr(p)
			}
		case *ast.ArrowFn:
			walkStmt(v.Body)
		case *ast.AwaitExpr:
			walkExpr(v.Arg)
		case *ast.DynamicImport:
			walkExpr(v.Arg)
		case *ast.ImportMetaExpr:
		case *ast.NewExpr:
			walkExpr(v.Callee)
			for _, a := range v.Args {
				walkExpr(a)
			}
		case *ast.ObjectExpr:
			for _, p := range v.Properties {
				if p.Value != nil {
					walkExpr(p.Value)
				}
			}
		case *ast.ArrayExpr:
			for _, el := range v.Elements {
				walkExpr(el)
			}
		case *ast.JSXElement:
			if v.Opening != nil {
				name := v.Opening.Name
				if name == "Suspense" || name == "suspense" {
					found = true
					return
				}
				for _, attr := range v.Opening.Attributes {
					if attr.Value != nil {
						walkExpr(attr.Value)
					}
				}
			}
			for _, c := range v.Children {
				walkJSXChild(c)
			}
		case *ast.JSXFragment:
			for _, c := range v.Children {
				walkJSXChild(c)
			}
		}
	}

	walkJSXChild = func(c ast.JSXChild) {
		if found {
			return
		}
		switch ch := c.(type) {
		case *ast.JSXExprContainer:
			walkExpr(ch.Expression)
		case *ast.JSXElementChild:
			walkExpr(ch.Element)
		case *ast.JSXFragmentChild:
			walkExpr(ch.Fragment)
		}
	}

	walkStmt = func(stmts []ast.Stmt) {
		if found {
			return
		}
		for _, stmt := range stmts {
			if found {
				return
			}
			switch s := stmt.(type) {
			case *ast.ExprStmt:
				walkExpr(s.Expression)
			case *ast.VarStmt:
				for _, d := range s.Decls {
					if d.Init != nil {
						walkExpr(d.Init)
					}
				}
			case *ast.FnDecl:
				walkStmt(s.Body)
			case *ast.ReturnStmt:
				if s.Value != nil {
					walkExpr(s.Value)
				}
			case *ast.BlockStmt:
				walkStmt(s.Body)
			case *ast.IfStmt:
				walkExpr(s.Test)
				walkStmt(s.Consequent)
				walkStmt(s.Alternate)
			case *ast.ForStmt:
				if s.Init != nil {
					walkStmt([]ast.Stmt{s.Init})
				}
				if s.Test != nil {
					walkExpr(s.Test)
				}
				walkStmt(s.Body)
			case *ast.ForInStmt:
				walkExpr(s.Right)
				walkStmt(s.Body)
			case *ast.WhileStmt:
				walkExpr(s.Test)
				walkStmt(s.Body)
			case *ast.DoWhileStmt:
				walkStmt(s.Body)
				walkExpr(s.Test)
			case *ast.SwitchStmt:
				walkExpr(s.Discriminant)
				for _, c := range s.Cases {
					if c.Test != nil {
						walkExpr(c.Test)
					}
					walkStmt(c.Body)
				}
			case *ast.TryStmt:
				walkStmt(s.Body)
				if s.Catch != nil {
					walkStmt(s.Catch.Body)
				}
				walkStmt(s.Finally)
			case *ast.ThrowStmt:
				walkExpr(s.Value)
			case *ast.ExportStmt:
				if s.Declaration != nil {
					walkStmt([]ast.Stmt{s.Declaration})
				}
			}
		}
	}

	walkStmt(prog.Body)
	return found
}
