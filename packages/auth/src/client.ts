export interface ClientOptions {
  basePath?: string;
  baseUrl?: string;
  fetch?: typeof fetch;
  navigate?: (url: string) => void;
}

let settings: ClientOptions = {};

export function configureAuthClient(options: ClientOptions): void {
  settings = { ...settings, ...options };
}

function basePath(): string {
  return (settings.basePath ?? "/api/auth").replace(/\/+$/, "");
}

function endpoint(path: string): string {
  return `${settings.baseUrl ?? ""}${basePath()}${path}`;
}

function http(): typeof fetch {
  return settings.fetch ?? ((input, init) => globalThis.fetch(input, init));
}

function go(url: string): void {
  if (settings.navigate) settings.navigate(url);
  else if (typeof window !== "undefined") window.location.href = url;
}

async function json<T = any>(res: Response): Promise<T> {
  try {
    return (await res.json()) as T;
  } catch {
    return {} as T;
  }
}

export async function getCsrfToken(): Promise<string> {
  const res = await http()(endpoint("/csrf"), { credentials: "same-origin", headers: { accept: "application/json" } });
  return (await json<{ csrfToken: string }>(res)).csrfToken;
}

export async function getSession<T = any>(): Promise<T | null> {
  const res = await http()(endpoint("/session"), { credentials: "same-origin", headers: { accept: "application/json" } });
  if (!res.ok) return null;
  const body = await json<T | null>(res);
  return body && Object.keys(body as object).length > 0 ? body : null;
}

export async function getProviders(): Promise<Record<string, { id: string; name: string; type: string; signinUrl: string; callbackUrl: string }>> {
  const res = await http()(endpoint("/providers"), { credentials: "same-origin", headers: { accept: "application/json" } });
  return json(res);
}

async function post(path: string, body: Record<string, unknown>): Promise<Response> {
  const csrfToken = await getCsrfToken();
  return http()(endpoint(path), {
    method: "POST",
    credentials: "same-origin",
    headers: { "content-type": "application/json", accept: "application/json", "x-auth-return-redirect": "1" },
    body: JSON.stringify({ ...body, csrfToken }),
  });
}

export interface SignInResult {
  ok: boolean;
  error: string | null;
  url: string | null;
  twoFactorRequired: boolean;
}

function readResult(res: Response, body: any): SignInResult {
  const url: string | null = typeof body?.url === "string" ? body.url : null;
  let error: string | null = typeof body?.error === "string" ? body.error : null;
  if (!error && url) {
    try {
      error = new URL(url, "http://local").searchParams.get("error");
    } catch {
      error = null;
    }
  }
  return { ok: res.ok && !error, error, url, twoFactorRequired: Boolean(body?.twoFactorRequired) };
}

export interface SignInOptions {
  callbackUrl?: string;
  redirect?: boolean;
  [field: string]: unknown;
}

export async function signIn(provider: string, options: SignInOptions = {}): Promise<SignInResult> {
  const { callbackUrl, redirect = true, ...fields } = options;
  const providers = await getProviders();
  const known = providers[provider];
  if (!known) {
    const fallback = `${basePath()}/signin`;
    if (redirect) go(fallback);
    return { ok: false, error: "UnknownProvider", url: fallback, twoFactorRequired: false };
  }
  const path = known.type === "credentials" ? `/callback/${provider}` : `/signin/${provider}`;
  const res = await post(path, { ...fields, callbackUrl: callbackUrl ?? currentUrl() });
  const result = readResult(res, await json(res));
  if (redirect && result.url && (!result.error || result.twoFactorRequired)) go(result.url);
  return result;
}

export async function signOut(options: { callbackUrl?: string; redirect?: boolean } = {}): Promise<{ url: string | null }> {
  const res = await post("/signout", { callbackUrl: options.callbackUrl ?? currentUrl() });
  const body = await json<{ url?: string }>(res);
  if ((options.redirect ?? true) && body.url) go(body.url);
  return { url: body.url ?? null };
}

export async function updateSession<T = any>(data: unknown): Promise<T | null> {
  const csrfToken = await getCsrfToken();
  const res = await http()(endpoint("/session"), {
    method: "POST",
    credentials: "same-origin",
    headers: { "content-type": "application/json", accept: "application/json" },
    body: JSON.stringify({ csrfToken, data }),
  });
  return res.ok ? json<T | null>(res) : null;
}

function currentUrl(): string {
  return typeof window !== "undefined" ? window.location.href : "/";
}

export interface TwoFactorStatus {
  enabled: boolean;
  pending: boolean;
  backupCodesRemaining: number;
  required: boolean;
}

export interface TwoFactorSetup {
  secret: string;
  otpauthUrl: string;
  algorithm: string;
  digits: number;
  period: number;
}

async function apiResult<T>(res: Response): Promise<{ ok: boolean; error: string | null; data: T }> {
  const body = await json<any>(res);
  return { ok: res.ok, error: res.ok ? null : String(body?.error ?? "Error"), data: body as T };
}

export const twoFactor = {
  async status() {
    const res = await http()(endpoint("/2fa/status"), { credentials: "same-origin", headers: { accept: "application/json" } });
    return apiResult<TwoFactorStatus>(res);
  },
  async setup() {
    return apiResult<TwoFactorSetup>(await post("/2fa/setup", {}));
  },
  async enable(code: string) {
    return apiResult<{ enabled: boolean; backupCodes: string[] }>(await post("/2fa/enable", { code }));
  },
  async disable(code: string) {
    return apiResult<{ enabled: boolean }>(await post("/2fa/disable", { code }));
  },
  async regenerateBackupCodes(code: string) {
    return apiResult<{ backupCodes: string[] }>(await post("/2fa/backup-codes", { code }));
  },
  async verify(code: string, options: { callbackUrl?: string; redirect?: boolean } = {}): Promise<SignInResult> {
    const res = await post("/2fa/verify", { code, callbackUrl: options.callbackUrl });
    const result = readResult(res, await json(res));
    if ((options.redirect ?? true) && result.ok && result.url) go(result.url);
    return result;
  },
};

function toB64url(buffer: ArrayBuffer | ArrayBufferView): string {
  const bytes = buffer instanceof ArrayBuffer ? new Uint8Array(buffer) : new Uint8Array(buffer.buffer, buffer.byteOffset, buffer.byteLength);
  let binary = "";
  for (const b of bytes) binary += String.fromCharCode(b);
  return btoa(binary).replace(/\+/g, "-").replace(/\//g, "_").replace(/=+$/, "");
}

function fromB64url(value: string): ArrayBuffer {
  const padded = value.replace(/-/g, "+").replace(/_/g, "/") + "=".repeat((4 - (value.length % 4)) % 4);
  const binary = atob(padded);
  const out = new Uint8Array(binary.length);
  for (let i = 0; i < binary.length; i++) out[i] = binary.charCodeAt(i);
  return out.buffer;
}

export function isPasskeySupported(): boolean {
  return typeof navigator !== "undefined" && Boolean(navigator.credentials) && typeof (globalThis as any).PublicKeyCredential !== "undefined";
}

function credentialsApi(): CredentialsContainer {
  if (typeof navigator === "undefined" || !navigator.credentials) throw new Error("this browser does not support passkeys");
  return navigator.credentials;
}

export interface PasskeyResult {
  ok: boolean;
  error: string | null;
  url?: string | null;
  credentialID?: string;
}

function ceremonyError(err: unknown): PasskeyResult {
  const name = (err as { name?: string })?.name;
  return { ok: false, error: name === "NotAllowedError" || name === "AbortError" ? "Cancelled" : "PasskeyError" };
}

export async function registerPasskey(options: { email?: string; name?: string; passkeyName?: string; callbackUrl?: string; redirect?: boolean } = {}): Promise<PasskeyResult> {
  const { redirect = true, ...rest } = options;
  const optionsRes = await post("/passkey/register/options", { email: rest.email, name: rest.name });
  const creation = await json<any>(optionsRes);
  if (!optionsRes.ok) return { ok: false, error: String(creation?.error ?? "Error") };
  let credential: any;
  try {
    credential = await credentialsApi().create({
      publicKey: {
        rp: creation.rp,
        user: { ...creation.user, id: fromB64url(creation.user.id) },
        challenge: fromB64url(creation.challenge),
        pubKeyCredParams: creation.pubKeyCredParams,
        timeout: creation.timeout,
        attestation: creation.attestation,
        authenticatorSelection: creation.authenticatorSelection,
        excludeCredentials: (creation.excludeCredentials ?? []).map((c: any) => ({ ...c, id: fromB64url(c.id) })),
      },
    });
  } catch (err) {
    return ceremonyError(err);
  }
  if (!credential) return { ok: false, error: "Cancelled" };
  const res = await post("/passkey/register/verify", {
    email: rest.email,
    name: rest.name,
    passkeyName: rest.passkeyName,
    callbackUrl: rest.callbackUrl ?? currentUrl(),
    credential: {
      id: credential.id,
      rawId: toB64url(credential.rawId),
      type: credential.type,
      authenticatorAttachment: credential.authenticatorAttachment ?? undefined,
      clientExtensionResults: credential.getClientExtensionResults?.() ?? {},
      response: {
        clientDataJSON: toB64url(credential.response.clientDataJSON),
        attestationObject: toB64url(credential.response.attestationObject),
        transports: credential.response.getTransports?.() ?? [],
      },
    },
  });
  const body = await json<any>(res);
  if (!res.ok) return { ok: false, error: String(body?.error ?? "Error") };
  if (redirect && body.url) go(body.url);
  return { ok: true, error: null, url: body.url ?? null, credentialID: body.credentialID };
}

export async function signInWithPasskey(options: { callbackUrl?: string; redirect?: boolean } = {}): Promise<PasskeyResult & { twoFactorRequired?: boolean }> {
  const optionsRes = await post("/passkey/authenticate/options", {});
  const request = await json<any>(optionsRes);
  if (!optionsRes.ok) return { ok: false, error: String(request?.error ?? "Error") };
  let credential: any;
  try {
    credential = await credentialsApi().get({
      publicKey: {
        challenge: fromB64url(request.challenge),
        rpId: request.rpId,
        timeout: request.timeout,
        userVerification: request.userVerification,
        allowCredentials: (request.allowCredentials ?? []).map((c: any) => ({ ...c, id: fromB64url(c.id) })),
      },
    });
  } catch (err) {
    return ceremonyError(err);
  }
  if (!credential) return { ok: false, error: "Cancelled" };
  const res = await post("/passkey/authenticate/verify", {
    callbackUrl: options.callbackUrl ?? currentUrl(),
    credential: {
      id: credential.id,
      rawId: toB64url(credential.rawId),
      type: credential.type,
      authenticatorAttachment: credential.authenticatorAttachment ?? undefined,
      clientExtensionResults: credential.getClientExtensionResults?.() ?? {},
      response: {
        clientDataJSON: toB64url(credential.response.clientDataJSON),
        authenticatorData: toB64url(credential.response.authenticatorData),
        signature: toB64url(credential.response.signature),
        userHandle: credential.response.userHandle ? toB64url(credential.response.userHandle) : null,
      },
    },
  });
  const body = await json<any>(res);
  if (!res.ok) return { ok: false, error: String(body?.error ?? "Error") };
  if ((options.redirect ?? true) && body.url) go(body.url);
  return { ok: true, error: null, url: body.url ?? null, twoFactorRequired: Boolean(body.twoFactorRequired) };
}

export interface PasskeyInfo {
  credentialID: string;
  name: string | null;
  deviceType: string;
  backedUp: boolean;
  transports: string[];
  createdAt: string | null;
}

export async function listPasskeys(): Promise<PasskeyInfo[]> {
  const res = await http()(endpoint("/passkey/list"), { credentials: "same-origin", headers: { accept: "application/json" } });
  if (!res.ok) return [];
  return (await json<{ passkeys: PasskeyInfo[] }>(res)).passkeys ?? [];
}

export async function deletePasskey(credentialID: string): Promise<boolean> {
  return (await post("/passkey/delete", { credentialID })).ok;
}
