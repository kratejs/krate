package diag

// KR83xx: render-scope reactivity of React source components (values computed
// in the component body, custom hook results, imported helpers).
const (
	// CodeStaleRenderValue: a render-body value that cannot be recomputed.
	CodeStaleRenderValue Code = "KR8301"
	// CodeUnbundledImport: an imported binding hydration code needs but the
	// compiler cannot bundle.
	CodeUnbundledImport Code = "KR8302"
	// CodeEnvNotExposed: a process.env / import.meta.env read in client or
	// statically rendered code that is not a public variable.
	CodeEnvNotExposed Code = "KR8310"
	// CodeDynamicTag: an element type held in a variable that the build
	// cannot resolve to one tag.
	CodeDynamicTag Code = "KR8320"
	// CodeUnresolvedMemberTag: a member tag (<ns.Button>) that names no
	// component the build can find.
	CodeUnresolvedMemberTag Code = "KR8321"
)

func init() {
	Register(
		CodeInfo{
			Code:  CodeStaleRenderValue,
			Title: "Render value keeps its first value",
			Explanation: "React recomputes every value in a component body on each render. Krate compiles a body " +
				"value that reads state, props, context or a custom hook result into a derived value that is " +
				"recomputed when its inputs change. This value could not be compiled that way: it is reassigned " +
				"inside a closure (an event handler or effect), or by a body statement that also writes another " +
				"variable, or it is read between the statements that build it. It keeps the value it had when the " +
				"component hydrated. Compute it in a single expression (const label = n > 1 ? \"items\" : \"item\"), " +
				"or keep the changing value in state.",
		},
		CodeInfo{
			Code:  CodeUnbundledImport,
			Title: "Imported value is not available to hydration code",
			Explanation: "Hydration code (an event handler, effect, derived value, text binding or a module-level " +
				"initializer) uses a binding imported from another local module, and the compiler could not copy " +
				"that module's declaration into the page's hydration script. Functions, classes, constants, lowered " +
				"namespaces, namespace imports and JSON, ?raw and ?url imports are copied; npm packages are " +
				"bundled for the browser. A binding created by a destructuring export, or one that only a side " +
				"effect initializes, cannot be copied, so at runtime the name would be undefined. Export it as a " +
				"function, class or plain const, or define it in the component's module.",
		},
		CodeInfo{
			Code:  CodeEnvNotExposed,
			Title: "Environment variable is not exposed to client code",
			Explanation: "Client code and statically rendered pages cannot read process.env or import.meta.env " +
				"values: they are build and server-time only and are never written into HTML or JavaScript. The " +
				"read yields undefined. Name the variable with the KRATE_PUBLIC_ prefix (KRATE_PUBLIC_API_URL) " +
				"to inline its value at build time, or read it in a server component, API route or server " +
				"action instead. import.meta.env.MODE, DEV, PROD, BASE_URL and SSR are always available.",
		},
		CodeInfo{
			Code:  CodeDynamicTag,
			Title: "Element type is not known at build time",
			Explanation: "`<Tag>` where `Tag` is a variable (`const Tag = as || \"div\"`, a prop, a value read from " +
				"state) is rendered as the element it names. The build resolves it when the variable folds to one " +
				"tag name: a string constant, a prop the caller passes or its default, or a condition on those. " +
				"This one does not, for example because it depends on a signal or on a value only known at request " +
				"time, and the page would otherwise contain an empty placeholder instead of the element and its " +
				"children, so the build fails. Pass the tag as a string the build can see, render each case as " +
				"its own element (`{wide ? <section>...</section> : <div>...</div>}`), or move the component to a " +
				"`*.runtime.tsx` file to choose the tag per request.",
		},
		CodeInfo{
			Code:  CodeUnresolvedMemberTag,
			Title: "Member tag does not resolve to a component",
			Explanation: "`<ui.Button>` names the export `Button` of a namespace import (`import * as ui from " +
				"\"./ui\"`), of a re-exported namespace (`export * as ui from \"./ui\"`), or a member of an object. " +
				"The build follows namespace imports through barrel files (`export { Button } from \"./Button\"`, " +
				"`export * from \"./Button\"`) to the function component the member names. This tag matched none: " +
				"the module exports no such member, the member is not a function component (a string, a class that " +
				"is not lowered, a value built at run time), or the object it reads from is not a namespace of " +
				"project modules. Rendering nothing would drop the element and its children, so the build fails. " +
				"Check the member's spelling and export, import the component by name, or declare it as a function.",
		},
	)
}
