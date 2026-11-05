package build

import (
	"github.com/evanw/esbuild/pkg/api"

	"github.com/kratejs/krate/packages/compiler/internal/tsstrip"
)

// esbuildBuild is api.Build for bundles that can reach project source, which may
// carry tier decorators.
func esbuildBuild(opts api.BuildOptions) api.BuildResult {
	tsstrip.HandleTierDecorators(&opts)
	return api.Build(opts)
}
