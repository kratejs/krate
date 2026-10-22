package diag

// KR83xx: render-scope reactivity of React source components (values computed
// in the component body, custom hook results, imported helpers).
const (
	// CodeStaleRenderValue: a render-body value that cannot be recomputed.
	CodeStaleRenderValue Code = "KR8301"
	// CodeUnbundledImport: an imported binding hydration code needs but the
	// compiler cannot bundle.
	CodeUnbundledImport Code = "KR8302"
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
			Explanation: "Hydration code (an event handler, effect, derived value or a module-level initializer) uses " +
				"a binding imported from another module, and the compiler could not copy that module's declaration " +
				"into the page's hydration script: the import is a package, a namespace or default import, a class, " +
				"or a declaration with side effects. At runtime the name would be undefined. Move the helper into a " +
				"local module and import it by name, or define it in the component's module.",
		},
	)
}
