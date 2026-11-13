//go:build !krate_no_media

package all

import (
	"path/filepath"

	sdkos "github.com/kratejs/krate/packages/native/sdk/os"

	"github.com/kratejs/krate/packages/native/sdk/media"
)

func init() {
	configureMedia = func(o Options) {
		c := media.Config{Policy: o.Media.Policy, NoRemember: o.Media.NoRemember}
		// App-wide decisions live in the app data directory.
		if dir, err := sdkos.AppDataDir(); err == nil {
			c.StorePath = filepath.Join(dir, "permissions.json")
		}
		media.Configure(c)
		media.SetAppName(o.Name)
	}
}
