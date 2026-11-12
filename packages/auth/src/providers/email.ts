import { readEnv } from "../env";
import type { EmailConfig, SendVerificationParams } from "../types";

export interface EmailOptions {
  id?: string;
  name?: string;
  from?: string;
  maxAge?: number;
  sendVerificationRequest?: (params: SendVerificationParams) => Promise<void> | void;
  generateVerificationToken?: () => Promise<string> | string;
  normalizeIdentifier?: (identifier: string) => string;
}

export function defaultNormalizeIdentifier(identifier: string): string {
  const trimmed = identifier.trim().toLowerCase();
  const at = trimmed.lastIndexOf("@");
  if (at < 1 || trimmed.indexOf("@") !== at) throw new Error("Invalid email address: expected a single @");
  const local = trimmed.slice(0, at);
  const domain = trimmed.slice(at + 1);
  if (!domain.includes(".") || /\s/.test(trimmed)) throw new Error("Invalid email address");
  return `${local}@${domain}`;
}

export function email(options: EmailOptions = {}): EmailConfig {
  return {
    id: options.id ?? "email",
    name: options.name ?? "Email",
    type: "email",
    from: options.from,
    maxAge: options.maxAge ?? 24 * 60 * 60,
    sendVerificationRequest:
      options.sendVerificationRequest ??
      (() => {
        throw new Error("email provider needs a sendVerificationRequest function");
      }),
    generateVerificationToken: options.generateVerificationToken,
    normalizeIdentifier: options.normalizeIdentifier ?? defaultNormalizeIdentifier,
  };
}

function escapeHtml(value: string): string {
  return value.replace(/&/g, "&amp;").replace(/</g, "&lt;").replace(/>/g, "&gt;").replace(/"/g, "&quot;");
}

export interface ResendOptions extends EmailOptions {
  apiKey?: string;
  subject?: string;
  endpoint?: string;
  fetch?: typeof fetch;
}

export function resend(options: ResendOptions = {}): EmailConfig {
  const from = options.from ?? readEnv("AUTH_RESEND_FROM") ?? "Krate <onboarding@resend.dev>";
  return email({
    ...options,
    id: options.id ?? "resend",
    name: options.name ?? "Resend",
    from,
    async sendVerificationRequest({ identifier, url, provider }) {
      const apiKey = options.apiKey ?? readEnv("AUTH_RESEND_KEY", "RESEND_API_KEY");
      if (!apiKey) throw new Error("Resend needs AUTH_RESEND_KEY");
      const host = new URL(url).host;
      const res = await (options.fetch ?? fetch)(options.endpoint ?? "https://api.resend.com/emails", {
        method: "POST",
        headers: { Authorization: `Bearer ${apiKey}`, "Content-Type": "application/json" },
        body: JSON.stringify({
          from: provider.from ?? from,
          to: identifier,
          subject: options.subject ?? `Sign in to ${host}`,
          html: `<p>Sign in to <strong>${escapeHtml(host)}</strong></p><p><a href="${escapeHtml(url)}">Sign in</a></p><p>If you did not request this email you can ignore it.</p>`,
          text: `Sign in to ${host}\n${url}\n\nIf you did not request this email you can ignore it.`,
        }),
      });
      if (!res.ok) throw new Error(`Resend error: ${res.status} ${JSON.stringify(await res.json().catch(() => ({})))}`);
    },
  });
}
