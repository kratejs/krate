//go:build !krate_no_app

package sdk

import (
	appmod "github.com/kratejs/krate/packages/native/sdk/app"
)

func init() {
	areas = append(areas, area{
		name: "app",
		attach: func(h *Host) {
			appmod.Register(h.table, h.env, h.supports)
		},
	})
}
