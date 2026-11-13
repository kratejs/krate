//go:build !krate_no_badge

package sdk

import (
	"github.com/kratejs/krate/packages/native/sdk/badge"
	"github.com/kratejs/krate/packages/native/sdk/internal/bridge"
)

func init() {
	areas = append(areas, area{
		name: "badge",
		attach: func(h *Host) {
			badge.Register(h.table, h.env)
		},
		supports: func(env *bridge.Env, c string) (bool, bool) {
			if c != "badge" {
				return false, false
			}
			return badge.Available(env), true
		},
	})
}
