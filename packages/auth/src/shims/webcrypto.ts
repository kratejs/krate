export const crypto: Crypto | undefined = typeof globalThis.crypto === "object" ? globalThis.crypto : undefined;
