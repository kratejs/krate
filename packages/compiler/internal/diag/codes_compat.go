package diag

// KR8xxx: islands, compiled packages and general React compatibility.
const (
	// CodeIslandLocalComponent: same-file Krate component inside an island.
	CodeIslandLocalComponent Code = "KR8007"
	// CodeIslandClientOnly: an island cannot be server-rendered.
	CodeIslandClientOnly Code = "KR8008"
	// CodeIslandAsset: an island asset or package could not be resolved.
	CodeIslandAsset Code = "KR8009"
	// CodePortalNotLowered: createPortal outside a child position.
	CodePortalNotLowered Code = "KR8010"
	// CodeSlotUnresolved: a slot with no build-time value and no client binding.
	CodeSlotUnresolved Code = "KR8011"
	// CodeReactLibNotInIsland: a React library's hooks are used where no
	// island can run them (a layout, a server component).
	CodeReactLibNotInIsland Code = "KR8013"
	// CodeCSSInJS: a css prop that nothing in the module understands.
	CodeCSSInJS Code = "KR8014"
	// CodeClientDirective: a client:* directive on something that is not an island.
	CodeClientDirective Code = "KR8012"
	// CodeLayoutProvider: a provider in a layout cannot be made available to
	// the islands of the pages it wraps.
	CodeLayoutProvider Code = "KR8015"
	// CodeProviderDOM: a provider in a layout renders markup of its own.
	CodeProviderDOM Code = "KR8016"
)

func init() {
	Register(
		CodeInfo{
			Code:  "KR8001",
			Title: "React API removed in React 19",
			Explanation: "The code uses an API that React 19 removed: findDOMNode, ReactDOM.render, " +
				"ReactDOM.hydrate, unmountComponentAtNode, createFactory or propTypes checking. Krate follows " +
				"React 19 semantics, so these have no equivalent. Use a ref instead of findDOMNode, and let " +
				"Krate mount pages instead of calling render or hydrate yourself.",
		},
		CodeInfo{
			Code:  "KR8002",
			Title: "React API not supported in source components",
			Explanation: "A React API used in a source component has no lowering to Krate signals, so the " +
				"component would render differently from React. Rewrite the component with a supported " +
				"pattern (see the React compatibility guide), or move it into a compiled React package so it " +
				"runs as an island with full React semantics.",
		},
		CodeInfo{
			Code:  "KR8003",
			Title: "Component renders nothing because it could not be compiled",
			Explanation: "Krate could not compile a component (for example an unresolved component name or " +
				"an unsupported class feature) and emitted an empty placeholder where React would render " +
				"content. Fix the reported cause; the placeholder is never a valid substitute for the " +
				"component's output.",
		},
		CodeInfo{
			Code:  "KR8004",
			Title: "Context consumer child is not a single render function",
			Explanation: "`<Ctx.Consumer>` renders the result of calling its child with the nearest provider " +
				"value, so its only child must be one function such as `{(value) => <span>{value}</span>}`. " +
				"Krate inlines that function at build time; it also needs a plain parameter (or a `{ a, b }` " +
				"pattern without renames or defaults). Read the value with `useContext(Ctx)` in a component " +
				"when the render function is more involved.",
		},
		CodeInfo{
			Code:  "KR8005",
			Title: "Legacy context API",
			Explanation: "A class component uses legacy context (`contextTypes`, `childContextTypes` or " +
				"`getChildContext`). React 19 removed it, and Krate does not pass legacy context: " +
				"`this.context` is empty. Create a context with `createContext`, provide it with " +
				"`<Ctx value={...}>` and read it with `static contextType = Ctx` or `useContext(Ctx)`.",
		},
		CodeInfo{
			Code:  "KR8006",
			Title: "Portal container cannot be resolved",
			Explanation: "`createPortal(children, container)` moves its children into a DOM node after " +
				"hydration. Krate resolves `document.body`, `document.getElementById(\"id\")` and " +
				"`document.querySelector(\"selector\")` with literal arguments, and any other expression " +
				"in the hydration scope of a client component; elsewhere the content mounts into " +
				"`document.body`, and a container that is not an element when the page hydrates keeps the " +
				"content unmounted. Make sure the container exists before the component hydrates (for " +
				"example a `<div id=\"modal-root\">` in the layout).",
		},
		CodeInfo{
			Code:  CodeIslandLocalComponent,
			Title: "Same-file component inside an island",
			Explanation: "An external package island contains a component declared in the page module. " +
				"The page component belongs to the Krate runtime and cannot run inside the island's Preact tree. " +
				"Move the component into its own module and import it, or render it outside the island.",
		},
		CodeInfo{
			Code:  CodeIslandClientOnly,
			Title: "Island stays client-only",
			Explanation: "The island has no build-time props, uses a browser-only API, or could not be " +
				"rendered by Preact during the build. Its placeholder is emitted without server-rendered " +
				"markup and the client mounts it after the island chunk loads. Pass build-time initial values " +
				"or keep the island client-only intentionally.",
		},
		CodeInfo{
			Code:  CodePortalNotLowered,
			Title: "createPortal is not in a child position",
			Explanation: "Krate lowers `createPortal(children, container)` when it is a child " +
				"(`{createPortal(...)}`) or a branch of a conditional child (`{open && createPortal(...)}`, " +
				"`{open ? createPortal(...) : null}`). Anywhere else the call is left as is and fails in the " +
				"browser because `createPortal` is not a global. Render the portal as a child, or use " +
				"`<Portal container={...}>`.",
		},
		CodeInfo{
			Code:  CodeSlotUnresolved,
			Title: "Expression has no build-time value and no client binding",
			Explanation: "The compiler could not compute the value of this expression while building the " +
				"page (it calls a function it cannot see into, reads an unsupported built-in, or depends on " +
				"data that only exists at runtime) and the component has no client code that could fill it " +
				"in, so the page renders it as empty text. Move the value into component state or an " +
				"effect, compute it from values the build can see, or make the component interactive so " +
				"the browser evaluates it.",
		},
		CodeInfo{
			Code:  CodeReactLibNotInIsland,
			Title: "React library component or hook used where no island can run it",
			Explanation: "A component calls a hook of an npm React library (zustand, react-hook-form, swr, " +
				"@tanstack/react-query, ...), or a static layout renders a compiled React component. " +
				"Those need React's render model, which Krate's lowered components do not have, so Krate " +
				"runs them on Preact: as an island in a page, and as a client boundary in a server-rendered " +
				"page, layout or component (icons and other components without state render to plain " +
				"HTML). A hook cannot be called from a server component: add \"use client\" to the module " +
				"that calls it, or replace the library with createSignal / createResource. A layout that is " +
				"baked into the static shell cannot host compiled components: make it async so it renders " +
				"per request, or render them from a page.",
		},
		CodeInfo{
			Code:  CodeCSSInJS,
			Title: "css prop is emotion's, not a DOM attribute",
			Explanation: "A `css={...}` prop on a DOM element is understood only by emotion's JSX runtime. In a " +
				"module that does not use it Krate renders `css` as a plain attribute, so the styles are lost. " +
				"Add `/** @jsxImportSource @emotion/react */` to the top of the file so the module runs as a " +
				"Preact island with emotion, or use CSS modules, Tailwind or a stylesheet.",
		},
		CodeInfo{
			Code:  CodeLayoutProvider,
			Title: "Layout provider cannot reach the islands of its pages",
			Explanation: "A layout wraps `{children}` in a component of a React library (a `QueryClientProvider`, a " +
				"`ThemeProvider`, a Redux `Provider`). Islands run in Preact, so the layout cannot give them context. " +
				"Krate lifts such wrappers into a module every island of the page renders inside, but only when their " +
				"props can be computed without the layout's own state: constants, imports, module-level values, and " +
				"`useState(() => value)` or `useMemo(() => value, [])` with no setter in use. A prop that reads the " +
				"layout's `params`, a signal or a state setter cannot be lifted. Build the value from constants, or " +
				"wrap the island's own module in the provider.",
		},
		CodeInfo{
			Code:  CodeProviderDOM,
			Title: "Layout provider renders markup around its children",
			Explanation: "A component a layout wraps around `{children}` renders elements of its own, so it is not a " +
				"context provider that can be moved into the islands: Krate renders the page content, and an island " +
				"cannot sit inside markup that belongs to the layout. The layout keeps the component as it was and " +
				"the islands of its pages do not see its context. Split the component into a provider that renders " +
				"only its children and the markup, or put the provider inside the island's own module.",
		},
		CodeInfo{
			Code:  CodeClientDirective,
			Title: "Client directive on a component that is not an island",
			Explanation: "`client:visible`, `client:idle` and `client:load` choose when an island mounts, and only " +
				"apply to the root tag of a compiled package component (or a module that uses a React library) " +
				"rendered as an island. On a Krate component or an HTML element they do nothing; Krate hydrates " +
				"those with the page. Remove the directive, or put it on the package component itself.",
		},
		CodeInfo{
			Code:  CodeIslandAsset,
			Title: "Island asset could not be built",
			Explanation: "Krate could not bundle the island's external package, local module, Preact " +
				"dependency, or generated chunk. Check the reported resolver error and install the missing " +
				"package (Preact 10.20 or newer) in the project.",
		},
	)
}
