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
	)
}
