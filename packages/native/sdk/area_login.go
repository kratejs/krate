//go:build !krate_no_login

package sdk

import (
	"github.com/kratejs/krate/packages/native/sdk/internal/bridge"
	"github.com/kratejs/krate/packages/native/sdk/login"
)

func init() {
	areas = append(areas, area{
		name: "login",
		attach: func(h *Host) {
			login.Register(h.table, h.env)
		},
		supports: func(env *bridge.Env, c string) (bool, bool) {
			if c != "login" {
				return false, false
			}
			return login.Available(env), true
		},
	})
}
