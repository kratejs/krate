package components

import (
	"strconv"

	kr "github.com/kratejs/krate/packages/compiler/goext/krate"
)

// WidgetProps are the props for <Widget/>.
type WidgetProps struct {
	Label string `json:"label"`
}

// krate:component runtime
//
// Widget is a runtime-tier Go component: it renders per request on the compiled
// Go host (not the Node SSR sidecar). Same shape as the TSX fixture so the two
// are comparable.
func Widget(p WidgetProps) kr.Node {
	kids := make([]kr.Node, 0, 50)
	for i := 0; i < 50; i++ {
		kids = append(kids, kr.Li(nil, kr.Text("row-"+strconv.Itoa(i))))
	}
	return kr.Section(kr.Class("widget"),
		kr.H2(nil, kr.Text(p.Label)),
		kr.Ul(nil, kids...),
	)
}
