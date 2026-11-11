package all

import "github.com/kratejs/krate/packages/native/sdk/window"

func windowOnViewEvent(fn func(view, kind string)) (func(), error) {
	return window.OnViewEvent(func(e window.ViewEvent) { fn(e.View, string(e.Kind)) })
}
