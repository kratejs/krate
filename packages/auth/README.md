# @krate/auth

Authentication for [Krate](https://krate.js.org): OAuth and OpenID Connect
providers, credentials, email magic links, Drizzle adapters, TOTP two-factor
authentication and passkeys.

[![License](https://img.shields.io/badge/license-Apache--2.0-blue.svg)](../../LICENSE)

```sh
npm install @krate/auth
```

Declare providers and options in `krate.config.ts`:

```typescript
export default defineConfig({
  auth: {
    providers: [{ id: "github" }, { id: "google" }],
    protect: [{ source: "/dashboard" }],
  },
});
```

Create the instance and mount it as an API route:

```typescript
import { defineAuth } from "@krate/auth";

export const auth = defineAuth({});
```

```typescript
import { auth } from "../../auth";

export const GET = auth.handlers.GET;
export const POST = auth.handlers.POST;
```

Read the full guide at [krate.js.org/docs/features/authentication](https://krate.js.org/docs/features/authentication/).
