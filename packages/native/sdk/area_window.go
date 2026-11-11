//go:build !krate_no_window

package sdk

import (
	"os"
	"runtime"

	"github.com/kratejs/krate/packages/native/sdk/internal/bridge"
	"github.com/kratejs/krate/packages/native/sdk/window"
)

func init() {
	areas = append(areas, area{
		name: "window",
		attach: func(h *Host) {
			window.Register(h.table, h.env)
		},
		supports: func(env *bridge.Env, c string) (bool, bool) {
			switch c {
			case "window":
				return true, true
			case "window:fullscreen", "window:frameless":
				_, _, _, err := env.Controls("")
				return err == nil, true
			case "window:alwaysOnTop", "window:position":
				_, _, _, err := env.Controls("")
				wayland := runtime.GOOS == "linux" && os.Getenv("WAYLAND_DISPLAY") != ""
				return err == nil && !wayland, true
			}
			return false, false
		},
	})
}
