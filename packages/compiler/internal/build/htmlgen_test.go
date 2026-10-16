package build

import "testing"

// TestExtractMetaAttr guards the content-attribute parser against bleeding past
// the closing quote. The previous implementation derived the closing quote from
// the wrong offset and captured from the next '=' in the head, so a description
// without an '=' swallowed the following tags.
func TestExtractMetaAttr(t *testing.T) {
	cases := []struct {
		name string
		head string
		attr string
		want string
	}{
		{"double quotes", `<meta name="description" content="Hello world." />`, "description", "Hello world."},
		{"single quotes", `<meta name='description' content='Single quoted' />`, "description", "Single quoted"},
		{"no equals in value", `<meta name="description" content="No equals sign here">`, "description", "No equals sign here"},
		{"bleed past close", `<meta name="description" content="a" /><link rel="stylesheet" href="/x.css" />`, "description", "a"},
		{"missing", `<title>Hi</title>`, "description", ""},
		{"property attrs not matched", `<meta property="og:image" content="x.png" />`, "description", ""},
	}
	for _, tc := range cases {
		if got := extractMetaAttr(tc.head, tc.attr); got != tc.want {
			t.Errorf("%s: got %q, want %q", tc.name, got, tc.want)
		}
	}
}
