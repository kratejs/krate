//go:build !krate_no_cookies

package sdk

import (
	"github.com/kratejs/krate/packages/native/platform/caps"
	"github.com/kratejs/krate/packages/native/sdk/cookies"
	"github.com/kratejs/krate/packages/native/sdk/internal/bridge"
)

func init() {
	areas = append(areas, area{
		name: "cookies",
		attach: func(h *Host) {
			cookies.Register(h.table, h.env)
		},
		supports: func(env *bridge.Env, c string) (bool, bool) {
			if c != "cookies" {
				return false, false
			}
			_, ok := env.App.(caps.SiteData)
			return ok, true
		},
	})
}
