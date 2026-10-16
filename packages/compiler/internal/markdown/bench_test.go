package markdown

import "testing"

const benchMarkdown = `
# Heading

Some **bold** and _italic_ text with a [link](https://example.com).

- item one
- item two
- item three

` + "```go\nfunc main() { println(\"hi\") }\n```" + `

> A blockquote with more text.
`

func BenchmarkRenderToHTML(b *testing.B) {
	cfg := DefaultConfig()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = RenderToHTML(benchMarkdown, cfg)
	}
}
