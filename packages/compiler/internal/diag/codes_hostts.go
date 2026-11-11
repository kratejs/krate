package diag

// Native (desktop) target: splitting TypeScript host code from the UI and
// bundling it for the embedded host runtime (KR7190 to KR7199).
const (
	// CodeHostBoundaryType: a host function parameter has a type that cannot cross.
	CodeHostBoundaryType Code = "KR7190"
	// CodeHostSplit: an inline host function cannot be moved out of its module.
	CodeHostSplit Code = "KR7191"
	// CodeHostBundle: bundling host code failed.
	CodeHostBundle Code = "KR7192"
	// CodeHostUnsupportedAPI: host code uses an API the embedded host runtime lacks.
	CodeHostUnsupportedAPI Code = "KR7193"
	// CodeHostImportMissing: the UI imports a name a host module does not export.
	CodeHostImportMissing Code = "KR7195"
	// CodeHostValueExport: the UI imports a host export that is neither a function nor a host signal.
	CodeHostValueExport Code = "KR7196"
)

func init() {
	Register(
		CodeInfo{
			Code:  CodeHostBoundaryType,
			Title: "Host function parameter cannot cross the boundary",
			Explanation: "Arguments and results of a host function travel between the webview and the native process " +
				"as data: strings, numbers, booleans, null, arrays, plain objects, Date, Map, Set and bytes. A " +
				"parameter typed as a function, as a class you declared, or as a DOM type (`HTMLElement`, `Node`, " +
				"`Event` and friends) can never be sent, so the call would fail at run time. Pass the plain data " +
				"you need from it instead, for example the element's `value` or an id. The check only reads " +
				"explicit parameter annotations and stays silent when it is not sure.",
		},
		CodeInfo{
			Code:  CodeHostSplit,
			Title: "Inline host function cannot be split out",
			Explanation: "An `@host` function inside a UI module is moved into the host bundle and replaced by a stub " +
				"in the UI. That needs a name to register and call it by, so an anonymous default export cannot be " +
				"split. Give the function a name, or move it to a `.host.ts` module.",
		},
		CodeInfo{
			Code:  CodeHostBundle,
			Title: "Host code failed to bundle",
			Explanation: "The host entry and the host modules it imports are bundled into one ES2020 script for the " +
				"embedded host runtime. The bundler reported an error, usually an import that cannot be resolved, a " +
				"syntax error, or syntax the bundle cannot express (top-level await in a script bundle).",
		},
		CodeInfo{
			Code:  CodeHostUnsupportedAPI,
			Title: "Host code uses an API the embedded runtime lacks",
			Explanation: "Host code runs on an embedded JavaScript runtime (QuickJS) by default. It has the ECMAScript " +
				"built-ins and a small set of web APIs, but no Node built-in modules and no DOM. The build found a " +
				"reference to an API the runtime does not provide, so the call would throw. Implement the work as a " +
				"Go host function, or run the module on a full runtime with `native.host.islands`. A reference " +
				"inside a feature check such as `typeof X !== \"undefined\"` is not reported.",
		},
		CodeInfo{
			Code:  CodeHostImportMissing,
			Title: "Import is not exported by the host module",
			Explanation: "UI code imports a name from a host module, but the module has no such export (or the export " +
				"is marked for another tier). The stub that replaces the host module in the UI bundle only has the " +
				"module's real exports, so the import would be undefined at run time.",
		},
		CodeInfo{
			Code:  CodeHostValueExport,
			Title: "Host export is not callable from the UI",
			Explanation: "The UI can call host functions and read host signals. An export that is a plain value (a " +
				"number, string, object or class) has no meaning across the boundary because the UI would get a " +
				"copy that never changes. Export a function that returns the value, or a `hostSignal()` for a value " +
				"that changes.",
		},
	)
}
