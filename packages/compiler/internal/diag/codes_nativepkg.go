package diag

// Native (desktop) target: packaging, signing, notarization and updater keys
// (KR7220 to KR7229).
const (
	// CodeNativePkgInput: nothing to package, or the input is unusable.
	CodeNativePkgInput Code = "KR7220"
	// CodeNativePkgTool: a platform tool the requested format needs is missing.
	CodeNativePkgTool Code = "KR7221"
	// CodeNativePkgFormat: unknown package format, or one the target OS cannot have.
	CodeNativePkgFormat Code = "KR7222"
	// CodeNativePkgIcon: the application icon cannot be read.
	CodeNativePkgIcon Code = "KR7223"
	// CodeNativeSignTool: the signing tool is missing or the host OS cannot sign.
	CodeNativeSignTool Code = "KR7224"
	// CodeNativeSignFailed: a signing or notarization command failed.
	CodeNativeSignFailed Code = "KR7225"
	// CodeNativeNotarize: notarization credentials are missing or incomplete.
	CodeNativeNotarize Code = "KR7226"
	// CodeNativeUpdaterKey: the updater signing key is missing or invalid.
	CodeNativeUpdaterKey Code = "KR7227"
	// CodeNativePkgWrite: writing a package file failed.
	CodeNativePkgWrite Code = "KR7228"
	// CodeNativePkgMeta: the app metadata cannot be packaged.
	CodeNativePkgMeta Code = "KR7229"
)

func init() {
	Register(
		CodeInfo{
			Code:  CodeNativePkgInput,
			Title: "Nothing to package",
			Explanation: "`krate native package` packages a built app. It looks in `dist-native/<os>-<arch>/` for a " +
				"release executable (`krate native build --release`), and falls back to the prebuilt-shell layout " +
				"(`dist-native/krate-shell` next to `dist-native/app/`, from `krate native build --shell auto`). " +
				"Build first, or pass `--target <os>/<arch>` to package a different platform's output.",
		},
		CodeInfo{
			Code:  CodeNativePkgTool,
			Title: "A packaging tool is not installed",
			Explanation: "Most formats are written by the compiler itself (zip, tar.gz, .deb, .rpm, MSIX, .app). " +
				"Some need a platform tool that cannot be replaced: `makensis` for an NSIS installer, " +
				"`appimagetool` for an AppImage, `hdiutil` (macOS only) for a .dmg. Install the tool or pick " +
				"another format with `--format`. On macOS without `hdiutil` the .app is delivered as a .zip.",
		},
		CodeInfo{
			Code:  CodeNativePkgFormat,
			Title: "Unknown or unsupported package format",
			Explanation: "Formats by platform: macOS `app`, `zip`, `dmg`, `mas`; Windows `zip`, `nsis`, `msix`, `msix-store`; " +
				"Linux `tar.gz`, `deb`, `rpm`, `appimage`, `flatpak`, `snap`. `native.bundle.windows.installer` " +
				"takes `msix` or `nsis`; `native.bundle.linux.formats` takes `deb`, `rpm`, `appimage`, `flatpak` and `snap`.",
		},
		CodeInfo{
			Code:  CodeNativePkgIcon,
			Title: "Application icon cannot be used",
			Explanation: "`native.icons` must name a PNG (or JPEG) file of at least 256x256 pixels; 1024x1024 " +
				"gives sharp icons on every platform. The packager makes .icns, .ico and PNG sizes from it. " +
				"Without `native.icons` a placeholder icon is generated.",
		},
		CodeInfo{
			Code:  CodeNativeSignTool,
			Title: "Signing tool not available",
			Explanation: "macOS signing needs `codesign` and runs on macOS only. Windows signing needs " +
				"`signtool` (on Windows) or `osslsigncode` (anywhere). Linux package signatures need `gpg`. " +
				"Sign on a machine that has the tool, or skip signing for development builds.",
		},
		CodeInfo{
			Code:  CodeNativeSignFailed,
			Title: "Signing or notarization failed",
			Explanation: "The signing tool exited with an error; its output is shown with secrets removed. " +
				"Common causes: the identity is not in the keychain, the certificate expired, the timestamp " +
				"server is unreachable, or Apple rejected the submission (run `xcrun notarytool log <id>`).",
		},
		CodeInfo{
			Code:  CodeNativeNotarize,
			Title: "Notarization credentials missing",
			Explanation: "`krate native notarize` needs credentials: a keychain profile (`--keychain-profile` or " +
				"`KRATE_NOTARY_PROFILE`, created with `xcrun notarytool store-credentials`), or an App Store Connect " +
				"API key (`KRATE_NOTARY_KEY` file path, `KRATE_NOTARY_KEY_ID`, `KRATE_NOTARY_ISSUER`), or an Apple ID " +
				"(`KRATE_NOTARY_APPLE_ID`, `KRATE_NOTARY_TEAM_ID`, `KRATE_NOTARY_PASSWORD` app-specific password). " +
				"Prefer the profile or API key: a password on the command line is visible to other users.",
		},
		CodeInfo{
			Code:  CodeNativeUpdaterKey,
			Title: "Updater signing key missing or invalid",
			Explanation: "Update files are signed with an Ed25519 key. `krate native keygen` creates the pair; put " +
				"the public key in `native.updater.publicKey` and give the private key to `krate native package` " +
				"as `KRATE_UPDATER_PRIVATE_KEY` (the base64 text) or `KRATE_UPDATER_PRIVATE_KEY_FILE` (a path). " +
				"The private key must match the public key in the config.",
		},
		CodeInfo{
			Code:  CodeNativePkgWrite,
			Title: "Writing a package failed",
			Explanation: "A package file or its staging directory could not be written. The message names the " +
				"path and the operating system error.",
		},
		CodeInfo{
			Code:  CodeNativePkgMeta,
			Title: "App metadata cannot be packaged",
			Explanation: "Packaging needs `native.appId` (reverse-DNS), `native.productName` and a semantic " +
				"`native.version`; a MSIX package also needs `native.bundle.windows.publisher`. The message names " +
				"the field and the value that was rejected.",
		},
	)
}
