import { createInterface } from 'node:readline/promises';
import type { Readable, Writable } from 'node:stream';

/**
 * Ask a question on the terminal. Resolves to '' when the input ends without an
 * answer (stdin closed, `< /dev/null`, a CI job with no input). A bare
 * `rl.question` never settles in that case, so the process used to exit 0
 * having created nothing.
 */
export async function ask(question: string, input: Readable, output: Writable): Promise<string> {
  const rl = createInterface({ input, output });
  try {
    const ended = new Promise<string>((resolve) => rl.once('close', () => resolve('')));
    const answer = await Promise.race([rl.question(question), ended]);
    return answer.trim();
  } finally {
    rl.close();
  }
}
