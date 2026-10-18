package build

import (
	"os"
	"path/filepath"

	"github.com/kratejs/krate/packages/compiler/internal/config"
	"github.com/kratejs/krate/packages/compiler/internal/fsutil"
)

// writeConfigSchema emits .krate/config.schema.json so editors can complete
// krate.config.* keys without importing @krate/core types.
func writeConfigSchema(root string) error {
	data, err := config.SchemaJSON()
	if err != nil {
		return err
	}
	dir := filepath.Join(root, ".krate")
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	return fsutil.WriteFileIfChanged(filepath.Join(dir, "config.schema.json"), data, 0644)
}
