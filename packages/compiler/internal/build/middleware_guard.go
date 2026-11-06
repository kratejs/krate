package build

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
)

// exportsMiddleware reports whether a bundled middleware module has a handler,
// from esbuild's metafile. An unreadable metafile is not proof of a bad module.
func exportsMiddleware(metafile string) bool {
	var meta struct {
		Outputs map[string]struct {
			Exports []string `json:"exports"`
		} `json:"outputs"`
	}
	if json.Unmarshal([]byte(metafile), &meta) != nil || len(meta.Outputs) == 0 {
		return true
	}
	for _, out := range meta.Outputs {
		for _, name := range out.Exports {
			if name == "middleware" || name == "default" {
				return true
			}
		}
	}
	return false
}

// checkBuildRecords fails when a record the server depends on is unreadable or
// corrupt. A missing one is fine: an app can have no request-time pages, and a
// dist folder can predate a record.
func checkBuildRecords(root, outDir string) error {
	if _, err := readServerManifest(outDir); err != nil && !errors.Is(err, errServerManifestMissing) {
		return err
	}
	for _, path := range []string{
		ResolveManifestPath(outDir, "manifest.json"),
		filepath.Join(root, ".krate", "api-routes.json"),
	} {
		data, err := os.ReadFile(path)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return fmt.Errorf("reading %s: %w", path, err)
		}
		if !json.Valid(data) {
			return fmt.Errorf("%s is corrupt\n  hint: run `krate build` again, and deploy the whole output", path)
		}
	}
	return nil
}

// recordedMiddleware reads the build's record of whether the app has a
// middleware module. known is false for a dist folder built before the record
// existed (or with an unreadable manifest).
func recordedMiddleware(outDir string) (has, known bool) {
	data, err := os.ReadFile(ResolveManifestPath(outDir, "manifest.json"))
	if err != nil {
		return false, false
	}
	var m struct {
		Middleware *bool `json:"middleware"`
	}
	if json.Unmarshal(data, &m) != nil || m.Middleware == nil {
		return false, false
	}
	return *m.Middleware, true
}

// checkMiddlewareDeployed decides whether production serving must fail closed
// for middleware. required is true when the app has middleware, so a missing
// module later on must never let requests through. It returns an error when the
// build recorded middleware but the compiled module is not at root/.krate. An
// old dist with no record and no module only gets a warning on warn: serving
// cannot tell "no middleware" from "middleware not deployed".
func checkMiddlewareDeployed(root, outDir string, dev bool, warn io.Writer) (required bool, err error) {
	file := filepath.Join(root, ".krate", "middleware.js")
	exists := regularFileExists(file)
	if dev {
		return exists, nil
	}
	has, known := recordedMiddleware(outDir)
	switch {
	case known && has && !exists:
		return true, fmt.Errorf("this app has middleware, but the compiled module %s is missing\n  hint: deploy the project's .krate/ directory alongside %s (krate build writes .krate/middleware.js); serving without it would skip your auth checks, so the server will not start",
			file, outDir)
	case !known && !exists:
		fmt.Fprintf(warn, "  %sWarning:%s %s has no middleware record (built by an older krate) and %s does not exist, so middleware is assumed absent\n    hint: run `krate build` again; if this app has middleware, deploy .krate/ alongside %s\n",
			cYellow, cReset, outDir, file, outDir)
	}
	return exists || (known && has), nil
}

// failMissing answers a request when middleware that must run is gone. It never
// calls the next handler.
func (g *middlewareGate) failMissing(w http.ResponseWriter, r *http.Request) {
	fmt.Fprintf(os.Stderr, "  %sMiddleware missing:%s %s %s: %s no longer exists, refusing the request\n    hint: deploy .krate/ alongside dist\n",
		cRed, cReset, r.Method, r.URL.Path, g.file)
	w.Header().Set("Cache-Control", "no-store")
	http.Error(w, "middleware unavailable", http.StatusInternalServerError)
}
