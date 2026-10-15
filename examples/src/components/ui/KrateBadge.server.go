package ui

import kr "github.com/kratejs/krate/packages/compiler/goext/krate"

// KrateBadgeProps are the props for <KrateBadge/>.
type KrateBadgeProps struct {
	Label string   `json:"label"`
	Items []string `json:"items"`
}

// krate:component server
//
// KrateBadge is a Go-authored server component: it is compiled into the Go host
// and rendered at build time. It returns a kr.Node built with the Go UI helper
// package, so text is escaped automatically and the markup is composed rather
// than concatenated.
func KrateBadge(p KrateBadgeProps) kr.Node {
	kids := []kr.Node{kr.Text(p.Label)}
	if len(p.Items) > 0 {
		kids = append(kids, kr.Ul(nil, kr.Each(p.Items, func(it string) kr.Node {
			return kr.Li(nil, kr.Text(it))
		})))
	}
	return kr.Span(kr.Class("krate-go-badge"), kids...)
}
