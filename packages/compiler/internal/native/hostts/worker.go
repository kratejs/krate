package hostts

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/evanw/esbuild/pkg/api"

	"github.com/kratejs/krate/packages/compiler/internal/config"
	"github.com/kratejs/krate/packages/compiler/internal/diag"
	"github.com/kratejs/krate/packages/compiler/internal/native/runtimes"
	"github.com/kratejs/krate/packages/compiler/internal/tsstrip"
)

// workerEntrySource is the generated entry of a worker bundle: the host child
// runtime first, then the worker's module (which registers its message
// handler with onMessage from @krate/app/worker-host), then the server start.
func workerEntrySource(module string) string {
	return "// generated worker entry\n" +
		"import { start as __krateStart } from \"krate-hostchild:runtime\";\n" +
		"import " + quote(module) + ";\n" +
		"__krateStart();\n"
}

// BuildWorker bundles a worker module as an ES module for a host child
// process on rt (node, bun or deno) and writes it, with its source map, to
// outFile. The module is the absolute path of the worker's entry. The returned
// diagnostics are the bundle errors; err is a failure to write the output.
func BuildWorker(root string, cfg *config.Config, module string, rt runtimes.Runtime, outFile string) ([]diag.Diagnostic, error) {
	if !rt.Child() {
		return nil, fmt.Errorf("hostts: %q is not a child runtime", rt)
	}
	b := &builder{root: root, cfg: cfg, opts: Options{OutDir: filepath.Dir(outFile)}}
	entry := workerEntrySource(filepath.ToSlash(module))
	external := append([]string{"node:*"}, nodeBuiltins...)
	opts := api.BuildOptions{
		EntryPoints:    []string{virtualEntry},
		AbsWorkingDir:  root,
		Bundle:         true,
		Format:         api.FormatESModule,
		Target:         api.ES2022,
		Platform:       api.PlatformNode,
		Conditions:     childConditions(rt),
		External:       external,
		Outfile:        outFile,
		Sourcemap:      api.SourceMapLinked,
		SourcesContent: api.SourcesContentInclude,
		Write:          false,
		LogLevel:       api.LogLevelSilent,
		Banner:         map[string]string{"js": childBanner},
		Define:         map[string]string{"process.env.NODE_ENV": `"production"`},
		Plugins:        []api.Plugin{b.virtualPlugin(entry, nil, true)},
	}
	tsstrip.HandleTierDecorators(&opts)
	r := api.Build(opts)
	if len(r.Errors) > 0 {
		for _, e := range r.Errors {
			d := b.bundleDiag(e, nil)
			d.Hint = "a worker is bundled as one ES module for " + string(rt) + ": fix the error, or install the package it imports"
			b.diags = append(b.diags, d)
		}
		return b.diags, nil
	}
	if err := os.MkdirAll(filepath.Dir(outFile), 0o755); err != nil {
		return nil, err
	}
	for _, f := range r.OutputFiles {
		if err := os.WriteFile(f.Path, f.Contents, 0o644); err != nil {
			return nil, err
		}
	}
	return nil, nil
}
