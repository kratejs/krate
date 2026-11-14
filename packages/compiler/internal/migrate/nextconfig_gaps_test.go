package migrate

import (
	"strings"
	"testing"
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

func TestNextConfigHasRedirectsNotEmittedUnconditionally(t *testing.T) {
	rep := scanConfig(t, map[string]string{"next.config.ts": `export default {
  async redirects() {
    return [
      { source: '/store', destination: '/templates', permanent: true, has: [{ type: 'query', key: 'ref' }] },
      { source: '/store', destination: '/templates', permanent: true, has: [{ type: 'query', key: 'utm_source' }] },
      { source: '/store', destination: '/templates', permanent: true, missing: [{ type: 'cookie', key: 'seen', value: '1' }] },
      { source: '/old', destination: '/new', permanent: true },
    ];
  },
};`})
	body := configBody(t, rep)
	if strings.Contains(body, `"/store"`) {
		t.Fatalf("conditional redirect emitted unconditionally:\n%s", body)
	}
	if !strings.Contains(body, `"/old"`) {
		t.Fatalf("unconditional redirect lost:\n%s", body)
	}
	for _, want := range []string{`has query "ref"`, `has query "utm_source"`, `missing cookie "seen" = "1"`} {
		found := false
		for _, m := range rep.Manual {
			if strings.Contains(m.Reason, `"/store"`) && strings.Contains(m.Reason, want) && strings.Contains(m.Hint, "middleware.ts") {
				found = true
			}
		}
		if !found {
			t.Errorf("no manual item for %s: %+v", want, rep.Manual)
		}
	}
}

func TestNextConfigHasRewriteAndHeaderRuleNotEmitted(t *testing.T) {
	rep := scanConfig(t, map[string]string{"next.config.js": `module.exports = {
  rewrites: async () => [
    { source: '/a', destination: '/b', has: [{ type: 'header', key: 'x-beta' }] },
    { source: '/c', destination: '/d' },
  ],
  headers: async () => [
    { source: '/p', has: [{ type: 'host', value: 'example.com' }], headers: [{ key: 'X-A', value: '1' }] },
    { source: '/q', headers: [{ key: 'X-B', value: '2' }] },
  ],
};`})
	body := configBody(t, rep)
	for _, bad := range []string{`"/a"`, `"/p"`, `"X-A"`} {
		if strings.Contains(body, bad) {
			t.Errorf("conditional rule leaked (%s):\n%s", bad, body)
		}
	}
	for _, want := range []string{`"/c"`, `"/q"`, `"X-B"`} {
		if !strings.Contains(body, want) {
			t.Errorf("static rule lost (%s):\n%s", want, body)
		}
	}
	if !hasManual(rep, "next.config.js", `"/a" is conditional (has header "x-beta")`) {
		t.Errorf("rewrite condition not reported: %+v", rep.Manual)
	}
	if !hasManual(rep, "next.config.js", `"/p" is conditional (has host = "example.com")`) {
		t.Errorf("header condition not reported: %+v", rep.Manual)
	}
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

func TestNextConfigHelperHeadersKeepStaticEntry(t *testing.T) {
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
	if !strings.Contains(body, `"Cache-Control": "public, max-age=31536000, immutable"`) {
		t.Fatalf("static header entry dropped:\n%s", body)
	}
	if strings.Contains(body, "X-Computed") {
		t.Errorf("computed entry leaked:\n%s", body)
	}
	if !hasManual(rep, "next.config.ts", `source "/(.*)": the headers value is computed`) {
		t.Errorf("helper rule not reported: %+v", rep.Manual)
	}
	if !hasManual(rep, "next.config.ts", `source "/assets/:path*": 1 of 2 header entries are computed`) {
		t.Errorf("partial rule not reported: %+v", rep.Manual)
	}
	var sec *ManualItem
	for i := range rep.Manual {
		if strings.Contains(rep.Manual[i].Reason, "security headers (CSP, HSTS, ...) were NOT migrated") {
			sec = &rep.Manual[i]
		}
	}
	if sec == nil {
		t.Fatalf("security item missing: %+v", rep.Manual)
	}
	// The names live only in the imported helper, not in next.config.
	if !strings.Contains(sec.Reason, "Content-Security-Policy") || !strings.Contains(sec.Reason, "Strict-Transport-Security") {
		t.Errorf("security item should name headers found in the helper: %s", sec.Reason)
	}
	if h := manualForSource(rep, "/(.*)").Hint; !strings.Contains(h, "import a local module") {
		t.Errorf("hint should say the helper can be imported: %q", h)
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
	if !strings.Contains(configBody(t, rep), `"static.example.com"`) {
		t.Errorf("static pattern lost")
	}
	if !hasManual(rep, "next.config.ts", "hostname is computed") {
		t.Fatalf("computed hostname not reported: %+v", rep.Manual)
	}
	for _, m := range rep.Manual {
		if strings.Contains(m.Reason, "hostname is computed") && !strings.Contains(m.Hint, "images.remotePatterns") {
			t.Errorf("hint should point at images.remotePatterns: %+v", m)
		}
	}

	rep = scanConfig(t, map[string]string{"next.config.ts": `export default { images: { remotePatterns: process.env.CDN ? [{ hostname: process.env.CDN }] : [] } };`})
	if !hasManual(rep, "next.config.ts", "remotePatterns is not a static array") {
		t.Errorf("computed remotePatterns array not reported: %+v", rep.Manual)
	}
}

func TestNextConfigPerKeyReports(t *testing.T) {
	rep := scanConfig(t, map[string]string{"next.config.ts": `import { loadEnvConfig } from '@next/env';
loadEnvConfig('../..');
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
