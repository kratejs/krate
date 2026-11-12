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

// Several representations of one clipboard value. write() puts every field
// that is set on the clipboard together, so a paste target takes the richest
// one it understands (like Electron's clipboard.write({ text, html })).
export interface ClipboardData {
  text?: string;
  html?: string;
  rtf?: string;
}

export function write(data: ClipboardData): Promise<void> {
  return call("krate/clipboard.write", [data]);
}

// readHtml resolves to the HTML fragment on the clipboard, or "".
export function readHtml(): Promise<string> {
  return call("krate/clipboard.readHtml");
}

// readRtf resolves to the RTF document on the clipboard, or "".
export function readRtf(): Promise<string> {
  return call("krate/clipboard.readRtf");
}

// formats lists what is on the clipboard: MIME types where the platform has
// one ("text/plain", "text/html", "text/rtf", "image/png"), else native names.
export function formats(): Promise<string[]> {
  return call("krate/clipboard.formats");
}

// readFormat resolves to the bytes of one format, or null when absent. A MIME
// name is mapped to the platform's own (a UTI on macOS, a registered format
// on Windows); text/html is the fragment, not the Windows CF_HTML envelope.
export function readFormat(name: string): Promise<Uint8Array | null> {
  return call("krate/clipboard.readFormat", [name]);
}

// writeFormat replaces the clipboard with a single format.
export function writeFormat(name: string, data: Uint8Array): Promise<void> {
  return call("krate/clipboard.writeFormat", [name, data]);
}
