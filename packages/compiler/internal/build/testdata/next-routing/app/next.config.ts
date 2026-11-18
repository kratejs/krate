import type { NextConfig } from 'next';
import { buildCacheRules } from './src/config/cache-headers';
import { buildSecurityHeaders } from './src/config/security-headers';

// A fixed env, so the golden does not depend on the machine that generated it.
const securityHeaders = buildSecurityHeaders(
  {
    APP_URL: 'https://app.example.test',
    PUBLIC_ASSET_BASE_URL: 'https://assets.example.test',
    NEXT_PUBLIC_UMAMI_URL: 'https://umami.example.test',
    NEXT_PUBLIC_TURNSTILE_SITE_KEY: 'site-key',
  },
  { dev: false },
);

const nextConfig: NextConfig = {
  reactStrictMode: true,
  async headers() {
    return [
      { source: '/:path*', headers: securityHeaders },
      ...buildCacheRules(),
      // has / missing on header rules, and a user Cache-Control on a page.
      { source: '/store', has: [{ type: 'cookie', key: 'beta' }], headers: [{ key: 'X-Beta', value: 'yes' }] },
      { source: '/templates', missing: [{ type: 'header', key: 'x-skip' }], headers: [{ key: 'X-Not-Skipped', value: '1' }] },
      { source: '/templates', headers: [{ key: 'Cache-Control', value: 'public, max-age=60' }] },
    ];
  },
  async redirects() {
    return [
      // DigitalDesigns, verbatim.
      { source: '/store/plans/compare', destination: '/store/compare', permanent: true },
      { source: '/store/plans', destination: '/store', permanent: true },
      { source: '/store', has: [{ type: 'query', key: 'vehicle' }], destination: '/templates', permanent: true },
      { source: '/store', has: [{ type: 'query', key: 'category' }], destination: '/templates', permanent: true },
      { source: '/store', has: [{ type: 'query', key: 'q' }], destination: '/templates', permanent: true },
      { source: '/store/:slug((?!compare$)[^/]+)', destination: '/templates/:slug', permanent: true },
      // has / missing / captures.
      { source: '/x-header', has: [{ type: 'header', key: 'x-beta', value: 'on' }], destination: '/templates', permanent: false },
      { source: '/x-cookie', has: [{ type: 'cookie', key: 'beta', value: '1' }], destination: '/store', permanent: true },
      { source: '/x-host', has: [{ type: 'host', value: 'old.example.test' }], destination: 'https://new.example.test/x-host', permanent: true },
      { source: '/x-named/:id', has: [{ type: 'query', key: 'ref', value: '(?<campaign>[a-z]+)' }], destination: '/templates/:id?c=:campaign', permanent: false },
      { source: '/x-missing', missing: [{ type: 'cookie', key: 'session' }], destination: '/store', permanent: false },
      { source: '/x-present', has: [{ type: 'query', key: 'tok' }], destination: '/templates/:tok', permanent: true },
      { source: '/x-code', destination: '/store', statusCode: 303 },
      { source: '/x-nested/:a/:b*', destination: '/templates/:a/:b*', permanent: false },
    ];
  },
};

export default nextConfig;
