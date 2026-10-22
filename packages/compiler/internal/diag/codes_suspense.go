package diag

// Suspense, lazy, use() and transitions (KR4xxx).
const (
	// CodeLazyLoader: lazy() was not given `() => import("./Module")`.
	CodeLazyLoader Code = "KR4001"
	// CodeLazyPlacement: lazy() is called inside a component or function.
	CodeLazyPlacement Code = "KR4002"
	// CodeUsePlacement: use(promise) is not the initializer of a declaration.
	CodeUsePlacement Code = "KR4003"
	// CodeUseDestructure: the result of use(promise) is destructured.
	CodeUseDestructure Code = "KR4004"
	// CodeProfilerOnRender: <Profiler onRender> is never called.
	CodeProfilerOnRender Code = "KR4005"
	// CodeLazyPackage: lazy() of a package is bundled with the page.
	CodeLazyPackage Code = "KR4006"
	// CodeLazyDefaultExport: the lazy module has no default component export.
	CodeLazyDefaultExport Code = "KR4007"
	// CodeSuspendNoBoundary: a component suspends outside any <Suspense>.
	CodeSuspendNoBoundary Code = "KR4008"
)

func init() {
	Register(
		CodeInfo{
			Code:  CodeLazyLoader,
			Title: "lazy() loader is not a static import()",
			Explanation: "Krate code-splits lazy components at build time, so the loader must be " +
				"`() => import(\"./Component\")` with a string literal path (optionally " +
				"`.then((m) => ({ default: m.Named }))`). Computed paths and other loaders cannot be resolved.",
		},
		CodeInfo{
			Code:  CodeLazyPlacement,
			Title: "lazy() is not declared at module level",
			Explanation: "Declare lazy components once at the top level of a module " +
				"(`const Chart = lazy(() => import(\"./Chart\"))`). React re-creates a lazy component " +
				"declared inside a component on every render, and Krate cannot split it.",
		},
		CodeInfo{
			Code:  CodeUsePlacement,
			Title: "use(promise) is not assigned to a variable",
			Explanation: "Krate lowers use(promise) to a reactive value, so it must initialize a " +
				"declaration in the component body: `const user = use(userPromise)`. The call may be " +
				"conditional inside that initializer. use(Context) works anywhere.",
		},
		CodeInfo{
			Code:  CodeUseDestructure,
			Title: "use(promise) result is destructured",
			Explanation: "The value of use(promise) is reactive and is read through its variable. " +
				"Assign it first and read fields from it: `const user = use(p); user.name`.",
		},
		CodeInfo{
			Code:  CodeProfilerOnRender,
			Title: "<Profiler onRender> is never called",
			Explanation: "Krate components do not re-render, so there are no commit timings to report. " +
				"<Profiler> renders its children; measure with the browser performance tools instead.",
		},
		CodeInfo{
			Code:  CodeLazyPackage,
			Title: "lazy() of a package is not code-split",
			Explanation: "Only local modules are split into separately loaded hydration chunks. A lazy " +
				"package import is compiled like a static import of that package.",
		},
		CodeInfo{
			Code:  CodeLazyDefaultExport,
			Title: "lazy() module has no default component export",
			Explanation: "The module passed to lazy() must `export default` a function component, or the " +
				"loader must map a named export: `import(\"./X\").then((m) => ({ default: m.Named }))`.",
		},
		CodeInfo{
			Code:  CodeSuspendNoBoundary,
			Title: "Component suspends outside a <Suspense> boundary",
			Explanation: "A component reads use(promise) or a resource but no <Suspense> encloses it, so " +
				"there is no fallback to show while it loads and its bindings stay empty until then. " +
				"Wrap it in <Suspense fallback={...}>.",
		},
	)
}
