package build

import (
	"github.com/evanw/esbuild/pkg/api"

	"github.com/kratejs/krate/packages/compiler/internal/config"
)

const authConfigGlobal = "__KRATE_AUTH_CONFIG__"

func authDefines(cfg *config.Config) map[string]string {
	if cfg == nil || !cfg.Auth.Active() {
		return map[string]string{authConfigGlobal: "null"}
	}
	raw, err := cfg.Auth.RuntimeJSON()
	if err != nil {
		return map[string]string{authConfigGlobal: "null"}
	}
	return map[string]string{authConfigGlobal: string(raw)}
}

func withAuthDefines(opts *api.BuildOptions, cfg *config.Config) {
	if opts.Define == nil {
		opts.Define = map[string]string{}
	}
	for k, v := range authDefines(cfg) {
		opts.Define[k] = v
	}
}
