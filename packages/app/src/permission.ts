// Ask the host which permissions this app has. Host id krate/permission.query,
// always allowed.
import { call, isKrateError } from "./index.js";

export type Permission =
  | "os:info"
  | "kv"
  | "db"
  | "fs"
  | "fs:read"
  | "os:secrets"
  | "os:secrets.read"
  | "shell:openExternal"
  | "net"
  | (string & {});

// True when app.json grants the permission (or a permission that implies it).
export function query(name: Permission): Promise<boolean> {
  return call("krate/permission.query", [name], { dedupe: true });
}

// True when e is the error a call fails with when its permission is missing.
export function isPermissionDenied(e: unknown): boolean {
  return isKrateError(e) && e.code === "permission_denied";
}

// The permission a denied call lacked, or undefined for any other error.
export function deniedPermission(e: unknown): string | undefined {
  if (!isPermissionDenied(e)) return undefined;
  const d = (e as { data?: unknown }).data as { permission?: unknown } | null;
  return typeof d?.permission === "string" ? d.permission : undefined;
}
