//go:build !krate_no_extension

package sdk

import (
	"github.com/kratejs/krate/packages/native/sdk/extension"
	"github.com/kratejs/krate/packages/native/sdk/internal/bridge"
)

func init() {
	areas = append(areas, area{
		name: "extension",
		attach: func(h *Host) {
			extension.Register(h.table, h.env)
		},
		supports: func(env *bridge.Env, c string) (bool, bool) {
			if c != "extension" {
				return false, false
			}
			return extension.Available(env), true
		},
	})
}
