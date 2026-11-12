//go:build !krate_no_port

package sdk

import (
	"github.com/kratejs/krate/packages/native/sdk/internal/bridge"
	"github.com/kratejs/krate/packages/native/sdk/port"
)

func init() {
	areas = append(areas, area{
		name: "port",
		attach: func(h *Host) {
			port.Register(h.table, h.env)
		},
		supports: func(env *bridge.Env, c string) (bool, bool) {
			if c != "port" {
				return false, false
			}
			return port.Available(env), true
		},
	})
}
