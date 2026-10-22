package ast

// Pos is the start of a node: a 1-based Line, a 1-based Col counted in UTF-16
// code units (the unit LSP clients and editors use), and the byte Offset into
// the source.
type Pos struct {
	Line   int
	Col    int
	Offset int
}

type Node interface {
	node()
	Pos() Pos
}

type Expr interface {
	Node
	expr()
}

type Stmt interface {
	Node
	stmt()
}

type Identifier struct {
	Position Pos
	Name     string
}

func (i *Identifier) node()    {}
func (i *Identifier) expr()    {}
func (i *Identifier) Pos() Pos { return i.Position }

type LitKind int

const (
	StringLit LitKind = iota
	NumberLit
	BoolLit
	NullLit
	RegexpLit
)

type Literal struct {
	Position Pos
	Kind     LitKind
	Value    string
}

func (l *Literal) node()    {}
func (l *Literal) expr()    {}
func (l *Literal) Pos() Pos { return l.Position }

type CallExpr struct {
	Position Pos
	Callee   Expr
	Args     []Expr
	// Optional marks an optional call `fn?.()`, emitted as `callee?.(...)`.
	Optional bool
}

func (c *CallExpr) node()    {}
func (c *CallExpr) expr()    {}
func (c *CallExpr) Pos() Pos { return c.Position }

type MemberExpr struct {
	Position Pos
	Object   Expr
	Property Expr
	Computed bool
	Optional bool
}

func (m *MemberExpr) node()    {}
func (m *MemberExpr) expr()    {}
func (m *MemberExpr) Pos() Pos { return m.Position }

type BinaryExpr struct {
	Position Pos
	Left     Expr
	Op       string
	Right    Expr
}

func (b *BinaryExpr) node()    {}
func (b *BinaryExpr) expr()    {}
func (b *BinaryExpr) Pos() Pos { return b.Position }

type UnaryExpr struct {
	Position Pos
	Op       string
	Arg      Expr
	Postfix  bool
}

func (u *UnaryExpr) node()    {}
func (u *UnaryExpr) expr()    {}
func (u *UnaryExpr) Pos() Pos { return u.Position }

type ConditionalExpr struct {
	Position   Pos
	Test       Expr
	Consequent Expr
	Alternate  Expr
}

func (c *ConditionalExpr) node()    {}
func (c *ConditionalExpr) expr()    {}
func (c *ConditionalExpr) Pos() Pos { return c.Position }

type TypeAssertion struct {
	Position Pos
	Expr     Expr
	TypeRef  string
}

func (t *TypeAssertion) node()    {}
func (t *TypeAssertion) expr()    {}
func (t *TypeAssertion) Pos() Pos { return t.Position }

type ArrowFn struct {
	Position   Pos
	Params     []*Param
	Body       []Stmt
	Expression bool
	Async      bool
	// Function marks a `function` expression (or an object/class method)
	// rather than an arrow: it has its own `this` and `arguments`.
	Function bool
	// Generator marks `function*` and `*method()` forms.
	Generator bool
	// Name is the optional binding name of a named function expression.
	Name string
}

func (a *ArrowFn) node()    {}
func (a *ArrowFn) expr()    {}
func (a *ArrowFn) Pos() Pos { return a.Position }

type ObjectExpr struct {
	Position   Pos
	Properties []*ObjectProp
}

func (o *ObjectExpr) node()    {}
func (o *ObjectExpr) expr()    {}
func (o *ObjectExpr) Pos() Pos { return o.Position }

type ObjectProp struct {
	Key       string
	Value     Expr
	Shorthand bool
	Spread    bool
	Method    bool
	// Computed marks a `[expr]: value` key; KeyExpr holds the expression and
	// Key holds its source-like spelling when it is a simple name or literal.
	Computed bool
	KeyExpr  Expr
	// Getter and Setter mark `get key() {}` / `set key(v) {}` accessors (Method
	// is also set; Value is the accessor function).
	Getter bool
	Setter bool
	// KeyQuoted records a string-literal key (`{ "a-b": 1 }`) and KeyNumeric
	// a numeric one (`{ 0x10: 1 }`, Key holds the source spelling).
	KeyQuoted  bool
	KeyNumeric bool
}

type ArrayExpr struct {
	Position Pos
	Elements []Expr
}

func (a *ArrayExpr) node()    {}
func (a *ArrayExpr) expr()    {}
func (a *ArrayExpr) Pos() Pos { return a.Position }

type TemplateExpr struct {
	Position Pos
	Parts    []Expr
	Raw      []string
}

func (t *TemplateExpr) node()    {}
func (t *TemplateExpr) expr()    {}
func (t *TemplateExpr) Pos() Pos { return t.Position }

type JSXElement struct {
	Position Pos
	Opening  *JSXOpening
	Children []JSXChild
	Closing  *JSXClosing
}

func (j *JSXElement) node()    {}
func (j *JSXElement) expr()    {}
func (j *JSXElement) Pos() Pos { return j.Position }

type JSXFragment struct {
	Position Pos
	Children []JSXChild
}

func (j *JSXFragment) node()    {}
func (j *JSXFragment) expr()    {}
func (j *JSXFragment) Pos() Pos { return j.Position }

type JSXOpening struct {
	Name        string
	Attributes  []*JSXAttr
	SelfClosing bool
}

type JSXClosing struct {
	Name string
}

type JSXChild interface {
	jsxChild()
}

type JSXText struct {
	Value string
}

func (j *JSXText) jsxChild() {}

type JSXExprContainer struct {
	Expression Expr
}

func (j *JSXExprContainer) jsxChild() {}

type JSXElementChild struct {
	Element *JSXElement
}

func (j *JSXElementChild) jsxChild() {}

type JSXFragmentChild struct {
	Fragment *JSXFragment
}

func (j *JSXFragmentChild) jsxChild() {}

type JSXAttr struct {
	Position Pos
	Name     string
	Value    Expr
	Spread   bool
}

func (j *JSXAttr) Pos() Pos { return j.Position }

// htmlAttrAliases maps React/JSX camelCase attribute names to their HTML
// equivalents. Includes the React DOM property renames plus SVG presentation
// attributes, so transpiled React source (and shadcn/radix components) emit
// valid HTML/SVG attributes.
var htmlAttrAliases = map[string]string{
	// React DOM property aliases.
	"className":       "class",
	"htmlFor":         "for",
	"tabIndex":        "tabindex",
	"readOnly":        "readonly",
	"autoComplete":    "autocomplete",
	"autoFocus":       "autofocus",
	"autoPlay":        "autoplay",
	"colSpan":         "colspan",
	"rowSpan":         "rowspan",
	"srcSet":          "srcset",
	"maxLength":       "maxlength",
	"minLength":       "minlength",
	"contentEditable": "contenteditable",
	"spellCheck":      "spellcheck",
	"formAction":      "formaction",
	"formEncType":     "formenctype",
	"formMethod":      "formmethod",
	"formNoValidate":  "formnovalidate",
	"formTarget":      "formtarget",
	"acceptCharset":   "accept-charset",
	"noValidate":      "novalidate",
	"crossOrigin":     "crossorigin",
	"defaultValue":    "value",
	"defaultChecked":  "checked",
	"dateTime":        "datetime",
	"accessKey":       "accesskey",
	"inputMode":       "inputmode",
	"enterKeyHint":    "enterkeyhint",
	"autoCapitalize":  "autocapitalize",
	"autoCorrect":     "autocorrect",
	"referrerPolicy":  "referrerpolicy",
	"srcLang":         "srclang",
	"itemScope":       "itemscope",
	"itemType":        "itemtype",
	"itemID":          "itemid",
	"allowFullScreen": "allowfullscreen",
	"playsInline":     "playsinline",
	"frameBorder":     "frameborder",
	"marginWidth":     "marginwidth",
	"marginHeight":    "marginheight",
	"charSet":         "charset",
	"httpEquiv":       "http-equiv",
	"cellPadding":     "cellpadding",
	"cellSpacing":     "cellspacing",
	"useMap":          "usemap",
	"captureAudio":    "capture",
	// SVG presentation attributes (camelCase in React, kebab/verbatim in SVG).
	"strokeWidth":         "stroke-width",
	"strokeLinecap":       "stroke-linecap",
	"strokeLinejoin":      "stroke-linejoin",
	"strokeDasharray":     "stroke-dasharray",
	"strokeDashoffset":    "stroke-dashoffset",
	"strokeMiterlimit":    "stroke-miterlimit",
	"strokeOpacity":       "stroke-opacity",
	"fillRule":            "fill-rule",
	"fillOpacity":         "fill-opacity",
	"clipRule":            "clip-rule",
	"clipPath":            "clip-path",
	"stopColor":           "stop-color",
	"stopOpacity":         "stop-opacity",
	"gradientUnits":       "gradientUnits",
	"patternUnits":        "patternUnits",
	"textAnchor":          "text-anchor",
	"dominantBaseline":    "dominant-baseline",
	"vectorEffect":        "vector-effect",
	"shapeRendering":      "shape-rendering",
	"floodColor":          "flood-color",
	"floodOpacity":        "flood-opacity",
	"markerEnd":           "marker-end",
	"markerMid":           "marker-mid",
	"markerStart":         "marker-start",
	"stdDeviation":        "stdDeviation",
	"viewBox":             "viewBox",
	"preserveAspectRatio": "preserveAspectRatio",
}

// HTMLAttrName maps a JSX attribute name to its HTML attribute name, translating
// React-style camelCase aliases (className, htmlFor, tabIndex, SVG props, ...) to
// their DOM equivalents. Unknown names pass through unchanged.
func HTMLAttrName(name string) string {
	if mapped, ok := htmlAttrAliases[name]; ok {
		return mapped
	}
	return name
}

type Param struct {
	Position Pos
	Name     string
	Default  Expr // Store the expression here
	IsRest   bool // Added this
	// Target is the structured binding pattern for a destructuring parameter:
	// an ObjectExpr/ArrayExpr in cover-grammar form (defaults are BinaryExpr
	// with Op "=", rest elements are UnaryExpr with Op "...", holes are nil).
	Target Expr
	// Pattern holds the source text of a destructuring parameter pattern
	// (e.g. `{items, label}` or `[a, b]`). When set, Name is the "{...}" sentinel
	// and codegen emits this pattern so the destructured locals are bound.
	Pattern string
}

func (p *Param) node()    {}
func (p *Param) Pos() Pos { return p.Position }

type VarKind int

const (
	VarConst VarKind = iota
	VarLet
	VarVar
	// VarUsing and VarAwaitUsing are explicit resource management
	// declarations (`using x = ...`, `await using x = ...`).
	VarUsing
	VarAwaitUsing
)

type Program struct {
	Position Pos
	Body     []Stmt
}

func (p *Program) node()    {}
func (p *Program) Pos() Pos { return p.Position }

type ImportStmt struct {
	Position  Pos
	Default   string
	Named     []NamedImport
	Namespace string
	Source    string
	// Attributes holds import attributes (`with { type: "json" }`).
	Attributes []ImportAttribute
}

// ImportAttribute is one `key: "value"` entry of an import/export `with {}`
// clause. Key is the attribute name (unquoted); Value is the decoded string.
type ImportAttribute struct {
	Key   string
	Value string
}

func (i *ImportStmt) node()    {}
func (i *ImportStmt) stmt()    {}
func (i *ImportStmt) Pos() Pos { return i.Position }

type NamedImport struct {
	Local  string
	Remote string
}

type ExportStmt struct {
	Position       Pos
	Declaration    Stmt
	Default        bool
	Local          string
	Named          []NamedExport // `export { A, B as C }` pairs (local -> exported)
	StarReexport   bool          // export * from 'source'
	Namespace      string        // export * as Namespace from 'source'
	ReexportSource string        // source path for re-exports (export * from '...' or export { X } from '...')
	// ReexportNamed holds the specifiers of `export { a as b } from '...'`
	// (Local is the name in the source module). Named stays reserved for
	// exports of local bindings.
	ReexportNamed []NamedExport
	// Attributes holds import attributes on a re-export (`with { type: "json" }`).
	Attributes []ImportAttribute
}

// NamedExport is one specifier in `export { Local, Local as Remote }`.
type NamedExport struct {
	Local  string
	Remote string
}

func (e *ExportStmt) node()    {}
func (e *ExportStmt) stmt()    {}
func (e *ExportStmt) Pos() Pos { return e.Position }

type VarStmt struct {
	Position Pos
	Kind     VarKind
	Decls    []*VarDecl
}

func (v *VarStmt) node()    {}
func (v *VarStmt) stmt()    {}
func (v *VarStmt) Pos() Pos { return v.Position }

type VarDecl struct {
	// Target is the structured binding pattern of a destructuring declaration
	// (cover-grammar ObjectExpr/ArrayExpr, see Param.Target). Pattern keeps
	// the pattern's source text for consumers that only need names.
	Target          Expr
	Name            string
	Names           []string
	IsDestructuring bool
	Pattern         string
	RestName        string
	Init            Expr
}

type FnDecl struct {
	Position  Pos
	Name      string
	Params    []*Param
	Body      []Stmt
	Async     bool
	Generator bool
	Export    bool
	Default   bool
}

func (f *FnDecl) node()    {}
func (f *FnDecl) stmt()    {}
func (f *FnDecl) Pos() Pos { return f.Position }

// ClassMemberKind distinguishes methods, fields, and constructors.
type ClassMemberKind int

const (
	ClassMethod ClassMemberKind = iota
	ClassField
	ClassConstructor
	// ClassGetter and ClassSetter are `get x() {}` / `set x(v) {}` accessors.
	ClassGetter
	ClassSetter
	// ClassStaticBlock is a `static { ... }` initialization block (Body).
	ClassStaticBlock
	// ClassAccessor is an auto-accessor field (`accessor x = 1`).
	ClassAccessor
)

// ClassMember is one member of a class body.
type ClassMember struct {
	Position Pos
	Kind     ClassMemberKind
	Name     string
	Private  bool
	Static   bool
	Async    bool
	// Generator marks a `*method()` generator method.
	Generator bool
	// Computed marks a `[expr]` member name held in KeyExpr.
	Computed bool
	KeyExpr  Expr
	// KeyQuoted/KeyNumeric record a string or numeric literal member name.
	KeyQuoted  bool
	KeyNumeric bool
	Params     []*Param
	Body       []Stmt
	Init       Expr
}

// ClassDecl is a class declaration. Krate lowers React class components from
// it; other classes are retained so compiled code is not silently dropped.
type ClassDecl struct {
	Position   Pos
	Name       string
	SuperClass Expr
	Members    []*ClassMember
}

func (c *ClassDecl) node()    {}
func (c *ClassDecl) stmt()    {}
func (c *ClassDecl) Pos() Pos { return c.Position }

type ReturnStmt struct {
	Position Pos
	Value    Expr
}

func (r *ReturnStmt) node()    {}
func (r *ReturnStmt) stmt()    {}
func (r *ReturnStmt) Pos() Pos { return r.Position }

type ExprStmt struct {
	Position   Pos
	Expression Expr
}

func (e *ExprStmt) node()    {}
func (e *ExprStmt) stmt()    {}
func (e *ExprStmt) Pos() Pos { return e.Position }

type IfStmt struct {
	Position   Pos
	Test       Expr
	Consequent []Stmt
	Alternate  []Stmt
}

func (i *IfStmt) node()    {}
func (i *IfStmt) stmt()    {}
func (i *IfStmt) Pos() Pos { return i.Position }

type BlockStmt struct {
	Position Pos
	Body     []Stmt
}

func (b *BlockStmt) node()    {}
func (b *BlockStmt) stmt()    {}
func (b *BlockStmt) Pos() Pos { return b.Position }

type ForStmt struct {
	Position Pos
	Init     Stmt
	Test     Expr
	Update   Expr
	Body     []Stmt
}

func (f *ForStmt) node()    {}
func (f *ForStmt) stmt()    {}
func (f *ForStmt) Pos() Pos { return f.Position }

type ForInStmt struct {
	Position Pos
	// Await marks `for await (... of ...)`.
	Await   bool
	Left    Expr
	Right   Expr
	Body    []Stmt
	IsForOf bool
	// Keyword is the declaration keyword of the loop binding ("const", "let",
	// "var"), or "" for a bare assignment target such as `for (x in obj)`. The
	// parser records it so codegen can reproduce the original binding form.
	Keyword string
}

func (f *ForInStmt) node()    {}
func (f *ForInStmt) stmt()    {}
func (f *ForInStmt) Pos() Pos { return f.Position }

type WhileStmt struct {
	Position Pos
	Test     Expr
	Body     []Stmt
}

func (w *WhileStmt) node()    {}
func (w *WhileStmt) stmt()    {}
func (w *WhileStmt) Pos() Pos { return w.Position }

type DoWhileStmt struct {
	Position Pos
	Body     []Stmt
	Test     Expr
}

func (d *DoWhileStmt) node()    {}
func (d *DoWhileStmt) stmt()    {}
func (d *DoWhileStmt) Pos() Pos { return d.Position }

type SwitchStmt struct {
	Position     Pos
	Discriminant Expr
	Cases        []*CaseClause
}

func (s *SwitchStmt) node()    {}
func (s *SwitchStmt) stmt()    {}
func (s *SwitchStmt) Pos() Pos { return s.Position }

type CaseClause struct {
	Position Pos
	Test     Expr
	Body     []Stmt
}

type TryStmt struct {
	Position Pos
	Body     []Stmt
	Catch    *CatchClause
	Finally  []Stmt
}

func (t *TryStmt) node()    {}
func (t *TryStmt) stmt()    {}
func (t *TryStmt) Pos() Pos { return t.Position }

type CatchClause struct {
	Position Pos
	Param    string
	// ParamTarget is the binding pattern of `catch ({ message })`; Param
	// then holds the pattern's source text.
	ParamTarget Expr
	Body        []Stmt
}

type ThrowStmt struct {
	Position Pos
	Value    Expr
}

func (t *ThrowStmt) node()    {}
func (t *ThrowStmt) stmt()    {}
func (t *ThrowStmt) Pos() Pos { return t.Position }

type BreakStmt struct {
	Position Pos
	Label    string
}

func (b *BreakStmt) node()    {}
func (b *BreakStmt) stmt()    {}
func (b *BreakStmt) Pos() Pos { return b.Position }

type ContinueStmt struct {
	Position Pos
	Label    string
}

func (c *ContinueStmt) node()    {}
func (c *ContinueStmt) stmt()    {}
func (c *ContinueStmt) Pos() Pos { return c.Position }

type NewExpr struct {
	Position Pos
	Callee   Expr
	Args     []Expr
}

func (n *NewExpr) node()    {}
func (n *NewExpr) expr()    {}
func (n *NewExpr) Pos() Pos { return n.Position }

type ThisExpr struct {
	Position Pos
}

func (t *ThisExpr) node()    {}
func (t *ThisExpr) expr()    {}
func (t *ThisExpr) Pos() Pos { return t.Position }

type AwaitExpr struct {
	Position Pos
	Arg      Expr
}

func (a *AwaitExpr) node()    {}
func (a *AwaitExpr) expr()    {}
func (a *AwaitExpr) Pos() Pos { return a.Position }

type DynamicImport struct {
	Position Pos
	Arg      Expr
	// Options is the optional second argument (`import(x, { with: {...} })`).
	Options Expr
}

func (d *DynamicImport) node()    {}
func (d *DynamicImport) expr()    {}
func (d *DynamicImport) Pos() Pos { return d.Position }

type ImportMetaExpr struct {
	Position Pos
}

func (im *ImportMetaExpr) node()    {}
func (im *ImportMetaExpr) expr()    {}
func (im *ImportMetaExpr) Pos() Pos { return im.Position }

// ClassExpr is a class expression (`const A = class extends B {}`).
type ClassExpr struct {
	Position Pos
	Class    *ClassDecl
}

func (c *ClassExpr) node()    {}
func (c *ClassExpr) expr()    {}
func (c *ClassExpr) Pos() Pos { return c.Position }

// TaggedTemplateExpr is a tagged template (`css` + "`a ${b}`").
type TaggedTemplateExpr struct {
	Position Pos
	Tag      Expr
	Quasi    *TemplateExpr
}

func (t *TaggedTemplateExpr) node()    {}
func (t *TaggedTemplateExpr) expr()    {}
func (t *TaggedTemplateExpr) Pos() Pos { return t.Position }

// MetaProperty is `new.target` (Meta "new", Property "target"). `import.meta`
// keeps its dedicated ImportMetaExpr node.
type MetaProperty struct {
	Position Pos
	Meta     string
	Property string
}

func (m *MetaProperty) node()    {}
func (m *MetaProperty) expr()    {}
func (m *MetaProperty) Pos() Pos { return m.Position }

// ParenExpr records grouping parentheses where they change meaning: around
// an optional chain that is then accessed or called (`(a?.b).c`), so the
// chain is not extended through the parentheses.
type ParenExpr struct {
	Position Pos
	Expr     Expr
}

func (p *ParenExpr) node()    {}
func (p *ParenExpr) expr()    {}
func (p *ParenExpr) Pos() Pos { return p.Position }

// LabeledStmt is `label: body`.
type LabeledStmt struct {
	Position Pos
	Label    string
	Body     Stmt
}

func (l *LabeledStmt) node()    {}
func (l *LabeledStmt) stmt()    {}
func (l *LabeledStmt) Pos() Pos { return l.Position }

// EmptyStmt is a lone `;` used as a statement body (`if (x) ;`).
type EmptyStmt struct {
	Position Pos
}

func (e *EmptyStmt) node()    {}
func (e *EmptyStmt) stmt()    {}
func (e *EmptyStmt) Pos() Pos { return e.Position }

// WithStmt is the sloppy-mode `with (object) body` statement.
type WithStmt struct {
	Position Pos
	Object   Expr
	Body     []Stmt
}

func (w *WithStmt) node()    {}
func (w *WithStmt) stmt()    {}
func (w *WithStmt) Pos() Pos { return w.Position }

// DebuggerStmt is the `debugger;` statement.
type DebuggerStmt struct {
	Position Pos
}

func (d *DebuggerStmt) node()    {}
func (d *DebuggerStmt) stmt()    {}
func (d *DebuggerStmt) Pos() Pos { return d.Position }
