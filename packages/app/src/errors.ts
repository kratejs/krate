export class KrateError extends Error {
  readonly code: string;
  readonly data: unknown;

  constructor(code: string, message: string, data: unknown = null) {
    super(message);
    this.name = "KrateError";
    this.code = code;
    this.data = data;
  }
}

export function isKrateError(e: unknown): e is KrateError {
  return e instanceof KrateError;
}
