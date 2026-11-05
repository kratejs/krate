package bundler

import (
	"path/filepath"
	"strings"
	"testing"
)

func mdxBundleError(t *testing.T, mdx string) string {
	t.Helper()
	root := t.TempDir()
	writeFiles(t, root, map[string]string{"src/pages/index.mdx": mdx})
	_, err := New(root).Bundle(filepath.Join(root, "src/pages/index.mdx"))
	if err == nil {
		t.Fatal("expected a build error")
	}
	return err.Error()
}

// A parse error in the generated JSX is reported at the line and column the
// offending tag or expression has in the .mdx file.
func TestMDXParseErrorPointsAtSourceLine(t *testing.T) {
	cases := map[string]struct{ src, line string }{
		"expression":  {"---\ntitle: t\n---\n\nintro\n\nValue {1 +} here\n", "index.mdx:7:11: expected an expression after `+`"},
		"attribute":   {"# h\n\nmore <b>x</b> and <Card a={}>x</Card>\n", "index.mdx:3:28: JSX attributes must be assigned"},
		"call":        {"intro\n\ntext\n\n{ foo( }\n\nafter\n", "index.mdx:5:8: expected an argument"},
		"unclosed":    {"intro\n\n<Card title=\"x\">\n\nunclosed card\n", "index.mdx:3:1: expected corresponding JSX closing tag for <Card>"},
		"after front": {"---\ntitle: t\n---\n\n\n\n<Card>\n\nnever closed\n", "index.mdx:7:1:"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			msg := mdxBundleError(t, c.src)
			if !strings.Contains(msg, c.line) {
				t.Errorf("error does not point at the source:\n%s\nwant %q", msg, c.line)
			}
			if strings.Contains(msg, "refer to the generated code") {
				t.Errorf("a mapped error must not claim generated positions:\n%s", msg)
			}
		})
	}
}
