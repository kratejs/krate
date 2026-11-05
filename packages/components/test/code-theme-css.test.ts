import { readFileSync } from 'node:fs';
import { join } from 'node:path';
import { describe, expect, it } from 'vitest';

const SRC = join(__dirname, '..', 'src');
const tokens = readFileSync(join(SRC, 'tokens.css'), 'utf8');
const code = readFileSync(join(SRC, 'code', 'code.css'), 'utf8');

describe('code surface follows the colour mode', () => {
  it('tokens.css sets light and dark base colours in every mode scope', () => {
    for (const scope of [
      ':where(:root) {',
      ":where(:root[data-theme='dark']) {",
      ":where(:root[data-theme='light']) {",
    ]) {
      const start = tokens.indexOf(scope);
      expect(start, scope).toBeGreaterThan(-1);
      const block = tokens.slice(start, tokens.indexOf('\n}', start));
      expect(block, scope).toContain('--krate-code-bg-base');
      expect(block, scope).toContain('--krate-code-text-base');
    }
    const media = tokens.slice(tokens.indexOf('@media (prefers-color-scheme: dark)'));
    expect(media.slice(0, media.indexOf('\n}\n\n'))).toContain('--krate-code-bg-base: #0d1117');
  });

  it('does not hard-code a dark surface for the public tokens', () => {
    expect(tokens).not.toMatch(/--krate-code-bg:\s*#/);
    expect(tokens).not.toMatch(/--krate-code-text:\s*#/);
  });

  it('code.css has no white-on-dark assumptions', () => {
    expect(code).not.toMatch(/rgba\(\s*255,\s*255,\s*255/);
    expect(code).not.toMatch(/#fafafa|#e6edf3|#0d1117|#161b22/i);
    expect(code).toContain('var(--krate-code-bg, var(--chroma-bg');
    expect(code).toContain('var(--krate-code-text, var(--chroma-fg');
  });
});
