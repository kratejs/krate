function read(name: string): string | undefined {
  try {
    const value = typeof process !== "undefined" ? process.env?.[name] : undefined;
    return value && value.length > 0 ? value : undefined;
  } catch {
    return undefined;
  }
}

export function readEnv(...names: string[]): string | undefined {
  for (const n of names) {
    const v = read(n);
    if (v !== undefined) return v;
  }
  return undefined;
}

export function isProduction(): boolean {
  return read("NODE_ENV") === "production";
}
