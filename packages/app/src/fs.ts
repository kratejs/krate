// Scoped file access. A path may be absolute or relative to the app data
// directory; it must lie inside the app directories or a path the user picked
// in a dialog, otherwise the call rejects with code "permission_denied" and
// data {permission: "fs", reason: "outside_scope", path}.
// Host ids krate/fs.*. Reads need fs:read, writes need fs.
import { call } from "./index.js";

export interface DirEntry {
  name: string;
  isDir: boolean;
  size: number;
  modTime: Date;
}

export interface FileInfo {
  name: string;
  size: number;
  isDir: boolean;
  modTime: Date;
  // Permission bits, for example 0o644.
  mode: number;
}

export function readFile(path: string): Promise<Uint8Array> {
  return call("krate/fs.readFile", [path]);
}

export function readText(path: string): Promise<string> {
  return call("krate/fs.readText", [path]);
}

export function readDir(path: string): Promise<DirEntry[]> {
  return call("krate/fs.readDir", [path]);
}

export function stat(path: string): Promise<FileInfo> {
  return call("krate/fs.stat", [path]);
}

export function exists(path: string): Promise<boolean> {
  return call("krate/fs.exists", [path]);
}

export function writeFile(path: string, data: Uint8Array): Promise<void> {
  return call("krate/fs.writeFile", [path, data]);
}

export function writeText(path: string, text: string): Promise<void> {
  return call("krate/fs.writeText", [path, text]);
}

// Creates the directory and any missing parents.
export function mkdir(path: string): Promise<void> {
  return call("krate/fs.mkdir", [path]);
}

// A directory needs recursive: true unless it is empty.
export function remove(path: string, opts: { recursive?: boolean } = {}): Promise<void> {
  return call("krate/fs.remove", opts.recursive ? [path, true] : [path]);
}

export function rename(from: string, to: string): Promise<void> {
  return call("krate/fs.rename", [from, to]);
}
