package diag

// KR2xxx: React effects, dependency arrays and hooks.
const (
	// CodeDepsUnresolved: a dependency array passed through a binding the
	// compiler cannot see into.
	CodeDepsUnresolved Code = "KR2001"
	// CodeEffectNoDeps: an effect without a dependency array.
	CodeEffectNoDeps Code = "KR2002"
	// CodeAsyncEffect: an async function passed as an effect callback.
	CodeAsyncEffect Code = "KR2003"
	// CodeCallbackNoDeps: useCallback without a dependency array.
	CodeCallbackNoDeps Code = "KR2004"
	// CodeMemoNoDeps: useMemo without a dependency array.
	CodeMemoNoDeps Code = "KR2005"
	// CodeDepsNotArray: a dependency argument that is not an array.
	CodeDepsNotArray Code = "KR2006"
	// CodeCleanupReadsState: an effect cleanup that reads state.
	CodeCleanupReadsState Code = "KR2007"
)

func init() {
	Register(
		CodeInfo{
			Code:  CodeDepsUnresolved,
			Title: "Dependency array is not statically resolvable",
			Explanation: "The dependency list is a binding the compiler cannot resolve to an array literal in the same " +
				"component (for example a parameter or an imported value). It is compared element by element (Object.is) " +
				"whenever it is re-read, but Krate components run once, so a captured array never changes and the hook " +
				"runs only on mount. Write the array inline, e.g. useEffect(fn, [a, b]), or bind it with " +
				"const deps = [a, b] in the component body.",
		},
		CodeInfo{
			Code:  CodeEffectNoDeps,
			Title: "Effect without a dependency array",
			Explanation: "React runs such an effect after every render. Krate has no re-renders, so the effect re-runs " +
				"after every commit that changes the component's own state (useState/useReducer) or its props, " +
				"which is when React would have re-rendered it because of its own state or props. Re-renders caused " +
				"only by a parent or a context are not modelled. Pass an explicit dependency array to make the trigger " +
				"explicit, or [] to run once.",
		},
		CodeInfo{
			Code:  CodeAsyncEffect,
			Title: "Async effect callback",
			Explanation: "An async function returns a Promise, not a cleanup function, so its cleanup is ignored (React " +
				"warns about this too). Declare an async function inside the effect and call it, returning a " +
				"synchronous cleanup.",
		},
		CodeInfo{
			Code:  CodeCallbackNoDeps,
			Title: "useCallback without a dependency array",
			Explanation: "React returns a new function on every render when the dependency array is omitted. Krate " +
				"components run once, so the callback keeps one identity for the component's lifetime; effects or " +
				"memos that list it as a dependency do not re-run. Pass the dependency array the callback reads.",
		},
		CodeInfo{
			Code:  CodeMemoNoDeps,
			Title: "useMemo without a dependency array",
			Explanation: "React recomputes such a memo on every render. Krate recomputes it when a signal it reads " +
				"changes, which matches whenever the value depends only on state and props. Pass the dependency array " +
				"to make recomputation explicit.",
		},
		CodeInfo{
			Code:  CodeDepsNotArray,
			Title: "Dependency argument is not an array",
			Explanation: "The second argument of useEffect/useLayoutEffect/useInsertionEffect/useMemo/useCallback must " +
				"be an array (React warns otherwise). The value is ignored and the hook is treated as having no " +
				"dependency array. Pass [a, b] or omit the argument.",
		},
		CodeInfo{
			Code:  CodeCleanupReadsState,
			Title: "Effect cleanup reads state",
			Explanation: "In React a cleanup closes over the values of the render its effect ran for, so it sees the " +
				"previous state. Krate state reads are live getters, so a cleanup sees the current (new) value. " +
				"Copy the value into a local in the effect body and read that in the cleanup: " +
				"const id = roomId; return () => leave(id).",
		},
	)
}
