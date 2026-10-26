package diag

// KR8xxx: islands, compiled packages and general React compatibility.
const (
	// CodeIslandLocalComponent: same-file Krate component inside an island.
	CodeIslandLocalComponent Code = "KR8007"
	// CodeIslandClientOnly: an island cannot be server-rendered.
	CodeIslandClientOnly Code = "KR8008"
	// CodeIslandAsset: an island asset or package could not be resolved.
	CodeIslandAsset Code = "KR8009"
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
			Code:  CodeIslandAsset,
			Title: "Island asset could not be built",
			Explanation: "Krate could not bundle the island's external package, local module, Preact " +
				"dependency, or generated chunk. Check the reported resolver error and install the missing " +
				"package (Preact 10.20 or newer) in the project.",
		},
	)
}
