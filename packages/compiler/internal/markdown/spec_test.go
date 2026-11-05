package markdown

import (
	"encoding/json"
	"os"
	"regexp"
	"strings"
	"testing"
)

type specExample struct {
	Example  int    `json:"example"`
	Section  string `json:"section"`
	Markdown string `json:"markdown"`
	HTML     string `json:"html"`
}

var voidSlashRe = regexp.MustCompile(` ?/>`)

// preA11yRe matches the focus and name attributes Krate adds to every <pre>.
var preA11yRe = regexp.MustCompile(` role="region" aria-label="[^"]*" tabindex="0"`)

func normSpecHTML(s string) string {
	s = preA11yRe.ReplaceAllString(strings.TrimSpace(s), "")
	return voidSlashRe.ReplaceAllString(s, ">")
}

func loadSpec(t *testing.T, name string) []specExample {
	t.Helper()
	data, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	var ex []specExample
	if err := json.Unmarshal(data, &ex); err != nil {
		t.Fatal(err)
	}
	return ex
}

// commonmarkDeviations lists spec examples Krate intentionally renders
// differently, with the reason. Everything else must match the CommonMark
// 0.31 reference output exactly.
var commonmarkDeviations = map[int]string{
	596: "autolink with a scheme outside the safeURL allowlist renders as text",
	598: "autolink with a scheme outside the safeURL allowlist renders as text",
	599: "autolink with a scheme outside the safeURL allowlist renders as text",
	601: "autolink with a scheme outside the safeURL allowlist renders as text",
}

func TestCommonMarkSpec(t *testing.T) {
	cfg := Config{} // no anchors, no highlighting, no GFM: pure CommonMark
	pass, fail := 0, 0
	for _, ex := range loadSpec(t, "commonmark_spec.json") {
		got := normSpecHTML(RenderToHTML(ex.Markdown, cfg))
		want := normSpecHTML(ex.HTML)
		if got == want {
			pass++
			continue
		}
		if _, ok := commonmarkDeviations[ex.Example]; ok {
			continue
		}
		fail++
		t.Errorf("example %d (%s)\nmarkdown: %q\nwant: %q\ngot:  %q", ex.Example, ex.Section, ex.Markdown, want, got)
	}
	t.Logf("commonmark: %d passed, %d failed, %d known deviations", pass, fail, len(commonmarkDeviations))
}
