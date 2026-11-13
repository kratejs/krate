// SQLite database app.db in the app data directory (WAL mode). Host ids
// krate/db.*; permission db.
//
// Parameters bind to ? placeholders. Values are null, number, string,
// Uint8Array (blob), boolean (stored as 0 or 1) and Date (stored as an ISO
// string). Rows hold null, number, string, Uint8Array and Date for columns
// the driver parses as dates. Integers beyond 2^53 lose precision in
// JavaScript; cast them to TEXT in SQL if the exact value matters.
//
// ATTACH, DETACH and VACUUM are rejected from the UI (code "forbidden_sql").
// Errors carry code "sql_error", "constraint" or "busy".
import { call } from "./index.js";

export type DbValue = null | number | string | boolean | Uint8Array | Date;
export type Row = Record<string, unknown>;

export interface ExecResult {
  rowsAffected: number;
  lastInsertId: number;
}

export interface Statement {
  sql: string;
  params?: DbValue[];
}

export function query<T = Row>(sql: string, ...params: DbValue[]): Promise<T[]> {
  return call("krate/db.query", params.length ? [sql, params] : [sql]);
}

export function exec(sql: string, ...params: DbValue[]): Promise<ExecResult> {
  return call("krate/db.exec", params.length ? [sql, params] : [sql]);
}

// Runs the statements in one transaction: all or nothing. On failure the
// error message names the failing statement ("statement 1: ...").
export function transaction(statements: Statement[]): Promise<ExecResult[]> {
  return call("krate/db.transaction", [statements]);
}
