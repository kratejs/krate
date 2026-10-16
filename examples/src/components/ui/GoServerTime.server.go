package ui

import (
	"time"

	kr "github.com/kratejs/krate/packages/compiler/goext/krate"
)

// GoServerTimeProps are the props for <GoServerTime/>.
type GoServerTimeProps struct {
	Label string `json:"label"`
}

// krate:component server
//
// GoServerTime is rendered at BUILD time (a server component), so the timestamp
// below is baked into the static HTML and never changes between requests.
func GoServerTime(p GoServerTimeProps) kr.Node {
	label := p.Label
	if label == "" {
		label = "Build time"
	}
	return kr.Span(
		kr.Class("go-time", "go-server-time"),
		kr.Text(label+": "+time.Now().Format(time.RFC3339)),
	)
}
