package gohost

import (
	"os"
	"path/filepath"
	"strings"
)

// SetupMissing reports whether editor intellisense setup is likely incomplete:
// no project go.mod, or a tsconfig that does not include `.krate/types`. Used to
// nudge `krate go:init` when Go source is compiled.
func SetupMissing(root string) bool {
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		return true
	}
	return !TsconfigIncludes(root, ".krate/types")
}

// TsconfigIncludes reports whether the project tsconfig.json `include` lists
// dir. Returns false when there is no tsconfig or it can't be read.
func TsconfigIncludes(root, dir string) bool {
	data, err := os.ReadFile(filepath.Join(root, "tsconfig.json"))
	if err != nil {
		return false
	}
	return strings.Contains(string(data), dir)
}

// GoextVendored reports whether the helper module has been vendored under
// .krate/goext.
func GoextVendored(root string) bool {
	_, err := os.Stat(filepath.Join(GoextVendorDir(root), "go.mod"))
	return err == nil
}

// ModuleGoVersion returns the `go` directive of the project go.mod, if any.
func ModuleGoVersion(root string) (string, bool) {
	data, err := os.ReadFile(filepath.Join(root, "go.mod"))
	if err != nil {
		return "", false
	}
	v, _ := SplitGoMod(string(data))
	return v, v != ""
}
