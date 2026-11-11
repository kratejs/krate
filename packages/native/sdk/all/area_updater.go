//go:build !krate_no_updater

package all

import (
	"github.com/kratejs/krate/packages/native/scheme"
	"github.com/kratejs/krate/packages/native/sdk/permission"
	sdkupdater "github.com/kratejs/krate/packages/native/sdk/updater"
)

func init() {
	dataAreas = append(dataAreas, func(r *scheme.Registry, set permission.Set) { sdkupdater.Register(r, set) })
	configureUpdater = func(o Options) {
		sdkupdater.Configure(sdkupdater.Config{URL: o.UpdaterURL, PublicKey: o.UpdaterPublicKey})
	}
}
