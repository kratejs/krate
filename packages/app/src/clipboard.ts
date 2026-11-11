// System clipboard. Writing needs the "clipboard" permission; reading needs
// "clipboard:read".
import { call } from "./index.js";

export function readText(): Promise<string> {
  return call("krate/clipboard.readText");
}

export function writeText(text: string): Promise<void> {
  return call("krate/clipboard.writeText", [text]);
}

// readImage resolves to PNG bytes, or null when the clipboard has no image.
export function readImage(): Promise<Uint8Array | null> {
  return call("krate/clipboard.readImage");
}

export function writeImage(png: Uint8Array): Promise<void> {
  return call("krate/clipboard.writeImage", [png]);
}

export function readFiles(): Promise<string[]> {
  return call("krate/clipboard.readFiles");
}

export function writeFiles(paths: string[]): Promise<void> {
  return call("krate/clipboard.writeFiles", [paths]);
}

export function clear(): Promise<void> {
  return call("krate/clipboard.clear");
}
