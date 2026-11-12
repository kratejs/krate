//go:build !krate_no_theme

package sdk

import (
	"github.com/kratejs/krate/packages/native/sdk/internal/bridge"
	"github.com/kratejs/krate/packages/native/sdk/theme"
)

func init() {
	areas = append(areas, area{
		name: "theme",
		attach: func(h *Host) {
			theme.Register(h.table, h.env)
		},
		supports: func(env *bridge.Env, c string) (bool, bool) {
			if c != "theme" {
				return false, false
			}
			return theme.Available(env), true
		},
	})
}
