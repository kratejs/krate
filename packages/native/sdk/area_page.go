//go:build !krate_no_page

package sdk

import (
	"github.com/kratejs/krate/packages/native/platform/caps"
	"github.com/kratejs/krate/packages/native/sdk/internal/bridge"
	"github.com/kratejs/krate/packages/native/sdk/page"
)

func init() {
	areas = append(areas, area{
		name: "page",
		attach: func(h *Host) {
			page.Register(h.table, h.env)
		},
		supports: func(env *bridge.Env, c string) (bool, bool) {
			if c != "page" {
				return false, false
			}
			_, w, _, err := env.Controls("")
			if err != nil || w == nil {
				return false, true
			}
			_, ok := w.(caps.PageControls)
			return ok, true
		},
	})
}
