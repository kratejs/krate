package diag

// KR803x: React 19 hooks and components that need more than a rename.
const (
	// CodeHOCUnresolved: a higher-order component application that cannot
	// be inlined at build time.
	CodeHOCUnresolved Code = "KR8030"
	// CodeReact19Prop: a prop of Activity or ViewTransition that is ignored.
	CodeReact19Prop Code = "KR8031"
)

func init() {
	Register(
		CodeInfo{
			Code:  CodeHOCUnresolved,
			Title: "Higher-order component cannot be resolved at build time",
			Explanation: "`const Page = withX(Inner)` (or `export default withX(Inner)`) is compiled by inlining the " +
				"component function that `withX` returns, with its parameters replaced by the arguments. That works " +
				"when `withX` is a function in the project whose body holds only that returned component function. " +
				"A wrapper imported from a package, or one with other statements around the returned function, cannot " +
				"be inlined, so the component would render nothing; the build fails instead. Declare the wrapper in " +
				"your source as `function withX(C) { return function Wrapped(props) { return <C {...props} /> } }`, " +
				"or write the wrapped component out by hand.",
		},
		CodeInfo{
			Code:  CodeReact19Prop,
			Title: "Prop of Activity, ViewTransition or SuspenseList is not supported",
			Explanation: "`<Activity>` supports `mode=\"visible\"` and `mode=\"hidden\"`. `<ViewTransition>` supports " +
				"`name`, `default`, `enter`, `exit`, `update` and `share` as strings, and ignores the event callbacks " +
				"(`onEnter`, `onExit`, `onUpdate`, `onShare`). `<SuspenseList>` renders its children and ignores " +
				"`revealOrder` and `tail`. Other props have no effect and are reported once per " +
				"element. This is a warning: the children always render.",
		},
	)
}
