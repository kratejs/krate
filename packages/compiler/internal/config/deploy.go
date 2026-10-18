package config

// DeployConfig selects the deploy adapter used by `krate deploy` and passes
// adapter-specific options (string key/value).
type DeployConfig struct {
	// Target is the adapter: "netlify", "vercel", "cloudflare", or "gh-pages".
	// `krate deploy --target` overrides it.
	Target string `json:"target,omitempty"`
	// Options are adapter-specific (e.g. vercel: cleanUrls, trailingSlash).
	Options map[string]string `json:"options,omitempty"`
}
