//go:build !krate_no_screen

package sdk

import (
	"github.com/kratejs/krate/packages/native/sdk/internal/bridge"
	"github.com/kratejs/krate/packages/native/sdk/screen"
)

func init() {
	areas = append(areas, area{
		name: "screen",
		attach: func(h *Host) {
			screen.Register(h.table, h.env)
		},
		supports: func(env *bridge.Env, c string) (bool, bool) {
			if c != "screen" {
				return false, false
			}
			return screen.Available(env), true
		},
	})
}
