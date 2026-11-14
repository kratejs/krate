package config

// DeployConfig selects the deploy adapter used by `krate deploy` and passes
// adapter-specific options (string key/value).
type DeployConfig struct {
	// Target is the adapter: "netlify", "vercel", "cloudflare", or "gh-pages".
	// `krate deploy --target` overrides it.
	Target string `json:"target,omitempty"`
	// Options are adapter-specific (e.g. vercel: cleanUrls, trailingSlash).
	Options map[string]string `json:"options,omitempty"`
	// Worker is a module whose exports are merged into the generated
	// Cloudflare Worker (scheduled, queue, email, Durable Object classes).
	// Empty uses src/cloudflare.ts (.js, .mjs, .mts) when it exists.
	Worker string `json:"worker,omitempty"`
	// Cloudflare holds options for the cloudflare target only.
	Cloudflare *CloudflareDeployConfig `json:"cloudflare,omitempty"`
}

// CloudflareDeployConfig is deploy.cloudflare.
type CloudflareDeployConfig struct {
	// Cache stores cacheable SSR responses in caches.default (see the
	// deployment guide for the rules).
	Cache bool `json:"cache,omitempty"`
}

// CloudflareCache reports whether deploy.cloudflare.cache is on.
func (d DeployConfig) CloudflareCache() bool {
	return d.Cloudflare != nil && d.Cloudflare.Cache
}
