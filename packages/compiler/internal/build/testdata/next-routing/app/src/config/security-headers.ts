/**
 * Single source of truth for the HTTP security headers Next sends on every route (see next.config.ts).
 * Pure: takes an env map, returns the header list, so it is unit-testable and has no Next imports.
 * Docs: docs/SECURITY_HEADERS.md (what is enforced vs report-only, how to promote).
 *
 * Caddy (deploy/Caddyfile) only sets HSTS, nosniff and Referrer-Policy; everything else comes from here.
 */

export interface SecurityHeader {
  key: string;
  value: string;
}

export interface SecurityHeaderOptions {
  /** Development adds 'unsafe-eval' (React dev tooling / HMR) to the report-only script-src. */
  dev?: boolean;
  /** Where CSP violation reports are POSTed. */
  reportUri?: string;
}

type Env = Record<string, string | undefined>;

/** `https://host:port` of a URL string, or null if it is empty or not an absolute http(s) URL. */
export function originOf(value: string | undefined): string | null {
  if (!value?.trim()) return null;
  try {
    const u = new URL(value.trim());
    return u.protocol === 'http:' || u.protocol === 'https:' ? u.origin : null;
  } catch {
    return null;
  }
}

function origins(...values: (string | undefined)[]): string[] {
  return [...new Set(values.map(originOf).filter((o): o is string => o !== null))];
}

/** Third-party hosts a sign-in or checkout form may be redirected to after posting to the app. */
const FORM_REDIRECT_TARGETS = [
  'https://discord.com', // Discord OAuth
  'https://apis.roblox.com', // Roblox OAuth
  'https://checkout.stripe.com', // Stripe Checkout
  'https://billing.stripe.com', // Stripe customer portal
];

/** Where Discord serves what a ticket transcript shows (/transcripts): attachments, stickers, emoji, avatars, embed images. */
const DISCORD_MEDIA = [
  'https://cdn.discordapp.com',
  'https://media.discordapp.net',
  'https://images-ext-1.discordapp.net',
  'https://images-ext-2.discordapp.net',
];

const TURNSTILE_ORIGIN = 'https://challenges.cloudflare.com';

const PERMISSIONS_POLICY = [
  'accelerometer', 'autoplay', 'bluetooth', 'browsing-topics', 'camera', 'display-capture', 'geolocation',
  'gyroscope', 'hid', 'interest-cohort', 'magnetometer', 'microphone', 'midi', 'payment', 'serial', 'usb',
  'xr-spatial-tracking',
]
  .map((f) => `${f}=()`)
  .join(', ');

const join = (name: string, sources: string[]) => [name, ...sources].join(' ');

/** Directives that cannot break rendering: enforced. */
export function enforcedCsp(env: Env): string {
  const app = origins(env.APP_URL);
  return [
    "base-uri 'self'",
    join('form-action', ["'self'", ...app, ...FORM_REDIRECT_TARGETS]),
    "object-src 'none'",
    "frame-ancestors 'none'",
  ].join('; ');
}

/** The full policy: reported (Content-Security-Policy-Report-Only), not enforced. */
export function reportOnlyCsp(env: Env, opts: SecurityHeaderOptions = {}): string {
  const assets = origins(env.PUBLIC_ASSET_BASE_URL, env.S3_PUBLIC_ENDPOINT, env.S3_ENDPOINT, env.NEXT_PUBLIC_BGMODEL_URL);
  const umami = origins(env.NEXT_PUBLIC_UMAMI_URL);
  // Cloudflare Turnstile (preview-session bot check): only allowed when it is configured.
  const turnstile = env.NEXT_PUBLIC_TURNSTILE_SITE_KEY?.trim() ? [TURNSTILE_ORIGIN] : [];
  const publicAssets = origins(env.PUBLIC_ASSET_BASE_URL, env.S3_PUBLIC_ENDPOINT, env.S3_ENDPOINT);
  return [
    "default-src 'self'",
    // 'unsafe-inline': Next injects inline bootstrap/flight scripts and nonces would force every page to dynamic
    // rendering (see docs/SECURITY_HEADERS.md). 'wasm-unsafe-eval': onnxruntime-web compiles WebAssembly.
    join('script-src', ["'self'", "'unsafe-inline'", "'wasm-unsafe-eval'", ...(opts.dev ? ["'unsafe-eval'"] : []), ...umami, ...turnstile]),
    "style-src 'self' 'unsafe-inline'",
    join('img-src', ["'self'", 'data:', 'blob:', ...DISCORD_MEDIA, ...publicAssets]),
    join('media-src', ["'self'", 'blob:', ...DISCORD_MEDIA, ...publicAssets]),
    join('font-src', ["'self'", 'data:', ...publicAssets]),
    join('connect-src', ["'self'", 'blob:', 'data:', ...assets, ...umami, ...turnstile]),
    "worker-src 'self' blob:",
    "manifest-src 'self'",
    turnstile.length ? join('frame-src', turnstile) : "frame-src 'none'",
    ...enforcedCsp(env).split('; '),
    `report-uri ${opts.reportUri ?? '/api/csp-report'}`,
  ].join('; ');
}

export function buildSecurityHeaders(env: Env = process.env, opts: SecurityHeaderOptions = {}): SecurityHeader[] {
  return [
    { key: 'X-Content-Type-Options', value: 'nosniff' },
    { key: 'Referrer-Policy', value: 'strict-origin-when-cross-origin' },
    { key: 'X-Frame-Options', value: 'DENY' },
    { key: 'Permissions-Policy', value: PERMISSIONS_POLICY },
    // Popups opened by us keep their opener; the app itself cannot be scripted by cross-origin windows.
    { key: 'Cross-Origin-Opener-Policy', value: 'same-origin-allow-popups' },
    // Intentionally no Cross-Origin-Resource-Policy: /api/share/*/preview and OG images must stay embeddable
    // by Discord/Twitter crawlers and unfurlers, which is cross-origin and cross-site.
    { key: 'Content-Security-Policy', value: enforcedCsp(env) },
    { key: 'Content-Security-Policy-Report-Only', value: reportOnlyCsp(env, opts) },
  ];
}
