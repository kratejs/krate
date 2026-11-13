//go:build !krate_no_notify

package sdk

import (
	"github.com/kratejs/krate/packages/native/sdk/internal/bridge"
	"github.com/kratejs/krate/packages/native/sdk/notify"
)

func init() {
	areas = append(areas, area{
		name: "notify",
		attach: func(h *Host) {
			notify.Register(h.table, h.env)
		},
		supports: func(env *bridge.Env, c string) (bool, bool) {
			if c != "notify" {
				return false, false
			}
			return notify.Available(env), true
		},
	})
}
