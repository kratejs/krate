//go:build !krate_no_downloads

package sdk

import (
	"github.com/kratejs/krate/packages/native/platform/caps"
	"github.com/kratejs/krate/packages/native/sdk/downloads"
	"github.com/kratejs/krate/packages/native/sdk/internal/bridge"
)

func init() {
	areas = append(areas, area{
		name: "downloads",
		attach: func(h *Host) {
			downloads.Register(h.table, h.env)
		},
		supports: func(env *bridge.Env, c string) (bool, bool) {
			if c != "downloads" {
				return false, false
			}
			_, ok := env.App.(caps.Downloads)
			return ok, true
		},
	})
}
