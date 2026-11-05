package build

import (
	"errors"
	"fmt"
	"os"

	"github.com/kratejs/krate/packages/compiler/internal/fsutil"
)

// OutputWriteError reports a build artifact that could not be written. It
// names the destination so the user can see which file is missing from dist/.
type OutputWriteError struct {
	Path string
	Err  error
}

func (e *OutputWriteError) Error() string {
	return fmt.Sprintf("cannot write build output %s: %v (check that the directory is writable and the disk is not full)", e.Path, e.Err)
}

func (e *OutputWriteError) Unwrap() error { return e.Err }

// writeOutput writes a shipped build artifact. Callers must return the error
// so the build fails instead of emitting HTML that links a missing file.
func writeOutput(path string, data []byte) error {
	if err := fsutil.WriteFileIfChanged(path, data, 0o644); err != nil {
		return &OutputWriteError{Path: path, Err: err}
	}
	return nil
}

// mkdirOutput creates a directory that shipped artifacts are written into.
func mkdirOutput(dir string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return &OutputWriteError{Path: dir, Err: err}
	}
	return nil
}

func isOutputWriteError(err error) bool {
	var w *OutputWriteError
	return errors.As(err, &w)
}
