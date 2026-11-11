// Native file and message dialogs. Needs the "dialog" permission.
import { call } from "./index.js";
import { clean } from "./events.js";

export interface Filter {
  name: string;
  // Glob patterns such as "*.png".
  patterns: string[];
}

export interface OpenOptions {
  title?: string;
  // Initial folder.
  dir?: string;
  filters?: Filter[];
  multiple?: boolean;
  showHidden?: boolean;
  // SDK id of the parent window (default: the main window).
  window?: string;
}

export interface SaveOptions {
  title?: string;
  dir?: string;
  // Suggested file name.
  name?: string;
  filters?: Filter[];
  // Ask before replacing an existing file.
  overwrite?: boolean;
  window?: string;
}

export type MessageKind = "info" | "warning" | "error" | "question";

export interface MessageOptions {
  kind?: MessageKind;
  title?: string;
  message: string;
  detail?: string;
  // Button labels, left to right (default: one "OK").
  buttons?: string[];
  // Index of the default button.
  default?: number;
  window?: string;
}

const KINDS: Record<MessageKind, number> = { info: 0, warning: 1, error: 2, question: 3 };

// openFile resolves to the chosen paths, or [] when the user cancels.
export function openFile(o: OpenOptions = {}): Promise<string[]> {
  return call("krate/dialog.OpenFile", [clean({ ...o })]);
}

export function openFolder(o: OpenOptions = {}): Promise<string[]> {
  return call("krate/dialog.OpenFolder", [clean({ ...o })]);
}

// saveFile resolves to the chosen path, or null when the user cancels.
export function saveFile(o: SaveOptions = {}): Promise<string | null> {
  return call("krate/dialog.SaveFile", [clean({ ...o })]);
}

// message resolves to the index of the pressed button, or -1.
export function message(o: MessageOptions): Promise<number> {
  const { kind, ...rest } = o;
  return call("krate/dialog.Message", [clean({ ...rest, kind: kind ? KINDS[kind] : undefined })]);
}

export async function confirm(title: string, text: string): Promise<boolean> {
  return (await message({ kind: "question", title, message: text, buttons: ["No", "Yes"], default: 1 })) === 1;
}
