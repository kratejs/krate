package build

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/kratejs/krate/packages/compiler/internal/diag"
	"github.com/kratejs/krate/packages/compiler/internal/native/runtimes"
)

// NativeScriptcCheck compiles the project's host code for scriptc and runs
// `scriptc coverage --fail-on=<failOn>` on the bundle. It does so whatever
// native.host.runtime says, so a project can be checked before it opts in.
// ran is false when the project has no TypeScript host code. The returned
// diagnostics point at the original file and line.
func (b *Builder) NativeScriptcCheck(failOn string) (ds []diag.Diagnostic, ran bool, err error) {
	if err := runtimes.ScriptcToolchainForCoverage(); err != nil {
		return nil, false, err
	}
	saved := b.Cfg
	cp := *b.Cfg
	n := nativeEffective(b.Cfg)
	n.Host.Runtime = string(runtimes.Scriptc)
	n.Experimental.Scriptc = true
	cp.Native = n
	b.Cfg = &cp
	defer func() { b.Cfg = saved }()

	b.NativeTarget = true
	if err := b.prepareNativeHost(); err != nil {
		return nil, false, err
	}
	h := b.NativeHost()
	if h == nil {
		return nil, false, nil
	}
	for _, g := range h.Groups {
		if g.Runtime != string(runtimes.Scriptc) {
			continue
		}
		smap, _ := os.ReadFile(g.SourceMap)
		origin := runtimes.Origin{
			Map: smap, Bundle: g.Bundle, Root: b.Root, Remap: h.Origin,
			RuntimeSource: func(src string) bool {
				return strings.HasPrefix(src, "krate-hostchild:") || strings.Contains(filepath.ToSlash(src), "packages/app/src/wire.ts")
			},
		}
		ds, err := runtimes.ScriptcCoverage(b.Root, g.Bundle, failOn, origin)
		if err != nil {
			return nil, true, err
		}
		return ds, true, nil
	}
	return nil, false, fmt.Errorf("the host build produced no scriptc bundle")
}
