//go:build !krate_no_chrome

package sdk

import (
	"github.com/kratejs/krate/packages/native/sdk/chrome"
	"github.com/kratejs/krate/packages/native/sdk/internal/bridge"
)

func init() {
	areas = append(areas, area{
		name: "chrome",
		attach: func(h *Host) {
			chrome.Register(h.table, h.env)
		},
		supports: func(env *bridge.Env, c string) (bool, bool) {
			return chrome.Supports(env, c)
		},
	})
}
