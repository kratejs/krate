package diag

// Native store and sandbox distribution (KR7300 to KR7304).
const (
	// CodeNativePkgUniversal: the arm64 and amd64 builds cannot be merged into
	// one macOS universal app.
	CodeNativePkgUniversal Code = "KR7300"
	// CodeNativeStoreConfig: a store build (Mac App Store, Microsoft Store)
	// lacks required identity fields or breaks a store rule.
	CodeNativeStoreConfig Code = "KR7301"
	// CodeNativeSandboxPkg: a Flatpak or Snap package cannot be generated.
	CodeNativeSandboxPkg Code = "KR7302"
	// CodeNativeStoreUpdater: native.updater is not allowed in a store build.
	CodeNativeStoreUpdater Code = "KR7303"
)

func init() {
	Register(
		CodeInfo{
			Code:  CodeNativePkgUniversal,
			Title: "Cannot merge a macOS universal app",
			Explanation: "`--arch universal` (or `--target darwin/universal`) merges a darwin/arm64 build and a " +
				"darwin/amd64 build into one `.app`: the executable and every Mach-O file the two builds share " +
				"(Go host children, compiled Node, Bun and Deno hosts) become universal files. Both builds must " +
				"exist and must contain the same files. Build both with `krate native build --release --arch universal`. " +
				"A host runtime tool that cannot produce the other architecture needs to be built on a machine that can, " +
				"or switched off for the universal app.",
		},
		CodeInfo{
			Code:  CodeNativeStoreConfig,
			Title: "Store build configuration problem",
			Explanation: "A Mac App Store build (`--target mas`) needs `native.bundle.macos.mas.teamId` and a " +
				"provisioning profile; a Microsoft Store build (`--format msix-store`) needs " +
				"`native.bundle.windows.store.identityName`, `publisherId` and `publisherDisplayName` exactly as " +
				"Partner Center shows them. The message names the field and the rule.",
		},
		CodeInfo{
			Code:  CodeNativeSandboxPkg,
			Title: "Flatpak or Snap package cannot be generated",
			Explanation: "The Flatpak manifest, AppStream metadata and snapcraft.yaml are generated from `native.*` " +
				"values; a value that breaks the format (an invalid license expression, a Flatpak runtime version " +
				"that is not a number, an unknown Snap grade) is reported with the field and a hint.",
		},
		CodeInfo{
			Code:  CodeNativeStoreUpdater,
			Title: "Self-updater in a store build",
			Explanation: "Stores deliver updates themselves: the Mac App Store, the Microsoft Store, Flathub and the " +
				"Snap Store reject apps that replace their own files. A store build removes `native.updater` from " +
				"the packaged app.json and warns; the running app also refuses updater calls inside a sandbox.",
		},
	)
}
