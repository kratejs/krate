package diag

// Native (desktop) release builds and derived permissions (KR7210 to KR7219).
const (
	// CodeNativePermission: the code reaches an SDK permission that
	// native.permissions does not allow.
	CodeNativePermission Code = "KR7210"
	// CodeNativeReleaseBuild: go build of the release executable failed.
	CodeNativeReleaseBuild Code = "KR7211"
	// CodeNativeNeedGo: a build step needs the Go toolchain and it is missing.
	CodeNativeNeedGo Code = "KR7212"
	// CodeNativeModuleVersion: the native Go module version cannot be determined.
	CodeNativeModuleVersion Code = "KR7213"
	// CodeNativeProfile: krate native profile cannot collect a profile.
	CodeNativeProfile Code = "KR7214"
)

func init() {
	Register(
		CodeInfo{
			Code:  CodeNativePermission,
			Title: "Code uses an SDK permission that native.permissions does not allow",
			Explanation: "A native build works out the SDK permissions the app needs from the code (imports from " +
				"`@krate/app/<area>` in UI and TypeScript host code, and `krate/<area>.<op>` ids) and writes them to " +
				"app.json. `native.permissions` is an allowlist: when it is set, every permission the code needs must " +
				"be listed there (`fs` also covers `fs:read`). The message names the import site and the permission. " +
				"Add the permission to `native.permissions`, or remove the call.",
		},
		CodeInfo{
			Code:  CodeNativeReleaseBuild,
			Title: "Release executable failed to compile",
			Explanation: "`krate native build --release` links the platform layer, the SDK, the app's Go host code and " +
				"the UI assets into one executable with `go build`. The output of the Go command is shown. Check that " +
				"the Go host packages compile for the target (`CGO_ENABLED=0`, so cgo packages cannot be used), and " +
				"run `go mod tidy` in the project.",
		},
		CodeInfo{
			Code:  CodeNativeNeedGo,
			Title: "The Go toolchain is required",
			Explanation: "Apps with Go host code always need `go` on PATH to build, and `native.release.compile: " +
				"\"always\"` asks for a compiled release executable. Install Go (the version in the native module's " +
				"go.mod), or for an app without Go host code set `native.release.compile` to \"auto\" or \"never\" to " +
				"get the prebuilt-shell layout (`dist-native/app` next to `krate-shell`).",
		},
		CodeInfo{
			Code:  CodeNativeModuleVersion,
			Title: "Cannot determine the native module version",
			Explanation: "A release build of an app without Go code generates a small Go module that requires " +
				"`github.com/kratejs/krate/packages/native`. Outside the Krate monorepo it needs the module version " +
				"that matches this compiler. A development build of the compiler has none: set " +
				"`KRATE_NATIVE_VERSION` (for example `v0.5.0`), or build from a release of `krate`.",
		},
		CodeInfo{
			Code:  CodeNativeProfile,
			Title: "krate native profile cannot collect a profile",
			Explanation: "`krate native profile` builds the app (a release executable when Go is available), runs it with " +
				"`KRATE_CPUPROFILE` set and copies the profile to `native.release.pgo` when the app exits. The message " +
				"says which step failed: the app produced no profile (it must exit normally, for example by closing its " +
				"window or sending SIGINT), or the profile could not be copied.",
		},
	)
}
