import { createContext, readDeclarative, resolveConfig, type ResolvedConfig } from "./config.js";
import { handleAuth } from "./handler.js";
import { readSession } from "./session.js";
import type { AuthOptions, Session } from "./types.js";

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

export { AuthError } from "./config.js";
export { MemoryAdapter, createMemoryStore } from "./adapters/memory.js";
export * from "./providers/catalog.js";
export { credentials } from "./providers/credentials.js";
export { email, resend, defaultNormalizeIdentifier } from "./providers/email.js";
export { passkey } from "./providers/passkey.js";
export { genericOAuth, defineOAuthProvider } from "./providers/oauth.js";
export { appleClientSecret } from "./providers/apple-secret.js";
export type { ResolvedConfig } from "./config.js";
export * from "./types.js";
