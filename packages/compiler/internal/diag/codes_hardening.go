package diag

// KR835x: build hardening checks that turn a silent mistake into an error.
const (
	// CodeUnsafeOutDir: an outDir that is, or contains, the project's sources.
	CodeUnsafeOutDir Code = "KR8350"
	// CodeScopePartUnpaired: a trigger or content of a CSS-signal widget
	// whose value no counterpart answers.
	CodeScopePartUnpaired Code = "KR8351"
	// CodeBuildTimeUnknown: a value a build-time rendered component needs but
	// the build cannot determine.
	CodeBuildTimeUnknown Code = "KR8352"
	// CodeHeadValueDeferred: a <Head> value the build could not evaluate.
	CodeHeadValueDeferred Code = "KR8353"
	// CodeJSXPropRenderedTwice: a JSX prop holding a component with state that
	// the receiving component renders in more than one place.
	CodeJSXPropRenderedTwice Code = "KR8354"
)

func init() {
	Register(
		CodeInfo{
			Code:  CodeUnsafeOutDir,
			Title: "outDir would delete project sources",
			Explanation: "The build empties outDir before it writes the site, and `krate clean` deletes it. An outDir " +
				"that is, or contains, the project root, the pages directory or the public directory (`outDir: \"..\"`, " +
				"`\".\"`, `\"src\"`) would delete the project's own files, so the build stops before anything is " +
				"removed. Point outDir at a dedicated directory such as `dist`; a directory outside the project that " +
				"holds none of your sources, such as `../site-dist`, is fine. Setting KRATE_ALLOW_UNSAFE_OUTDIR=1 " +
				"skips this check.",
		},
		CodeInfo{
			Code:  CodeScopePartUnpaired,
			Title: "Trigger and content do not share a value",
			Explanation: "`Dialog`, `Sheet`, `Popover`, `DropdownMenu` and the other widgets built on a CSS-signal " +
				"scope connect a trigger to its content by `value`: `<Dialog.Trigger value=\"a\">` opens " +
				"`<Dialog.Content value=\"a\">`. A trigger and a content that both leave `value` off are paired " +
				"with each other (the value is `on`). This one has no value, but the widget has no part without a " +
				"value to answer it (every content has a value, or every trigger does), so it would never open or " +
				"show anything. Give the trigger and the content the same `value`, or remove it from both.",
		},
		CodeInfo{
			Code:  CodeBuildTimeUnknown,
			Title: "Value needed at build time is unknown",
			Explanation: "A component without state (no createSignal, useState or other hook that holds state) is " +
				"rendered to static HTML while the site is built, so every value it renders has to be known " +
				"then: a prop, a local variable, or a module constant. This expression is none of those, for " +
				"example it names an identifier that is not defined where the component renders (`unresolved " +
				"identifier x`), reads the clock or the browser, or calls a function the build cannot run. A " +
				"constant that holds JSX (an array of sections whose `body` is a fragment) is known, and so is " +
				"JSX passed as a prop: both are rendered to HTML at build time. There " +
				"is no run time to fall back on, so the build fails instead of printing a guess. Define or import " +
				"the name, pass the value as a prop, or give the component state so it hydrates in the browser. " +
				"A value that differs per request belongs in a `*.runtime.tsx` component.",
		},
		CodeInfo{
			Code:  CodeHeadValueDeferred,
			Title: "Head value cannot be evaluated at build time",
			Explanation: "Tags inside `<Head>` (and `<title>`, `<meta>` and `<link>` rendered on their own) are " +
				"written into the static HTML so crawlers and link previews see them. A call to an imported helper " +
				"is evaluated at build time when it is plain code: string and template work, JSON, `URL`, " +
				"`encodeURIComponent`, and calls to other functions of your project. This value is none of those " +
				"(it reads the browser, the clock, `process.env`, an object built by a class, or something else " +
				"the build cannot run), so the tag would only be completed in the browser and crawlers would " +
				"see it empty. Compute it from plain values, pass it as a prop, or move the tag to a page whose " +
				"metadata comes from `generateMetadata`.",
		},
		CodeInfo{
			Code:  CodeJSXPropRenderedTwice,
			Title: "Component with state in a prop is rendered twice",
			Explanation: "A prop that holds JSX with a component that has state (`slot={<Probe />}`, where Probe uses " +
				"createSignal or useState) is one component instance. The receiving component reads the prop in more " +
				"than one place, but the instance can only be mounted in one of them, so the other copy shows the " +
				"server markup without ever hydrating. Render the prop in one place, or give each place its own " +
				"element (a separate prop for each place).",
		},
	)
}
