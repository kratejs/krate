package build

import (
	"fmt"
	"os"
	"strings"

	"github.com/kratejs/krate/packages/compiler/internal/diag"
	"github.com/kratejs/krate/packages/compiler/internal/native"
	"github.com/kratejs/krate/packages/compiler/internal/native/hostts"
)

// NativeHost returns the host code split of the last native build, or nil
// for a web build or a project without TypeScript host code.
func (b *Builder) NativeHost() *hostts.Result { return b.nativeHost }

// prepareNativeHost splits the TypeScript host code of a native build: it
// writes the host bundle and arms the UI bundlers with the stub overlays.
// A web build is untouched.
func (b *Builder) prepareNativeHost() error {
	b.nativeHost = nil
	if !b.NativeTarget {
		return nil
	}
	p, err := native.Analyze(b.Root, b.Cfg)
	if err != nil {
		return err
	}
	fail := func(ds []diag.Diagnostic, what string) error {
		errs := make([]error, len(ds))
		for i, d := range ds {
			errs[i] = d
			fmt.Fprintf(os.Stderr, "  %sHost code error:%s %s\n", cRed, cReset, strings.ReplaceAll(diag.FormatDiagnostics([]error{d}), "\n", "\n    "))
		}
		return &multiError{summary: fmt.Sprintf("build failed: %d %s", len(ds), what), errs: errs}
	}
	if len(p.Diagnostics) > 0 {
		return fail(p.Diagnostics, "host code placement error(s)")
	}
	hasTS := false
	for _, m := range p.Modules {
		if m.Rel != "" && !strings.HasSuffix(m.Rel, ".go") {
			hasTS = true
		}
	}
	if !hasTS {
		return nil
	}
	res, ds, err := hostts.Build(b.Root, b.Cfg, p, hostts.Options{Resolve: b.importResolver().Resolve})
	if err != nil {
		return err
	}
	var errs, warns []diag.Diagnostic
	for _, d := range ds {
		if d.IsWarning() {
			warns = append(warns, d)
		} else {
			errs = append(errs, d)
		}
	}
	if len(warns) > 0 {
		printWarningsOnce(os.Stderr, warns)
	}
	if len(errs) > 0 {
		return fail(errs, "host code error(s)")
	}
	b.nativeHost = res
	return nil
}
