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
    expect(media.slice(0, media.indexOf('\n}\n\n'))).toContain('--krate-code-bg-base: #171717');
  });

  it('the dark code surface matches the dark card surface', () => {
    for (const scope of ['@media (prefers-color-scheme: dark)', ":where(:root[data-theme='dark']) {"]) {
      const start = tokens.indexOf(scope);
      const block = tokens.slice(start, tokens.indexOf('\n}', start));
      const card = block.match(/--krate-card-bg:\s*(#[0-9a-f]+)/i)?.[1];
      const code = block.match(/--krate-code-bg-base:\s*(#[0-9a-f]+)/i)?.[1];
      expect(card, scope).toBeDefined();
      expect(code, scope).toBe(card);
    }
  });

  it('the neutral ramp has no blue cast', () => {
    for (const [, hex] of tokens.matchAll(/--krate-(?:bg|bg-subtle|surface|surface-hover|border|border-strong|card-bg|fg|muted|code-bg-base|code-text-base):\s*(#[0-9a-f]{6})\b/gi)) {
      const [r, g, b] = [1, 3, 5].map((i) => parseInt(hex.slice(i, i + 2), 16));
      expect(Math.max(r, g, b) - Math.min(r, g, b), hex).toBe(0);
    }
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
