package build

import (
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/kratejs/krate/packages/compiler/ast"
	"github.com/kratejs/krate/packages/compiler/internal/diag"
	"github.com/kratejs/krate/packages/compiler/internal/imageproc"
	"github.com/kratejs/krate/packages/compiler/internal/irtree"
)

// imageScope maps identifiers visible at an <Image> to their build-time
// literal initializer. A nil value marks a shadowing binding that is not a
// literal (a parameter or a computed local), so it never resolves.
type imageScope map[string]ast.Expr

func (s imageScope) child() imageScope {
	c := make(imageScope, len(s))
	for k, v := range s {
		c[k] = v
	}
	return c
}

// declare records the bindings of a statement list that sit directly in scope.
func (s imageScope) declare(stmts []ast.Stmt) {
	for _, st := range stmts {
		if ex, ok := st.(*ast.ExportStmt); ok && ex.Declaration != nil {
			st = ex.Declaration
		}
		vs, ok := st.(*ast.VarStmt)
		if !ok {
			continue
		}
		for _, d := range vs.Decls {
			if d.IsDestructuring {
				for _, n := range d.Names {
					s[n] = nil
				}
				if d.RestName != "" {
					s[d.RestName] = nil
				}
				continue
			}
			if d.Name == "" {
				continue
			}
			if vs.Kind == ast.VarConst && d.Init != nil && imageLiteral(d.Init) {
				s[d.Name] = d.Init
			} else {
				s[d.Name] = nil
			}
		}
	}
}

func imageLiteral(e ast.Expr) bool {
	switch x := e.(type) {
	case *ast.Literal:
		return x.Kind == ast.StringLit || x.Kind == ast.NumberLit || x.Kind == ast.BoolLit
	case *ast.TemplateExpr:
		for _, p := range x.Parts {
			if _, ok := p.(*ast.Literal); !ok {
				return false
			}
		}
		return true
	}
	return false
}

// shadowParams marks function parameters as non-literal bindings.
func (s imageScope) shadowParams(params []*ast.Param) {
	for _, p := range params {
		if p == nil {
			continue
		}
		if p.Name != "" {
			s[p.Name] = nil
		}
		for _, n := range patternNames(p.Pattern) {
			s[n] = nil
		}
	}
}

// patternNames extracts identifier names from a destructuring pattern's source.
func patternNames(pat string) []string {
	var out []string
	cur := strings.Builder{}
	flush := func() {
		if cur.Len() > 0 {
			out = append(out, cur.String())
			cur.Reset()
		}
	}
	for _, r := range pat {
		if r == '_' || r == '$' || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			cur.WriteRune(r)
		} else {
			flush()
		}
	}
	flush()
	return out
}

// imageDiag is a located <Image> problem found while lowering.
type imageDiag struct {
	pos  ast.Pos
	code diag.Code
	msg  string
	hint string
	warn bool
}

// imageLowerer lowers <Image> elements to <picture>. In early mode (the
// universal pass before folding) an element with a value it cannot resolve is
// left alone for the late hook; in late mode the same condition is an error.
type imageLowerer struct {
	b     *Builder
	early bool
	scope imageScope
	diags []imageDiag
}

func (l *imageLowerer) errAt(pos ast.Pos, code diag.Code, msg, hint string) {
	l.diags = append(l.diags, imageDiag{pos: pos, code: code, msg: msg, hint: hint})
}

// str resolves a literal-valued expression to its string form.
func (l *imageLowerer) str(e ast.Expr) (string, bool) {
	switch x := e.(type) {
	case *ast.Literal:
		switch x.Kind {
		case ast.StringLit, ast.NumberLit, ast.BoolLit:
			return x.Value, true
		}
	case *ast.Identifier:
		if v := l.scope[x.Name]; v != nil {
			return l.str(v)
		}
	case *ast.TemplateExpr:
		var sb strings.Builder
		for _, p := range x.Parts {
			lit, ok := p.(*ast.Literal)
			if !ok {
				return "", false
			}
			sb.WriteString(lit.Value)
		}
		return sb.String(), true
	}
	return "", false
}

// TransformUniversalImages lowers every <Image> in prog whose props are known
// at build time. Elements that depend on values resolved later (route params,
// folded content, list items) are left for the late lowering hook.
func (b *Builder) TransformUniversalImages(prog *ast.Program) {
	b.transformImages(prog)
}

// transformImages is TransformUniversalImages returning the diagnostics.
func (b *Builder) transformImages(prog *ast.Program) []imageDiag {
	l := &imageLowerer{b: b, early: true, scope: imageScope{}}
	l.scope.declare(prog.Body)
	for _, stmt := range prog.Body {
		l.walkStmt(stmt)
	}
	return l.diags
}

func (l *imageLowerer) walkStmts(stmts []ast.Stmt) {
	for _, s := range stmts {
		l.walkStmt(s)
	}
}

func (l *imageLowerer) walkStmt(node ast.Node) {
	if node == nil {
		return
	}
	switch n := node.(type) {
	case *ast.FnDecl:
		outer := l.scope
		l.scope = outer.child()
		l.scope.shadowParams(n.Params)
		l.scope.declare(n.Body)
		l.walkStmts(n.Body)
		l.scope = outer
	case *ast.ExportStmt:
		if n.Declaration != nil {
			l.walkStmt(n.Declaration)
		}
	case *ast.ReturnStmt:
		n.Value = l.expr(n.Value)
	case *ast.ExprStmt:
		n.Expression = l.expr(n.Expression)
	case *ast.IfStmt:
		l.walkStmts(n.Consequent)
		l.walkStmts(n.Alternate)
	case *ast.ForStmt:
		l.walkStmts(n.Body)
	case *ast.WhileStmt:
		l.walkStmts(n.Body)
	case *ast.VarStmt:
		for _, decl := range n.Decls {
			if decl.Init != nil {
				decl.Init = l.expr(decl.Init)
			}
		}
	}
}

func (l *imageLowerer) expr(expr ast.Expr) ast.Expr {
	switch e := expr.(type) {
	case nil:
		return nil
	case *ast.JSXElement:
		if e.Opening.Name == "Image" {
			if out := l.lower(e); out != nil {
				return out
			}
			return e
		}
		l.children(e.Children)
	case *ast.JSXFragment:
		l.children(e.Children)
	case *ast.CallExpr:
		for i, arg := range e.Args {
			e.Args[i] = l.expr(arg)
		}
	}
	return expr
}

func (l *imageLowerer) children(children []ast.JSXChild) {
	for i, child := range children {
		if el, ok := child.(*ast.JSXElementChild); ok {
			if out, ok := l.expr(el.Element).(*ast.JSXElement); ok {
				el.Element = out
				children[i] = el
			}
		}
	}
}

// lowerImageLate is the irtree hook: every prop is resolved by now, so an
// unresolvable one is an error rather than a reason to wait.
func (b *Builder) lowerImageLate(el *ast.JSXElement) (*ast.JSXElement, []irtree.HookDiag) {
	l := &imageLowerer{b: b, scope: imageScope{}}
	out := l.lower(el)
	var ds []irtree.HookDiag
	for _, d := range l.diags {
		ds = append(ds, irtree.HookDiag{Code: d.code, Msg: d.msg, Hint: d.hint, Warning: d.warn})
	}
	return out, ds
}

func (l *imageLowerer) intProp(attr *ast.JSXAttr) (int, bool) {
	val, ok := l.str(attr.Value)
	if !ok {
		if !l.early {
			l.errAt(attr.Position, diag.CodeImageDynamicProp,
				fmt.Sprintf("<Image %s must be a number known at build time", attr.Name),
				"Use a numeric literal or a const initialised with one; the build needs the size to generate variants.")
		}
		return 0, false
	}
	n, err := strconv.Atoi(strings.TrimSpace(val))
	if err != nil || n < 0 {
		l.errAt(attr.Position, diag.CodeImageDynamicProp,
			fmt.Sprintf("<Image %s=%q> is not a whole number of pixels", attr.Name, val),
			"Pass an integer such as width={640}; use CSS for percentage sizes.")
		return 0, false
	}
	return n, true
}

// lower compiles one <Image> into <picture>/<img>. It returns nil when the
// element must wait (early mode) or failed (diagnostics recorded).
func (l *imageLowerer) lower(orig *ast.JSXElement) *ast.JSXElement {
	b := l.b
	var src, loading, sizes, placeholder, className string
	var reqW, reqH, quality int
	var priority, hasAlt bool
	var altExpr, sizesExpr, classExpr, loadingExpr, styleAttr ast.Expr
	var extraAttrs []*ast.JSXAttr
	failed := false
	wait := false

	for _, attr := range orig.Opening.Attributes {
		if attr.Spread {
			extraAttrs = append(extraAttrs, attr)
			continue
		}
		if attr.Value == nil {
			switch attr.Name {
			case "priority":
				priority = true
			default:
				extraAttrs = append(extraAttrs, attr)
			}
			continue
		}
		switch attr.Name {
		case "src":
			v, ok := l.str(attr.Value)
			if !ok {
				if l.early {
					wait = true
				} else {
					l.errAt(attr.Position, diag.CodeImageDynamicProp, "<Image src> must be a string known at build time",
						"Use a string literal or a const initialised with one; images are optimised during the build.")
					failed = true
				}
				continue
			}
			src = v
		case "width":
			if n, ok := l.intProp(attr); ok {
				reqW = n
			} else if l.early {
				wait = true
			} else {
				failed = true
			}
		case "height":
			if n, ok := l.intProp(attr); ok {
				reqH = n
			} else if l.early {
				wait = true
			} else {
				failed = true
			}
		case "quality":
			if n, ok := l.intProp(attr); ok {
				quality = n
			} else if l.early {
				wait = true
			} else {
				failed = true
			}
		case "alt":
			hasAlt = true
			altExpr = attr.Value
		case "loading":
			if v, ok := l.str(attr.Value); ok {
				loading = v
			} else {
				loadingExpr = attr.Value
			}
		case "priority":
			v, ok := l.str(attr.Value)
			if !ok {
				if l.early {
					wait = true
				} else {
					l.errAt(attr.Position, diag.CodeImageDynamicProp, "<Image priority> must be true or false at build time", "Use priority or priority={true}.")
					failed = true
				}
				continue
			}
			priority = v == "true"
		case "sizes":
			if v, ok := l.str(attr.Value); ok {
				sizes = v
			} else {
				sizesExpr = attr.Value
			}
		case "placeholder":
			if v, ok := l.str(attr.Value); ok {
				placeholder = v
			}
		case "className":
			if v, ok := l.str(attr.Value); ok {
				className = v
			} else {
				classExpr = attr.Value
			}
		case "style":
			styleAttr = attr.Value
		default:
			extraAttrs = append(extraAttrs, attr)
		}
	}
	if wait && !failed {
		return nil
	}
	if failed {
		return nil
	}
	if src == "" {
		l.errAt(orig.Position, diag.CodeImageSource, "<Image> needs a src", `Add src="/photo.jpg" (a file under the public directory) or an allowed remote URL.`)
		return nil
	}
	if !hasAlt {
		l.diags = append(l.diags, imageDiag{pos: orig.Position, code: diag.CodeImageAlt, warn: true,
			msg:  "<Image> has no alt text",
			hint: `Add alt="describing the image", or alt="" if it is purely decorative.`})
	}

	imgCfg := b.Cfg.Images
	if quality <= 0 {
		quality = imgCfg.Quality
	}
	if sizes == "" && sizesExpr == nil {
		sizes = imgCfg.Sizes
	}
	if sizes == "" && sizesExpr == nil {
		sizes = "(max-width: 768px) 100vw, 50vw"
	}
	wantPlaceholder := imgCfg.PlaceholderEnabled()
	if placeholder != "" {
		wantPlaceholder = placeholder != "empty"
	}

	publicDir := b.Cfg.PublicDir
	if publicDir == "" {
		publicDir = filepath.Join(b.Root, "public")
	}
	// `src` is a URL, not a filesystem path: a leading "/" is the site root on
	// every platform and must not be read as an OS-absolute path.
	resolvedSrc := src
	if strings.HasPrefix(src, "/") {
		resolvedSrc = filepath.Join(publicDir, strings.TrimPrefix(src, "/"))
	} else if !filepath.IsAbs(src) {
		resolvedSrc = filepath.Join(publicDir, src)
	}

	remote := strings.HasPrefix(src, "http://") || strings.HasPrefix(src, "https://")
	var result *imageproc.ImageResult
	switch {
	case remote:
		u, perr := url.Parse(src)
		if perr != nil || u.Hostname() == "" || !imgCfg.RemoteAllowed(u.Hostname(), u.Path) {
			host := src
			if perr == nil && u.Hostname() != "" {
				host = u.Hostname()
			}
			l.errAt(orig.Position, diag.CodeImageRemote,
				fmt.Sprintf("<Image> remote src %q is not allowed", src),
				fmt.Sprintf("Add %q to images.remotePatterns in krate.config.ts, or download the file into the public directory.", host))
			return nil
		}
		result = &imageproc.ImageResult{Width: reqW, Height: reqH}
	case imgCfg.Unoptimized:
		result = &imageproc.ImageResult{Width: reqW, Height: reqH}
	default:
		var perr error
		result, perr = imageproc.ProcessImageWith(b.Root, resolvedSrc, reqW, reqH, imageproc.Options{
			Quality:     quality,
			Breakpoints: imgCfg.Breakpoints,
			Placeholder: wantPlaceholder,
			Formats:     imgCfg.Formats,
		})
		if perr != nil {
			rel := resolvedSrc
			if r, rerr := filepath.Rel(b.Root, resolvedSrc); rerr == nil {
				rel = filepath.ToSlash(r)
			}
			if errors.Is(perr, fs.ErrNotExist) {
				l.errAt(orig.Position, diag.CodeImageSource,
					fmt.Sprintf("<Image> source %q was not found (looked for %s)", src, rel),
					"Check the path: a src starting with / is relative to the public directory.")
			} else {
				msg := strings.ReplaceAll(perr.Error(), b.Root+string(filepath.Separator), "")
				l.errAt(orig.Position, diag.CodeImageSource, fmt.Sprintf("<Image> %q cannot be processed: %s", src, msg),
					"Use a PNG, JPEG, GIF, WebP or SVG file.")
			}
			return nil
		}
	}

	base := b.Cfg.BaseURLPath()
	result.Src = "/" + strings.TrimPrefix(src, "/")
	if remote {
		result.Src = src
	} else {
		result.Src = base + result.Src
	}

	if loading == "" && loadingExpr == nil {
		loading = "lazy"
	}
	if priority {
		loading = "eager"
		loadingExpr = nil
	}

	str := func(v string) ast.Expr { return &ast.Literal{Kind: ast.StringLit, Value: v} }
	num := func(n int) ast.Expr { return &ast.Literal{Kind: ast.NumberLit, Value: strconv.Itoa(n)} }
	pick := func(lit string, e ast.Expr) ast.Expr {
		if e != nil {
			return e
		}
		return str(lit)
	}

	imgAttrs := []*ast.JSXAttr{
		{Name: "src", Value: str(result.Src)},
		{Name: "alt", Value: pick("", altExpr)},
		{Name: "loading", Value: pick(loading, loadingExpr)},
		{Name: "decoding", Value: str("async")},
	}
	if result.Width > 0 {
		imgAttrs = append(imgAttrs, &ast.JSXAttr{Name: "width", Value: num(result.Width)})
	}
	if result.Height > 0 {
		imgAttrs = append(imgAttrs, &ast.JSXAttr{Name: "height", Value: num(result.Height)})
	}

	vector := len(result.WebP) == 0 && len(result.Fallback) == 0
	// CLS mitigation: reserve the aspect ratio so the browser can size the image
	// before it loads, with a blurred LQIP behind it. The width attribute is
	// the display size and max-width keeps it from overflowing, so a small
	// requested width is never upscaled to fill the container.
	var decls [][2]string
	if !vector && result.Width > 0 && result.Height > 0 {
		decls = append(decls, [2]string{"max-width", "100%"}, [2]string{"height", "auto"},
			[2]string{"aspect-ratio", fmt.Sprintf("%d/%d", result.Width, result.Height)})
		if reqW > 0 && reqH > 0 {
			decls = append(decls, [2]string{"object-fit", "cover"})
		}
		if result.Placeholder != "" {
			decls = append(decls, [2]string{"background-image", "url(" + result.Placeholder + ")"},
				[2]string{"background-size", "cover"}, [2]string{"background-position", "center"})
		}
	}
	if style, ok := l.mergeStyle(decls, styleAttr, orig.Position); ok {
		if style != nil {
			imgAttrs = append(imgAttrs, &ast.JSXAttr{Name: "style", Value: style})
		}
	} else {
		return nil
	}
	if priority {
		imgAttrs = append(imgAttrs, &ast.JSXAttr{Name: "fetchpriority", Value: str("high")})
	}
	if className != "" || classExpr != nil {
		imgAttrs = append(imgAttrs, &ast.JSXAttr{Name: "className", Value: pick(className, classExpr)})
	}

	if vector {
		imgAttrs = append(imgAttrs, extraAttrs...)
		return &ast.JSXElement{
			Position: orig.Position,
			Opening:  &ast.JSXOpening{Name: "img", Attributes: imgAttrs, SelfClosing: true},
		}
	}

	webpSrcset := srcsetString(result.WebP, base)
	fallbackSrcset := srcsetString(result.Fallback, base)
	source := func(mime, srcset string) *ast.JSXElement {
		return &ast.JSXElement{Opening: &ast.JSXOpening{
			Name: "source",
			Attributes: []*ast.JSXAttr{
				{Name: "type", Value: str(mime)},
				{Name: "srcSet", Value: str(srcset)},
				{Name: "sizes", Value: pick(sizes, sizesExpr)},
			},
			SelfClosing: true,
		}}
	}
	if fallbackSrcset != "" {
		imgAttrs = append(imgAttrs,
			&ast.JSXAttr{Name: "srcSet", Value: str(fallbackSrcset)},
			&ast.JSXAttr{Name: "sizes", Value: pick(sizes, sizesExpr)})
	}
	imgAttrs = append(imgAttrs, extraAttrs...)

	var pictureAttrs []*ast.JSXAttr
	if className != "" || classExpr != nil {
		pictureAttrs = append(pictureAttrs, &ast.JSXAttr{Name: "className", Value: pick(className, classExpr)})
	}
	imgEl := &ast.JSXElement{Opening: &ast.JSXOpening{Name: "img", Attributes: imgAttrs, SelfClosing: true}}
	return &ast.JSXElement{
		Position: orig.Position,
		Opening:  &ast.JSXOpening{Name: "picture", Attributes: pictureAttrs},
		Children: []ast.JSXChild{
			&ast.JSXElementChild{Element: source("image/webp", webpSrcset)},
			&ast.JSXElementChild{Element: source(result.FallbackMime, fallbackSrcset)},
			&ast.JSXElementChild{Element: imgEl},
		},
		Closing: &ast.JSXClosing{Name: "picture"},
	}
}

// mergeStyle combines the generated layout declarations with a user style.
// The user's declarations come last so they win. A nil result with ok means
// there is nothing to emit.
func (l *imageLowerer) mergeStyle(decls [][2]string, user ast.Expr, pos ast.Pos) (ast.Expr, bool) {
	if user == nil {
		if len(decls) == 0 {
			return nil, true
		}
		return &ast.Literal{Kind: ast.StringLit, Value: joinDecls(decls)}, true
	}
	if s, ok := l.str(user); ok {
		if len(decls) == 0 {
			return &ast.Literal{Kind: ast.StringLit, Value: s}, true
		}
		return &ast.Literal{Kind: ast.StringLit, Value: joinDecls(decls) + ";" + strings.TrimLeft(s, "; ")}, true
	}
	if obj, ok := user.(*ast.ObjectExpr); ok {
		props := make([]*ast.ObjectProp, 0, len(decls)+len(obj.Properties))
		for _, d := range decls {
			props = append(props, &ast.ObjectProp{Key: camelCSS(d[0]), Value: &ast.Literal{Kind: ast.StringLit, Value: d[1]}})
		}
		props = append(props, obj.Properties...)
		return &ast.ObjectExpr{Position: obj.Position, Properties: props}, true
	}
	l.errAt(pos, diag.CodeImageDynamicProp, "<Image style> must be a string or an object literal",
		"The image adds its own sizing styles and can only merge them with a literal style.")
	return nil, false
}

func joinDecls(decls [][2]string) string {
	parts := make([]string, len(decls))
	for i, d := range decls {
		parts[i] = d[0] + ":" + d[1]
	}
	return strings.Join(parts, ";")
}

func camelCSS(prop string) string {
	parts := strings.Split(prop, "-")
	for i := 1; i < len(parts); i++ {
		if parts[i] != "" {
			parts[i] = strings.ToUpper(parts[i][:1]) + parts[i][1:]
		}
	}
	return strings.Join(parts, "")
}

// srcsetString renders a responsive srcset attribute ("path 640w, path 1024w").
func srcsetString(entries []imageproc.SrcsetEntry, base string) string {
	if len(entries) == 0 {
		return ""
	}
	parts := make([]string, 0, len(entries))
	for _, e := range entries {
		parts = append(parts, fmt.Sprintf("%s%s %dw", base, e.FilePath, e.Width))
	}
	return strings.Join(parts, ", ")
}

// imageDiagError converts an early-pass diagnostic into a located error.
func imageDiagError(file string, d imageDiag) diag.Diagnostic {
	out := diag.New(file, d.pos.Line, d.pos.Col, d.msg, d.hint, "")
	out.Code = d.code
	if d.warn {
		out.Severity = diag.SeverityWarning
	}
	return out
}
