import { describe, expect, it } from 'vitest';
import { readFileSync } from 'node:fs';
import { join } from 'node:path';
import { projectNames, toDisplayName, toValidPackageName } from '../src/names.js';

const fallbacks = { pkg: 'krate-app', display: 'Krate App' };

describe('projectNames', () => {
  it('names a project after the directory it creates', () => {
    expect(projectNames('/work/My Site', fallbacks)).toEqual({ pkg: 'my-site', display: 'My Site' });
    expect(projectNames('/work/blog_engine', fallbacks)).toEqual({ pkg: 'blog_engine', display: 'Blog Engine' });
  });

  it('does not name a project "." when scaffolding into the current directory', () => {
    // `create-krate-app .` resolves to the current directory; the old code named
    // it after the text typed, "." (display name ".", package "krate-app").
    expect(projectNames('/work/acme-docs', fallbacks)).toEqual({ pkg: 'acme-docs', display: 'Acme Docs' });
  });

  it('falls back when the directory name leaves nothing usable', () => {
    expect(projectNames('/work/123', fallbacks)).toEqual({ pkg: 'krate-app', display: '123' });
    expect(projectNames('/', fallbacks).pkg).toBe('krate-app');
  });
});

describe('toValidPackageName', () => {
  it('lowercases, joins words and drops characters npm rejects', () => {
    expect(toValidPackageName(' Hello World! ', 'x')).toBe('hello-world');
    expect(toValidPackageName('9lives', 'x')).toBe('lives');
    expect(toValidPackageName('???', 'x')).toBe('x');
  });
});

describe('toDisplayName', () => {
  it('title-cases the words', () => {
    expect(toDisplayName('my-cool_app', 'x')).toBe('My Cool App');
    expect(toDisplayName('', 'Fallback')).toBe('Fallback');
  });
});

describe('scaffolder copies', () => {
  it('keeps names.ts and prompt.ts identical in both scaffolders', () => {
    const base = join(__dirname, '..', '..');
    for (const file of ['names.ts', 'prompt.ts']) {
      expect(readFileSync(join(base, 'create-krate-docs', 'src', file), 'utf-8')).toBe(
        readFileSync(join(base, 'create-krate-app', 'src', file), 'utf-8'),
      );
    }
  });
});
