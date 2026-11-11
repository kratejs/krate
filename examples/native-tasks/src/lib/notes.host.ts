import { hostSignal } from '@krate/app/host';

// Host code runs in the native process, not in the webview.
export const noteCount = hostSignal(0);

const notes: string[] = [];

export async function addNote(text: string): Promise<number> {
  notes.push(text);
  noteCount.set(notes.length);
  return notes.length;
}

export async function greet(name: string): Promise<string> {
  return `Hello, ${name}, from the host.`;
}
