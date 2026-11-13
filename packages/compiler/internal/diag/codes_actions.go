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
			Explanation: `A "use server" directive at the top of a function inside a component is hoisted into a server ` +
				`action. It is rejected inside a "use client" module, in a class, when it uses this, and when it captures values from a ` +
				`component that is not async (captured values are sent to the browser encrypted and need a per-request ` +
				`render). Move the function into a module whose first statement is "use server" and import it, or ` +
				`declare it inside an async server component.`,
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
			Code:  CodeActionExportNotFunction,
			Title: `"use server" module exports a non-function`,
			Explanation: `Every export of a "use server" module becomes a public endpoint and must be a function. ` +
				`An export built by a wrapper call (export const save = createAction(...)) is accepted and checked ` +
				`when the module loads; a literal, object, array or class can never be an action and is rejected at build time.`,
		},
		CodeInfo{
			Code:  CodeActionReexport,
			Title: `"use server" module uses a star re-export`,
			Explanation: `A star re-export (export * or export * as) cannot be turned into named endpoints. ` +
				`Re-export the actions by name: export { save } from "./other".`,
		},
		CodeInfo{
			Code:        CodeActionImportMissing,
			Title:       "Imported server action does not exist",
			Explanation: `The import names an export that the "use server" module does not declare.`,
		},
	)
}
