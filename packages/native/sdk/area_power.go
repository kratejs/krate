//go:build !krate_no_power

package sdk

import (
	"github.com/kratejs/krate/packages/native/sdk/internal/bridge"
	"github.com/kratejs/krate/packages/native/sdk/power"
)

func init() {
	areas = append(areas, area{
		name: "power",
		attach: func(h *Host) {
			power.Register(h.table, h.env)
		},
		supports: func(env *bridge.Env, c string) (bool, bool) {
			if c != "power" {
				return false, false
			}
			return power.Available(env), true
		},
	})
}
