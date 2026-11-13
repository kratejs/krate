package pkg

// Signer signs a distribution while it is being packaged. Package calls
// SignStage after the unpacked distribution is written and before any
// archive or installer is built from it, and SignFile on each installer.
type Signer interface {
	// SignStage signs the staged distribution (the .app on macOS, the
	// executables of the install tree on Windows).
	SignStage(stage string) error
	// SignFile signs one produced file: format is "dmg", "nsis", "msix",
	// "deb", "rpm", "appimage" or "tar.gz".
	SignFile(path, format string) error
}
