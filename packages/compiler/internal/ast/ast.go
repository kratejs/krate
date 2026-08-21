package ast

type Pos struct {
	Line int
	Col  int
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

func (i *Identifier) node()  {}
func (i *Identifier) expr()  {}
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
	Key      string
	Value    Expr
	Shorthand bool
	Spread    bool
	Method    bool
}

type ArrayExpr struct {
	Position Pos
	Elements []Expr
}

func (a *ArrayExpr) node()    {}
func (a *ArrayExpr) expr()    {}
func (a *ArrayExpr) Pos() Pos { return a.Position }

type TemplateExpr struct {
	Position  Pos
	Parts     []Expr
	Raw       []string
}

func (t *TemplateExpr) node()    {}
func (t *TemplateExpr) expr()    {}
func (t *TemplateExpr) Pos() Pos { return t.Position }

type JSXElement struct {
	Position  Pos
	Opening   *JSXOpening
	Children  []JSXChild
	Closing   *JSXClosing
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
	Name       string
	Attributes []*JSXAttr
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

type Param struct {
    Position Pos
    Name     string
    Default  Expr // Store the expression here
    IsRest   bool // Added this
}

func (p *Param) node()    {}
func (p *Param) Pos() Pos { return p.Position }

type VarKind int

const (
	VarConst VarKind = iota
	VarLet
	VarVar
)

type Program struct {
	Position Pos
	Body     []Stmt
}

func (p *Program) node()   {}
func (p *Program) Pos() Pos { return p.Position }

type ImportStmt struct {
	Position  Pos
	Default   string
	Named     []NamedImport
	Namespace string
	Source    string
}

func (i *ImportStmt) node()   {}
func (i *ImportStmt) stmt()   {}
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
	StarReexport   bool   // export * from 'source'
	ReexportSource string // source path for re-exports (export * from '...' or export { X } from '...')
}

func (e *ExportStmt) node()   {}
func (e *ExportStmt) stmt()   {}
func (e *ExportStmt) Pos() Pos { return e.Position }

type VarStmt struct {
	Position Pos
	Kind     VarKind
	Decls    []*VarDecl
}

func (v *VarStmt) node()   {}
func (v *VarStmt) stmt()   {}
func (v *VarStmt) Pos() Pos { return v.Position }

type VarDecl struct {
	Name           string
	Names          []string
	IsDestructuring bool
	RestName       string
	Init           Expr
}

type FnDecl struct {
	Position Pos
	Name     string
	Params   []*Param
	Body     []Stmt
	Async    bool
	Export   bool
	Default  bool
}

func (f *FnDecl) node()   {}
func (f *FnDecl) stmt()   {}
func (f *FnDecl) Pos() Pos { return f.Position }

type ReturnStmt struct {
	Position Pos
	Value    Expr
}

func (r *ReturnStmt) node()   {}
func (r *ReturnStmt) stmt()   {}
func (r *ReturnStmt) Pos() Pos { return r.Position }

type ExprStmt struct {
	Position   Pos
	Expression Expr
}

func (e *ExprStmt) node()   {}
func (e *ExprStmt) stmt()   {}
func (e *ExprStmt) Pos() Pos { return e.Position }

type IfStmt struct {
	Position    Pos
	Test        Expr
	Consequent  []Stmt
	Alternate   []Stmt
}

func (i *IfStmt) node()   {}
func (i *IfStmt) stmt()   {}
func (i *IfStmt) Pos() Pos { return i.Position }

type BlockStmt struct {
	Position Pos
	Body     []Stmt
}

func (b *BlockStmt) node()   {}
func (b *BlockStmt) stmt()   {}
func (b *BlockStmt) Pos() Pos { return b.Position }

type ForStmt struct {
	Position  Pos
	Init      Stmt
	Test      Expr
	Update    Expr
	Body      []Stmt
}

func (f *ForStmt) node()   {}
func (f *ForStmt) stmt()   {}
func (f *ForStmt) Pos() Pos { return f.Position }

type ForInStmt struct {
	Position Pos
	Left     Expr
	Right    Expr
	Body     []Stmt
	IsForOf  bool
}

func (f *ForInStmt) node()   {}
func (f *ForInStmt) stmt()   {}
func (f *ForInStmt) Pos() Pos { return f.Position }

type WhileStmt struct {
	Position Pos
	Test     Expr
	Body     []Stmt
}

func (w *WhileStmt) node()   {}
func (w *WhileStmt) stmt()   {}
func (w *WhileStmt) Pos() Pos { return w.Position }

type DoWhileStmt struct {
	Position Pos
	Body     []Stmt
	Test     Expr
}

func (d *DoWhileStmt) node()   {}
func (d *DoWhileStmt) stmt()   {}
func (d *DoWhileStmt) Pos() Pos { return d.Position }

type SwitchStmt struct {
	Position Pos
	Discriminant Expr
	Cases    []*CaseClause
}

func (s *SwitchStmt) node()   {}
func (s *SwitchStmt) stmt()   {}
func (s *SwitchStmt) Pos() Pos { return s.Position }

type CaseClause struct {
	Position Pos
	Test     Expr
	Body     []Stmt
}

type TryStmt struct {
	Position  Pos
	Body      []Stmt
	Catch     *CatchClause
	Finally   []Stmt
}

func (t *TryStmt) node()   {}
func (t *TryStmt) stmt()   {}
func (t *TryStmt) Pos() Pos { return t.Position }

type CatchClause struct {
	Position Pos
	Param    string
	Body     []Stmt
}

type ThrowStmt struct {
	Position Pos
	Value    Expr
}

func (t *ThrowStmt) node()   {}
func (t *ThrowStmt) stmt()   {}
func (t *ThrowStmt) Pos() Pos { return t.Position }

type BreakStmt struct {
	Position Pos
	Label    string
}

func (b *BreakStmt) node()   {}
func (b *BreakStmt) stmt()   {}
func (b *BreakStmt) Pos() Pos { return b.Position }

type ContinueStmt struct {
	Position Pos
	Label    string
}

func (c *ContinueStmt) node()   {}
func (c *ContinueStmt) stmt()   {}
func (c *ContinueStmt) Pos() Pos { return c.Position }

type NewExpr struct {
	Position Pos
	Callee   Expr
	Args     []Expr
}

func (n *NewExpr) node()   {}
func (n *NewExpr) expr()   {}
func (n *NewExpr) Pos() Pos { return n.Position }

type ThisExpr struct {
	Position Pos
}

func (t *ThisExpr) node()   {}
func (t *ThisExpr) expr()   {}
func (t *ThisExpr) Pos() Pos { return t.Position }

type AwaitExpr struct {
	Position Pos
	Arg      Expr
}

func (a *AwaitExpr) node()   {}
func (a *AwaitExpr) expr()   {}
func (a *AwaitExpr) Pos() Pos { return a.Position }
