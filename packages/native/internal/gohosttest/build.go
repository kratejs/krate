// Package gohosttest builds tiny Go host child programs for tests. It does
// what the Krate build does for a real app: a throwaway module whose main
// calls gohost.Serve, with a replace directive to this module.
package gohosttest

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// ModuleRoot returns the directory of the native module (the one holding
// go.mod), found from this source file.
func ModuleRoot() (string, error) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		return "", fmt.Errorf("gohosttest: cannot locate the module")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", "..")), nil
}

// Build compiles mainSrc (a complete main package) in a temporary module
// under dir and returns the path of the binary named out. mainSrc may import
// this module's packages.
func Build(dir, out, mainSrc string) (string, error) {
	root, err := ModuleRoot()
	if err != nil {
		return "", err
	}
	if _, err := exec.LookPath("go"); err != nil {
		return "", fmt.Errorf("gohosttest: go toolchain not found: %w", err)
	}
	mod := filepath.Join(dir, "mod-"+strings.TrimSuffix(filepath.Base(out), filepath.Ext(out)))
	if err := os.MkdirAll(mod, 0o755); err != nil {
		return "", err
	}
	gomod := fmt.Sprintf("module gohosttestchild\n\ngo 1.26\n\nrequire github.com/kratejs/krate/packages/native v0.0.0\n\nreplace github.com/kratejs/krate/packages/native => %s\n", filepath.ToSlash(root))
	if err := os.WriteFile(filepath.Join(mod, "go.mod"), []byte(gomod), 0o644); err != nil {
		return "", err
	}
	if sum, err := os.ReadFile(filepath.Join(root, "go.sum")); err == nil {
		_ = os.WriteFile(filepath.Join(mod, "go.sum"), sum, 0o644)
	}
	if err := os.WriteFile(filepath.Join(mod, "main.go"), []byte(mainSrc), 0o644); err != nil {
		return "", err
	}
	bin := out
	if !filepath.IsAbs(bin) {
		bin = filepath.Join(dir, out)
	}
	if runtime.GOOS == "windows" && !strings.HasSuffix(bin, ".exe") {
		bin += ".exe"
	}
	tmp := bin + ".tmp"
	cmd := exec.Command("go", "build", "-mod=mod", "-o", tmp, ".")
	cmd.Dir = mod
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0", "GOFLAGS=-mod=mod")
	if b, err := cmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("go build: %v\n%s", err, b)
	}
	// Rename into place so a running binary at bin is never written to.
	if err := os.Rename(tmp, bin); err != nil {
		return "", err
	}
	return bin, nil
}

// TempDir makes a scratch directory under the user cache directory (not the
// RAM-backed system temp directory) and returns it with its cleanup function.
func TempDir(prefix string) (string, func(), error) {
	base, err := os.UserCacheDir()
	if err != nil {
		base = os.TempDir()
	}
	base = filepath.Join(base, "krate", "native-test")
	if err := os.MkdirAll(base, 0o755); err != nil {
		return "", nil, err
	}
	d, err := os.MkdirTemp(base, prefix)
	if err != nil {
		return "", nil, err
	}
	return d, func() { os.RemoveAll(d) }, nil
}

// ChildSource returns the source of a child whose functions report version.
// It registers:
//
//	t.Version() string                 the version string
//	t.Add(a, b int) int
//	t.Rev(b []byte) []byte
//	t.Wait()                           blocks until its context ends
//	stream t.Count(n int)              emits 0..n-1
//	signal t.Count                     starts at the version's number, then +1 per t.Bump()
//	t.Bump()                           increments the signal
func ChildSource(version string, start int) string {
	return strings.NewReplacer("VERSION", fmt.Sprintf("%q", version), "START", fmt.Sprint(start)).Replace(childSrc)
}

const childSrc = `package main

import (
	"github.com/kratejs/krate/packages/native/gohost"
	"github.com/kratejs/krate/packages/native/krate"
	"github.com/kratejs/krate/packages/native/scheme"
	"github.com/kratejs/krate/packages/native/wire"
)

func main() {
	count := krate.NewSignal[int64](START)
	gohost.Serve(func(r *scheme.Registry) {
		r.Func("t.Version", func(ctx krate.Ctx, a *wire.Decoder, out *wire.Encoder) error {
			if err := a.BeginArray(); err != nil { return err }
			if err := a.EndArray(); err != nil { return err }
			out.String(VERSION)
			return nil
		})
		r.Func("t.Add", func(ctx krate.Ctx, a *wire.Decoder, out *wire.Encoder) error {
			if err := a.BeginArray(); err != nil { return err }
			x, err := a.Int()
			if err != nil { return err }
			y, err := a.Int()
			if err != nil { return err }
			if err := a.EndArray(); err != nil { return err }
			out.Int(x + y)
			return nil
		})
		r.Func("t.Rev", func(ctx krate.Ctx, a *wire.Decoder, out *wire.Encoder) error {
			if err := a.BeginArray(); err != nil { return err }
			b, err := a.Bytes()
			if err != nil { return err }
			if err := a.EndArray(); err != nil { return err }
			rev := make([]byte, len(b))
			for i := range b { rev[len(b)-1-i] = b[i] }
			out.Bytes(rev)
			return nil
		})
		r.Func("t.Wait", func(ctx krate.Ctx, a *wire.Decoder, out *wire.Encoder) error {
			<-ctx.Done()
			return ctx.Err()
		})
		r.Func("t.Bump", func(ctx krate.Ctx, a *wire.Decoder, out *wire.Encoder) error {
			count.Update(func(v int64) int64 { return v + 1 })
			out.Null()
			return nil
		})
		r.Stream("t.Count", func(ctx krate.Ctx, a *wire.Decoder, emit func(func(*wire.Encoder)) error) error {
			if err := a.BeginArray(); err != nil { return err }
			n, err := a.Int()
			if err != nil { return err }
			if err := a.EndArray(); err != nil { return err }
			for i := int64(0); i < n; i++ {
				if err := emit(func(e *wire.Encoder) { e.Int(i) }); err != nil { return err }
			}
			return nil
		})
		r.Signal("t.Count", scheme.SignalOf(count, func(e *wire.Encoder, v int64) { e.Int(v) }))
	})
}
`
