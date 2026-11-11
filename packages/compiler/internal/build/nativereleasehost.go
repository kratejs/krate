package build

import (
	"errors"
	"os"
	"path/filepath"
	"sort"

	"github.com/kratejs/krate/packages/compiler/internal/diag"
	"github.com/kratejs/krate/packages/compiler/internal/native/hostts"
	"github.com/kratejs/krate/packages/compiler/internal/native/runtimes"
)

// releaseHostDir is the directory, next to a release executable, that holds
// the compiled host children. app.json names them as "host/<file>".
const releaseHostDir = "host"

// releaseHostChild is one host child a release build compiles into an
// executable for every target.
type releaseHostChild struct {
	rt     runtimes.Runtime
	bundle string // absolute path of the child bundle
	name   string // executable base name, without extension
}

// planReleaseHostChildren turns the staged app directory into the layout of a
// release: every child group (node, bun, deno, scriptc) is replaced by an
// executable entry in app.json (Entry "host/<name>", no Command; the shell
// appends ".exe" on Windows and looks next to the running executable), and the
// script copies are removed from the stage. A scriptc group takes over the ids
// the in-process QuickJS bundle holds as its development stand-in, so that
// bundle is dropped when it serves nothing else.
func planReleaseHostChildren(ar *NativeAppResult) ([]releaseHostChild, error) {
	var plan []releaseHostChild
	var children []NativeHostChildMeta
	scriptcIDs := map[string]bool{}
	var scriptc *hostts.Group
	for i := range ar.HostGroups {
		g := &ar.HostGroups[i]
		rt := runtimes.Runtime(g.Runtime)
		switch {
		case rt == runtimes.Scriptc:
			scriptc = g
			for _, r := range g.Registered {
				scriptcIDs[r.ID] = true
			}
		case rt.Child():
			name := "krate-host-" + g.Runtime
			for _, c := range ar.Manifest.HostChildren {
				if c.Runtime != g.Runtime {
					continue
				}
				c.Entry, c.Map, c.Command = releaseHostDir+"/"+name, "", nil
				children = append(children, c)
			}
			plan = append(plan, releaseHostChild{rt: rt, bundle: g.Bundle, name: name})
			if err := os.RemoveAll(filepath.Join(ar.AppDir, "host", g.Runtime)); err != nil {
				return nil, err
			}
		}
	}
	if scriptc != nil {
		name := "krate-host-scriptc"
		meta := NativeHostChildMeta{
			Runtime: "scriptc", Entry: releaseHostDir + "/" + name,
			Funcs: append([]string{}, scriptc.Funcs...), Streams: append([]string{}, scriptc.Streams...), Signals: append([]string{}, scriptc.Signals...),
		}
		sort.Strings(meta.Funcs)
		sort.Strings(meta.Streams)
		sort.Strings(meta.Signals)
		children = append(children, meta)
		plan = append(plan, releaseHostChild{rt: runtimes.Scriptc, bundle: scriptc.Bundle, name: name})
		for _, g := range ar.HostGroups {
			if g.Runtime != string(runtimes.QuickJS) || ar.Manifest.HostJS == nil {
				continue
			}
			only := true
			for _, r := range g.Registered {
				if !scriptcIDs[r.ID] {
					only = false
				}
			}
			if only {
				ar.Manifest.HostJS = nil
				_ = os.RemoveAll(filepath.Join(ar.AppDir, "host", "host.js"))
				_ = os.Remove(filepath.Join(ar.AppDir, "host", "host.js.map"))
			}
		}
	}
	ar.Manifest.HostChildren = children
	return plan, nil
}

// buildReleaseHostChildren compiles the planned children for one target into
// <outDir>/<os>-<arch>/host and returns their paths relative to outDir.
func (b *Builder) buildReleaseHostChildren(plan []releaseHostChild, goos, goarch, outDir string, ar *NativeAppResult) ([]string, error) {
	if len(plan) == 0 {
		return nil, nil
	}
	eff := nativeEffective(b.Cfg)
	bundle := eff.Host.Bundle == nil || *eff.Host.Bundle
	dir := filepath.Join(outDir, goos+"-"+goarch, releaseHostDir)
	if err := os.RemoveAll(dir); err != nil {
		return nil, err
	}
	var out []string
	for _, c := range plan {
		printf("  %s>%s Compiling the %s host (%s/%s)\n", cGreen, cReset, c.rt, goos, goarch)
		exe, err := runtimes.BuildRelease(c.rt, bundle, runtimes.Target{OS: goos, Arch: goarch}, dir, runtimes.ReleaseInput{
			Entry: c.bundle, Name: c.name, Version: eff.Host.Version, Permissions: ar.Manifest.Permissions,
		})
		if err != nil {
			var d diag.Diagnostic
			if errors.As(err, &d) {
				return nil, nativeDiagError([]diag.Diagnostic{d}, "native release error(s)")
			}
			var se *runtimes.ScriptcError
			if errors.As(err, &se) {
				return nil, nativeDiagError(se.Diagnostics, "scriptc error(s)")
			}
			return nil, err
		}
		out = append(out, filepath.ToSlash(mustRel(outDir, exe)))
	}
	return out, nil
}
