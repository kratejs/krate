// Package gohost owns the Go host: it scans Go API routes and Go components,
// scaffolds the generated module (.krate/goapi) + vendored helper module
// (.krate/goext), compiles the host, renders components at build time, and
// generates TypeScript declarations. internal/build wires it into the build.
package gohost

import (
	"path/filepath"
	"runtime"
)

var isWindows = runtime.GOOS == "windows"

// Canonical paths and module identifiers for the Go host, in one place.
const (
	KrateDirName        = ".krate"
	HostModuleName      = "goapi"
	GoextModuleName     = "goext"
	HostBinaryName      = "goapi-server"
	RoutesManifestName  = "goapi-routes.json"
	DefaultModulePath   = "krate-goapi"
	GoWorkName          = "go.work"
	DefaultAPIDir       = "src/api"
	DefaultComponentDir = "src/components"
	// GoextModulePath is the real module path the helper packages publish under
	// (mirrors pluginsdk); user Go code imports its /goext/{krate,runtime}.
	GoextModulePath = "github.com/kratejs/krate/packages/compiler"
	// GoextRuntimeImport / KrateImport are the helper import paths.
	GoextRuntimeImport = GoextModulePath + "/goext/runtime"
	KrateImport        = GoextModulePath + "/goext/krate"
)

// HostModuleDir returns <root>/.krate/goapi (the generated host module).
func HostModuleDir(root string) string {
	return filepath.Join(root, KrateDirName, HostModuleName)
}

// GoextVendorDir returns <root>/.krate/goext (the vendored helper module).
func GoextVendorDir(root string) string {
	return filepath.Join(root, KrateDirName, GoextModuleName)
}

// RoutesManifestPath returns <root>/.krate/goapi-routes.json.
func RoutesManifestPath(root string) string {
	return filepath.Join(root, KrateDirName, RoutesManifestName)
}

// HostBinaryPath returns the compiled host binary path (OS suffix added).
func HostBinaryPath(root string) string {
	name := HostBinaryName
	if isWindows {
		name += ".exe"
	}
	return filepath.Join(root, KrateDirName, name)
}
