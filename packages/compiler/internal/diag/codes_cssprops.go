package diag

// KR833x: CSS signals passed between components.
const (
	// CodeScopeHandleAtRuntime: a CSS scope handle that exists only at build
	// time is read by code that runs in the browser.
	CodeScopeHandleAtRuntime Code = "KR8330"
	// CodeCSSSignalInBuildTimeComponent: a CSS signal getter or setter passed
	// to a component that is rendered at build time and reads it.
	CodeCSSSignalInBuildTimeComponent Code = "KR8331"
)

func init() {
	Register(
		CodeInfo{
			Code:  CodeScopeHandleAtRuntime,
			Title: "CSS scope handle is read by code that runs in the browser",
			Explanation: "A CSS scope handle (`const tab = createCSSChoice(...)`) is a build-time value: the " +
				"compiler turns `tab.trigger()`, `tab.panel()`, `tab.close()` and `tab.current()` spreads into " +
				"labels, panel classes and CSS, and nothing of the handle exists in the browser. Passing it as a " +
				"prop works when the receiving component only uses those helpers in its JSX, at any depth. This " +
				"component reads the handle some other way (in an event handler, an effect, a computed value or a " +
				"call to another function), or it was passed to an external island, so the read would fail in the " +
				"browser. Use the helpers in the JSX of the component that receives the handle, or pass the " +
				"getter and setter values (`const [tab, setTab] = createCSSChoice(...)`) to a component with its " +
				"own client state, or use createSignal.",
		},
		CodeInfo{
			Code:  CodeCSSSignalInBuildTimeComponent,
			Title: "CSS signal value passed to a component that renders at build time",
			Explanation: "The state of a CSS signal lives in the page (hidden inputs and :has() selectors), so no " +
				"value exists at build time. A component without client state is rendered to static HTML during " +
				"the build, which makes a CSS signal getter or setter passed to it unreadable: `props.tab()` has " +
				"nothing to return. Pass the scope handle instead (`const tab = createCSSChoice(...)`, " +
				"`<Child scope={tab} />`) and use `props.scope.current()`, `props.scope.panel()` or " +
				"`props.scope.trigger()` in the child, or give the child its own state with createSignal.",
		},
	)
}
