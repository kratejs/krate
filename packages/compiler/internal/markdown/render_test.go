package markdown

import (
	"strings"
	"testing"
)

func mustContain(t *testing.T, name, got string, wants ...string) {
	t.Helper()
	for _, w := range wants {
		if !strings.Contains(got, w) {
			t.Errorf("%s: missing %q in:\n%s", name, w, got)
		}
	}
}

func mustNotContain(t *testing.T, name, got string, bads ...string) {
	t.Helper()
	for _, b := range bads {
		if strings.Contains(got, b) {
			t.Errorf("%s: unexpected %q in:\n%s", name, b, got)
		}
	}
}
