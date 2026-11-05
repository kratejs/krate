package migrate

import (
	"regexp"
	"strings"
)

var metadataDeclRe = regexp.MustCompile(`(?m)^\s*export\s+(?:(?:const|let|var)\s+(?:metadata|generateMetadata)\b|(?:async\s+)?function\s+generateMetadata\b)`)

// PageMetadataTags renders the head tags of a page module's static
// `export const metadata` (or a generateMetadata that returns a literal object)
// the way `krate migrate nextjs` does when it wraps the page in <Head>:
// title, description, keywords, robots, canonical, icon, theme color, Open
// Graph and Twitter tags. declared reports whether the module has either export
// at all; tags is empty when it does but nothing in it could be resolved at
// build time (a dynamic value), so the caller can say so instead of dropping
// the metadata silently.
func PageMetadataTags(src []byte) (tags string, declared bool) {
	if !metadataDeclRe.Match(src) {
		return "", false
	}
	obj, ok := staticMetadata(src)
	if !ok {
		obj, ok = staticGenerateMetadata(src)
	}
	if !ok {
		return "", true
	}
	head := pageMetadataHead(obj)
	head = strings.TrimSuffix(strings.TrimPrefix(head, "<Head>"), "</Head>")
	return head, true
}
