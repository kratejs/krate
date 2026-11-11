//go:build !krate_no_tray

package sdk

import (
	"github.com/kratejs/krate/packages/native/sdk/internal/bridge"
	"github.com/kratejs/krate/packages/native/sdk/tray"
)

func init() {
	areas = append(areas, area{
		name: "tray",
		attach: func(h *Host) {
			tray.Register(h.table, h.env)
		},
		supports: func(env *bridge.Env, c string) (bool, bool) {
			if c != "tray" {
				return false, false
			}
			return tray.Available(env), true
		},
	})
}
