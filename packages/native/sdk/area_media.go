//go:build !krate_no_media

package sdk

import (
	"github.com/kratejs/krate/packages/native/sdk/internal/bridge"
	"github.com/kratejs/krate/packages/native/sdk/media"
)

func init() {
	areas = append(areas, area{
		name: "media",
		attach: func(h *Host) {
			media.Register(h.table, h.env)
		},
		supports: func(env *bridge.Env, c string) (bool, bool) {
			if c != "media" {
				return false, false
			}
			return media.Available(env), true
		},
	})
}
