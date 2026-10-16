package plugin

import (
	"fmt"
	"strings"
	"sync"

	"github.com/kratejs/krate/packages/compiler/internal/jsruntime"
	"github.com/kratejs/krate/packages/compiler/internal/pluginapi"
	"github.com/kratejs/krate/packages/compiler/internal/version"
)

// krateCapabilities backs the richer `krate` object handed to JS plugin hooks.
// It carries build-wide metadata plus the side-effects produced by the
// capability host functions (emitFile/injectHead/injectCSS), which are merged
// into the hook's returned output after it runs. A fresh value is created per
// hook invocation, so one hook cannot observe another's emissions.
type krateCapabilities struct {
	Root     string
	OutDir   string
	PagesDir string
	DevMode  bool
	Pages    []string
	Version  string
	Config   interface{}

	mu       sync.Mutex
	files    []fileEntry
	headHTML []string
	rawCSS   []string
}

// newKrateCapabilities assembles the metadata for a hook invocation.
func newKrateCapabilities(root, outDir string, env CommunityEnv, hookName string, hookCtx interface{}) *krateCapabilities {
	pages := pagesFromCtx(hookCtx)
	if pages == nil {
		pages = []string{}
	}
	return &krateCapabilities{
		Root:     root,
		OutDir:   outDir,
		PagesDir: env.PagesDir,
		DevMode:  env.DevMode,
		Pages:    pages,
		Version:  version.Value, Config: env.Config,
	}
}

// metadata returns the serializable fields of the krate object. Functions are
// attached in the invocation script (JSON cannot carry them).
func (c *krateCapabilities) metadata() map[string]interface{} {
	return map[string]interface{}{
		"root":        c.Root,
		"projectRoot": c.Root,
		"outDir":      c.OutDir,
		"pagesDir":    c.PagesDir,
		"devMode":     c.DevMode,
		"dev":         c.DevMode,
		"pages":       c.Pages,
		"version":     c.Version,
		"config":      c.Config,
	}
}

// applyTo merges capability side-effects into the hook's output envelope before
// it is applied to the build.
func (c *krateCapabilities) applyTo(output *communityOutput) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if len(c.files) > 0 {
		output.Files = append(output.Files, c.files...)
	}
	if len(c.headHTML) > 0 {
		joined := strings.Join(c.headHTML, "")
		if output.HeadHTML == nil {
			output.HeadHTML = new(string)
		}
		*output.HeadHTML += joined
	}
	if len(c.rawCSS) > 0 {
		joined := strings.Join(c.rawCSS, "\n")
		if output.RawCSS == nil {
			output.RawCSS = new(string)
		}
		if *output.RawCSS != "" {
			*output.RawCSS += "\n"
		}
		*output.RawCSS += joined
	}
}

// registerKrateHostFuncs installs the Go-backed capability functions on the
// runtime. The invocation script then exposes them as methods on `krate`.
func registerKrateHostFuncs(rt *jsruntime.Runtime, cap *krateCapabilities) error {
	if err := rt.RegisterFunc("_krate_resolveFile", func(args []any) (any, error) {
		spec, err := stringArg(args, 0, "resolveFile")
		if err != nil {
			return nil, err
		}
		return pluginapi.Resolve(cap.Root, spec)
	}); err != nil {
		return err
	}

	if err := rt.RegisterFunc("_krate_readFile", func(args []any) (any, error) {
		spec, err := stringArg(args, 0, "readFile")
		if err != nil {
			return nil, err
		}
		return pluginapi.ReadFile(cap.Root, spec)
	}); err != nil {
		return err
	}

	if err := rt.RegisterFunc("_krate_emitFile", func(args []any) (any, error) {
		path, err := stringArg(args, 0, "emitFile")
		if err != nil {
			return nil, err
		}
		content, err := stringArg(args, 1, "emitFile")
		if err != nil {
			return nil, err
		}
		cap.mu.Lock()
		cap.files = append(cap.files, fileEntry{Path: path, Content: content})
		cap.mu.Unlock()
		return nil, nil
	}); err != nil {
		return err
	}

	if err := rt.RegisterFunc("_krate_writeFileToRoot", func(args []any) (any, error) {
		rel, err := stringArg(args, 0, "writeFileToRoot")
		if err != nil {
			return nil, err
		}
		content, err := stringArg(args, 1, "writeFileToRoot")
		if err != nil {
			return nil, err
		}
		if err := pluginapi.WriteFileToRoot(cap.Root, rel, []byte(content)); err != nil {
			return nil, err
		}
		return nil, nil
	}); err != nil {
		return err
	}

	if err := rt.RegisterFunc("_krate_injectHead", func(args []any) (any, error) {
		html, err := stringArg(args, 0, "injectHead")
		if err != nil {
			return nil, err
		}
		cap.mu.Lock()
		cap.headHTML = append(cap.headHTML, html)
		head, css := strings.Join(cap.headHTML, ""), strings.Join(cap.rawCSS, "\n")
		cap.mu.Unlock()
		return injectResult{HeadHTML: head, RawCSS: css}, nil
	}); err != nil {
		return err
	}

	if err := rt.RegisterFunc("_krate_injectCSS", func(args []any) (any, error) {
		css, err := stringArg(args, 0, "injectCSS")
		if err != nil {
			return nil, err
		}
		cap.mu.Lock()
		cap.rawCSS = append(cap.rawCSS, css)
		head, cssOut := strings.Join(cap.headHTML, ""), strings.Join(cap.rawCSS, "\n")
		cap.mu.Unlock()
		return injectResult{HeadHTML: head, RawCSS: cssOut}, nil
	}); err != nil {
		return err
	}

	// krate.log is diagnostic output (verbose-only); krate.warn is always shown.
	// Both go through the runtime's console so they carry the [plugin:<name>]
	// prefix and are indistinguishable from console.* in destination.
	if err := rt.RegisterFunc("_krate_log", func(args []any) (any, error) {
		if verbose.Load() {
			rt.Log(args)
		}
		return nil, nil
	}); err != nil {
		return err
	}

	if err := rt.RegisterFunc("_krate_warn", func(args []any) (any, error) {
		rt.Warn(args)
		return nil, nil
	}); err != nil {
		return err
	}

	return nil
}

// injectResult is the value krate.injectHead/injectCSS return. It is a struct
// (not a map) because the QuickJS host-function bridge JSON-encodes structs but
// cannot marshal a bare map.
type injectResult struct {
	HeadHTML string `json:"headHTML"`
	RawCSS   string `json:"rawCSS"`
}

// stringArg extracts a required string argument for a host function.
func stringArg(args []any, i int, fn string) (string, error) {
	if len(args) <= i {
		return "", fmt.Errorf("krate.%s requires %d argument(s)", fn, i+1)
	}
	s, ok := args[i].(string)
	if !ok {
		return "", fmt.Errorf("krate.%s argument %d must be a string", fn, i+1)
	}
	return s, nil
}

// pagesFromCtx derives the current page list from a hook context.
func pagesFromCtx(hookCtx interface{}) []string {
	switch c := hookCtx.(type) {
	case *BuildHookCtx:
		return c.Pages
	case *BuildResultHookCtx:
		pages := make([]string, 0, len(c.Pages))
		for _, p := range c.Pages {
			pages = append(pages, p.Page)
		}
		return pages
	case *ParseHookCtx:
		return []string{c.Page}
	case *MarkdownHookCtx:
		return []string{c.Page}
	case *RenderHookCtx:
		return []string{c.Page}
	case *PageHookCtx:
		return []string{c.Page}
	}
	return nil
}
