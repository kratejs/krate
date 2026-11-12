import type { PasskeyProviderConfig } from "../types.js";

export function passkey(options: { name?: string } = {}): PasskeyProviderConfig {
  return { id: "passkey", name: options.name ?? "Passkey", type: "passkey" };
}
