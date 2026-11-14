import type { RequestContext } from "./config.js";
import type { CredentialsConfig, EmailConfig, OAuthConfig, Provider } from "./types.js";

export function escapeHtml(value: unknown): string {
  return String(value ?? "")
    .replace(/&/g, "&amp;")
    .replace(/</g, "&lt;")
    .replace(/>/g, "&gt;")
    .replace(/"/g, "&quot;")
    .replace(/'/g, "&#39;");
}

const STYLE = `:root{--bg:#0a0a0a;--surface:#141414;--border:#262626;--text:#fafafa;--muted:#a1a1a1;--accent:#3b82f6;--accent-hover:#2563eb;--accent-text:#fff;--danger:#ef4444;--radius:.5rem;--font:system-ui,sans-serif}
*{box-sizing:border-box}body{margin:0;min-height:100vh;display:flex;align-items:center;justify-content:center;background:var(--bg);color:var(--text);font-family:var(--font);padding:1rem}
main{width:100%;max-width:24rem;background:var(--surface);border:1px solid var(--border);border-radius:var(--radius);padding:1.5rem}
h1{font-size:1.25rem;margin:0 0 1rem}p{color:var(--muted);margin:0 0 1rem}form{margin:0 0 .75rem;display:grid;gap:.5rem}
label{font-size:.875rem;color:var(--muted)}input{width:100%;padding:.6rem .75rem;background:var(--bg);color:var(--text);border:1px solid var(--border);border-radius:var(--radius);font:inherit}
button{width:100%;padding:.6rem .75rem;background:var(--accent);color:var(--accent-text);border:0;border-radius:var(--radius);font:inherit;cursor:pointer}
button:hover{background:var(--accent-hover)}button.secondary{background:var(--bg);color:var(--text);border:1px solid var(--border)}button.secondary:hover{background:var(--border)}
.error{color:var(--danger);font-size:.875rem;margin:0 0 1rem}hr{border:0;border-top:1px solid var(--border);margin:1rem 0}`;

function layout(title: string, body: string): Response {
  const html = `<!doctype html><html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><meta name="robots" content="noindex"><title>${escapeHtml(title)}</title><style>${STYLE}</style></head><body><main>${body}</main></body></html>`;
  return new Response(html, { status: 200, headers: { "content-type": "text/html; charset=utf-8", "cache-control": "no-store", "content-security-policy": "default-src 'none'; style-src 'unsafe-inline'; form-action 'self'; base-uri 'none'; frame-ancestors 'none'" } });
}

const ERRORS: Record<string, string> = {
  Configuration: "There is a problem with the server configuration.",
  AccessDenied: "You do not have permission to sign in.",
  Verification: "The sign in link is no longer valid. It may have been used already or it may have expired.",
  CredentialsSignin: "Sign in failed. Check that the details you provided are correct.",
  OAuthSignin: "Could not start the sign in. Try again.",
  OAuthCallback: "Could not complete the sign in with the provider. Try again.",
  OAuthAccountNotLinked: "That email is already used by another sign in method. Sign in with the method you used originally.",
  MissingCSRF: "The sign in form expired. Reload the page and try again.",
  InvalidCode: "That code is not valid.",
  RateLimited: "Too many attempts. Wait a moment and try again.",
  Default: "Something went wrong. Try again.",
};

export function errorMessage(code: string | null | undefined): string {
  return ERRORS[code ?? ""] ?? ERRORS.Default!;
}

function hidden(name: string, value: string): string {
  return `<input type="hidden" name="${escapeHtml(name)}" value="${escapeHtml(value)}">`;
}

export interface SignInPageInput {
  ctx: RequestContext;
  csrfToken: string;
  callbackUrl: string;
  error?: string | null;
}

export function signInPage({ ctx, csrfToken, callbackUrl, error }: SignInPageInput): Response {
  const base = ctx.config.basePath;
  const sections: string[] = [];
  const oauth = ctx.config.providers.filter((p): p is OAuthConfig => p.type === "oauth" || p.type === "oidc");
  const email = ctx.config.providers.filter((p): p is EmailConfig => p.type === "email");
  const creds = ctx.config.providers.filter((p): p is CredentialsConfig => p.type === "credentials");
  for (const p of oauth) {
    sections.push(`<form method="POST" action="${escapeHtml(base)}/signin/${escapeHtml(p.id)}">${hidden("csrfToken", csrfToken)}${hidden("callbackUrl", callbackUrl)}<button type="submit" class="secondary">Sign in with ${escapeHtml(p.name)}</button></form>`);
  }
  for (const p of email) {
    sections.push(`<form method="POST" action="${escapeHtml(base)}/signin/${escapeHtml(p.id)}">${hidden("csrfToken", csrfToken)}${hidden("callbackUrl", callbackUrl)}<label for="email-${escapeHtml(p.id)}">Email</label><input id="email-${escapeHtml(p.id)}" name="email" type="email" autocomplete="email" required><button type="submit">Sign in with ${escapeHtml(p.name)}</button></form>`);
  }
  for (const p of creds) {
    const fields = Object.entries(p.credentials)
      .map(([key, field]) => `<label for="${escapeHtml(p.id)}-${escapeHtml(key)}">${escapeHtml(field.label ?? key)}</label><input id="${escapeHtml(p.id)}-${escapeHtml(key)}" name="${escapeHtml(key)}" type="${escapeHtml(field.type ?? "text")}" placeholder="${escapeHtml(field.placeholder ?? "")}" required>`)
      .join("");
    sections.push(`<form method="POST" action="${escapeHtml(base)}/callback/${escapeHtml(p.id)}">${hidden("csrfToken", csrfToken)}${hidden("callbackUrl", callbackUrl)}${fields}<button type="submit">Sign in with ${escapeHtml(p.name)}</button></form>`);
  }
  const body = `<h1>Sign in</h1>${error ? `<p class="error">${escapeHtml(errorMessage(error))}</p>` : ""}${sections.join("<hr>") || "<p>No sign in methods are configured.</p>"}`;
  return layout("Sign in", body);
}

export function signOutPage({ ctx, csrfToken, callbackUrl }: Omit<SignInPageInput, "error">): Response {
  const body = `<h1>Sign out</h1><p>Are you sure you want to sign out?</p><form method="POST" action="${escapeHtml(ctx.config.basePath)}/signout">${hidden("csrfToken", csrfToken)}${hidden("callbackUrl", callbackUrl)}<button type="submit">Sign out</button></form>`;
  return layout("Sign out", body);
}

export function errorPage(ctx: RequestContext, code: string | null): Response {
  const body = `<h1>Authentication error</h1><p class="error">${escapeHtml(errorMessage(code))}</p><form method="GET" action="${escapeHtml(ctx.config.basePath)}/signin"><button type="submit">Back to sign in</button></form>`;
  const res = layout("Authentication error", body);
  return new Response(res.body, { status: code === "Configuration" ? 500 : code === "AccessDenied" ? 403 : 200, headers: res.headers });
}

export function verifyRequestPage(): Response {
  return layout("Check your email", `<h1>Check your email</h1><p>A sign in link has been sent to your email address.</p>`);
}

export function twoFactorPage(ctx: RequestContext, csrfToken: string, callbackUrl: string, error?: string | null): Response {
  const body = `<h1>Two-factor authentication</h1><p>Enter the code from your authenticator app, or one of your backup codes.</p>${error ? `<p class="error">${escapeHtml(errorMessage(error))}</p>` : ""}<form method="POST" action="${escapeHtml(ctx.config.basePath)}/2fa/verify">${hidden("csrfToken", csrfToken)}${hidden("callbackUrl", callbackUrl)}<label for="code">Code</label><input id="code" name="code" inputmode="text" autocomplete="one-time-code" required autofocus><button type="submit">Verify</button></form>`;
  return layout("Two-factor authentication", body);
}

export function providerSummary(ctx: RequestContext): Record<string, { id: string; name: string; type: string; signinUrl: string; callbackUrl: string }> {
  const out: Record<string, { id: string; name: string; type: string; signinUrl: string; callbackUrl: string }> = {};
  for (const p of ctx.config.providers as Provider[]) {
    out[p.id] = {
      id: p.id,
      name: p.name,
      type: p.type,
      signinUrl: `${ctx.baseUrl}${ctx.config.basePath}/signin/${p.id}`,
      callbackUrl: `${ctx.baseUrl}${ctx.config.basePath}/callback/${p.id}`,
    };
  }
  return out;
}
