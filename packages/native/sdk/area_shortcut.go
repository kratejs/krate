//go:build !krate_no_shortcut

package sdk

import (
	"github.com/kratejs/krate/packages/native/sdk/internal/bridge"
	"github.com/kratejs/krate/packages/native/sdk/shortcut"
)

func init() {
	areas = append(areas, area{
		name: "shortcut",
		attach: func(h *Host) {
			shortcut.RegisterOps(h.table, h.env)
		},
		supports: func(env *bridge.Env, c string) (bool, bool) {
			if c != "shortcut" {
				return false, false
			}
			return shortcut.Available(env), true
		},
	})
}
