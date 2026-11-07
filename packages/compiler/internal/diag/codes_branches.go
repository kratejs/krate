package diag

// KR836x: conditional rendering where component state is lost.
const (
	// CodeBranchStateReset: two branches of a conditional render the same
	// component type, but the build cannot keep one instance across the flip.
	CodeBranchStateReset Code = "KR8360"
)

func init() {
	Register(
		CodeInfo{
			Code:  CodeBranchStateReset,
			Title: "Component state resets when the condition flips",
			Explanation: "`cond ? <Form mode=\"edit\" /> : <Form mode=\"new\" />` renders the same component type at " +
				"the same position. React keeps that instance when `cond` flips, so its state, effects and refs " +
				"survive, and Krate does the same by lowering the pair to one element with conditional props " +
				"(`<Form mode={cond ? \"edit\" : \"new\"} />`). A few shapes cannot be lowered that way: a render-prop " +
				"function as children, a spread whose position differs between the branches, a `ref` that differs, a " +
				"condition with side effects, a lazy component, or an external island. For those the branch that " +
				"is shown is mounted and the other is unmounted, so flipping the condition restarts the component " +
				"with fresh state and runs its cleanups and mount effects again. Render one element and pass the " +
				"differing values as props, make both branches the same shape, or give the branches different `key` " +
				"props when the restart is what you want (different keys never warn).",
		},
	)
}
