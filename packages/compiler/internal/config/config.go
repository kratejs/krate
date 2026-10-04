package config

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/kratejs/krate/packages/compiler/internal/markdown"
)

// Default request body caps (bytes). API requests may be larger than page
// form/middleware payloads.
const (
	defaultMaxBodyBytes    int64 = 1 << 20  // 1 MiB
	defaultMaxAPIBodyBytes int64 = 25 << 20 // 25 MiB
)

type DevServer struct {
	Port int  `json:"port"`
	Open bool `json:"open"`
}

// ServerConfig holds production/preview (`krate serve`) server settings. When
// Port is 0, Serve falls back to DevServer.Port (then 3000) for compatibility
// with projects that only configure devServer.
type ServerConfig struct {
	// Host is the bind address (default "" = all interfaces).
	Host string `json:"host,omitempty"`
	// Port is the listen port. 0 falls back to devServer.port (then 3000).
	Port int `json:"port,omitempty"`
	// MaxBodySize caps request bodies in bytes. 0 uses defaults (1 MiB for
	// pages/middleware, larger for API routes).
	MaxBodySize int64 `json:"maxBodySize,omitempty"`
}

// CORSConfig configures cross-origin resource sharing headers. Disabled by
// default (the framework serves same-origin by default).
type CORSConfig struct {
	Enabled     bool     `json:"enabled,omitempty"`
	Origins     []string `json:"origins,omitempty"` // default ["*"]
	Methods     []string `json:"methods,omitempty"`
	Headers     []string `json:"headers,omitempty"`
	Credentials bool     `json:"credentials,omitempty"`
	MaxAge      int      `json:"maxAge,omitempty"` // seconds
}

// SidecarConfig is a custom API sidecar: a user-provided HTTP service that owns
// some or all `/api/*` routes. In supervised mode, Command (+Args/Port) starts
// and manages the process; in proxy-only mode, Target points at an
// already-running service. Requests are always forwarded first; a 404 falls
// through to Krate's built-in Go/TS/QuickJS API handlers, so the sidecar can own
// exactly the routes it wants (including dynamic segments like /users/[id]) with
// no route declaration needed.
type SidecarConfig struct {
	// Command starts a supervised sidecar process (e.g. "node", "go", "python").
	// When set, Krate launches and stops it with the server.
	Command string            `json:"command,omitempty"`
	Args    []string          `json:"args,omitempty"`
	Cwd     string            `json:"cwd,omitempty"`
	Env     map[string]string `json:"env,omitempty"`
	// Port the sidecar listens on. Required in supervised mode; with Target
	// empty it also defines the proxy target (http://127.0.0.1:<port>).
	Port int `json:"port,omitempty"`
	// Target is a base URL for proxy-only mode, e.g. "http://localhost:8080".
	Target string `json:"target,omitempty"`
	// Prefix is the path prefix the sidecar owns (default "/api"). Requests
	// under it are forwarded first.
	Prefix string `json:"prefix,omitempty"`
}

// APIConfig groups API-related configuration.
type APIConfig struct {
	Sidecar *SidecarConfig `json:"sidecar,omitempty"`
}

// PluginConfig represents a plugin entry from the krate config file.
// For built-in plugins, only Name is needed (matches Go-side init() registration).
// For community plugins, Module points to a JavaScript or TypeScript module
// (.js/.mjs/.cjs/.ts/.tsx) that is bundled (esbuild transpiles the TS) and
// executed inside the embedded QuickJS runtime. The module must
// export a default object { name, order, hooks: { BeforeBuild(ctx, options,
// krate) {...}, ... } } or a factory function (options) => that object.
// Order controls execution priority (lower runs first, default 50).
type PluginConfig struct {
	Name    string                 `json:"name"`
	Module  string                 `json:"module"`
	Order   int                    `json:"order,omitempty"`
	Options map[string]interface{} `json:"options,omitempty"`
}

type TailwindCfg struct {
	Enabled  bool     `json:"enabled,omitempty"`
	ScanDirs []string `json:"scanDirs,omitempty"`
	// Content is the Tailwind-style content glob list; when set it takes
	// precedence over ScanDirs.
	Content []string `json:"content,omitempty"`
	// Preflight enables the Tailwind base reset (opt-in; changes page styling).
	Preflight bool `json:"preflight,omitempty"`
	// Strict reports classes that produced no rule as build warnings.
	Strict bool `json:"strict,omitempty"`
	// DarkMode is "media" (default), "class", or "selector".
	DarkMode string `json:"darkMode,omitempty"`
	// ExecuteConfig runs tailwind.config via `npx tsx` instead of static parse.
	ExecuteConfig bool `json:"executeConfig,omitempty"`
}

type CSPConfig struct {
	Enabled   bool   `json:"enabled,omitempty"`
	Directive string `json:"directive,omitempty"` // custom CSP directive string; empty = auto-generate
}

type SSRConfig struct {
	// RendererPort is the port for the Node.js SSR renderer server.
	// If 0, defaults to DevServer.Port + 10 (dev) or 3100 (prod).
	RendererPort int `json:"rendererPort,omitempty"`
	// Timeout is the max time (ms) to wait for a page to render server-side.
	// Default: 5000ms.
	Timeout int `json:"timeout,omitempty"`
	// MaxCacheSize is the max number of ISR pages to keep in memory cache.
	// Default: 128.
	MaxCacheSize int `json:"maxCacheSize,omitempty"`
	// MiddlewareRuntime controls which runtime executes middleware.ts.
	// "quickjs" (default) = embedded, "node"/"bun"/"deno" = sidecar.
	MiddlewareRuntime string `json:"middlewareRuntime,omitempty"`
	// APIRuntime controls which runtime executes API routes.
	// "quickjs" (default) = embedded, "node"/"bun"/"deno" = sidecar.
	APIRuntime string `json:"apiRuntime,omitempty"`
	// SSRRuntime selects which runtime launches the SSR sidecar renderer.
	// "node" (default) = plain node on the staged driver, "bun" = `bun run`,
	// "deno" = `deno run --allow-net --allow-read --allow-env --allow-sys`.
	SSRRuntime string `json:"ssrRuntime,omitempty"`
	// Streaming forces ALL pages to render in streaming SSR mode,
	// regardless of per-page `export const config = { streaming: true }`.
	// When enabled, every page goes through the server renderer with
	// Suspense-based streaming (fallback → resolved replacement).
	Streaming bool `json:"streaming,omitempty"`
}

type PathAlias struct {
	Prefix  string   `json:"prefix"`  // e.g. "@/*"
	Targets []string `json:"targets"` // e.g. ["./src/*"]
}

// PathAliases is a []PathAlias that also unmarshals the tsconfig-style object
// form used in krate.config.ts, e.g. `{ "@/*": ["./src/*"], "@/x": "./src/x" }`.
// The array form (`[{ prefix, targets }]`) remains supported.
type PathAliases []PathAlias

func (p *PathAliases) UnmarshalJSON(data []byte) error {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 || string(trimmed) == "null" {
		*p = nil
		return nil
	}
	switch trimmed[0] {
	case '[':
		var arr []PathAlias
		if err := json.Unmarshal(data, &arr); err != nil {
			return err
		}
		*p = arr
		return nil
	case '{':
		var obj map[string]json.RawMessage
		if err := json.Unmarshal(data, &obj); err != nil {
			return err
		}
		var out PathAliases
		for prefix, raw := range obj {
			var targets []string
			switch raw[0] {
			case '"':
				var s string
				if err := json.Unmarshal(raw, &s); err != nil {
					return err
				}
				targets = []string{s}
			case '[':
				if err := json.Unmarshal(raw, &targets); err != nil {
					return err
				}
			default:
				return fmt.Errorf("pathAliases value for %q must be a string or string[]", prefix)
			}
			if len(targets) > 0 {
				out = append(out, PathAlias{Prefix: prefix, Targets: targets})
			}
		}
		*p = out
		return nil
	}
	return fmt.Errorf("pathAliases must be an object or an array of { prefix, targets }")
}

type Redirect struct {
	Source      string `json:"source"`      // e.g. "/old-page"
	Destination string `json:"destination"` // e.g. "/new-page"
	Permanent   bool   `json:"permanent"`   // true = 301, false = 302
}

type Rewrite struct {
	Source      string `json:"source"`      // e.g. "/legacy/*"
	Destination string `json:"destination"` // e.g. "/docs/:splat"
}

type SEOConfig struct {
	BaseURL     string `json:"baseUrl,omitempty"`     // e.g. "https://example.com"
	SiteName    string `json:"siteName,omitempty"`    // e.g. "My Site"
	Description string `json:"description,omitempty"` // default meta description
	Image       string `json:"image,omitempty"`       // default OG image URL
}

type RobotsConfig struct {
	Allow    string `json:"allow,omitempty"`    // e.g. "/" (default: all)
	Disallow string `json:"disallow,omitempty"` // e.g. "/admin/"
	Sitemap  string `json:"sitemap,omitempty"`  // e.g. "https://example.com/sitemap.xml"
}

type Config struct {
	Entry       string          `json:"entry"`
	OutDir      string          `json:"outDir"`
	PagesDir    string          `json:"pagesDir"`
	PublicDir   string          `json:"publicDir"`
	Minify      bool            `json:"minify"`
	MinifyHTML  bool            `json:"minifyHTML,omitempty"`
	MinifyCSS   bool            `json:"minifyCSS,omitempty"`
	MinifyJS    bool            `json:"minifyJS,omitempty"`
	Sourcemap   bool            `json:"sourcemap"`
	DevServer   DevServer       `json:"devServer"`
	Server      ServerConfig    `json:"server,omitempty"`
	CORS        CORSConfig      `json:"cors,omitempty"`
	API         APIConfig       `json:"api,omitempty"`
	Plugins     []PluginConfig  `json:"plugins,omitempty"`
	Markdown    markdown.Config `json:"markdown,omitempty"`
	Tailwind    TailwindCfg     `json:"tailwind,omitempty"`
	CSP         CSPConfig       `json:"csp,omitempty"`
	Runtime     string          `json:"runtime,omitempty"`
	SSR         SSRConfig       `json:"ssr,omitempty"`
	PathAliases PathAliases     `json:"pathAliases,omitempty"` // from tsconfig.json paths or config object
	TSBaseDir   string          `json:"tsBaseDir,omitempty"`   // baseUrl resolved to absolute path
	Redirects   []Redirect      `json:"redirects,omitempty"`   // config-based redirects
	Rewrites    []Rewrite       `json:"rewrites,omitempty"`    // config-based rewrites
	SEO         SEOConfig       `json:"seo,omitempty"`         // SEO metadata (baseUrl, siteName, description)
	Robots      RobotsConfig    `json:"robots,omitempty"`      // robots.txt config

	// BasePath is the URL path prefix the whole site is served under (e.g.
	// "/docs"). Empty means root. Used for emitted asset URLs and the SPA
	// router so the site works when hosted under a sub-path.
	BasePath string `json:"basePath,omitempty"`

	// Output selects the site output mode. "" (default) allows request-time
	// rendering (SSR/ISR/streaming + dynamic route fallbacks). "static" makes
	// the build fully static: dynamic routes render only the params returned by
	// generateStaticParams, unknown params 404, and SSR/ISR/streaming are
	// disabled. Per-page `export const dynamicParams = true` can re-enable
	// dynamic fallback for a specific route.
	Output string `json:"output,omitempty"`

	// Server components: build-time rendered, no client JS shipped
	// Can also be marked via // @server directive or *.server.tsx file convention
	ServerComponents []string `json:"serverComponents,omitempty"`

	// Runtime server components: executed at runtime via quickjs or sidecar
	// Can also be marked via // @runtime directive or *.runtime.tsx file convention
	RuntimeComponents []string `json:"runtimeComponents,omitempty"`

	// Server directories: all components in these dirs are treated as @server.
	// Paths are relative to project root (e.g. "src/components/server").
	ServerDirs []string `json:"serverDirs,omitempty"`

	// Runtime directories: all components in these dirs are treated as @runtime.
	// Paths are relative to project root (e.g. "src/components/runtime").
	RuntimeDirs []string `json:"runtimeDirs,omitempty"`

	// Content declares typed content collections (`content: defineContent({...})`
	// or a plain object). Each key is a collection name mapping to a `dir` and
	// a frontmatter `schema`. Krate validates entries and generates types.
	Content map[string]any `json:"content,omitempty"`

	// Checks configures compiler-enforced quality gates (a11y/SEO/perf). When
	// present and active, `krate build` runs the rules and fails on findings at
	// or above the configured `failOn` severity. `krate check` uses the same
	// rules. See internal/check.
	Checks map[string]any `json:"checks,omitempty"`
}

func (c *Config) ShouldMinifyHTML() bool { return c.MinifyHTML || c.Minify }
func (c *Config) ShouldMinifyCSS() bool  { return c.MinifyCSS || c.Minify }
func (c *Config) ShouldMinifyJS() bool   { return c.MinifyJS || c.Minify }

// ServerPort returns the listen port for `krate serve`: server.port, else
// devServer.port, else 3000.
func (c *Config) ServerPort() int {
	if c.Server.Port != 0 {
		return c.Server.Port
	}
	if c.DevServer.Port != 0 {
		return c.DevServer.Port
	}
	return 3000
}

// MaxBodyBytes returns the request body cap for general (page/middleware)
// requests, honoring server.maxBodySize when set.
func (c *Config) MaxBodyBytes() int64 {
	if c.Server.MaxBodySize > 0 {
		return c.Server.MaxBodySize
	}
	return defaultMaxBodyBytes
}

// MaxAPIBodyBytes returns the request body cap for API requests (larger default).
func (c *Config) MaxAPIBodyBytes() int64 {
	if c.Server.MaxBodySize > 0 {
		return c.Server.MaxBodySize
	}
	return defaultMaxAPIBodyBytes
}

// BaseURLPath returns BasePath normalized to either "" or "/prefix" (no trailing
// slash). Used for asset URLs and the SPA router base.
func (c *Config) BaseURLPath() string {
	p := strings.TrimSpace(c.BasePath)
	if p == "" || p == "/" {
		return ""
	}
	if !strings.HasPrefix(p, "/") {
		p = "/" + p
	}
	return strings.TrimRight(p, "/")
}

func Default() *Config {
	return &Config{
		Entry:     "src/index.tsx",
		OutDir:    "dist",
		PagesDir:  "src/pages",
		PublicDir: "public",
		Minify:    true,
		Sourcemap: false,
		Markdown:  markdown.DefaultConfig(),
	}
}

// Warnings collects non-fatal config issues (unknown keys, suspicious values).
// It is populated by Load and read by the CLI to print notices.
var Warnings []string

// Validate runs semantic checks on a loaded config. Fatal issues are returned
// as an error; tolerated-but-suspicious ones are appended to Warnings.
func (c *Config) ValidateConfig() error {
	warnings, err := c.Validate()
	if err != nil {
		return err
	}
	Warnings = append(Warnings, warnings...)
	return nil
}

// Load reads the krate config. If configPath is provided and non-empty, it uses
// that file directly. Otherwise it looks for krate.config.ts in root.
func Load(root string, configPath ...string) (*Config, error) {
	Warnings = nil
	cfg := Default()

	tsPath := ""
	if len(configPath) > 0 && configPath[0] != "" {
		tsPath = configPath[0]
		if !filepath.IsAbs(tsPath) {
			tsPath = filepath.Join(root, tsPath)
		}
	} else {
		tsPath = filepath.Join(root, "krate.config.ts")
	}

	if _, err := os.Stat(tsPath); err == nil {
		data, readErr := os.ReadFile(tsPath)
		if readErr != nil {
			return nil, fmt.Errorf("reading config %s: %w", tsPath, readErr)
		}
		// Try JS execution first (resolves imports, plugin factories, etc.)
		if err := executeTSConfig(tsPath, cfg); err != nil {
			// A config that uses module imports cannot be handled by the static
			// parser. Falling back to it would only produce a misleading
			// "expected export, got import" error — so surface the real reason
			// (most commonly: packages aren't installed correctly).
			if configUsesModules(string(data)) {
				return nil, configNotExecutableError(tsPath, err)
			}
			// Otherwise fall back to static parse (simple literal configs).
			if parseErr := parseTSConfig(string(data), cfg); parseErr != nil {
				return nil, fmt.Errorf("parsing config %s: %w", tsPath, parseErr)
			}
		}
	}

	if err := cfg.ValidateConfig(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	cfg.Resolve(root)

	cfg.LoadTSConfigPaths(root)

	return cfg, nil
}

// ParseOptions unmarshals the plugin's Options map into a typed struct via JSON round-trip.
func (pc PluginConfig) ParseOptions(dest interface{}) error {
	data, err := json.Marshal(pc.Options)
	if err != nil {
		return fmt.Errorf("marshaling plugin options: %w", err)
	}
	if err := json.Unmarshal(data, dest); err != nil {
		return fmt.Errorf("unmarshaling plugin options into %T: %w", dest, err)
	}
	return nil
}

func (c *Config) Resolve(root string) {
	if !filepath.IsAbs(c.Entry) {
		c.Entry = filepath.Join(root, c.Entry)
	}
	if !filepath.IsAbs(c.OutDir) {
		c.OutDir = filepath.Join(root, c.OutDir)
	}
	if !filepath.IsAbs(c.PagesDir) {
		c.PagesDir = filepath.Join(root, c.PagesDir)
	}
	if !filepath.IsAbs(c.PublicDir) {
		c.PublicDir = filepath.Join(root, c.PublicDir)
	}
}

// LoadTSConfigPaths reads tsconfig.json and extracts compilerOptions.paths
// and compilerOptions.baseUrl into the Config's PathAliases and TSBaseDir fields.
func (c *Config) LoadTSConfigPaths(root string) {
	tsconfigPath := filepath.Join(root, "tsconfig.json")
	data, err := os.ReadFile(tsconfigPath)
	if err != nil {
		return // no tsconfig.json, nothing to do
	}

	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		return // invalid JSON, skip silently
	}

	compilerOpts, ok := raw["compilerOptions"].(map[string]interface{})
	if !ok {
		return
	}

	var baseDir string
	if baseUrl, ok := compilerOpts["baseUrl"].(string); ok && baseUrl != "" {
		baseDir = filepath.Join(root, baseUrl)
	} else {
		baseDir = root
	}
	c.TSBaseDir = baseDir

	pathsRaw, ok := compilerOpts["paths"].(map[string]interface{})
	if !ok {
		return
	}

	for prefix, targetsRaw := range pathsRaw {
		var targets []string
		switch v := targetsRaw.(type) {
		case []interface{}:
			for _, t := range v {
				if s, ok := t.(string); ok {
					targets = append(targets, s)
				}
			}
		case string:
			targets = []string{v}
		}
		if len(targets) > 0 {
			c.PathAliases = append(c.PathAliases, PathAlias{
				Prefix:  prefix,
				Targets: targets,
			})
		}
	}
}
