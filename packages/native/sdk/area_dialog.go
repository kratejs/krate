//go:build !krate_no_dialog

package sdk

import (
	"github.com/kratejs/krate/packages/native/platform/caps"
	"github.com/kratejs/krate/packages/native/sdk/dialog"
	"github.com/kratejs/krate/packages/native/sdk/internal/bridge"
)

func init() {
	areas = append(areas, area{
		name: "dialog",
		attach: func(h *Host) {
			dialog.Register(h.table, h.env)
		},
		supports: func(env *bridge.Env, c string) (bool, bool) {
			if c != "dialog" {
				return false, false
			}
			_, ok := env.App.(caps.Dialogs)
			return ok, true
		},
	})
}
