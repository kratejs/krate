package build

import (
	"os"
	"path/filepath"
)

// serverOutDir is the directory under the output dir that holds server-only
// artifacts (API bundles, manifests, server bundles). It is never served and
// is stripped from static deploys.
const serverOutDir = ".krate"

// ServerOutPath joins parts under <outDir>/.krate.
func ServerOutPath(outDir string, parts ...string) string {
	return filepath.Join(append([]string{outDir, serverOutDir}, parts...)...)
}

// ManifestPath returns where the full build manifest is written.
func ManifestPath(outDir string) string { return ServerOutPath(outDir, "manifest.json") }

// ServerManifestPath returns where the SSR renderer manifest is written.
func ServerManifestPath(outDir string) string {
	return ServerOutPath(outDir, "server-manifest.json")
}

// apiBundleDir and apiNodeBundleDir hold the QuickJS and Node API bundles.
func apiBundleDir(outDir string) string     { return ServerOutPath(outDir, "api") }
func apiNodeBundleDir(outDir string) string { return ServerOutPath(outDir, "api-node") }

// ResolveManifestPath returns the manifest to read: the current location, or the
// pre-.krate location (dist/<name>) left by an older build when only that exists.
func ResolveManifestPath(outDir, name string) string {
	cur := ServerOutPath(outDir, name)
	if _, err := os.Stat(cur); err == nil {
		return cur
	}
	legacy := filepath.Join(outDir, name)
	if _, err := os.Stat(legacy); err == nil {
		return legacy
	}
	return cur
}
