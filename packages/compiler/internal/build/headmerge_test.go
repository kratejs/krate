package build

import "testing"

// These cases mirror MERGE_CASES in packages/runtime/src/head-tags.test.ts: the
// Go host and the portable handler must produce the same document head.
func TestMergeHead(t *testing.T) {
	cases := []struct{ name, doc, title, head, want string }{
		{
			name:  "adds tags before </head>, title when the shell has none",
			doc:   "<html><head><meta charset=UTF-8></head><body>x</body></html>",
			title: "Post",
			head:  `<meta name="description" content="d"><link rel="canonical" href="https://e.com/a">`,
			want: "<html><head><meta charset=UTF-8><title>Post</title>" +
				`<meta name="description" content="d"><link rel="canonical" href="https://e.com/a"></head><body>x</body></html>`,
		},
		{
			name: "page wins over baked tags with the same name, property and canonical (minified shell)",
			doc: "<head><meta name=description content=\"layout\"/><meta property=og:title content=Site /><meta property=og:site_name content=Site />" +
				"<link rel=canonical href=https://e.com/layout /><title>Baked</title></head>",
			title: "Fresh",
			head: `<meta name="description" content="page"><meta property="og:title" content="Page">` +
				`<link rel="canonical" href="https://e.com/page">`,
			want: "<head><meta property=og:site_name content=Site /><title>Fresh</title>" +
				`<meta name="description" content="page"><meta property="og:title" content="Page">` +
				`<link rel="canonical" href="https://e.com/page"></head>`,
		},
		{
			name: "repeated property keeps every tag, repeated name keeps the last",
			doc:  "<head></head>",
			head: `<meta property="og:image" content="a"><meta property="og:image" content="b">` +
				`<meta name="robots" content="index"><meta name="robots" content="noindex">`,
			want: `<head><meta property="og:image" content="a"><meta property="og:image" content="b"><meta name="robots" content="noindex"></head>`,
		},
		{
			name: "identical tags are not added twice, scripts and links are appended",
			doc:  `<head><link rel=stylesheet href=/a.css><script type=application/ld+json>{"a":1}</script></head>`,
			head: `<link rel="stylesheet" href="/a.css"><link rel="stylesheet" href="/b.css">` +
				`<script type="application/ld+json">{"a":1}</script><script type="application/ld+json">{"b":2}</script>`,
			want: `<head><link rel=stylesheet href=/a.css><script type=application/ld+json>{"a":1}</script>` +
				`<link rel="stylesheet" href="/b.css"><script type="application/ld+json">{"b":2}</script></head>`,
		},
		{
			name: "a meta inside a baked script is not a tag, other elements are dropped",
			doc:  `<head><script>var s="<meta name=description content=x>";</script></head>`,
			head: `<meta name="description" content="p"><div>no</div><p>no</p>`,
			want: `<head><script>var s="<meta name=description content=x>";</script><meta name="description" content="p"></head>`,
		},
		{
			name:  "escaped attribute values pass through",
			doc:   "<head></head>",
			title: "A &amp; B",
			head:  `<meta name="description" content="About &lt;&quot;x&quot;&gt; &amp; more">`,
			want:  `<head><title>A &amp; B</title><meta name="description" content="About &lt;&quot;x&quot;&gt; &amp; more"></head>`,
		},
		{
			name: "no head tags and no title leaves the document alone",
			doc:  "<head><title>Baked</title></head>",
			want: "<head><title>Baked</title></head>",
		},
		{
			name:  "title only replaces the baked one",
			doc:   "<head><title>Baked</title><meta name=description content=keep></head>",
			title: "New",
			want:  "<head><title>New</title><meta name=description content=keep></head>",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := mergeHead(c.doc, c.title, c.head); got != c.want {
				t.Errorf("mergeHead:\n got: %s\nwant: %s", got, c.want)
			}
		})
	}
	if got := mergeHead("<p>x</p>", "T", `<meta name="a" content="b">`); got != "<p>x</p>" {
		t.Errorf("a document without a head section must be left alone, got %q", got)
	}
}
