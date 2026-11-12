import { createContext, readDeclarative, resolveConfig, type ResolvedConfig } from "./config";
import { handleAuth } from "./handler";
import { readSession } from "./session";
import type { AuthOptions, Session } from "./types";

export interface AuthInstance {
  handlers: { GET: (request: Request) => Promise<Response>; POST: (request: Request) => Promise<Response> };
  GET: (request: Request) => Promise<Response>;
  POST: (request: Request) => Promise<Response>;
  auth: (request: Request) => Promise<Session | null>;
  config: () => ResolvedConfig;
}

export function defineAuth(options: AuthOptions = {}): AuthInstance {
  let resolved: ResolvedConfig | null = null;
  const config = () => (resolved ??= resolveConfig(options, readDeclarative()));
  const handle = (request: Request) => handleAuth(request, config());
  return {
    handlers: { GET: handle, POST: handle },
    GET: handle,
    POST: handle,
    auth: async (request: Request) => {
      const cfg = config();
      const ctx = createContext(cfg, request);
      const state = await readSession(ctx);
      return state.session;
    },
    config,
  };
}

export { AuthError } from "./config";
export { MemoryAdapter, createMemoryStore } from "./adapters/memory";
export * from "./providers/catalog";
export { credentials } from "./providers/credentials";
export { email, resend, defaultNormalizeIdentifier } from "./providers/email";
export { passkey } from "./providers/passkey";
export { genericOAuth, defineOAuthProvider } from "./providers/oauth";
export { appleClientSecret } from "./providers/apple-secret";
export type { ResolvedConfig } from "./config";
export * from "./types";
