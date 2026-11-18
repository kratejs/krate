package migrate

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/config"
)

// A config with only static keys keeps the JSON-shaped output it always had.
func TestNextConfigStaticOnlyOutputIsUnchanged(t *testing.T) {
	rep := scanConfig(t, map[string]string{"next.config.js": `module.exports = {
  basePath: '/docs',
  trailingSlash: true,
  redirects: async () => [{ source: '/a', destination: '/b', permanent: true }],
  rewrites: async () => [{ source: '/r', destination: '/s' }],
  headers: async () => [{ source: '/(.*)', headers: [{ key: 'X-Frame-Options', value: 'DENY' }] }],
  images: { remotePatterns: [{ hostname: 'cdn.example.com', pathname: '/img/**' }], domains: ['old.example.com'] },
};`})
	const want = `import { defineConfig } from '@krate/core';

export default defineConfig({
  "basePath": "/docs",
  "compress": {
    "brotli": true,
    "gzip": true
  },
  "devServer": {
    "open": false,
    "port": 3000
  },
  "entry": "src/pages/index.tsx",
  "headers": [
    {
      "source": "/(.*)",
      "headers": {
        "X-Frame-Options": "DENY"
      }
    }
  ],
  "images": {
    "remotePatterns": [
      {
        "hostname": "cdn.example.com",
        "pathname": "/img/**"
      },
      {
        "hostname": "old.example.com"
      }
    ]
  },
  "minify": true,
  "outDir": "dist",
  "pagesDir": "src/pages",
  "publicDir": "public",
  "redirects": [
    {
      "source": "/a",
      "destination": "/b",
      "statusCode": 308
    }
  ],
  "rewrites": [
    {
      "source": "/r",
      "destination": "/s"
    }
  ],
  "sourcemap": false,
  "trailingSlash": "always"
});
`
	if got := configBody(t, rep); got != want {
		t.Fatalf("static-only krate.config.ts changed:\n%s", got)
	}
}

func TestNextConfigImagesKeepProtocolPortSearch(t *testing.T) {
	rep := scanConfig(t, map[string]string{"next.config.js": `module.exports = {
  images: { remotePatterns: [
    { protocol: 'http', hostname: 'assets.local', port: '9000', pathname: '/img/**', search: '' },
    { protocol: 'https', hostname: 'cdn.example.com' },
  ] },
};`})
	body := configBody(t, rep)
	for _, w := range []string{`"protocol": "http"`, `"port": "9000"`, `"search": ""`, `"protocol": "https"`} {
		if !strings.Contains(body, w) {
			t.Errorf("remotePatterns lost %s:\n%s", w, body)
		}
	}
	for _, m := range rep.Manual {
		if strings.Contains(m.Reason, "remotePatterns") {
			t.Errorf("static patterns should not be manual: %+v", m)
		}
	}
}

const appSecurityHeaders = `export interface SecurityHeader { key: string; value: string }
type Env = Record<string, string | undefined>;

function originOf(value: string | undefined): string | null {
  if (!value?.trim()) return null;
  try {
    const u = new URL(value.trim());
    return u.protocol === 'http:' || u.protocol === 'https:' ? u.origin : null;
  } catch {
    return null;
  }
}

export function buildSecurityHeaders(env: Env, opts: { dev?: boolean } = {}): SecurityHeader[] {
  const app = originOf(env.APP_URL);
  const formAction = ["'self'", app].filter(Boolean).join(' ');
  const script = opts.dev ? "'self' 'unsafe-inline' 'unsafe-eval'" : "'self' 'unsafe-inline'";
  return [
    { key: 'Content-Security-Policy', value: "frame-ancestors 'none'; form-action " + formAction },
    { key: 'Content-Security-Policy-Report-Only', value: "script-src " + script + "; report-uri /api/csp-report" },
    { key: 'X-Frame-Options', value: 'DENY' },
    { key: 'X-Content-Type-Options', value: 'nosniff' },
    { key: 'Referrer-Policy', value: 'strict-origin-when-cross-origin' },
  ];
}
`

const appCacheHeaders = `const IMMUTABLE = 'public, max-age=31536000, immutable';

export function buildCacheRules() {
  return [
    { source: '/_k/chunks/:path*', headers: [{ key: 'Cache-Control', value: IMMUTABLE }] },
    { source: '/brand/:path*', headers: [{ key: 'Cache-Control', value: 'public, max-age=3600' }] },
  ];
}
`

// Shaped like a real monorepo app: helper imports, a computed const, headers()
// returning a const plus a spread call, an env-built remotePatterns list,
// loadEnvConfig and import.meta.dirname.
const appNextConfig = `import { loadEnvConfig } from '@next/env';
import path from 'node:path';
import type { NextConfig } from 'next';
import { buildCacheRules } from './src/config/cache-headers';
import { buildSecurityHeaders } from './src/config/security-headers';

// Load the monorepo-root .env.
loadEnvConfig(path.resolve(import.meta.dirname, '../..'), process.env.NODE_ENV !== 'production', undefined, true);

const remotePatterns: NonNullable<NonNullable<NextConfig['images']>['remotePatterns']> = [
  { protocol: 'https', hostname: 'cdn.discordapp.com' },
];
if (process.env.PUBLIC_ASSET_BASE_URL) {
  try {
    const u = new URL(process.env.PUBLIC_ASSET_BASE_URL);
    remotePatterns.push({
      protocol: u.protocol.replace(':', '') as 'http' | 'https',
      hostname: u.hostname,
      ...(u.port ? { port: u.port } : {}),
    });
  } catch {
    // invalid URL is reported at runtime
  }
}

// Snapshot after the root .env is loaded.
const securityHeaders = buildSecurityHeaders({ ...process.env }, { dev: process.env.NODE_ENV !== 'production' });

const nextConfig: NextConfig = {
  reactStrictMode: true,
  images: { remotePatterns },
  async headers() {
    return [{ source: '/:path*', headers: securityHeaders }, ...buildCacheRules()];
  },
};

export default nextConfig;
`

func appFixture(t *testing.T) *Report {
	t.Helper()
	return scanConfig(t, map[string]string{
		"next.config.ts":                 appNextConfig,
		"src/config/security-headers.ts": appSecurityHeaders,
		"src/config/cache-headers.ts":    appCacheHeaders,
	})
}

func TestNextConfigAppShapedHeadersAndImagesAreCarried(t *testing.T) {
	rep := appFixture(t)
	for _, m := range rep.Manual {
		r := m.Reason + " " + m.Source
		if strings.Contains(r, "krate.config.ts") || strings.Contains(r, "headers") || strings.Contains(r, "remotePatterns") || strings.Contains(r, "security") {
			t.Errorf("unexpected manual item: %+v", m)
		}
	}
	const want = `import { defineConfig } from '@krate/core';
import { buildCacheRules } from './src/config/cache-headers';
import { buildSecurityHeaders } from './src/config/security-headers';
import type { KrateConfig as NextConfig } from '@krate/core';

const remotePatterns: NonNullable<NonNullable<NextConfig['images']>['remotePatterns']> = [
  { protocol: 'https', hostname: 'cdn.discordapp.com' },
];
if (process.env.PUBLIC_ASSET_BASE_URL) {
  try {
    const u = new URL(process.env.PUBLIC_ASSET_BASE_URL);
    remotePatterns.push({
      protocol: u.protocol.replace(':', '') as 'http' | 'https',
      hostname: u.hostname,
      ...(u.port ? { port: u.port } : {}),
    });
  } catch {
    // invalid URL is reported at runtime
  }
}

// Snapshot after the root .env is loaded.
const securityHeaders = buildSecurityHeaders({ ...process.env }, { dev: process.env.NODE_ENV !== 'production' });

export default defineConfig({
  "compress": {
    "brotli": true,
    "gzip": true
  },
  "devServer": {
    "open": false,
    "port": 3000
  },
  "entry": "src/pages/index.tsx",
  "envDir": "../..",
  "headers": [
    { source: '/:path*', headers: securityHeaders },
    ...buildCacheRules()
  ],
  "images": {
    "remotePatterns": [
      ...remotePatterns
    ]
  },
  "minify": true,
  "outDir": "dist",
  "pagesDir": "src/pages",
  "publicDir": "public",
  "sourcemap": false,
  "trailingSlash": "never"
});
`
	body := configBody(t, rep)
	if body != want {
		t.Fatalf("generated krate.config.ts differs.\n--- got ---\n%s\n--- want ---\n%s", body, want)
	}
	if !hasInfo(rep, `set security.hsts: "off"`) {
		t.Errorf("HSTS info missing: %q", rep.Info)
	}
}

// writeScanned writes the planned krate.config.ts and the project's own files
// next to it, so the config can be loaded for real.
func loadPlannedConfig(t *testing.T, rep *Report) *config.Config {
	t.Helper()
	root := rep.Root
	if err := os.WriteFile(filepath.Join(root, "krate.config.ts"), []byte(loadableConfig(configBody(t, rep))), 0o644); err != nil {
		t.Fatal(err)
	}
	config.Warnings = nil
	cfg, err := config.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}

func TestNextConfigAppShapedConfigEvaluatesLikeNext(t *testing.T) {
	t.Setenv("APP_URL", "https://dd.example.com/")
	t.Setenv("PUBLIC_ASSET_BASE_URL", "http://assets.example.com:9000")
	t.Setenv("NODE_ENV", "production")
	rep := appFixture(t)
	cfg := loadPlannedConfig(t, rep)

	// What Next's headers() returns for this env, with each [{key, value}]
	// list folded to a map (the last repeated key wins).
	wantHeaders := []config.HeaderRule{
		{Source: "/:path*", Headers: map[string]string{
			"Content-Security-Policy":             "frame-ancestors 'none'; form-action 'self' https://dd.example.com",
			"Content-Security-Policy-Report-Only": "script-src 'self' 'unsafe-inline'; report-uri /api/csp-report",
			"X-Frame-Options":                     "DENY",
			"X-Content-Type-Options":              "nosniff",
			"Referrer-Policy":                     "strict-origin-when-cross-origin",
		}},
		{Source: "/_k/chunks/:path*", Headers: map[string]string{"Cache-Control": "public, max-age=31536000, immutable"}},
		{Source: "/brand/:path*", Headers: map[string]string{"Cache-Control": "public, max-age=3600"}},
	}
	if !reflect.DeepEqual(cfg.Headers, wantHeaders) {
		got, _ := json.MarshalIndent(cfg.Headers, "", " ")
		t.Fatalf("headers differ from Next's:\n%s", got)
	}
	port9000 := "9000"
	wantImages := []config.ImageRemotePattern{
		{Hostname: "cdn.discordapp.com", Protocol: "https"},
		{Hostname: "assets.example.com", Protocol: "http", Port: &port9000},
	}
	if !reflect.DeepEqual(cfg.Images.RemotePatterns, wantImages) {
		got, _ := json.Marshal(cfg.Images.RemotePatterns)
		t.Fatalf("remotePatterns differ from Next's: %s", got)
	}
}

func TestNextConfigCarriedCodeSeesNodeEnvAndOptionalPatterns(t *testing.T) {
	t.Setenv("APP_URL", "")
	t.Setenv("PUBLIC_ASSET_BASE_URL", "")
	t.Setenv("NODE_ENV", "development")
	rep := appFixture(t)
	cfg := loadPlannedConfig(t, rep)
	if got := cfg.Headers[0].Headers["Content-Security-Policy-Report-Only"]; !strings.Contains(got, "'unsafe-eval'") {
		t.Errorf("dev flag lost: %q", got)
	}
	if len(cfg.Images.RemotePatterns) != 1 {
		t.Errorf("asset pattern added without PUBLIC_ASSET_BASE_URL: %+v", cfg.Images.RemotePatterns)
	}
}

// A helper that imports Next.js cannot run in krate.config.ts: the rule is
// commented out with the error and reported; the others still work.
func TestNextConfigUnrunnableHelperIsCommentedOutAndReported(t *testing.T) {
	rep := scanConfig(t, map[string]string{
		"lib/csp.ts": `import { NextResponse } from 'next/server';
export function cspHeaders() { return [{ key: 'Content-Security-Policy', value: String(NextResponse) }]; }
`,
		"next.config.ts": `import { cspHeaders } from './lib/csp';
export default {
  async headers() {
    return [
      { source: '/(.*)', headers: cspHeaders() },
      { source: '/static/:path*', headers: [{ key: 'Cache-Control', value: 'public, max-age=60' }] },
    ];
  },
};`,
	})
	body := configBody(t, rep)
	if !strings.Contains(body, "// krate migrate: headers[0] was not carried over") || !strings.Contains(body, "// { source: '/(.*)', headers: cspHeaders() }") {
		t.Errorf("failing rule not commented out:\n%s", body)
	}
	if strings.Contains(body, "\n    { source: '/(.*)'") {
		t.Errorf("failing rule still live:\n%s", body)
	}
	if !strings.Contains(body, `"Cache-Control": "public, max-age=60"`) {
		t.Errorf("static rule lost:\n%s", body)
	}
	if strings.Contains(body, "import { cspHeaders }") {
		t.Errorf("import of a disabled piece kept:\n%s", body)
	}
	var item *ManualItem
	for i := range rep.Manual {
		if strings.Contains(rep.Manual[i].Reason, "headers[0] could not be carried") {
			item = &rep.Manual[i]
		}
	}
	if item == nil {
		t.Fatalf("no manual item: %+v", rep.Manual)
	}
	if !strings.Contains(item.Reason, "next/server") {
		t.Errorf("manual item should quote the error naming the import: %s", item.Reason)
	}
}

func TestNextConfigPartialComputedRuleFallsBackToItsStaticHeaders(t *testing.T) {
	rep := scanConfig(t, map[string]string{"next.config.ts": `export default {
  async headers() {
    return [{ source: '/assets/:path*', headers: [
      { key: 'Cache-Control', value: 'public, max-age=31536000, immutable' },
      { key: 'X-Computed', value: compute() },
    ] }];
  },
};`})
	body := configBody(t, rep)
	if !strings.Contains(body, `"Cache-Control": "public, max-age=31536000, immutable"`) || strings.Contains(body, "\n    { key: 'X-Computed'") {
		t.Errorf("static headers should survive:\n%s", body)
	}
	if !hasManual(rep, "krate.config.ts", "its static headers were translated instead") {
		t.Errorf("fallback not reported: %+v", rep.Manual)
	}
}

func TestNextConfigEnvBuiltPatternsAreCarried(t *testing.T) {
	t.Setenv("CDN_URL", "https://img.example.org")
	rep := scanConfig(t, map[string]string{"next.config.ts": `const cdn = new URL(process.env.CDN_URL ?? 'https://cdn.example.com');
export default {
  images: {
    remotePatterns: [
      { protocol: 'https', hostname: cdn.hostname },
      { protocol: 'https', hostname: 'static.example.com' },
    ],
  },
};`})
	for _, m := range rep.Manual {
		if strings.Contains(m.Reason, "remotePatterns") {
			t.Errorf("unexpected manual item: %+v", m)
		}
	}
	cfg := loadPlannedConfig(t, rep)
	var hosts []string
	for _, p := range cfg.Images.RemotePatterns {
		hosts = append(hosts, p.Hostname)
	}
	if !reflect.DeepEqual(hosts, []string{"static.example.com", "img.example.org"}) {
		t.Errorf("hosts = %v", hosts)
	}
}

func TestNextConfigStaticConstRemotePatternsStayStatic(t *testing.T) {
	rep := scanConfig(t, map[string]string{"next.config.ts": `const remotePatterns = [{ protocol: 'https', hostname: 'a.example.com' }];
export default { images: { remotePatterns } };`})
	body := configBody(t, rep)
	if !strings.HasPrefix(body, "import { defineConfig } from '@krate/core';\n\nexport default") || !strings.Contains(body, `"hostname": "a.example.com"`) {
		t.Errorf("static const list should be translated, not carried:\n%s", body)
	}
}

func TestNextConfigCarriedValuesMergeIntoExistingConfig(t *testing.T) {
	rep := scanConfig(t, map[string]string{
		"krate.config.ts": "import { defineConfig } from '@krate/core';\n\nexport default defineConfig({\n  site: { name: 'x' },\n});\n",
		"next.config.ts": `import { buildCacheRules } from './cache';
export default { async headers() { return [...buildCacheRules()]; } };`,
		"cache.ts": `export function buildCacheRules() { return [{ source: '/a', headers: [{ key: 'X-A', value: '1' }] }]; }`,
	})
	merged := findPlan(rep, "krate.config.ts")
	if merged == nil {
		t.Fatalf("merge not planned: %+v %+v", rep.Warnings, rep.Manual)
	}
	got := string(merged.Content)
	if !strings.Contains(got, "import { buildCacheRules } from './cache';") || !strings.Contains(got, "...buildCacheRules()") {
		t.Errorf("code not merged:\n%s", got)
	}
	if strings.Index(got, "import { buildCacheRules }") > strings.Index(got, "export default") {
		t.Errorf("import must precede the export:\n%s", got)
	}
}

func TestNextConfigMissingHSTSInfo(t *testing.T) {
	if missingHSTS([]map[string]string{{"Cache-Control": "x"}}) {
		t.Error("no security headers: no info")
	}
	if !missingHSTS([]map[string]string{{"X-Frame-Options": "DENY"}}) {
		t.Error("security headers without HSTS: info")
	}
	if missingHSTS([]map[string]string{{"X-Frame-Options": "DENY"}, {"strict-transport-security": "max-age=1"}}) {
		t.Error("HSTS present: no info")
	}
}

func TestPreambleDropsTypeImportsAndLoadEnvConfig(t *testing.T) {
	src := `import { loadEnvConfig } from '@next/env';
import type { NextConfig } from 'next';
import { a } from './a';
import { unused } from './unused';
loadEnvConfig('.');
const base = 1;
const derived = a(base);
const other = 2;
derived.push(1);
console.log(other);
export default {};
`
	ps := preambleFor(t, src)
	got := ps.build([]string{"...derived"})
	want := "import { a } from './a';\n\nconst base = 1;\nconst derived = a(base);\nderived.push(1);"
	if got != want {
		t.Errorf("preamble:\n%s\nwant:\n%s", got, want)
	}
}

func preambleFor(t *testing.T, src string) *preambleSource {
	t.Helper()
	path := filepath.Join(t.TempDir(), "next.config.ts")
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	data, _, _ := readNextConfig(path)
	return data.Preamble
}

func TestSplitTopAndCodeMask(t *testing.T) {
	src := "[ {a: 1, b: `x,${[1,2]}`}, '],', /[,]/.test(s) ? 1 : 2, ...f(1, 2), ]"
	parts, ok := splitTop(src, 0)
	if !ok || len(parts) != 4 {
		t.Fatalf("parts = %q ok=%v", parts, ok)
	}
	if parts[1] != "'],'" || parts[3] != "...f(1, 2)" {
		t.Errorf("parts = %q", parts)
	}
	if ids := identifiers("const x = 'skip' + y // z\n/* w */"); !ids["x"] || !ids["y"] || ids["skip"] || ids["z"] || ids["w"] {
		t.Errorf("identifiers = %v", ids)
	}
}
