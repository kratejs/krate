//go:build !krate_no_worker

package sdk

import (
	"github.com/kratejs/krate/packages/native/sdk/internal/bridge"
	"github.com/kratejs/krate/packages/native/sdk/worker"
)

func init() {
	areas = append(areas, area{
		name: "worker",
		attach: func(h *Host) {
			worker.Register(h.table, h.env)
		},
		supports: func(env *bridge.Env, c string) (bool, bool) {
			if c != "worker" {
				return false, false
			}
			return worker.Available(env), true
		},
	})
}
