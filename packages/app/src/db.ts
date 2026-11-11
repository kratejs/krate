// Preliminary: backed by the Go SDK later.
import { call } from "./index.js";

export function query<T = Record<string, unknown>>(sql: string, ...params: unknown[]): Promise<T[]> {
  return call("krate/db.Query", [sql, ...params]);
}

export function exec(sql: string, ...params: unknown[]): Promise<{ rowsAffected: number; lastInsertId?: number }> {
  return call("krate/db.Exec", [sql, ...params]);
}
