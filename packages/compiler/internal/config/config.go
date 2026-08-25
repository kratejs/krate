package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"krate-compiler/internal/markdown"
)

type DevServer struct {
	Port int  `json:"port"`
	Open bool `json:"open"`
}

// PluginConfig represents a plugin entry from the krate config file.
// For built-in plugins, only Name is needed (matches Go-side init() registration).
// For community plugins, Module points to a JavaScript file (.js/.mjs/.cjs) that
// is bundled and executed inside the embedded QuickJS runtime. The module must
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
	// ServerComponentRuntime controls which runtime executes @runtime server components.
	// "quickjs" (default) = embedded, "node"/"bun"/"deno" = sidecar.
	ServerComponentRuntime string `json:"serverComponentRuntime,omitempty"`
	// SSRRuntime controls which runtime executes SSR/streaming pages.
	// "quickjs" = embedded, "node"/"bun"/"deno" (default) = sidecar.
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
	Plugins     []PluginConfig  `json:"plugins,omitempty"`
	EmitReact   bool            `json:"emitReact"`
	Markdown    markdown.Config `json:"markdown,omitempty"`
	Tailwind    TailwindCfg     `json:"tailwind,omitempty"`
	CSP         CSPConfig       `json:"csp,omitempty"`
	Runtime     string          `json:"runtime,omitempty"`
	SSR         SSRConfig       `json:"ssr,omitempty"`
	PathAliases []PathAlias     `json:"pathAliases,omitempty"` // from tsconfig.json paths
	TSBaseDir   string          `json:"tsBaseDir,omitempty"`   // baseUrl resolved to absolute path
	Redirects   []Redirect      `json:"redirects,omitempty"`   // config-based redirects
	Rewrites    []Rewrite       `json:"rewrites,omitempty"`    // config-based rewrites
	SEO         SEOConfig       `json:"seo,omitempty"`         // SEO metadata (baseUrl, siteName, description)
	Robots      RobotsConfig    `json:"robots,omitempty"`      // robots.txt config

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
}

func (c *Config) ShouldMinifyHTML() bool { return c.MinifyHTML || c.Minify }
func (c *Config) ShouldMinifyCSS() bool  { return c.MinifyCSS || c.Minify }
func (c *Config) ShouldMinifyJS() bool   { return c.MinifyJS || c.Minify }

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

// Load reads the krate config. If configPath is provided and non-empty, it uses
// that file directly. Otherwise it looks for krate.config.ts in root.
func Load(root string, configPath ...string) (*Config, error) {
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
		// Try JS execution first (resolves imports, plugin factories, etc.)
		if err := executeTSConfig(tsPath, cfg); err != nil {
			// Fall back to static parse if npx/Node.js isn't available
			data, readErr := os.ReadFile(tsPath)
			if readErr != nil {
				return nil, fmt.Errorf("reading config: %w", readErr)
			}
			if parseErr := parseTSConfig(string(data), cfg); parseErr != nil {
				return nil, fmt.Errorf("parsing config: %w", parseErr)
			}
		}
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
