import { hostSignal } from '@krate/app/host';
import * as kv from '@krate/app/kv';

// Host code runs in the native process, not in the webview.
export const noteCount = hostSignal(0);

const notes: string[] = [];

export async function addNote(text: string): Promise<number> {
  notes.push(text);
  noteCount.set(notes.length);
  // The same import the UI uses; here it runs in-process, with no HTTP.
  await kv.set('last-note', text);
  return notes.length;
}

export async function lastNote(): Promise<string> {
  return (await kv.get<string>('last-note')) ?? '';
}

export async function greet(name: string): Promise<string> {
  return `Hello, ${name}, from the host.`;
}
