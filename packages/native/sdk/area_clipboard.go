//go:build !krate_no_clipboard

package sdk

import (
	"github.com/kratejs/krate/packages/native/platform/caps"
	"github.com/kratejs/krate/packages/native/sdk/clipboard"
	"github.com/kratejs/krate/packages/native/sdk/internal/bridge"
)

func init() {
	areas = append(areas, area{
		name: "clipboard",
		attach: func(h *Host) {
			clipboard.Register(h.table, h.env)
		},
		supports: func(env *bridge.Env, c string) (bool, bool) {
			switch c {
			case "clipboard", "clipboard:image", "clipboard:files":
				_, ok := env.App.(caps.Clipboard)
				return ok, true
			case "clipboard:html", "clipboard:rtf", "clipboard:formats":
				_, ok := env.App.(caps.RichClipboard)
				return ok, true
			}
			return false, false
		},
	})
}
