package diag

// KR7xxx: React 19 actions and server actions.
const (
	// CodeInlineUseServer: a "use server" directive inside a function body.
	CodeInlineUseServer Code = "KR7001"
	// CodeFormStatusNoElement: useFormStatus() in a component that does not
	// return a DOM element.
	CodeFormStatusNoElement Code = "KR7002"
	// CodeActionStateInitial: the useActionState initial state is not known at
	// build time, so the no-JS form fallback sends null.
	CodeActionStateInitial Code = "KR7003"
	// CodeActionExportNotFunction: a "use server" module exports a non-function.
	CodeActionExportNotFunction Code = "KR7004"
	// CodeActionReexport: a "use server" module re-exports from another module.
	CodeActionReexport Code = "KR7005"
	// CodeActionImportMissing: an import names an export the "use server"
	// module does not have.
	CodeActionImportMissing Code = "KR7006"
)

func init() {
	Register(
		CodeInfo{
			Code:  CodeInlineUseServer,
			Title: `Inline "use server" function`,
			Explanation: `React allows a "use server" directive at the top of an async function inside a component. ` +
				`Krate only supports file-level server actions: move the function into a module whose first ` +
				`statement is "use server", export it, and import it where the form uses it.`,
		},
		CodeInfo{
			Code:  CodeFormStatusNoElement,
			Title: "useFormStatus() component does not render an element",
			Explanation: `Krate finds the form a useFormStatus() call belongs to from the DOM element the component ` +
				`returns. When the component returns another component, a string, or nothing, the status cannot be ` +
				`connected and always reads as idle. Return a DOM element (for example the submit <button>).`,
		},
		CodeInfo{
			Code:  CodeActionStateInitial,
			Title: "useActionState initial state is not known at build time",
			Explanation: `A form whose action is a useActionState dispatch around a server action still posts to the ` +
				`action URL without JavaScript. The previous state for that submission is the initial state, encoded ` +
				`into the URL at build time; when it cannot be evaluated the server receives null.`,
		},
		CodeInfo{
			Code:        CodeActionExportNotFunction,
			Title:       `"use server" module exports a non-function`,
			Explanation: `Every export of a "use server" module becomes a public endpoint and must be an async function.`,
		},
		CodeInfo{
			Code:        CodeActionReexport,
			Title:       `"use server" module re-exports another module`,
			Explanation: `Re-exports cannot be bundled as server actions. Import the function and re-declare it as an export.`,
		},
		CodeInfo{
			Code:        CodeActionImportMissing,
			Title:       "Imported server action does not exist",
			Explanation: `The import names an export that the "use server" module does not declare.`,
		},
	)
}
