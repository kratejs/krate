package build

import (
	"strings"
	"testing"
)

func rootOf(t *testing.T, html string) string {
	t.Helper()
	i := strings.Index(html, "<div id=root>")
	j := strings.Index(html, "</div> ")
	if i < 0 || j < i {
		t.Fatalf("no root in:\n%s", html)
	}
	return html[i : j+6]
}
