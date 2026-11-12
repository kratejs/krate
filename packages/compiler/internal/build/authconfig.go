package build

import (
	"github.com/evanw/esbuild/pkg/api"

	"github.com/kratejs/krate/packages/compiler/internal/config"
)

const authConfigGlobal = "__KRATE_AUTH_CONFIG__"

func authBanner(cfg *config.Config) string {
	if cfg == nil || !cfg.Auth.Active() {
		return ""
	}
	raw, err := cfg.Auth.RuntimeJSON()
	if err != nil {
		return ""
	}
	return "globalThis." + authConfigGlobal + "=" + string(raw) + ";\n"
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
