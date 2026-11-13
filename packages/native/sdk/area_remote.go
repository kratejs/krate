//go:build !krate_no_remote

package sdk

import (
	"github.com/kratejs/krate/packages/native/sdk/internal/bridge"
	"github.com/kratejs/krate/packages/native/sdk/remote"
)

func init() {
	areas = append(areas, area{
		name: "remote",
		attach: func(h *Host) {
			remote.Register(h.table, h.env)
		},
		supports: func(env *bridge.Env, c string) (bool, bool) {
			switch c {
			case "remote", "remote:intercept", "remote:network":
				return remote.Available(env), true
			}
			return false, false
		},
	})
}
