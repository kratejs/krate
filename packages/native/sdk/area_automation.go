//go:build !krate_no_automation

package sdk

import (
	"github.com/kratejs/krate/packages/native/sdk/automation"
	"github.com/kratejs/krate/packages/native/sdk/internal/bridge"
)

func init() {
	areas = append(areas, area{
		name: "automation",
		attach: func(h *Host) {
			automation.Register(h.table, h.env)
		},
		supports: func(env *bridge.Env, c string) (bool, bool) {
			if c != "automation" {
				return false, false
			}
			return automation.Available(env), true
		},
	})
}
