import { describe, expect, it } from 'vitest';
import { escapeDisplayName, escapeJsxText, escapeStringLiteral } from '../src/escape.js';

describe('display name escaping', () => {
  it('neutralises JSX expression and tag syntax in text', () => {
    expect(escapeJsxText('my{x}')).toBe('my&#123;x&#125;');
    expect(escapeJsxText('a<b>&c')).toBe('a&lt;b&gt;&amp;c');
  });

  it('escapes quotes, backslashes and newlines for string literals', () => {
    expect(escapeStringLiteral('say "hi"\\\n')).toBe('say \\"hi\\"\\\\\\n');
  });

  it('chooses the escaping from the file extension', () => {
    expect(escapeDisplayName('.tsx', 'My{x}')).toBe('My&#123;x&#125;');
    expect(escapeDisplayName('.ts', 'My"x')).toBe('My\\"x');
    expect(escapeDisplayName('.json', 'My"x')).toBe('My\\"x');
    expect(escapeDisplayName('.md', 'My{x}')).toBe('My{x}');
  });
});

describe('scaffolder copies', () => {
  it('keeps the docs scaffolder escape helper identical', async () => {
    const { readFileSync } = await import('node:fs');
    const { join } = await import('node:path');
    const a = readFileSync(join(__dirname, '..', 'src', 'escape.ts'), 'utf-8');
    const b = readFileSync(join(__dirname, '..', '..', 'create-krate-docs', 'src', 'escape.ts'), 'utf-8');
    expect(b).toBe(a);
  });
});
