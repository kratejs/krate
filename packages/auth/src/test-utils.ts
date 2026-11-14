import { defineAuth, type AuthInstance } from "./index";
import { resolveConfig } from "./config";
import type { AuthOptions, DeclarativeConfig } from "./types";

export const ORIGIN = "http://localhost:3000";
export const SECRET = "test-secret-with-plenty-of-entropy-0123456789";

export class Client {
  jar = new Map<string, string>();
  constructor(public auth: AuthInstance, public origin = ORIGIN) {}

  cookieHeader(): string {
    return [...this.jar.entries()].map(([k, v]) => `${k}=${encodeURIComponent(v)}`).join("; ");
  }

  absorb(res: Response): void {
    for (const line of res.headers.getSetCookie()) {
      const [pair, ...attrs] = line.split(";");
      const idx = pair!.indexOf("=");
      const name = pair!.slice(0, idx).trim();
      const value = decodeURIComponent(pair!.slice(idx + 1));
      const maxAge = attrs.map((a) => a.trim()).find((a) => a.toLowerCase().startsWith("max-age="));
      if (value === "" || (maxAge && Number(maxAge.split("=")[1]) <= 0)) this.jar.delete(name);
      else this.jar.set(name, value);
    }
  }

  async request(path: string, init: RequestInit & { json?: unknown; form?: Record<string, string> } = {}): Promise<Response> {
    const headers = new Headers(init.headers);
    const cookie = this.cookieHeader();
    if (cookie) headers.set("cookie", cookie);
    let body: BodyInit | undefined = init.body ?? undefined;
    if (init.json !== undefined) {
      headers.set("content-type", "application/json");
      body = JSON.stringify(init.json);
    } else if (init.form) {
      headers.set("content-type", "application/x-www-form-urlencoded");
      body = new URLSearchParams(init.form).toString();
    }
    const req = new Request(this.origin + path, { ...init, headers, body, redirect: "manual" });
    const handler = (init.method ?? "GET").toUpperCase() === "GET" ? this.auth.handlers.GET : this.auth.handlers.POST;
    const res = await handler(req);
    this.absorb(res);
    return res;
  }

  async csrf(): Promise<string> {
    const res = await this.request("/api/auth/csrf");
    return ((await res.json()) as { csrfToken: string }).csrfToken;
  }

  async post(path: string, fields: Record<string, string> = {}, extra: { json?: boolean; headers?: Record<string, string> } = {}): Promise<Response> {
    const csrfToken = fields.csrfToken ?? (await this.csrf());
    if (extra.json) return this.request(path, { method: "POST", json: { ...fields, csrfToken }, headers: extra.headers });
    return this.request(path, { method: "POST", form: { ...fields, csrfToken }, headers: extra.headers });
  }

  async session(): Promise<any> {
    const res = await this.request("/api/auth/session");
    return res.json();
  }
}

export function makeAuth(options: AuthOptions = {}, declarative?: DeclarativeConfig): AuthInstance {
  const instance = defineAuth({ secret: SECRET, ...options });
  if (declarative) {
    const original = instance.config;
    let cached: ReturnType<typeof original> | null = null;
    instance.config = () => (cached ??= resolveConfig({ secret: SECRET, ...options }, declarative));
    const handle = (request: Request) => import("./handler").then((m) => m.handleAuth(request, instance.config()));
    instance.handlers = { GET: handle, POST: handle };
    instance.GET = handle;
    instance.POST = handle;
  }
  return instance;
}

export function makeClient(options: AuthOptions = {}, declarative?: DeclarativeConfig): Client {
  return new Client(makeAuth(options, declarative));
}
