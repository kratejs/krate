//go:build !krate_no_menu

package sdk

import (
	"github.com/kratejs/krate/packages/native/platform/caps"
	"github.com/kratejs/krate/packages/native/sdk/internal/bridge"
	"github.com/kratejs/krate/packages/native/sdk/menu"
)

func init() {
	areas = append(areas, area{
		name: "menu",
		attach: func(h *Host) {
			menu.Register(h.table, h.env)
		},
		supports: func(env *bridge.Env, c string) (bool, bool) {
			if c != "menu" {
				return false, false
			}
			_, ok := env.App.(caps.Menus)
			return ok, true
		},
	})
}
