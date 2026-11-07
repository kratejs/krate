import { describe, expect, it } from 'vitest';
import { PassThrough } from 'node:stream';
import { ask } from '../src/prompt.js';

function streams() {
  const input = new PassThrough();
  const output = new PassThrough();
  return { input, output };
}

describe('ask', () => {
  it('returns the trimmed answer', async () => {
    const { input, output } = streams();
    const answer = ask('Name? ', input, output);
    input.write('  my-site  \n');
    expect(await answer).toBe('my-site');
  });

  it('returns an empty string when the input ends without an answer', async () => {
    // `create-krate-app < /dev/null`: the question never settled, and the
    // process exited 0 with nothing created.
    const { input, output } = streams();
    const answer = ask('Name? ', input, output);
    input.end();
    expect(await answer).toBe('');
  });

  it('reads an answer that was piped in before the input closed', async () => {
    const { input, output } = streams();
    input.end('piped-name\n');
    expect(await ask('Name? ', input, output)).toBe('piped-name');
  });
});
