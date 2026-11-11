package build

import (
	"fmt"
	"os"
	"strings"

	"github.com/kratejs/krate/packages/compiler/internal/diag"
	"github.com/kratejs/krate/packages/compiler/internal/native"
)

// checkHostMarkers fails a web build that carries host code markers: host code
// only runs in a native build.
func (b *Builder) checkHostMarkers() error {
	if b.NativeTarget {
		return nil
	}
	ds, err := native.CheckWebBuild(b.Root, b.Cfg)
	if err != nil || len(ds) == 0 {
		return err
	}
	errs := make([]error, len(ds))
	for i, d := range ds {
		errs[i] = d
		fmt.Fprintf(os.Stderr, "  %sHost code error:%s %s\n", cRed, cReset, strings.ReplaceAll(diag.FormatDiagnostics([]error{d}), "\n", "\n    "))
	}
	return &multiError{summary: fmt.Sprintf("build failed: %d host code marker(s) in a web build", len(ds)), errs: errs}
}
