package build

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/config"
)

func buildWithOutput(t *testing.T, files map[string]string) (string, string) {
	t.Helper()
	root := t.TempDir()
	for rel, content := range files {
		writeFileRel(t, root, rel, content)
	}
	cfg := config.Default()
	cfg.PagesDir = filepath.Join(root, "src", "pages")
	cfg.OutDir = filepath.Join(root, "dist")
	cfg.Minify = false
	out, err := captureOutput(func() error { return New(root, cfg).BuildAll() })
	if err != nil {
		t.Fatalf("BuildAll: %v\n%s", err, out)
	}
	return readOut(t, cfg.OutDir, "index.html"), out
}

const seoLib = "export const SITE = 'https://example.com';\n" +
	"export function absoluteUrl(p: string): string { return new URL(p, SITE).toString(); }\n" +
	"export const ogImage = (slug: string) => `${SITE}/og/${encodeURIComponent(slug)}.png`;\n" +
	"export function jsonLd(o: object) { return JSON.stringify({ '@context': 'https://schema.org', ...o }); }"

func TestHeadHelperCallsResolveAtBuildTime(t *testing.T) {
	html, out := buildWithOutput(t, map[string]string{
		"src/lib/seo.ts": seoLib,
		"src/pages/index.tsx": `import { createSignal } from '@krate/runtime';
import { absoluteUrl, ogImage, jsonLd } from '../lib/seo';
export default function Page() {
  const [n, setN] = createSignal(0);
  return <div><Head><title>{'Pricing | ' + 'Acme'}</title><link rel="canonical" href={absoluteUrl('/pricing')} /><meta property="og:image" content={ogImage('a b')} /><script type="application/ld+json">{jsonLd({ '@type': 'Thing' })}</script></Head><button onClick={() => setN(n() + 1)}>{n()}</button></div>;
}`,
	})
	head := html[:strings.Index(html, "</head>")]
	for _, want := range []string{
		`<title>Pricing | Acme</title>`,
		`href="https://example.com/pricing"`,
		`content="https://example.com/og/a%20b.png"`,
		`"@context":"https://schema.org"`,
	} {
		if !strings.Contains(head, want) {
			t.Errorf("head misses %s:\n%s", want, head)
		}
	}
	if strings.Contains(head, "data-kattr") || strings.Contains(out, "KR8353") {
		t.Errorf("head value was deferred:\n%s\n%s", head, out)
	}
}

func TestHeadValueTheBuildCannotEvaluateIsReported(t *testing.T) {
	_, out := buildWithOutput(t, map[string]string{
		"src/lib/seo.ts": seoLib,
		"src/pages/index.tsx": `import { absoluteUrl } from '../lib/seo';
export default function Page() {
  return <main><Head><link rel="canonical" href={absoluteUrl(window.location.pathname)} /></Head><h1>x</h1></main>;
}`,
	})
	if !strings.Contains(out, "KR8353") || !strings.Contains(out, "crawlers will not see it") {
		t.Errorf("expected a KR8353 warning:\n%s", out)
	}
}
