package migrate

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/config"
)

func scanConfig(t *testing.T, files map[string]string) *Report {
	t.Helper()
	root := t.TempDir()
	writeNextFile(t, root, "app/page.tsx", `export default function Page() { return <h1>Home</h1>; }`)
	for name, body := range files {
		writeNextFile(t, root, name, body)
	}
	rep, err := ScanNext(root)
	if err != nil {
		t.Fatal(err)
	}
	return rep
}

// loadableConfig swaps the @krate/core import for a local defineConfig so a
// test can evaluate the migrated config without the package installed.
func loadableConfig(body string) string {
	return regexp.MustCompile(`import \{ defineConfig \} from ["']@krate/core["'];?`).ReplaceAllString(body, "const defineConfig = (c: any) => c;")
}

func configBody(t *testing.T, rep *Report) string {
	t.Helper()
	cfg := findPlan(rep, "krate.config.ts")
	if cfg == nil {
		t.Fatal("krate.config.ts not planned")
	}
	return string(cfg.Content)
}

func manualForSource(rep *Report, source string) ManualItem {
	for _, m := range rep.Manual {
		if strings.Contains(m.Reason, `source "`+source+`"`) {
			return m
		}
	}
	return ManualItem{}
}

const ddNextConfig = `export default {
  async redirects() {
    return [
      { source: '/store/plans/compare', destination: '/store/compare', permanent: true },
      { source: '/store/plans', destination: '/store', permanent: true },
      { source: '/store', has: [{ type: 'query', key: 'vehicle' }], destination: '/templates', permanent: true },
      { source: '/store', has: [{ type: 'query', key: 'category' }], destination: '/templates', permanent: true },
      { source: '/store', has: [{ type: 'query', key: 'q' }], destination: '/templates', permanent: true },
      { source: '/store/:slug((?!compare$)[^/]+)', destination: '/templates/:slug', permanent: true },
    ];
  },
};`

// The redirects of the DigitalDesigns app translate with no manual item and
// no middleware: has becomes a config condition, permanent becomes 308.
func TestNextConfigDigitalDesignsRedirectsTranslate(t *testing.T) {
	rep := scanConfig(t, map[string]string{"next.config.ts": ddNextConfig})
	for _, m := range rep.Manual {
		if m.Source == "next.config.ts" {
			t.Errorf("unexpected manual item: %+v", m)
		}
	}
	body := configBody(t, rep)
	for _, want := range []string{
		`"source": "/store"`, `"type": "query"`, `"key": "vehicle"`, `"key": "category"`, `"key": "q"`,
		`"statusCode": 308`, `/store/:slug((?!compare$)[^/]+)`, `"destination": "/templates/:slug"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("config lacks %s:\n%s", want, body)
		}
	}
	if strings.Contains(body, `"permanent"`) {
		t.Errorf("config should carry statusCode, not permanent:\n%s", body)
	}
	if findPlan(rep, "middleware.ts") != nil || findPlan(rep, "src/middleware.ts") != nil {
		t.Errorf("a middleware was created although every rule is expressible")
	}
	// The written config loads and keeps every rule.
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "krate.config.ts"), []byte(loadableConfig(body)), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(root)
	if err != nil {
		t.Fatalf("migrated config does not load: %v", err)
	}
	if len(cfg.Redirects) != 6 || cfg.Redirects[2].Has[0].Key != "vehicle" || cfg.Redirects[5].Status() != 308 {
		t.Errorf("redirects after load: %+v", cfg.Redirects)
	}
}

func TestNextConfigRedirectStatusCodes(t *testing.T) {
	rep := scanConfig(t, map[string]string{"next.config.js": `module.exports = {
  redirects: async () => [
    { source: '/a', destination: '/x', permanent: false },
    { source: '/b', destination: '/x', permanent: true },
    { source: '/c', destination: '/x', statusCode: 303 },
    { source: '/d', destination: '/x', statusCode: 301 },
  ],
};`})
	body := configBody(t, rep)
	re := regexp.MustCompile(`(?s)"source": "/(\w)",\s*"destination": "/x",\s*"statusCode": (\d+)`)
	got := map[string]string{}
	for _, m := range re.FindAllStringSubmatch(body, -1) {
		got[m[1]] = m[2]
	}
	want := map[string]string{"a": "307", "b": "308", "c": "303", "d": "301"}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("redirect /%s status = %q, want %s\n%s", k, got[k], v, body)
		}
	}
}

func TestNextConfigConditionsOnRewritesAndHeaders(t *testing.T) {
	rep := scanConfig(t, map[string]string{"next.config.js": `module.exports = {
  rewrites: async () => [
    { source: '/a', destination: '/b', has: [{ type: 'header', key: 'x-beta' }] },
    { source: '/c', destination: '/d', missing: [{ type: 'cookie', key: 'seen', value: '1' }] },
  ],
  headers: async () => [
    { source: '/p', has: [{ type: 'host', value: 'example.com' }], headers: [{ key: 'X-A', value: '1' }] },
    { source: '/q', headers: [{ key: 'X-B', value: '2' }] },
  ],
};`})
	for _, m := range rep.Manual {
		if m.Source == "next.config.js" {
			t.Errorf("unexpected manual item: %+v", m)
		}
	}
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "krate.config.ts"), []byte(loadableConfig(configBody(t, rep))), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Rewrites) != 2 || cfg.Rewrites[0].Has[0].Key != "x-beta" || cfg.Rewrites[1].Missing[0].Value != "1" {
		t.Errorf("rewrites: %+v", cfg.Rewrites)
	}
	if len(cfg.Headers) != 2 || cfg.Headers[0].Has[0].Type != "host" || cfg.Headers[0].Has[0].Value != "example.com" || cfg.Headers[1].Has != nil {
		t.Errorf("headers: %+v", cfg.Headers)
	}
}

func TestNextConfigUnrunnableRegexRewriteAndHeaderAreManual(t *testing.T) {
	rep := scanConfig(t, map[string]string{"next.config.js": `module.exports = {
  rewrites: async () => [
    { source: '/a', destination: '/b', has: [{ type: 'query', key: 'k', value: '(?<=x)y' }] },
  ],
  headers: async () => [
    { source: '/h/:p((?<=a)b)', headers: [{ key: 'X-A', value: '1' }] },
  ],
};`})
	if !hasManual(rep, "next.config.js", `rewrites[0] source "/a" uses a regex feature Go cannot run`) {
		t.Errorf("rewrite not reported: %+v", rep.Manual)
	}
	if !hasManual(rep, "next.config.js", `headers[0] source "/h/:p((?<=a)b)" uses a regex feature Go cannot run`) {
		t.Errorf("header not reported: %+v", rep.Manual)
	}
	body := configBody(t, rep)
	if strings.Contains(body, `"/a"`) || strings.Contains(body, `X-A`) {
		t.Errorf("an untranslatable rule leaked into the config:\n%s", body)
	}
}

// A value regex Go cannot run moves that one rule into the middleware, in a
// marked block, without a manual item.
func TestNextConfigLookbehindRedirectGoesToMiddleware(t *testing.T) {
	rep := scanConfig(t, map[string]string{
		"next.config.js": `module.exports = {
  redirects: async () => [
    { source: '/old', destination: '/new', permanent: true },
    { source: '/store/:slug', has: [{ type: 'header', key: 'x-ref', value: '(?<=pre-)(?<id>\\d+)' }], destination: '/t/:slug/:id', permanent: false },
  ],
};`,
		"middleware.ts": `import { NextResponse } from 'next/server';
export function middleware(request: Request) {
  return NextResponse.next();
}`,
	})
	for _, m := range rep.Manual {
		if m.Source == "next.config.js" || m.Source == "middleware.ts" {
			t.Errorf("unexpected manual item: %+v", m)
		}
	}
	body := configBody(t, rep)
	if !strings.Contains(body, `"/old"`) || strings.Contains(body, `/store/:slug`) {
		t.Errorf("config should keep /old only:\n%s", body)
	}
	mw := findPlan(rep, "middleware.ts")
	if mw == nil {
		t.Fatal("middleware.ts not planned")
	}
	src := string(mw.Content)
	wants := []string{
		"// krate migrate: next.config redirects[1] (a regex Go cannot run in krate.config.ts)",
		"__krateNextRedirect(request, { pattern: \"^/store(?:/([^/#?]+?))/?$\", names: [\"slug\"], mods: {}, destination: \"/t/:slug/:id\", status: 307,",
		`"value":"(?<=pre-)(?<id>\\d+)"`,
		"if (__krateRedirect) return __krateRedirect;",
		"// krate migrate: next.config redirect helper (begin)",
	}
	for _, w := range wants {
		if !strings.Contains(src, w) {
			t.Errorf("middleware lacks %q:\n%s", w, src)
		}
	}
	// The block is the first statement of the function.
	fn := src[strings.Index(src, "export function middleware"):]
	if i, j := strings.Index(fn, "krate migrate: next.config redirects[1]"), strings.Index(fn, "NextResponse"); i < 0 || (j >= 0 && j < i) {
		t.Errorf("the redirect block is not at the start of the function:\n%s", fn)
	}
	if !hasInfo(rep, "redirects[1]") {
		t.Errorf("no info line: %+v", rep.Info)
	}
}

func TestNextConfigLookbehindRedirectCreatesMiddleware(t *testing.T) {
	rep := scanConfig(t, map[string]string{"next.config.js": `module.exports = {
  redirects: async () => [
    { source: '/x', has: [{ type: 'cookie', key: 'c', value: '(?<=a)b' }], destination: '/y', permanent: true },
  ],
};`})
	mw := findPlan(rep, "middleware.ts")
	if mw == nil {
		t.Fatal("middleware.ts not created")
	}
	src := string(mw.Content)
	if !strings.Contains(src, "export function middleware(request: Request)") || !strings.Contains(src, "status: 308") || !strings.Contains(src, "pattern: \"^/x$\"") {
		t.Errorf("unexpected middleware:\n%s", src)
	}
	for _, m := range rep.Manual {
		if m.Source == "next.config.js" {
			t.Errorf("unexpected manual item: %+v", m)
		}
	}
}

func TestNextConfigLookbehindRedirectManualWhenMiddlewareUneditable(t *testing.T) {
	rep := scanConfig(t, map[string]string{
		"next.config.js": `module.exports = {
  redirects: async () => [
    { source: '/x', has: [{ type: 'cookie', key: 'c', value: '(?<=a)b' }], destination: '/y', permanent: true },
  ],
};`,
		"middleware.ts": `export const middleware = (request: Request) => undefined;`,
	})
	if !hasManual(rep, "next.config", `redirects[0] source "/x" uses a regex Go cannot run`) {
		t.Errorf("want a manual item: %+v", rep.Manual)
	}
}

func hasInfo(rep *Report, substring string) bool {
	for _, i := range rep.Info {
		if strings.Contains(i, substring) {
			return true
		}
	}
	return false
}

func TestNextConfigDuplicateRulesWarn(t *testing.T) {
	rep := scanConfig(t, map[string]string{"next.config.js": `module.exports = {
  redirects: async () => [
    { source: '/x', destination: '/y', permanent: true },
    { source: '/x', destination: '/y', permanent: true },
  ],
};`})
	if !hasWarning(rep, "2 identical rules for \"/x\"") {
		t.Fatalf("duplicate warning missing: %+v", rep.Warnings)
	}
}

func TestNextConfigHelperHeadersAreCarriedAsCode(t *testing.T) {
	rep := scanConfig(t, map[string]string{
		"lib/security-headers.ts": `export function securityHeaders() {
  return [
    { key: 'Content-Security-Policy', value: 'default-src self' },
    { key: 'Strict-Transport-Security', value: 'max-age=63072000' },
  ];
}`,
		"next.config.ts": `import { securityHeaders } from './lib/security-headers';
export default {
  async headers() {
    return [
      { source: '/(.*)', headers: securityHeaders() },
      { source: '/assets/:path*', headers: [
        { key: 'Cache-Control', value: 'public, max-age=31536000, immutable' },
        { key: 'X-Computed', value: compute() },
      ] },
    ];
  },
};`,
	})
	body := configBody(t, rep)
	if !strings.Contains(body, "import { securityHeaders } from './lib/security-headers';") || !strings.Contains(body, "{ source: '/(.*)', headers: securityHeaders() },") {
		t.Fatalf("helper rule not carried as code:\n%s", body)
	}
	if !strings.Contains(body, `"Cache-Control": "public, max-age=31536000, immutable"`) {
		t.Fatalf("static header entry dropped:\n%s", body)
	}
	if strings.Contains(body, "\n    { key: 'X-Computed'") {
		t.Errorf("failing computed entry still live:\n%s", body)
	}
	// The helper runs: only the rule with the undefined compute() is reported.
	if hasManual(rep, "krate.config.ts", "headers[0]") {
		t.Errorf("working helper rule was reported: %+v", rep.Manual)
	}
	if !hasManual(rep, "krate.config.ts", "headers[1] could not be carried") || !hasManual(rep, "krate.config.ts", "compute") {
		t.Errorf("failing rule not reported with its error: %+v", rep.Manual)
	}
	if hasManual(rep, "next.config.ts", "NOT migrated") {
		t.Errorf("security item should not fire for carried headers: %+v", rep.Manual)
	}
}

func TestNextConfigNonArrayHeadersStillFlagsSecurity(t *testing.T) {
	rep := scanConfig(t, map[string]string{"next.config.js": `// sets X-Frame-Options and a CSP via the helper
module.exports = { headers: buildHeaders };`})
	if !hasManual(rep, "next.config.js", "headers (not a static array)") {
		t.Errorf("skipped headers not reported: %+v", rep.Manual)
	}
	if !hasManual(rep, "next.config.js", "security headers (CSP, HSTS, ...) were NOT migrated") {
		t.Errorf("security item missing: %+v", rep.Manual)
	}
}

func TestNextConfigStaticSecurityHeadersAreNotFlagged(t *testing.T) {
	rep := scanConfig(t, map[string]string{"next.config.js": `module.exports = {
  headers: async () => [{ source: '/(.*)', headers: [{ key: 'X-Frame-Options', value: 'DENY' }] }],
};`})
	if !strings.Contains(configBody(t, rep), "X-Frame-Options") {
		t.Fatal("static header not translated")
	}
	if hasManual(rep, "next.config.js", "NOT migrated") {
		t.Errorf("fully translated headers must not be flagged: %+v", rep.Manual)
	}
}

func TestNextConfigEnvBuiltRemotePatterns(t *testing.T) {
	rep := scanConfig(t, map[string]string{"next.config.ts": `const cdn = new URL(process.env.CDN_URL ?? 'https://cdn.example.com');
export default {
  images: {
    remotePatterns: [
      { protocol: 'https', hostname: cdn.hostname },
      { protocol: 'https', hostname: 'static.example.com' },
    ],
  },
};`})
	body := configBody(t, rep)
	if !strings.Contains(body, `"static.example.com"`) || !strings.Contains(body, "{ protocol: 'https', hostname: cdn.hostname }") || !strings.Contains(body, "const cdn = new URL(") {
		t.Errorf("static pattern or carried code missing:\n%s", body)
	}
	for _, m := range rep.Manual {
		if strings.Contains(m.Reason, "remotePatterns") {
			t.Errorf("carried patterns should not be manual: %+v", m)
		}
	}

	rep = scanConfig(t, map[string]string{"next.config.ts": `export default { images: { remotePatterns: process.env.CDN ? [{ hostname: process.env.CDN }] : [] } };`})
	if body := configBody(t, rep); !strings.Contains(body, "...(process.env.CDN ? [{ hostname: process.env.CDN }] : [])") {
		t.Errorf("computed remotePatterns array not carried:\n%s", body)
	}
}

func TestNextConfigPerKeyReports(t *testing.T) {
	rep := scanConfig(t, map[string]string{"next.config.ts": `import { loadEnvConfig } from '@next/env';
loadEnvConfig(process.env.ENV_ROOT);
export default {
  output: 'standalone',
  transpilePackages: ['@acme/ui'],
  serverExternalPackages: ['sharp'],
  outputFileTracingIncludes: { '/api/x': ['./data/**'] },
  reactStrictMode: true,
  experimental: { authInterrupts: true, ppr: true, turbo: {} },
};`})
	for key, hint := range map[string]string{
		"loadEnvConfig":        "project root only",
		"output: 'standalone'": "--target node",
		"transpilePackages":    "transpilePackages",
		"experimental.ppr":     "ppr",
	} {
		var found *ManualItem
		for i := range rep.Manual {
			if strings.Contains(rep.Manual[i].Reason, key) {
				found = &rep.Manual[i]
			}
		}
		if found == nil {
			t.Errorf("no manual item for %s: %+v", key, rep.Manual)
			continue
		}
		if !strings.Contains(found.Hint, hint) {
			t.Errorf("hint for %s = %q, want it to contain %q", key, found.Hint, hint)
		}
	}
	body := configBody(t, rep)
	if !strings.Contains(body, `"serverExternalPackages"`) || !strings.Contains(body, `"sharp"`) ||
		!strings.Contains(body, `"outputFileTracingIncludes"`) || !strings.Contains(body, `"data/**"`) {
		t.Errorf("serverExternalPackages and outputFileTracingIncludes must be carried over:\n%s", body)
	}
	if hasManual(rep, "next.config.ts", "serverExternalPackages") || hasManual(rep, "next.config.ts", "outputFileTracingIncludes") {
		t.Errorf("translated keys must not also be manual items: %+v", rep.Manual)
	}
	if !hasWarning(rep, "reactStrictMode") || !hasWarning(rep, "experimental.turbo") {
		t.Errorf("informational warnings missing: %+v", rep.Warnings)
	}
	if hasWarning(rep, "authInterrupts") || hasManual(rep, "next.config.ts", "authInterrupts") {
		t.Errorf("authInterrupts needs no report")
	}
	if hasWarning(rep, "review custom image loaders") {
		t.Errorf("generic warning should be gone: %+v", rep.Warnings)
	}
}

func TestNextConfigComputedBasePathAndEnvAreManual(t *testing.T) {
	rep := scanConfig(t, map[string]string{"next.config.js": `module.exports = { basePath: process.env.BASE, env: { A: process.env.A } };`})
	if !hasManual(rep, "next.config.js", "basePath is computed") || !hasManual(rep, "next.config.js", "env is not a static string map") {
		t.Errorf("computed basePath/env not reported: %+v", rep.Manual)
	}
}

// Next.js compresses responses unless compress: false; the migrated config
// precompresses assets (brotli and best-ratio gzip) so they are not left to
// the server's on-the-fly gzip.
func TestMigrateNextCompressBecomesPrecompression(t *testing.T) {
	config := func(nextConfig string) string {
		t.Helper()
		files := map[string]string{"src/app/page.tsx": "export default function P() { return <main/>; }\n"}
		if nextConfig != "" {
			files["next.config.mjs"] = nextConfig
		}
		rep, err := ScanNext(gapsProject(t, files))
		if err != nil {
			t.Fatal(err)
		}
		for _, f := range rep.Files {
			if f.Target == "krate.config.ts" {
				return string(f.Content)
			}
		}
		t.Fatalf("no krate.config.ts planned")
		return ""
	}
	want := regexp.MustCompile(`"compress": \{\s*"brotli": true,\s*"gzip": true\s*\}`)
	if got := config(""); !want.MatchString(got) {
		t.Errorf("default config does not precompress:\n%s", got)
	}
	if got := config("export default { compress: true };\n"); !want.MatchString(got) {
		t.Errorf("compress: true does not precompress:\n%s", got)
	}
	if got := config("export default { compress: false };\n"); strings.Contains(got, "compress") {
		t.Errorf("compress: false still precompresses:\n%s", got)
	}
}
