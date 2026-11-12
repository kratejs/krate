import type { DeclarativeProvider, Provider } from "../types";
import { catalog } from "./catalog";
import { genericOAuth, type OAuthUserOptions } from "./oauth";
import { email } from "./email";
import { passkey } from "./passkey";

export function resolveDeclarativeProvider(decl: DeclarativeProvider): Provider | null {
  const options = decl.options ?? {};
  const named = (p: Provider): Provider => (decl.name ? ({ ...p, name: decl.name } as Provider) : p);
  if (decl.type === "oauth" || decl.type === "oidc") {
    return named(genericOAuth({ ...(options as OAuthUserOptions), id: decl.id, name: decl.name, type: decl.type }));
  }
  if (decl.type === "passkey" || decl.id === "passkey") return named(passkey());
  if (decl.type === "email") return named(email({ id: decl.id, ...options }));
  if (decl.type === "credentials" || decl.id === "credentials") return null;
  const factory = catalog[decl.id];
  return factory ? named(factory({ ...options, name: decl.name })) : null;
}
