---
title: Authentication
description: Sign-in with OAuth, passwords, magic links, passkeys and two-factor authentication, configured from krate.config.
order: 12
---

# Authentication

`@krate/auth` adds sign-in to a Krate app. It follows the model of
NextAuth (Auth.js): a `defineAuth()` instance, provider objects, callbacks,
adapters, and a handler that serves `/api/auth/*`. It also ships what
NextAuth leaves to plugins: two-factor authentication, passkeys, route
protection driven by config, and a browser client.

- 56 built-in OAuth and OpenID Connect providers, plus a generic provider for any other.
- Credentials, email magic links and Resend.
- JWT sessions (encrypted, stored in a cookie) or database sessions.
- Drizzle adapter for SQLite, PostgreSQL and MySQL.
- TOTP two-factor authentication with backup codes.
- Passkeys (WebAuthn) with ES256, EdDSA and RS256.
- Runs in Krate's embedded runtime, so API routes and middleware need no Node.js process.

## Quick start

```sh
npm install @krate/auth
```

Declare the parts that are data in `krate.config.ts`:

```typescript
import { defineConfig } from "@krate/core";

export default defineConfig({
  auth: {
    providers: [{ id: "github" }, { id: "google" }],
    pages: { signIn: "/login" },
    protect: [{ source: "/dashboard" }],
  },
});
```

Create the instance in code. This is where callbacks, adapters and
credentials logic live:

```typescript
import { defineAuth } from "@krate/auth";

export const auth = defineAuth({});
```

Mount the handler as one catch-all API route:

```typescript
import { auth } from "../../auth";

export const GET = auth.handlers.GET;
export const POST = auth.handlers.POST;
```

Save that as `src/api/auth/[...all].ts`, and the instance above as
`src/auth.ts`. Protect pages with middleware:

```typescript
import { auth } from "./src/auth";

export function middleware(request: Request) {
  return auth.middleware(request);
}
```

Set the secret and the provider credentials in `.env`:

```sh
AUTH_SECRET=<output of: openssl rand -base64 33>
AUTH_GITHUB_ID=
AUTH_GITHUB_SECRET=
AUTH_GOOGLE_ID=
AUTH_GOOGLE_SECRET=
```

Register `http://localhost:3000/api/auth/callback/github` (and the same path
for each provider) as the OAuth redirect URI. Visit
`/api/auth/signin` for the built-in sign-in page, or build your own and use
the [browser client](#browser-client).

## Configuration

Everything that is plain data goes in the `auth` key of `krate.config.ts`.
The compiler validates it (unknown keys are warned about, invalid values fail
the build) and delivers it to your API routes and middleware. Anything that
needs a function stays in `defineAuth()`. When both set the same option, the
code wins. Lists such as `protect` are merged.

| Key | Default | Description |
|-----|---------|-------------|
| `enabled` | `true` when something is configured | Set `false` to turn the handlers and middleware off |
| `basePath` | `"/api/auth"` | Where the handler is mounted |
| `trustHost` | `false` | Trust the request host. Needed in production unless `AUTH_URL` is set |
| `debug` | `false` | Log debug messages |
| `providers` | `[]` | `{ id, type?, name?, enabled?, options? }`, see [Providers](#providers) |
| `session.strategy` | `"jwt"`, or `"database"` when an adapter is set | Where the session lives |
| `session.maxAge` | 30 days | Seconds a session stays valid |
| `session.updateAge` | 1 day | Seconds between session refreshes |
| `pages` | built-in pages | `signIn`, `signOut`, `error`, `verifyRequest`, `newUser`, `twoFactor` |
| `cookies` | secure on https | `prefix`, `secure`, `sameSite`, `domain` |
| `adapter.dialect` | none | `"sqlite"`, `"postgres"` or `"mysql"` |
| `adapter.tables` | `user`, `account`, ... | Rename tables: `users`, `accounts`, `sessions`, `verificationTokens`, `authenticators`, `twoFactors` |
| `twoFactor` | off | See [Two-factor authentication](#two-factor-authentication) |
| `passkeys` | off | See [Passkeys](#passkeys) |
| `protect` | `[]` | See [Protecting routes](#protecting-routes) |
| `rateLimit` | 20 per 60 seconds | `enabled`, `max`, `window` (seconds) |
| `redirects` | none | `signedIn`, `signedOut`, `allowed` (extra origins callback URLs may use) |
| `linking.automatic` | `false` | Link a provider account to an existing user with the same email |

Secrets never belong in config. A provider option named `clientSecret`,
`secret`, `password`, `apiKey`, `privateKey` or `token` fails the build with a
hint to use an environment variable instead, because config values are
compiled into the server bundle.

`session.strategy: "database"`, `twoFactor.enabled` and `passkeys.enabled`
all need `adapter.dialect`, and the build says so when it is missing. The
dialect in config only declares intent and names the tables. The adapter
itself is created in code, as shown below.

## Environment variables

| Variable | Purpose |
|----------|---------|
| `AUTH_SECRET` | Required. Encrypts sessions and signs state. A list is accepted in code for rotation |
| `AUTH_URL` | Public URL of the app. Set it in production |
| `AUTH_TRUST_HOST` | `true` to trust the request host (behind a proxy you control) |
| `AUTH_<PROVIDER>_ID` | OAuth client id, for example `AUTH_GITHUB_ID` |
| `AUTH_<PROVIDER>_SECRET` | OAuth client secret |
| `AUTH_<PROVIDER>_ISSUER` | Issuer or base URL for providers that need one |
| `AUTH_RESEND_KEY` | API key for the Resend provider |
| `AUTH_DEBUG` | `true` to log debug messages |

`NEXTAUTH_SECRET` and `NEXTAUTH_URL` are read as fallbacks, and the older
`<PROVIDER>_CLIENT_ID` and `<PROVIDER>_CLIENT_SECRET` names work as well.

## Providers

Declare a built-in provider by id. Its credentials come from the environment:

```typescript
auth: {
  providers: [
    { id: "github" },
    { id: "google", options: { scope: "openid email profile" } },
    { id: "okta", name: "Company SSO" },
  ],
}
```

Built-in ids: `42-school`, `apple`, `asgardeo`, `atlassian`, `auth0`,
`authentik`, `azure-ad-b2c`, `bitbucket`, `box`, `cognito`, `coinbase`,
`descope`, `discord`, `dribbble`, `dropbox`, `duende-identityserver6`,
`facebook`, `fusionauth`, `github`, `gitlab`, `google`, `hubspot`,
`huggingface`, `kakao`, `keycloak`, `kinde`, `line`, `linkedin`, `logto`,
`mastodon`, `mattermost`, `microsoft-entra-id`, `naver`, `netlify`, `notion`,
`okta`, `onelogin`, `orcid`, `osu`, `patreon`, `pinterest`, `reddit`,
`roblox`, `salesforce`, `slack`, `spotify`, `strava`, `twitch`, `twitter`,
`webex`, `wikimedia`, `workos`, `yandex`, `zitadel`, `zoho` and `zoom`.

Providers that run on your own tenant or instance take their issuer from
`AUTH_<PROVIDER>_ISSUER` (Auth0, Okta, Keycloak, Cognito, Authentik, GitLab
self-hosted, Mastodon, Mattermost and similar). Microsoft Entra ID reads
`options.tenantId` and defaults to the multi-tenant endpoint.

The same providers are exported as functions when you want code-level control:

```typescript
import { defineAuth, github, google } from "@krate/auth";

export const auth = defineAuth({
  providers: [
    github({ allowDangerousEmailAccountLinking: false }),
    google({ clientId: process.env.GOOGLE_ID, clientSecret: process.env.GOOGLE_SECRET }),
  ],
});
```

### Any other OAuth or OpenID Connect service

```typescript
auth: {
  providers: [
    { id: "acme", type: "oidc", options: { issuer: "https://sso.acme.example" } },
    {
      id: "legacy",
      type: "oauth",
      options: {
        authorization: "https://legacy.example/oauth/authorize",
        token: "https://legacy.example/oauth/token",
        userinfo: "https://legacy.example/api/me",
        scope: "profile email",
      },
    },
  ],
}
```

Use `genericOAuth()` in code when you need a `profile` function to map the
provider's response to a user.

Authorization uses the code flow with PKCE and a state cookie, and a nonce
for OpenID Connect. The ID token's issuer, audience, expiry and nonce are
validated. Its signature is not, because the token is received directly from
the token endpoint over TLS (OpenID Connect Core, section 3.1.3.7).

### Sign in with Apple

Apple wants a client secret that is a short-lived JWT. Generate it from the
`.p8` key:

```typescript
import { apple, appleClientSecret } from "@krate/auth";

const clientSecret = appleClientSecret({
  teamId: process.env.APPLE_TEAM_ID!,
  keyId: process.env.APPLE_KEY_ID!,
  clientId: process.env.AUTH_APPLE_ID!,
  privateKey: process.env.APPLE_PRIVATE_KEY!,
});

export const auth = defineAuth({ providers: [apple({ clientSecret })] });
```

The secret is valid for up to six months, so regenerate it when the app
starts rather than storing it.

### Credentials

Checking a password is your code, so credentials providers are declared in
`defineAuth()`:

```typescript
import { defineAuth, credentials } from "@krate/auth";
import { verifyPassword } from "./lib/passwords";
import { findUserByEmail } from "../db/queries/users";

export const auth = defineAuth({
  providers: [
    credentials({
      async authorize({ email, password }) {
        const user = await findUserByEmail(email);
        if (!user || !(await verifyPassword(password, user.passwordHash))) return null;
        return { id: user.id, name: user.name, email: user.email };
      },
    }),
  ],
});
```

Return `null` for a failed sign-in. Do not say which half was wrong. Failed
attempts are rate limited per client address and per email.

### Email magic links

The email provider needs an adapter, because the one-time token is stored
hashed in the verification token table:

```typescript
import { defineAuth, resend } from "@krate/auth";

export const auth = defineAuth({
  adapter,
  providers: [resend({ from: "App <login@example.com>" })],
});
```

`resend()` reads `AUTH_RESEND_KEY`. For another service use `email({
sendVerificationRequest })` and send the `url` you are given. Links expire
after 24 hours (`maxAge`) and work once. Addresses are trimmed and lowercased.

## Sessions

With the JWT strategy the session is an encrypted token (AES-256-GCM, key
derived from `AUTH_SECRET`) in an `HttpOnly` cookie, split across cookies when
it grows past 4 KB. With the database strategy the cookie holds an opaque id
and the row lives in your database.

Read the session on the server:

```typescript
import { auth } from "../auth";

export async function GET(request: Request) {
  const session = await auth.auth(request);
  if (!session) return Response.json({ error: "unauthorized" }, { status: 401 });
  return Response.json({ id: session.user.id, email: session.user.email });
}
```

The session has the shape `{ user: { id, name, email, image, role?,
twoFactorEnabled? }, expires }`. Add your own fields with callbacks:

```typescript
export const auth = defineAuth({
  callbacks: {
    jwt({ token, user }) {
      if (user) token.plan = user.plan;
      return token;
    },
    session({ session, token }) {
      return { ...session, plan: token?.plan };
    },
  },
});
```

| Callback | Use |
|----------|-----|
| `signIn({ user, account, profile })` | Return `false` to deny, or a path to redirect to |
| `redirect({ url, baseUrl })` | Control where users land after sign-in or sign-out |
| `jwt({ token, user, account, trigger })` | Add claims. `trigger` is `"signIn"`, `"signUp"` or `"update"` |
| `session({ session, token, user })` | Shape what `auth()` and `/api/auth/session` return |
| `authorized({ request, auth })` | Decide per request in middleware |

Events (`signIn`, `signOut`, `createUser`, `updateUser`, `linkAccount`,
`session`, `twoFactorEnabled`, `twoFactorDisabled`, `passkeyRegistered`) are
passed to `events` for side effects such as audit logs.

## Database adapter

```typescript
import { createClient } from "@libsql/client";
import { drizzle } from "drizzle-orm/libsql";
import { DrizzleAdapter, createSqliteSchema } from "@krate/auth/drizzle";

export const schema = createSqliteSchema();
export const db = drizzle(createClient({ url: process.env.DATABASE_URL! }));
export const adapter = DrizzleAdapter(db, schema);
```

Pass `adapter` to `defineAuth({ adapter })`. Use `createPgSchema()` with a
`drizzle-orm/node-postgres` or PGlite client for PostgreSQL, and
`createMysqlSchema()` for MySQL. The adapter is tested against SQLite (libsql)
and PostgreSQL (PGlite). The MySQL schema and DDL are generated and checked,
but the suite has no live MySQL server, so try it against yours before relying
on it.

Export the tables from your schema file so `drizzle-kit` includes them in
migrations:

```typescript
import { createSqliteSchema } from "@krate/auth/drizzle/sqlite";

export const { users, accounts, sessions, verificationTokens, authenticators, twoFactors } =
  createSqliteSchema();
```

For quick starts and tests, `authTablesSql(dialect)` from
`@krate/auth/drizzle/ddl` returns `CREATE TABLE IF NOT EXISTS` statements.
`MemoryAdapter()` from `@krate/auth/memory` keeps everything in memory for
development.

If you rename tables with `auth.adapter.tables`, the schema factories read the
names while running under Krate. Tools that run outside Krate, such as
`drizzle-kit`, do not see your config, so pass the same names to the factory:
`createSqliteSchema({ users: "app_users" })`.

Emails are stored lowercased. A provider account is unique on `(provider,
providerAccountId)`.

### Account linking

If a user signs in with a provider whose email already belongs to another
account, sign-in fails with `OAuthAccountNotLinked`. This stops someone from
taking over an account through a provider that does not verify emails. Set
`linking.automatic` or a provider's `allowDangerousEmailAccountLinking` only
for providers that do verify them. A user who is already signed in can link
another provider by signing in with it.

## Protecting routes

List rules in config and call `auth.middleware`. Patterns match a path and
everything below it:

```typescript
auth: {
  protect: [
    { source: "/admin", roles: ["admin"], status: 404 },
    { source: "/api/private", status: 401 },
    { source: "/account", redirect: "/login" },
    { source: "/dashboard" },
  ],
}
```

| Pattern | Matches |
|---------|---------|
| `/dashboard` | `/dashboard` and everything below it |
| `/users/:id` | One segment, and below |
| `/users/*` | Exactly one segment |
| `/docs/**` or `/docs/:path*` | Any depth |
| `/files/:rest+` | At least one more segment |

The first matching rule wins. Anonymous users get a redirect to the sign-in
page with `callbackUrl` for pages, or `401` for API requests and requests
that accept JSON. `redirect` sends them to a specific page, and `status`
answers with `401`, `403` or `404` instead. `roles` compares against
`session.user.role` and answers `403` (or `404`) when it does not match. The
auth routes themselves are never blocked.

For logic that does not fit a rule, use the `authorized` callback. It runs for
every request the middleware sees:

```typescript
export const auth = defineAuth({
  callbacks: {
    authorized({ request, auth }) {
      if (new URL(request.url).pathname.startsWith("/members")) return Boolean(auth);
      return true;
    },
  },
});
```

Return `true` to allow, `false` to send the user to sign in, or a `Response`.
Middleware fails closed: if the session cannot be read it answers `503`.
With database sessions the adapter is called from middleware, so run
middleware on a runtime where your database driver works. See
[Middleware](/docs/features/middleware/).

## Two-factor authentication

Enable it in config and create the adapter with two-factor storage (the
Drizzle adapter has it):

```typescript
auth: {
  adapter: { dialect: "sqlite" },
  twoFactor: { enabled: true, issuer: "My App" },
}
```

Users enrol while signed in:

| Request | Result |
|---------|--------|
| `POST /api/auth/2fa/setup` | A new secret and an `otpauth://` URL to show as a QR code |
| `POST /api/auth/2fa/enable` with `{ code }` | Confirms the code, returns 10 backup codes once |
| `GET /api/auth/2fa/status` | `{ enabled, pending, backupCodesRemaining, required }` |
| `POST /api/auth/2fa/backup-codes` with `{ code }` | Replaces the backup codes. Needs a current app code |
| `POST /api/auth/2fa/disable` with `{ code }` | Turns it off. Accepts an app code or a backup code |

After enrolment, every sign-in method (credentials, OAuth, magic links) stops
short of creating a session and redirects to `/api/auth/2fa`, or to
`pages.twoFactor`. The user submits an app code or a backup code to
`POST /api/auth/2fa/verify`, and only then receives the session. A sign-in that
used a user-verified passkey counts as two factors and skips the challenge.

Details that matter:

- Codes are RFC 6238 TOTP (SHA-1, 6 digits, 30 seconds by default; SHA-256 and
  SHA-512 are available). The window accepts one step either side.
- A code is accepted once. The last used step is stored, so a code that was
  just used cannot be replayed.
- The shared secret is encrypted at rest with a key derived from `AUTH_SECRET`.
  Changing the secret without keeping the old one in the rotation list makes
  enrolled secrets unreadable.
- Backup codes are stored as salted hashes and each works once.
- Verification attempts are limited to 5 per window for each user.
- With `twoFactor.required`, signed-in users who have not enrolled get
  `session.user.twoFactorSetupRequired: true`. Enforce it in the `authorized`
  callback or in your UI.

## Passkeys

```typescript
auth: {
  adapter: { dialect: "sqlite" },
  passkeys: { enabled: true, rpName: "My App" },
}
```

Passkeys can create an account, sign a user in without a username
(discoverable credentials), and be added to an existing account. Defaults are
`userVerification: "preferred"`, `residentKey: "required"` and a 60 second
timeout. `rpId` defaults to the host name, and `origins` lists extra origins
allowed to run the ceremony (for example a separate front end).

| Request | Purpose |
|---------|---------|
| `POST /api/auth/passkey/register/options` | Start registration. Signed in: adds a passkey. Anonymous: creates an account (`email`, `name` optional) |
| `POST /api/auth/passkey/register/verify` | Finish registration with the browser's credential |
| `POST /api/auth/passkey/authenticate/options` | Start a sign-in |
| `POST /api/auth/passkey/authenticate/verify` | Finish a sign-in |
| `GET /api/auth/passkey/list` | The signed-in user's passkeys |
| `POST /api/auth/passkey/delete` with `{ credentialID }` | Remove one of their own passkeys |

The server verifies the challenge, origin, relying party hash, user presence,
user verification when required, and the signature. The signature counter
must increase, which detects cloned authenticators. Each challenge is stored
once and consumed on use, so a captured request cannot be replayed.

Known limits: attestation statements are not verified (the server records the
format and trusts the credential, which is how sign-in passkeys are normally
used), and an anonymous registration for an email that already has an account
answers `409` so the person signs in and adds the passkey from their account.

## Browser client

```typescript
import { signIn, signOut, getSession, twoFactor, registerPasskey, signInWithPasskey } from "@krate/auth/client";

await signIn("github", { callbackUrl: "/dashboard" });
await signIn("credentials", { email, password, redirect: false });
await signInWithPasskey();
await registerPasskey({ passkeyName: "Laptop" });

const challenge = await signIn("credentials", { email, password });
if (challenge.twoFactorRequired) await twoFactor.verify(code);

await signOut();
```

`signIn` returns `{ ok, error, url, twoFactorRequired }` and navigates to the
result unless you pass `redirect: false`. The client also exposes
`getSession`, `getCsrfToken`, `getProviders`, `updateSession`,
`twoFactor.{status,setup,enable,disable,regenerateBackupCodes,verify}`,
`listPasskeys`, `deletePasskey` and `isPasskeySupported`. Call
`configureAuthClient({ basePath })` if you changed the mount path.

## Endpoints

| Method and path | Purpose |
|-----------------|---------|
| `GET /providers` | Configured providers |
| `GET /csrf` | CSRF token, also sets its cookie |
| `GET /session`, `POST /session` | Read the session, or update it with `{ data }` |
| `GET /signin`, `POST /signin/:provider` | Sign-in page, or start an OAuth or email sign-in |
| `GET /callback/:provider` | OAuth and magic link return |
| `POST /callback/credentials` | Credentials sign-in |
| `GET /signout`, `POST /signout` | Confirmation page, or sign out |
| `GET /error`, `GET /verify-request` | Built-in status pages |
| `/2fa/*`, `/passkey/*` | See above |

All paths are relative to `basePath`. Requests that send a JSON body, an
`Accept: application/json` header or `X-Auth-Return-Redirect: 1` get JSON
(`{ url }` instead of a redirect, `{ error }` instead of an error page).

## Security notes

- Every state-changing request needs the CSRF token (cookie plus matching
  field or `X-CSRF-Token` header) and, when an `Origin` header is present, a
  same-origin value.
- Callback URLs must be relative or on the app's own origin (or listed in
  `redirects.allowed`). Anything else falls back to the home page.
- Sign-in, verification and ceremony endpoints are rate limited. The counters
  live in each worker's memory, so run behind a proxy rate limit as well for a
  multi-instance deployment.
- In production, set `AUTH_URL` or `trustHost`. Without one the handler
  refuses to guess its own origin.
- Cookies are `HttpOnly`, `SameSite=Lax`, and use the `__Secure-` and
  `__Host-` prefixes on https.
- Session and state tokens are encrypted, bound to the cookie they belong to,
  and rejected after `maxAge`. List more than one secret in code to rotate.

## Coming from NextAuth

| NextAuth | Krate |
|----------|-------|
| `NextAuth({ ... })` in `auth.ts` | `defineAuth({ ... })`, with data options in `krate.config.ts` |
| `export const { handlers, auth, signIn, signOut } = NextAuth(...)` | `auth.handlers`, `auth.auth(request)`, `auth.middleware`. Sign-in and sign-out run from `@krate/auth/client` |
| `app/api/auth/[...nextauth]/route.ts` | `src/api/auth/[...all].ts` |
| `middleware.ts` with `export { auth as middleware }` | `export function middleware(request) { return auth.middleware(request); }` |
| `@auth/drizzle-adapter` | `DrizzleAdapter(db, createSqliteSchema())` |
| `AUTH_SECRET`, `AUTH_GITHUB_ID` | The same names |
| `callbacks`, `events`, `pages`, `session`, `cookies` | The same shapes |

Differences to know: the session token is an encrypted JWT like NextAuth's,
but not byte-compatible, so existing sessions end when you switch. The
database tables are named like the Auth.js ones with `authenticators`
for passkeys and `twoFactors` added, and `users` gains `role` and
`twoFactorEnabled`.
