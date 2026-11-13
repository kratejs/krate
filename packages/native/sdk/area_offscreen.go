//go:build !krate_no_offscreen

package sdk

import (
	"github.com/kratejs/krate/packages/native/sdk/internal/bridge"
	"github.com/kratejs/krate/packages/native/sdk/offscreen"
)

func init() {
	areas = append(areas, area{
		name: "offscreen",
		attach: func(h *Host) {
			offscreen.Register(h.table, h.env)
		},
		supports: func(env *bridge.Env, c string) (bool, bool) {
			if c != "offscreen" {
				return false, false
			}
			return offscreen.Available(env), true
		},
	})
}
