// Package plug is the public SDK for writing Krate plugins in Go. A plugin is a
// standalone main package that calls plug.Serve with the hooks it implements;
// the Krate compiler launches it as a subprocess over HashiCorp go-plugin
// (net/rpc). The plugin author ships per-platform binaries, typically inside an
// npm package whose config points Krate at the right binary for the host.
//
// Hook contracts mirror the JavaScript plugin surface: the compiler hands each
// hook a JSON context and expects the concrete Go types described below. Hook
// contexts are mutable - edit fields (or the *ast.Program for AfterParse) and
// return nil. Dispatch marshals the changed state back for the host to apply.
package plug

import (
	"encoding/json"
	"fmt"
	"net/rpc"
	"os"

	"github.com/hashicorp/go-hclog"
	"github.com/hashicorp/go-plugin"

	"github.com/kratejs/krate/packages/compiler/ast"
	"github.com/kratejs/krate/packages/compiler/internal/astjson"
	"github.com/kratejs/krate/packages/compiler/internal/pluginapi"
)

// Handshake constants shared by the plugin binary and the Krate host.
const (
	// ProtocolVersion identifies the wire contract between host and plugin.
	ProtocolVersion = 1
	// MagicCookieKey and MagicCookieValue distinguish a Krate plugin process
	// from any other executable the host may be pointed at.
	MagicCookieKey   = "KRATE_PLUGIN_MAGIC_COOKIE"
	MagicCookieValue = "f58a3d1f-9c86-4b2a-9e7f-8b3c2a1d4e5f"
	// PluginName is the go-plugin plugin type name in the plugin map.
	PluginName = "krate"
)

// HandshakeConfig is the go-plugin handshake both sides must agree on.
var HandshakeConfig = plugin.HandshakeConfig{
	ProtocolVersion:  ProtocolVersion,
	MagicCookieKey:   MagicCookieKey,
	MagicCookieValue: MagicCookieValue,
}

// Hooks groups the optional hook implementations a plugin provides. Only
// non-nil hooks are invoked. Every hook receives a mutable context pointer;
// return an error to abort the build/serve operation.
type Hooks struct {
	// BeforeBuild runs before pages are compiled. Files/GeneratedPages set on
	// the context are applied by the host.
	BeforeBuild func(ctx *BuildArgs) error

	// AfterParse runs after a page's source parses. ctx.Program carries the
	// parsed program; edit it in place and the host renders the edited tree.
	AfterParse func(ctx *ParseArgs) error

	// AfterMarkdownParse runs after markdown pages render to HTML. ctx.HTML is
	// the rendered markup before layout wrapping.
	AfterMarkdownParse func(ctx *MarkdownArgs) error

	// AfterRender runs after a page renders but before layout wrapping.
	AfterRender func(ctx *RenderArgs) error

	// GenerateRoutes runs after BeforeBuild. Set ctx.Routes to synthesize
	// virtual pages alongside real source pages.
	GenerateRoutes func(ctx *BuildArgs) error

	// AfterPage runs after a page is fully built including layout wrapping.
	AfterPage func(ctx *PageArgs) error

	// AfterBuild runs once after every page is built.
	AfterBuild func(ctx *BuildResultArgs) error

	// ServeRequest runs at request time before a page is served. Set ctx.Action
	// to "rewrite" (with ctx.NewURL) to rewrite the path or "respond" (with
	// Status/Headers/Body) to short-circuit.
	ServeRequest func(ctx *ServeRequestArgs) error

	// ServeResponse runs at request time after a non-streaming response has
	// been buffered. Edit Status/Headers/Body on the context to rewrite it.
	ServeResponse func(ctx *ServeResponseArgs) error
}

// ---------------------------------------------------------------------------
// Hook context types
// ---------------------------------------------------------------------------

// Result is embedded in every hook context. Dispatch collects whatever the
// hook set and returns it to the host in the unified output envelope.
type Result struct {
	Files          []File          `json:"files,omitempty"`
	Routes         []Route         `json:"routes,omitempty"`
	GeneratedPages []GeneratedPage `json:"generatedPages,omitempty"`
	HTML           *string         `json:"html,omitempty"`
	HeadHTML       *string         `json:"headHTML,omitempty"`
	RawCSS         *string         `json:"rawCSS,omitempty"`
	Scripts        []string        `json:"scripts,omitempty"`
	MetaTags       []string        `json:"metaTags,omitempty"`
	// Ast is the edited program document for AfterParse. Set by Dispatch from
	// ParseArgs.Program; plugins do not construct it directly.
	Ast json.RawMessage `json:"ast,omitempty"`
}

// EmitFile appends a file to the hook's output. The host writes `files` into the
// output directory (outDir-relative paths, traversal-guarded). It is the Go
// counterpart of the JS `krate.emitFile`.
func (r *Result) EmitFile(path, content string) {
	r.Files = append(r.Files, File{Path: path, Content: content})
}

// InjectHead appends markup to the page <head>. It takes effect for the
// AfterRender and AfterPage hooks; the host folds it into the context's own
// HeadHTML before applying. It is the Go counterpart of `krate.injectHead`.
func (r *Result) InjectHead(html string) {
	if r.HeadHTML == nil {
		s := ""
		r.HeadHTML = &s
	}
	*r.HeadHTML += html
}

// InjectCSS appends raw CSS. It takes effect for the AfterRender hook. It is the
// Go counterpart of `krate.injectCSS`.
func (r *Result) InjectCSS(css string) {
	if r.RawCSS == nil {
		s := ""
		r.RawCSS = &s
	}
	if *r.RawCSS != "" {
		*r.RawCSS += "\n"
	}
	*r.RawCSS += css
}

// ResolveFile resolves a plugin file specifier to an absolute path without
// hand-rolling a node_modules walk. Absolute/relative paths are anchored to
// projectRoot and must stay inside it; bare specifiers resolve through
// node_modules first. Traversal outside projectRoot is rejected.
func ResolveFile(projectRoot, spec string) (string, error) {
	return pluginapi.Resolve(projectRoot, spec)
}

// ReadFile resolves spec (see ResolveFile) and returns its contents as a string.
func ReadFile(projectRoot, spec string) (string, error) {
	return pluginapi.ReadFile(projectRoot, spec)
}

// WriteFileToRoot writes content to rel, interpreted relative to projectRoot
// (e.g. "public/logo.svg"), creating parent directories as needed. A path that
// escapes projectRoot is rejected. It is the Go counterpart of the JS
// `krate.writeFileToRoot`.
func WriteFileToRoot(projectRoot, rel string, content []byte) error {
	return pluginapi.WriteFileToRoot(projectRoot, rel, content)
}

// pluginLogName is set by Serve so Log/Warn can attribute output.
var pluginLogName string

// Log writes a diagnostic line to stderr, prefixed with the plugin name. It is
// the Go counterpart of the JS `krate.log`.
func Log(args ...interface{}) {
	writePluginLog("", args...)
}

// Warn writes a warning line to stderr, prefixed with the plugin name. It is
// the Go counterpart of the JS `krate.warn`.
func Warn(args ...interface{}) {
	writePluginLog("WARN: ", args...)
}

// writePluginLog formats "[plugin:<name>] <tag><args>" to stderr. Plugin
// subprocess stderr is inherited by the Krate host, so it reaches the user.
func writePluginLog(tag string, args ...interface{}) {
	label := pluginLogName
	if label == "" {
		label = "unnamed"
	}
	fmt.Fprintf(os.Stderr, "[plugin:%s] %s%s\n", label, tag, fmt.Sprint(args...))
}

// KrateInfo carries build-wide metadata on hook contexts that do not already
// declare their own root/outDir fields. It mirrors the fields of the JS `krate`
// object so plugins get the same context in both runtimes.
type KrateInfo struct {
	// ProjectRoot is the absolute path to the project root.
	ProjectRoot string `json:"projectRoot,omitempty"`
	// OutDir is the absolute path to the output directory.
	OutDir string `json:"outDir,omitempty"`
	// PagesDir is the absolute path to the configured pages directory.
	PagesDir string `json:"pagesDir,omitempty"`
	// Version is the running Krate compiler version.
	Version string `json:"version,omitempty"`
	// DevMode is true during `krate dev`.
	DevMode bool `json:"devMode,omitempty"`
	// Pages is the current page list (paths).
	Pages []string `json:"pages,omitempty"`
	// Config is the resolved krate config object.
	Config interface{} `json:"config,omitempty"`
}

// File is a single file written to the output directory.
type File struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

// Route is a virtual page synthesized by a plugin.
type Route struct {
	Path    string            `json:"path"`
	Content string            `json:"content"`
	Title   string            `json:"title"`
	Layout  string            `json:"layout,omitempty"`
	Data    map[string]string `json:"data,omitempty"`
}

// GeneratedPage is a page file produced by a plugin that should enter the
// normal page pipeline.
type GeneratedPage struct {
	Path  string `json:"path"`
	Route string `json:"route"`
}

// PageResult describes one built page in AfterBuild.
type PageResult struct {
	Page     string `json:"page"`
	OutName  string `json:"outName"`
	HTML     string `json:"html"`
	HeadHTML string `json:"headHTML"`
	HasJS    bool   `json:"hasJS"`
}

// BuildArgs is passed to BeforeBuild and GenerateRoutes.
type BuildArgs struct {
	Result
	Root           string           `json:"root"`
	ProjectRoot    string           `json:"projectRoot,omitempty"`
	OutDir         string           `json:"outDir"`
	PagesDir       string           `json:"pagesDir,omitempty"`
	Version        string           `json:"version,omitempty"`
	Config         interface{}      `json:"config,omitempty"`
	Pages          []string         `json:"pages"`
	GeneratedPages *[]GeneratedPage `json:"generatedPages,omitempty"`
	DevMode        bool             `json:"devMode"`
}

// ParseArgs is passed to AfterParse. Program is reconstructed from the
// kind-tagged document the host sends; editing it and returning modifies the
// build.
type ParseArgs struct {
	Result
	KrateInfo
	Page    string       `json:"page"`
	Program *ast.Program `json:"-"`
}

// UnmarshalJSON hydrates the context, decoding the "program" document via the
// astjson codec into a live *ast.Program for the hook to edit.
func (p *ParseArgs) UnmarshalJSON(data []byte) error {
	var raw struct {
		Result
		Page    string          `json:"page"`
		Program json.RawMessage `json:"program"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	p.Result = raw.Result
	p.Page = raw.Page
	if len(raw.Program) > 0 {
		prog, err := astjson.DecodeProgram(raw.Program)
		if err != nil {
			return err
		}
		p.Program = prog
	}
	return nil
}

// MarkdownArgs is passed to AfterMarkdownParse.
type MarkdownArgs struct {
	Result
	KrateInfo
	Page  string `json:"page"`
	HTML  string `json:"html"`
	Title string `json:"title"`
	Route string `json:"route"`
}

// RenderArgs is passed to AfterRender. HTML, HeadHTML and RawCSS are filled
// from the host and are mutable; their final values are applied by the host.
type RenderArgs struct {
	Result
	KrateInfo
	Page     string `json:"page"`
	HTML     string `json:"html"`
	HeadHTML string `json:"headHTML"`
	HasJS    bool   `json:"hasJS"`
	RawCSS   string `json:"rawCSS"`
}

// PageArgs is passed to AfterPage with the final page output.
type PageArgs struct {
	Result
	KrateInfo
	Page     string `json:"page"`
	OutName  string `json:"outName"`
	HTML     string `json:"html"`
	HeadHTML string `json:"headHTML"`
	HasJS    bool   `json:"hasJS"`
}

// BuildResultArgs is passed to AfterBuild with every page's results.
type BuildResultArgs struct {
	Result
	Root        string       `json:"root"`
	ProjectRoot string       `json:"projectRoot,omitempty"`
	OutDir      string       `json:"outDir"`
	PagesDir    string       `json:"pagesDir,omitempty"`
	Version     string       `json:"version,omitempty"`
	Config      interface{}  `json:"config,omitempty"`
	Pages       []PageResult `json:"pages"`
	CSS         string       `json:"css"`
}

// ServeRequestArgs is the request-time context for the ServeRequest hook.
type ServeRequestArgs struct {
	Result
	URL     string            `json:"url"`
	Method  string            `json:"method"`
	Path    string            `json:"path"`
	Headers map[string]string `json:"headers"`
	// Action is "continue", "rewrite", or "respond".
	Action string `json:"action,omitempty"`
	Status int    `json:"status,omitempty"`
	NewURL string `json:"newURL,omitempty"`
	Body   string `json:"body,omitempty"`
}

// ServeResponseArgs is the request-time context for the ServeResponse hook.
// It wraps a buffered non-streaming response.
type ServeResponseArgs struct {
	Result
	URL     string            `json:"url"`
	Method  string            `json:"method"`
	Path    string            `json:"path"`
	Status  int               `json:"status"`
	Headers map[string]string `json:"headers"`
	Body    string            `json:"body"`
}

// ---------------------------------------------------------------------------
// go-plugin bridge
// ---------------------------------------------------------------------------

// DispatchRequest is the gob-encoded unit of RPC. Kind distinguishes build
// hooks from serve-time hooks; the plugin returns a uniform result envelope.
type DispatchRequest struct {
	Kind string          `json:"kind"`
	Hook string          `json:"hook"`
	Args json.RawMessage `json:"args"`
}

// PluginServer is the net/rpc service object a plugin binary serves.
type PluginServer interface {
	Dispatch(req DispatchRequest, out *json.RawMessage) error
}

// Bridge adapts the plugin binary and the Krate host to go-plugin. On the
// server side Server returns the Dispatch service; on the client side Client
// returns an RPCClient bound to the running subprocess.
type Bridge struct {
	Impl PluginServer
}

func (b *Bridge) Server(*plugin.MuxBroker) (interface{}, error) {
	return b.Impl, nil
}

func (b *Bridge) Client(broker *plugin.MuxBroker, c *rpc.Client) (interface{}, error) {
	return &RPCClient{client: c}, nil
}

// RPCClient is the host-side handle to a running plugin.
type RPCClient struct {
	client *rpc.Client
}

// Dispatch sends one hook invocation to the plugin and returns the raw output
// envelope JSON. Kind must be "build" or "serve".
func (c *RPCClient) Dispatch(kind, hook string, args json.RawMessage) (json.RawMessage, error) {
	var out json.RawMessage
	err := c.client.Call("Plugin.Dispatch", DispatchRequest{Kind: kind, Hook: hook, Args: args}, &out)
	return out, err
}

// Close terminates the RPC connection.
func (c *RPCClient) Close() error {
	return c.client.Close()
}

// pluginServer dispatches a hook invocation to the user's Hooks.
type pluginServer struct {
	hooks Hooks
}

// Dispatch runs the named hook against its context and returns the output
// envelope for the host to apply.
func (s *pluginServer) Dispatch(req DispatchRequest, out *json.RawMessage) error {
	if req.Kind == "serve" {
		return s.dispatchServe(req, out)
	}

	var (
		env Result
		err error
	)

	switch req.Hook {
	case "BeforeBuild", "GenerateRoutes":
		var ctx BuildArgs
		if err = json.Unmarshal(req.Args, &ctx); err != nil {
			break
		}
		fn := s.hooks.BeforeBuild
		if req.Hook == "GenerateRoutes" {
			fn = s.hooks.GenerateRoutes
		}
		if fn != nil {
			err = fn(&ctx)
		}
		env = ctx.Result
	case "AfterParse":
		var ctx ParseArgs
		if err = json.Unmarshal(req.Args, &ctx); err != nil {
			break
		}
		if s.hooks.AfterParse != nil {
			err = s.hooks.AfterParse(&ctx)
		}
		env = ctx.Result
		if ctx.Program != nil {
			doc, derr := astjson.EncodeProgram(ctx.Program)
			if derr != nil {
				return derr
			}
			env.Ast = json.RawMessage(doc)
		}
	case "AfterMarkdownParse":
		var ctx MarkdownArgs
		if err = json.Unmarshal(req.Args, &ctx); err != nil {
			break
		}
		if s.hooks.AfterMarkdownParse != nil {
			err = s.hooks.AfterMarkdownParse(&ctx)
		}
		env = ctx.Result
		env.HTML = &ctx.HTML
	case "AfterRender":
		var ctx RenderArgs
		if err = json.Unmarshal(req.Args, &ctx); err != nil {
			break
		}
		if s.hooks.AfterRender != nil {
			err = s.hooks.AfterRender(&ctx)
		}
		// Fold capability-helper emissions (Result.InjectHead/InjectCSS) into the
		// concrete context fields the host applies.
		if ctx.Result.HeadHTML != nil {
			ctx.HeadHTML += *ctx.Result.HeadHTML
		}
		if ctx.Result.RawCSS != nil {
			ctx.RawCSS = joinCSS(ctx.RawCSS, *ctx.Result.RawCSS)
		}
		env = ctx.Result
		env.HTML = &ctx.HTML
		env.HeadHTML = &ctx.HeadHTML
		env.RawCSS = &ctx.RawCSS
	case "AfterPage":
		var ctx PageArgs
		if err = json.Unmarshal(req.Args, &ctx); err != nil {
			break
		}
		if s.hooks.AfterPage != nil {
			err = s.hooks.AfterPage(&ctx)
		}
		if ctx.Result.HeadHTML != nil {
			ctx.HeadHTML += *ctx.Result.HeadHTML
		}
		env = ctx.Result
		env.HTML = &ctx.HTML
		env.HeadHTML = &ctx.HeadHTML
	case "AfterBuild":
		var ctx BuildResultArgs
		if err = json.Unmarshal(req.Args, &ctx); err != nil {
			break
		}
		if s.hooks.AfterBuild != nil {
			err = s.hooks.AfterBuild(&ctx)
		}
		env = ctx.Result
	default:
		return &unknownHookError{hook: req.Hook}
	}

	if err != nil {
		return err
	}
	b, merr := json.Marshal(env)
	if merr != nil {
		return merr
	}
	*out = json.RawMessage(b)
	return nil
}

type unknownHookError struct{ hook string }

func (e *unknownHookError) Error() string {
	return "plugin: unknown hook " + e.hook
}

// joinCSS concatenates two CSS fragments with a single newline separator.
func joinCSS(base, add string) string {
	if base == "" {
		return add
	}
	if add == "" {
		return base
	}
	return base + "\n" + add
}

// dispatchServe handles the request-time ServeRequest and ServeResponse hooks.
// Both hooks echo their (possibly edited) context back as the result.
func (s *pluginServer) dispatchServe(req DispatchRequest, out *json.RawMessage) error {
	switch req.Hook {
	case "ServeRequest":
		var ctx ServeRequestArgs
		if err := json.Unmarshal(req.Args, &ctx); err != nil {
			return err
		}
		if s.hooks.ServeRequest != nil {
			if err := s.hooks.ServeRequest(&ctx); err != nil {
				return err
			}
		}
		b, err := json.Marshal(ctx)
		if err != nil {
			return err
		}
		*out = b
		return nil
	case "ServeResponse":
		var ctx ServeResponseArgs
		if err := json.Unmarshal(req.Args, &ctx); err != nil {
			return err
		}
		if s.hooks.ServeResponse != nil {
			if err := s.hooks.ServeResponse(&ctx); err != nil {
				return err
			}
		}
		b, err := json.Marshal(ctx)
		if err != nil {
			return err
		}
		*out = b
		return nil
	default:
		return &unknownHookError{hook: req.Hook}
	}
}

// Serve launches the plugin subprocess protocol. Call it from main and do not
// return after invoking it; the plugin exits when the host shuts it down.
func Serve(name string, hooks Hooks) {
	pluginLogName = name
	logger := hclog.New(&hclog.LoggerOptions{
		Name:   name,
		Level:  hclog.Warn,
		Output: os.Stderr,
	})
	srv := &pluginServer{hooks: hooks}
	plugin.Serve(&plugin.ServeConfig{
		HandshakeConfig: HandshakeConfig,
		Plugins: map[string]plugin.Plugin{
			PluginName: &Bridge{Impl: srv},
		},
		Logger: logger,
	})
}
