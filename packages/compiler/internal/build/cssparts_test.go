package build

import (
	"regexp"
	"testing"
)

// assertForResolves fails when any for="..." has no matching id="..." in html.
func assertForResolves(t *testing.T, html string) {
	t.Helper()
	ids := map[string]bool{}
	for _, m := range regexp.MustCompile(`id=("?)([^"\s>]+)`).FindAllStringSubmatch(html, -1) {
		ids[m[2]] = true
	}
	fors := regexp.MustCompile(`for=("?)([^"\s>]+)`).FindAllStringSubmatch(html, -1)
	if len(fors) == 0 {
		t.Fatalf("expected trigger labels with for=:\n%.900s", html)
	}
	for _, m := range fors {
		if !ids[m[2]] {
			t.Errorf("label for=%q has no matching controller id:\n%.1200s", m[2], html)
		}
	}
}
