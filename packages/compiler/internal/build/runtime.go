package build

import (
	"os"
	"path/filepath"

	"krate-compiler/internal/renderer"
)

func loadRuntimeFromDisk(projectRoot string) string {
	names := []string{"krate-hydrate.js", "krate-runtime.js"}

	// Build candidate directories relative to the project root.
	// findKrateRoot walks up from projectRoot to locate the compiler package.
	var candidateDirs []string

	// 1. From project root, walk up to find the krate compiler directory
	krateRoot := findKrateRoot(projectRoot)
	if krateRoot != "" {
		// krateRoot = packages/compiler — runtime is at packages/runtime/dist/
		monorepoRoot := filepath.Dir(krateRoot)
		candidateDirs = append(candidateDirs, filepath.Join(monorepoRoot, "packages", "runtime", "dist"))
	}

	// 2. CWD-relative paths (works when running CLI from packages/compiler/)
	candidateDirs = append(candidateDirs,
		"packages/runtime/dist",
		"../runtime/dist",
	)

	// 3. node_modules fallback (for published packages)
	candidateDirs = append(candidateDirs, "node_modules/krate-runtime/dist")

	// 4. Binary-relative path (works when installed as a Go binary)
	candidateDirs = append(candidateDirs, filepath.Join(filepath.Dir(os.Args[0]), "..", "runtime", "dist"))

	// 5. Walk up from project root checking each parent for packages/runtime/dist/
	dir := projectRoot
	for {
		candidate := filepath.Join(dir, "packages", "runtime", "dist")
		candidateDirs = append(candidateDirs, candidate)
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}

	seen := make(map[string]bool)
	var uniqueDirs []string
	for _, d := range candidateDirs {
		if !seen[d] {
			seen[d] = true
			uniqueDirs = append(uniqueDirs, d)
		}
	}

	for _, name := range names {
		for _, dir := range uniqueDirs {
			path := filepath.Join(dir, name)
			data, err := os.ReadFile(path)
			if err == nil {
				return string(data)
			}
		}
	}

	return ""
}

// writeRuntimeChunk writes the krate runtime to a shared chunk file in dist/chunks/.
// Returns the relative path (e.g. "chunks/runtime.abc123.js") for use in <script> tags.
// Returns empty string if runtime is unavailable.
func writeRuntimeChunk(outDir string, shouldMinify bool, projectRoot string) string {
	runtime := loadRuntimeFromDisk(projectRoot)
	if runtime == "" {
		return ""
	}

	// Append the shared hydration infrastructure (findSlot, __safe, $esc,
	// kbind* helpers) so per-page hydration scripts only contain their own
	// signals and binding calls instead of duplicating this boilerplate.
	runtime += renderer.HydrationBootstrapJS

	runtimeToWrite := runtime
	if shouldMinify {
		runtimeToWrite = minifyJSBase(runtime)
	}

	hash := hashContent([]byte(runtimeToWrite))
	filename := "chunks/runtime." + hash + ".js"

	chunksDir := filepath.Join(outDir, "chunks")
	os.MkdirAll(chunksDir, 0755)

	relPath := filename
	os.WriteFile(filepath.Join(outDir, relPath), []byte(runtimeToWrite), 0644)

	return relPath
}
