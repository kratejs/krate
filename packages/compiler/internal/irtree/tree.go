package irtree

import (
	"fmt"
	"strings"

	"github.com/kratejs/krate/packages/compiler/ast"
	"github.com/kratejs/krate/packages/compiler/internal/diag"
	"github.com/kratejs/krate/packages/compiler/internal/jsvalue"
)

// SlotID
// SlotID is a stable, deterministic identifier for every dynamic region
// in the component tree. Built from tree path + optional data key.
type SlotID string

// joinSlotID concatenates parent and child with a dot separator.
func joinSlotID(parent, child string) SlotID {
	if parent == "" {
		return SlotID(child)
	}
	return SlotID(parent + "." + child)
}

// joinSlotIDKey appends a key suffix with a colon separator.
func joinSlotIDKey(parent SlotID, key string) SlotID {
	return SlotID(string(parent) + ":" + key)
}

// ComponentTier
type ComponentTier int

const (
	TierUnknown ComponentTier = iota
	TierStatic                // @static directive, *.static.tsx
	TierServer                // @server directive, *.server.tsx, config list
	TierRuntime               // @runtime directive, *.runtime.tsx, config list
	TierClient                // default: signals/handlers need hydration
)

// String returns the tier name for debugging.
func (t ComponentTier) String() string {
	switch t {
	case TierStatic:
		return "static"
	case TierServer:
		return "server"
	case TierRuntime:
		return "runtime"
	case TierClient:
		return "client"
	default:
		return "unknown"
	}
}

// Annotations
// Annotations holds static analysis results for a program.
// Defined here to avoid import cycles between annotator and irtree.
type Annotations struct {
	ComponentTiers map[string]ComponentTier
	SourceFile     string
	HasStreaming   bool
	HasSuspense    bool
	EntryPoint     string
	Signals        map[string]ast.Expr
	UsedComponents map[string]bool
	Functions      map[string]*ast.FnDecl
	// ComponentSources maps each used function name to the source file it was
	// defined in, so tier classification uses the component's OWN file (e.g.
	// *.runtime.tsx convention) instead of the page's directive.
	ComponentSources map[string]string
	// ComponentRaw maps each used function name to the raw source text of the
	// module it was defined in, for directive detection per module.
	ComponentRaw map[string]string
	// CVAFactories maps a module-level `const X = cva(base, config)` binding to
	// its resolved spec, across the entry and all imported modules. Used to fold
	// shadcn/ui variant calls to static class strings.
	CVAFactories map[string]*CVASpec
	// ContextDefaults maps a module-level `const X = createContext(v)` binding to
	// the literal default v, so `X.useContext()` folds during SSR.
	ContextDefaults map[string]string
	// ClassComponents records class declarations that extend Component/
	// PureComponent, so the builder can emit a specific diagnostic (class
	// component lowering is not implemented yet).
	ClassComponents map[string]bool
	// ClassRejections holds, per class component that could not be lowered, a
	// warning positioned at the unsupported member (shouldComponentUpdate, a
	// static or private member, an unknown this.x read, ...). The builder
	// reports it when the component is used.
	ClassRejections map[string]diag.Diagnostic
	// LazyComponents maps a component binding declared with React.lazy() to
	// its declaration. Its hydration code goes into a separate chunk.
	LazyComponents map[string]LazyComponent
}

// (Annotations continues below.)

// SlotNode interface
// Every child of a ComponentNode implements this interface. The emitter
// type-switches on the concrete type to produce HTML + metadata.
type SlotNode interface {
	slotNode()
	GetID() SlotID
}

// StaticHTML - pure HTML, no dynamic behavior
type StaticHTML struct {
	HTML string
}

func (s *StaticHTML) slotNode()     {}
func (s *StaticHTML) GetID() SlotID { return "" }

// GoComponentRef identifies an imported Go component (a `<Card>` where Card comes
// from `./card.go`).
type GoComponentRef struct {
	File string        // absolute path of the component's .go file
	Func string        // exported Go function name
	Tier ComponentTier // TierServer (build-time) or TierRuntime (per request)
}

// GoComponentSlot renders an imported Go component. Server-tier slots are
// rendered at build time by the Go host (batch); runtime-tier slots become
// request-time regions.
type GoComponentSlot struct {
	ID       SlotID
	Ref      GoComponentRef
	Props    map[string]string // attr name -> JSON-encoded value ("" for children)
	Children []SlotNode
	Pos      ast.Pos // JSX tag-name position at the call site (for SSR baking)
}

func (s *GoComponentSlot) slotNode()     {}
func (s *GoComponentSlot) GetID() SlotID { return s.ID }

// TextSlot - simple signal read: {count()}
type TextSlot struct {
	ID      SlotID
	Signal  SignalDecl
	Initial string
}

func (s *TextSlot) slotNode()     {}
func (s *TextSlot) GetID() SlotID { return s.ID }

// ExprSlot - complex expression: {x() > 0 ? "yes" : "no"}
type ExprSlot struct {
	ID         SlotID
	ExprSource string
	Initial    string
	Signals    []string
}

func (s *ExprSlot) slotNode()     {}
func (s *ExprSlot) GetID() SlotID { return s.ID }

// ConditionalSlot - ternary with JSX in branches
// Both branches are statically rendered (render-both) wrapped in togglable
// wrapper elements. Hydration toggles their visibility based on the test.
type ConditionalSlot struct {
	ID            SlotID
	ExprSource    string // full ternary JS (unused at hydration; kept for compat)
	TestJS        string // JS expression for the ternary test (used to toggle)
	Initial       string
	InitialActive bool // whether the consequent branch is active initially
	Consequent    []SlotNode
	Alternate     []SlotNode
	Signals       []string
}

func (s *ConditionalSlot) slotNode()     {}
func (s *ConditionalSlot) GetID() SlotID { return s.ID }

// ListSlot - .map() rendering
type ListSlot struct {
	ID         SlotID
	ExprSource string
	Items      []*ListItem
	// Keyed reports that the map callback returns keyed JSX: rows are matched
	// by key on update instead of by index.
	Keyed   bool
	Signals []string
	// Bind is the runtime list binding (see keyed.go); nil keeps the
	// whole-region re-render of ExprSource.
	Bind *ListBinding
	// Components lists the client component names referenced by the list's
	// map body JSX (e.g. <Toast> in toasts().map(t => <Toast/>)). These must
	// be emitted into the hydration scope so the runtime `h()` call can invoke
	// the component function when re-rendering the list.
	Components []string
}

func (s *ListSlot) slotNode()     {}
func (s *ListSlot) GetID() SlotID { return s.ID }

// ListBinding is the hydration form of a list slot: krateBindList(slot,
// () => Source, Key, Render, accessor ? 1 : 0).
type ListBinding struct {
	Source   string
	Key      string
	Render   string
	Accessor bool
}

// ListItem represents a single item in a ListSlot. Key is the item's marker
// key: "$" plus the folded key prop for keyed rows, or the index.
type ListItem struct {
	Key      string
	Contents []SlotNode
	Data     map[string]string
}

// ComponentSlot - embedded child component instance
type ComponentSlot struct {
	ID        SlotID
	Component *ComponentNode
}

func (s *ComponentSlot) slotNode()     {}
func (s *ComponentSlot) GetID() SlotID { return s.ID }

// SuspenseMode - how a Suspense boundary is emitted
type SuspenseMode int

const (
	// SuspenseModeDefault falls back to baking the fallback inside markers.
	SuspenseModeDefault SuspenseMode = iota
	// SuspenseModeStatic bakes the resolved (primary) content inside markers
	// because everything in the boundary is statically renderable at build time.
	SuspenseModeStatic
	// SuspenseModeRegion bakes the fallback inside markers and defers the
	// primary to a per-region request-time render (runtime/server components
	// or props driven by params/query). Go splices the region result in place.
	SuspenseModeRegion
)

func (m SuspenseMode) String() string {
	switch m {
	case SuspenseModeStatic:
		return "static"
	case SuspenseModeRegion:
		return "region"
	default:
		return "default"
	}
}

// SuspenseSlot - streaming boundary
type SuspenseSlot struct {
	ID       SlotID
	Fallback []SlotNode
	Resolved []SlotNode
	Primary  *ComponentNode
	StreamID string
	Mode     SuspenseMode
}

func (s *SuspenseSlot) slotNode()     {}
func (s *SuspenseSlot) GetID() SlotID { return s.ID }

// MetaSlot - Head/Script/Style content routed to page metadata
type MetaSlot struct {
	ComponentName string
	Children      []SlotNode
}

func (s *MetaSlot) slotNode()     {}
func (s *MetaSlot) GetID() SlotID { return "" }

// ChildrenSlot - placeholder for {children} in layouts
type ChildrenSlot struct {
	Content string // filled by injectChildrenHTML at emit time
}

func (s *ChildrenSlot) slotNode()     {}
func (s *ChildrenSlot) GetID() SlotID { return "" }

// AttrBinding - dynamic attribute: <div class={expr()}>
type AttrBinding struct {
	ElementSlotID SlotID
	AttrName      string
	SignalName    string
	ExprSource    string
	Initial       string
	InitialExpr   ast.Expr
	IsString      bool
	// Prop is the JSX prop name (defaultValue, defaultChecked, ...) when it
	// differs from AttrName, so form-control defaults bind the right property.
	Prop string
}

// HandlerDecl - event handler: onClick={() => fn()}
type HandlerDecl struct {
	ElementSlotID SlotID
	// Prop is the React prop name (onClick, onChangeCapture); the runtime maps
	// it to native events, phases and delegation.
	Prop    string
	Event   string
	Body    string
	Signals []string
	// Capture attaches a capture-phase listener (onClickCapture). Direct
	// attaches a listener on the element itself (capture handlers and
	// non-bubbling events), bypassing the delegated bubble-phase listener.
	Capture bool
	Direct  bool
}

// RefBinding - ref={someVar} on an element
// RefBinding captures an element's `ref` attribute so the hydration bundle can
// assign the live DOM node to the referenced variable after the page loads.
// Target is the JS variable expression to assign (e.g. "wrapRef").
type RefBinding struct {
	ElementSlotID SlotID
	// Target is the assignment target for object refs (`ref={myRef}` ->
	// `myRef.current = el` or `myRef = el`). Mutually exclusive with Callback.
	Target string
	// Callback holds the full JS for callback refs (`ref={(el) => {...}}`).
	// When set, the callback receives the mounted element directly and no
	// assignment wrapper is generated. Mutually exclusive with Target.
	Callback string
	// Adaptive marks an identifier ref whose runtime value may be either a
	// `{current}` ref object or a callback (e.g. a ref forwarded through props
	// or params). The emitted setter invokes it as a function or assigns
	// `.current` accordingly.
	Adaptive bool
}

// SignalDecl - per-instance signal declaration
type SignalDecl struct {
	Name        string
	SetterName  string
	Initial     string
	IsString    bool
	InitialExpr ast.Expr
	// RawInit is a JS expression source to emit as the signal's initial value
	// when the initializer is non-constant (e.g. createSignal(Math.random())).
	// When non-empty it takes precedence over Initial/IsString so the client
	// evaluates the real expression instead of a dropped/undefined value.
	RawInit string
	// FactoryJS is a complete JS factory call (`createReducer(..., ...)`) emitted
	// verbatim in place of `createSignal(...)`. Used for reactive primitives
	// whose setter is not a plain createSignal write (e.g. createReducer's
	// reducer dispatch). Empty for ordinary signals.
	FactoryJS string
	// OptionsJS is the second-argument source for `createSignal(value, options)`
	// (e.g. `{ persist: 'key' }`), emitted verbatim so runtime persistence
	// survives hydration. Empty when the signal has no options.
	OptionsJS string
}

// ComponentNode - a single component instance in the tree
type ComponentNode struct {
	ID   SlotID
	Name string
	Tier ComponentTier
	Fn   *ast.FnDecl
	// Lazy marks an instance of a React.lazy() component (see LazyComponent).
	// LazyLoader is set when its loader is not a static import.
	Lazy       bool
	LazyLoader string
	Props      map[string]ast.Expr
	// RuntimeProps holds resolved prop values for runtime-tier components
	// (prop name -> value). These are serialized into the page's runtime props
	// script so the serve-time renderer can pass them to the component.
	RuntimeProps map[string]any
	// Runtime selects the region render engine for a runtime-tier node: "" (Node
	// SSR sidecar) or "go" (Go host, rendered via /__krate/gorender).
	Runtime string

	Signals      []SignalDecl
	AttrBindings []AttrBinding
	Handlers     []HandlerDecl
	RefBindings  []RefBinding
	Effects      []string
	Memos        []string
	ExtraVars    []string
	// FuncProps records the call-site props whose values are function
	// references (a local function or arrow in the parent scope) rather than
	// const-foldable values. Such props cannot be serialized to a literal: the
	// child must read them from the __krate_props registry at runtime, and any
	// `var X = props.<funcProp>` local must be emitted as a live read of
	// props.<funcProp> instead of being const-folded to the function's name.
	// IslandBoot holds page-scope registration statements for external islands
	// whose props come from this component's signals. They run inside the
	// component IIFE so signal getters and setters are in scope.
	IslandBoot []string
	FuncProps  map[string]bool
	// FuncPropAliases holds `var <local>=props.<funcProp>` declarations for
	// locals that alias a function prop. They must be emitted after the
	// `var props=__krate_props[...]` binding but before any effects/handlers
	// that invoke the aliased function.
	FuncPropAliases []string
	// PreSignalVars holds local variable declarations that must be emitted
	// BEFORE signal initializers. Signal RawInits like createSignal(initial.x)
	// evaluate local values at hydration time, so any local var they reference
	// has to be declared first (JS hoists `var` names to undefined, not values).
	PreSignalVars []string
	// BodyUses lists every signal read or written anywhere in the component's
	// function body (including named functions and control flow), so the
	// reactive validator knows these signals are used even when they don't
	// appear in a binding/handler/effect signature.
	BodyUses []string

	Children []SlotNode

	InstanceID string
	SourceFile string
	Line       int

	// SSR-evaluated components: prop-driven with no signals.
	// The emitter uses SSREval to render these at build time.
	IsSSREval       bool
	SSREvalBindings map[string]jsvalue.Value
	// SSREvalAttrs holds the call-site attribute expressions of an SSREval
	// component, so props the IR folder cannot represent (JSX values) are
	// evaluated by the SSR evaluator itself.
	SSREvalAttrs map[string]ast.Expr
	// LocalVars holds build-time folded local constants (including cva/cn class
	// strings); they are merged into SSREvalBindings so SSR-evaluated components
	// resolve local variables such as `const classes = cn(cva(...))`.
	LocalVars map[string]jsvalue.Value
	// RestProps holds the call-site attributes collected by the component's
	// rest parameter (e.g. `{ className, ...props }`), so SSREval can expand a
	// `{...props}` spread on an intrinsic element. RestPropsName is that
	// parameter's local name.
	RestProps     map[string]ast.Expr
	RestPropsName string

	// CallSiteChildren stores the raw JSX children from the call site.
	// For SSREval components, the emitter evaluates these via SSREval and
	// passes them as the "children" binding. For signal components, they
	// are used to fill {props.children} slots in emitClient.
	CallSiteChildren []ast.JSXChild

	// CallSiteSlots stores the tree-processed call site children.
	// These are used by emitClient to replace <!--__children__--> placeholders
	// with properly-rendered (and hydrated) child component output.
	CallSiteSlots []SlotNode

	// CallSiteChildrenText holds the compile-time text of the call-site
	// children when they are fully static (e.g. the template literal passed to
	// <Code>{`...`}</Code>). SSREval uses it so <SyntaxHighlight> can chroma-
	// highlight the original code instead of already-rendered HTML.
	CallSiteChildrenText string

	// ReturnSlots stores the tree-processed slots of the component's own
	// return-statement JSX. Used by SSREval components so interactive client
	// components nested inside a signal-less wrapper are still emitted through
	// the tree path (preserving data-k/data-kh hydration markers) instead of
	// being rendered as flat static HTML.
	ReturnSlots []SlotNode
}

// ComponentTree
type ComponentTree struct {
	Root         *ComponentNode
	HasLinks     bool
	Pages        map[string]*ComponentNode
	RuntimeStore *RuntimePropStore
	Functions    map[string]*ast.FnDecl
	// CVAFactories holds module-wide `const X = cva(...)` specs so the emitter
	// can fold variant calls in prop-driven (SSREval) components.
	CVAFactories map[string]*CVASpec
	// ContextDefaults holds module-wide `const X = createContext(v)` defaults so
	// the emitter can fold `X.useContext()` during SSREval.
	ContextDefaults map[string]string
	// ModuleConstInits holds module-level constant initializers (entry module
	// plus imports) so SSREval resolves module constants.
	ModuleConstInits map[string]ast.Expr

	// ExternalComponents are unresolved JSX components imported from bare npm
	// packages. Krate cannot statically evaluate arbitrary React libraries, so
	// the build emits a placeholder and a runtime bundle that mounts them.
	ExternalComponents []ExternalComponent

	// Warnings holds non-fatal problems collected while building the tree (for
	// example, an unresolved component that will not render). Each carries the
	// file:line:col of the offending node, its source line and a hint, with
	// Severity set to diag.SeverityWarning. The build prints them to stderr.
	Warnings []diag.Diagnostic

	// CSSSignalsCSS is the generated stylesheet for every CSS signal scope
	// compiled into this tree (empty when none). The build appends it to the
	// page's own CSS so it lands in the hashed external stylesheet.
	CSSSignalsCSS string
	// HasCSSSignals is true when the tree compiled at least one CSS signal scope
	// or condition. Unlike scanning the emitted CSS for a class prefix, this is
	// authoritative and drives the per-variant stylesheet split and UA siblings.
	HasCSSSignals bool
	// NoJSFallbackCSS is the `@supports not selector(:has(*))` reveal-all rule
	// (empty when none). The build wraps it in <noscript> for legacy/hybrid
	// modes so panels stay visible when scripting is disabled on engines that
	// also lack :has().
	NoJSFallbackCSS string
	// NeedsCSSARIA is true when a CSS signal scope uses a role that requires the
	// tiny ARIA micro-runtime (tabs/listbox/disclosure) to keep synthesized
	// aria-selected/aria-expanded in sync. False means the page stays zero-JS.
	NeedsCSSARIA bool
	// Errors holds hard errors for CSS signals that could not be compiled
	// (unsupported trigger element, dynamic class, state read as text, ...). A
	// non-empty set must fail the build: there is no fallback.
	Errors []error
}

// RuntimePropStore
type RuntimePropStore struct {
	Components map[string]any
	Counter    int
}

func NewRuntimePropStore() *RuntimePropStore {
	return &RuntimePropStore{
		Components: make(map[string]any),
	}
}

// ComponentSignature - serializable metadata for SPA reconciliation
type ComponentSignature struct {
	ComponentID     SlotID
	Tier            ComponentTier
	Signals         []SignalDecl
	Handlers        []HandlerDecl
	RefBindings     []RefBinding
	Effects         []string
	Memos           []string
	ExtraVars       []string
	PreSignalVars   []string
	FuncPropAliases []string
	IslandBoot      []string
	BodyUses        []string
	Children        []SlotID
	SlotBindings    []SlotBinding
	AttrBindings    []AttrBinding
	// Boundary is the stream ID of the innermost <Suspense> boundary whose
	// content holds this instance ("" outside any boundary).
	Boundary string
	// Lazy marks an instance of a lazy() component; LazyName is its name.
	// Its hydration code (and its descendants') is loaded as a separate chunk.
	Lazy     bool
	LazyName string
	// InLists names the list slots whose rows contain this component. When
	// such a list is bound at runtime the rows (and this component) are
	// rendered and adopted by the list, so the signature is not hydrated.
	InLists []SlotID
	// ParentID is the enclosing client component, empty at the top level.
	// Hydration declares it so React effects run children before parents.
	ParentID SlotID
	// Conds lists the conditional branches the component renders inside,
	// outermost first. Effects of a component in a hidden branch are cleaned
	// up and re-run when the branch is shown again.
	Conds []CondBranch
	// ListSlot is the innermost list slot whose items contain the component;
	// replacing that list's content on the client unmounts the component.
	ListSlot SlotID
}

// CondBranch names one branch ("c" consequent, "a" alternate) of a
// conditional slot and whether it is the branch rendered on the server.
type CondBranch struct {
	SlotID         SlotID
	Branch         string
	InitiallyShown bool
	// Boundary marks a Suspense boundary's branch: the component's effects
	// wait for the reveal, but it is not remounted.
	Boundary bool
}

// SlotBinding - describes a content binding for hydration
// SlotBinding maps a slot ID to its type and expression source, so the hydrate
// code can generate createEffect calls to bind signals to DOM text nodes.
type SlotBinding struct {
	SlotID  SlotID
	Type    string   // "text", "expr", "conditional", "list"
	ExprJS  string   // JS expression to evaluate (e.g. "count()" or "(x() > 0 ? ... : ...)")
	Signals []string // signal names this binding depends on
	// List is set for list slots bound through the keyed list runtime.
	List *ListBinding
}

// Helpers

// itoa is a simple int-to-string conversion.
func itoa(i int) string {
	return fmt.Sprintf("%d", i)
}

// sanitizeTagName converts a tag name to a safe slot ID segment.
func sanitizeTagName(name string) string {
	name = strings.ReplaceAll(name, ".", "-")
	name = strings.ReplaceAll(name, "/", "-")
	name = strings.ReplaceAll(name, ":", "-")
	return name
}

// ExternalImport records a binding imported from a bare npm package.
type ExternalImport struct {
	Source    string
	Local     string
	Remote    string
	Default   bool
	Namespace bool
}

// ExternalComponent is an unresolved package component that is mounted at
// runtime instead of being statically rendered.
type ExternalComponent struct {
	ID      string
	Source  string
	Local   string
	Members []string
	// Namespace is set when Local is a namespace import.
	Namespace bool
	Props     map[string]any
	// Subtree is the serialized JSX for the whole external subtree (captured at
	// the topmost external component), rendered by the runtime island.
	Subtree string
	// PropsJS is a JS object literal of page-scope signal values the subtree
	// reads (key -> value expression), evaluated in the page's hydration scope.
	PropsJS string
	// File/Line/Col locate the usage for diagnostics.
	File string
	Line int
	Col  int
}
