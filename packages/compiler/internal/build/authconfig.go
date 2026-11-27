package build

import (
	"github.com/evanw/esbuild/pkg/api"

	"github.com/kratejs/krate/packages/compiler/internal/config"
)

const authConfigGlobal = "__KRATE_AUTH_CONFIG__"

func authBanner(cfg *config.Config) string {
	if cfg == nil || !cfg.Auth.Declared() {
		return ""
	}
	raw, err := cfg.Auth.RuntimeJSON()
	if err != nil {
		return ""
	}
	return "globalThis." + authConfigGlobal + "=" + string(raw) + ";\n"
}

// serverPageBanner is the banner of a server page bundle: the base path for
// server actions and the auth config.
func serverPageBanner(base, auth string) map[string]string {
	banner := basePathBanner(base)
	if auth == "" {
		return banner
	}
	if banner == nil {
		banner = map[string]string{}
	}
	banner["js"] = auth + banner["js"]
	return banner
}

func withAuthConfig(opts *api.BuildOptions, cfg *config.Config) {
	banner := authBanner(cfg)
	if banner == "" {
		return
	}
	if opts.Banner == nil {
		opts.Banner = map[string]string{}
	}
	opts.Banner["js"] = banner + opts.Banner["js"]
}
