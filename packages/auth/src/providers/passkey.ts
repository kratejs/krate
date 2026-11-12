import type { PasskeyProviderConfig } from "../types";

export function passkey(options: { name?: string } = {}): PasskeyProviderConfig {
  return { id: "passkey", name: options.name ?? "Passkey", type: "passkey" };
}
