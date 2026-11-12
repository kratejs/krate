//go:build !krate_no_crash

package sdk

import (
	"github.com/kratejs/krate/packages/native/sdk/crash"
	"github.com/kratejs/krate/packages/native/sdk/internal/bridge"
)

func init() {
	areas = append(areas, area{
		name: "crash",
		attach: func(h *Host) {
			crash.Register(h.table, h.env)
		},
		supports: func(env *bridge.Env, c string) (bool, bool) {
			if c != "crash" {
				return false, false
			}
			return crash.Available(env), true
		},
	})
}
