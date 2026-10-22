package diag

// KR3xxx: React class components and error boundaries.
const (
	CodeClassNoRender          Code = "KR3001"
	CodeClassUnknownThis       Code = "KR3002"
	CodeClassStaticMember      Code = "KR3003"
	CodeClassComputedMember    Code = "KR3004"
	CodeClassWillMount         Code = "KR3005"
	CodeClassLegacyContext     Code = "KR3006"
	CodeClassStaticThis        Code = "KR3007"
	CodeBoundaryNoDerivedState Code = "KR3008"
	CodeBoundaryBuildFallback  Code = "KR3009"
	CodeBuildTimeThrow         Code = "KR3010"
)

func init() {
	Register(
		CodeInfo{CodeClassNoRender, "Class component has no render method",
			"A React class component must define render(). Without it Krate cannot lower the class and the component renders nothing. Add render() returning JSX, or convert the class to a function component."},
		CodeInfo{CodeClassUnknownThis, "Unknown instance property on a class component",
			"The class reads this.<name>, but <name> is not a field, method, accessor, props, state, context, setState or forceUpdate, and nothing assigns it. Lowered classes have no React base-class internals (this.refs, this.updater, this.isMounted). Declare the property as a field or method, or read it from props or state."},
		CodeInfo{CodeClassStaticMember, "Unsupported static class member",
			"Only defaultProps, propTypes, displayName, contextType, getDerivedStateFromProps and getDerivedStateFromError are lowered. Other static fields, methods and static blocks have no instance equivalent after lowering; move them to module scope next to the component."},
		CodeInfo{CodeClassComputedMember, "Computed class member name",
			"Class members named with a computed key ([expr]) cannot be resolved at build time. Give the member a plain name."},
		CodeInfo{CodeClassWillMount, "componentWillMount is not supported",
			"UNSAFE_componentWillMount (and componentWillMount) runs before the first render, which Krate performs at build time. Initialize state in the constructor or a state field, and move side effects to componentDidMount. componentWillReceiveProps and componentWillUpdate (and their UNSAFE_ forms) are supported."},
		CodeInfo{CodeClassLegacyContext, "Legacy class context is not supported",
			"static contextTypes and childContextTypes (the pre-16.3 context API) are not lowered. Use static contextType = MyContext and this.context, or a function component with useContext."},
		CodeInfo{CodeClassStaticThis, "this inside a static lifecycle",
			"getDerivedStateFromProps and getDerivedStateFromError are static: React calls them without an instance and Krate lowers them to plain functions. Use their parameters instead of this."},
		CodeInfo{CodeBoundaryNoDerivedState, "Error boundary without getDerivedStateFromError",
			"A class with componentDidCatch but no static getDerivedStateFromError catches errors, but React has no state update to render a fallback with: the failed subtree is unmounted and its last DOM stays on the page until componentDidCatch calls setState. Add getDerivedStateFromError returning the fallback state."},
		CodeInfo{CodeBoundaryBuildFallback, "Error boundary rendered its fallback at build time",
			"A component inside an error boundary threw while the page was built. Krate rendered the boundary's fallback (the state returned by getDerivedStateFromError) instead of failing the build, and calls componentDidCatch when the page hydrates. Fix the error, or keep the fallback if it is expected."},
		CodeInfo{CodeBuildTimeThrow, "Component threw while the page was built",
			"A component threw (a throw statement ran) while Krate rendered the page at build time, and no error boundary encloses it, so the build fails. Fix the error, guard it with a condition, or wrap the component in an error boundary to render a fallback instead."},
	)
}
