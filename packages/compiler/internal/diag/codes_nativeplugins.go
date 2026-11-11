package diag

// Native (desktop) plugins: manifests, resolution and the per-app shell
// (KR7250 to KR7259).
const (
	// CodePluginManifestMissing: a plugin in native.plugins has no krate-plugin.json.
	CodePluginManifestMissing Code = "KR7250"
	// CodePluginManifestInvalid: a krate-plugin.json is malformed or a field is invalid.
	CodePluginManifestInvalid Code = "KR7251"
	// CodePluginDuplicate: two ops, permissions or plugins share a name.
	CodePluginDuplicate Code = "KR7252"
	// CodePluginGoImport: the plugin's Go import path does not resolve to its package.
	CodePluginGoImport Code = "KR7253"
	// CodePluginNeedsNewerKrate: the plugin requires a newer Krate than this one.
	CodePluginNeedsNewerKrate Code = "KR7254"
	// CodePluginPermission: a plugin permission is unknown or not allowed.
	CodePluginPermission Code = "KR7255"
	// CodePluginShell: building the per-app shell of an app with plugins failed.
	CodePluginShell Code = "KR7256"
	// CodePluginOp: an op the manifest lists cannot be bound to Go code.
	CodePluginOp Code = "KR7257"
)

func init() {
	Register(
		CodeInfo{
			Code:  CodePluginManifestMissing,
			Title: "Plugin has no krate-plugin.json",
			Explanation: "Every entry of `native.plugins` names a directory, an npm package or a Go module that contains a " +
				"`krate-plugin.json` next to its Go package. The compiler could not find one at the resolved location. " +
				"Check the path or package name, install the package (npm i), or create the manifest with " +
				"`krate native plugin init <name>`.",
		},
		CodeInfo{
			Code:  CodePluginManifestInvalid,
			Title: "Invalid plugin manifest",
			Explanation: "`krate-plugin.json` must be valid JSON with a `name` (lowercase words joined by hyphens, 2 to 40 " +
				"characters), a `go` import path, optional `version`, `krate` (the minimum Krate version), `ts`, " +
				"`permissions` (a list of `{name, description}`) and `ops` (a list of `{name, permission, manual}`). Op names " +
				"are exported Go identifiers; permission names are lowercase letters, digits and hyphens, and an op's " +
				"`permission` must be one the manifest declares. The message names the field.",
		},
		CodeInfo{
			Code:  CodePluginDuplicate,
			Title: "Duplicate plugin, op or permission",
			Explanation: "Plugin names, the ops of one plugin and the permissions of one plugin must each be unique, because " +
				"an op is the host id `plugin/<name>.<op>` and a permission is `plugin:<name>.<permission>`. Rename one of the " +
				"two, or remove the plugin that is listed twice in `native.plugins`.",
		},
		CodeInfo{
			Code:  CodePluginGoImport,
			Title: "Plugin Go import path does not resolve",
			Explanation: "The `go` field of `krate-plugin.json` must be the import path of the plugin's Go package: the module " +
				"path from the nearest `go.mod` plus the package's directory inside that module. The plugin also has to " +
				"live in a Go module (its own `go.mod`, or the app's). For a `go:` plugin the module has to be downloadable " +
				"(`go mod download`); give the version after an `@` or require it in the app's go.mod.",
		},
		CodeInfo{
			Code:  CodePluginNeedsNewerKrate,
			Title: "Plugin needs a newer Krate",
			Explanation: "The plugin's `krate` field names the oldest Krate version it works with, and this compiler is older. " +
				"Upgrade Krate (`npm i @krate/core@latest`) or use an older release of the plugin.",
		},
		CodeInfo{
			Code:  CodePluginPermission,
			Title: "Unknown or disallowed plugin permission",
			Explanation: "A plugin permission is written `plugin:<plugin>.<permission>` and has to be declared by an enabled " +
				"plugin. It appears in `native.permissions` or `native.window.permissions` without such a plugin or " +
				"declaration. A build derives the plugin permissions the code needs; list them in `native.permissions` only " +
				"to restrict the app.",
		},
		CodeInfo{
			Code:  CodePluginShell,
			Title: "Could not build the shell for the app's plugins",
			Explanation: "An app with native plugins runs under a shell that is built for it with the Go toolchain, because " +
				"plugins are Go code linked into the shell process. The Go command failed or is missing. The output of " +
				"`go build` is shown; the plugin's package has to compile for the target with CGO_ENABLED=0, and its " +
				"module has to be resolvable (`go mod download`).",
		},
		CodeInfo{
			Code:  CodePluginOp,
			Title: "Plugin op cannot be bound",
			Explanation: "Each op in `krate-plugin.json` (not marked `manual`) must be an exported function or an exported " +
				"`*krate.Signal[T]` variable of the plugin's Go package, with a signature the host-call bindings support " +
				"(see Go host code). The Go error is shown with the position in the plugin.",
		},
	)
}
