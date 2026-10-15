package ui

import (
	"time"

	kr "github.com/kratejs/krate/packages/compiler/goext/krate"
)

// GoRuntimeTimeProps are the props for <GoRuntimeTime/>.
type GoRuntimeTimeProps struct {
	Label string `json:"label"`
}

// krate:component runtime
//
// GoRuntimeTime runs at REQUEST time in the Go host, so the timestamp below is
// fresh for every request (compare it with <GoServerTime/>, which is baked).
func GoRuntimeTime(p GoRuntimeTimeProps) kr.Node {
	label := p.Label
	if label == "" {
		label = "Runtime time"
	}
	return kr.Span(
		kr.Class("go-time", "go-runtime-time"),
		kr.Text(label+": "+time.Now().Format(time.RFC3339Nano)),
	)
}
