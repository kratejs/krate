// Package version exposes the running Krate compiler version to internal
// packages (plugin context assembly, diagnostics) so a single value is threaded
// everywhere instead of hardcoded strings.
package version

// Value is the compiler version. The krate CLI overrides it at startup from the
// build-time ldflags value (`-X main.version=...`); the default is used by tests
// and library consumers.
var Value = "dev"
