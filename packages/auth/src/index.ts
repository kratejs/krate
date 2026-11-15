import { createContext, readDeclarative, resolveConfig, type ResolvedConfig } from "./config.js";
import { handleAuth } from "./handler.js";
import { createMiddleware, type AuthMiddleware } from "./middleware.js";
import { scopedSession, serverSignIn, serverSignOut, type SignInOptions, type SignOutOptions } from "./server.js";
import { readSession } from "./session.js";
import type { AuthOptions, Session } from "./types.js";

export interface AuthInstance {
  handlers: { GET: (request: Request) => Promise<Response>; POST: (request: Request) => Promise<Response> };
  GET: (request: Request) => Promise<Response>;
  POST: (request: Request) => Promise<Response>;
  /** The session of a Request, or of the current request scope when called with no argument. */
  auth: {
    (): Promise<Session | null>;
    (request: Request): Promise<Session | null>;
  };
  /** Server-side sign-in for server actions and route handlers; redirects like the form POST. */
  signIn: (provider?: string, options?: SignInOptions, authorizationParams?: Record<string, string>) => Promise<string | undefined>;
  /** Server-side sign-out for server actions and route handlers. */
  signOut: (options?: SignOutOptions) => Promise<string | undefined>;
  middleware: AuthMiddleware;
  config: () => ResolvedConfig;
}

/** Options, or a function that returns them on first use (nothing connects at import time). */
export function defineAuth(options: AuthOptions | (() => AuthOptions) = {}): AuthInstance {
  let resolved: ResolvedConfig | null = null;
  const config = () => (resolved ??= resolveConfig(typeof options === "function" ? options() : options, readDeclarative()));
  const handle = (request: Request) => handleAuth(request, config());
  const instance: AuthInstance = {
    handlers: { GET: handle, POST: handle },
    GET: handle,
    POST: handle,
    auth: (async (request?: Request) => {
      const cfg = config();
      if (!request) return scopedSession(cfg);
      const ctx = createContext(cfg, request);
      const state = await readSession(ctx);
      return state.session;
    }) as AuthInstance["auth"],
    signIn: (provider, signInOptions, authorizationParams) => serverSignIn(config(), provider, signInOptions, authorizationParams),
    signOut: (signOutOptions) => serverSignOut(config(), signOutOptions),
    middleware: createMiddleware(() => instance.config()),
    config,
  };
  return instance;
}

export { AuthError } from "./config.js";
export { createMiddleware, type AuthMiddleware } from "./middleware.js";
export type { SignInOptions, SignOutOptions } from "./server.js";
export { matchesPath } from "./match.js";
export { MemoryAdapter, createMemoryStore } from "./adapters/memory.js";
export * from "./providers/catalog.js";
export { credentials } from "./providers/credentials.js";
export { email, resend, defaultNormalizeIdentifier } from "./providers/email.js";
export { passkey } from "./providers/passkey.js";
export { genericOAuth, defineOAuthProvider } from "./providers/oauth.js";
export { appleClientSecret } from "./providers/apple-secret.js";
export type { ResolvedConfig } from "./config.js";
export * from "./types.js";
