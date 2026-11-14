import type { CredentialField, CredentialsConfig, User } from "../types.js";

export interface CredentialsOptions {
  id?: string;
  name?: string;
  credentials?: Record<string, CredentialField>;
  authorize: (credentials: Record<string, string>, request: Request) => Promise<User | null | undefined | false> | User | null | undefined | false;
}

export function credentials(options: CredentialsOptions): CredentialsConfig {
  return {
    id: options.id ?? "credentials",
    name: options.name ?? "Credentials",
    type: "credentials",
    credentials: options.credentials ?? {
      email: { label: "Email", type: "email" },
      password: { label: "Password", type: "password" },
    },
    authorize: options.authorize,
  };
}
