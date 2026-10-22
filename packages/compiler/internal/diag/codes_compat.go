package diag

// KR8xxx: islands, compiled packages and general React compatibility.
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
				"in the component's hydration scope; a container that is not an element when the page " +
				"hydrates leaves the content in place. Make sure the container exists in the page " +
				"before the component hydrates (for example a `<div id=\"modal-root\">` in the layout).",
		},
	)
}
