import { describe, it, expect } from 'vitest';
import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';
import * as react from './react';

// Every hook the React compatibility guide lists must be importable from the
// compat module, which build-time evaluation (metadata) bundles in place of React.
describe('react compat exports', () => {
  const guide = readFileSync(resolve(process.cwd(), '../web/src/content/docs/guides/react-compatibility.md'), 'utf8');
  const table = guide.slice(guide.indexOf('### Hooks'), guide.indexOf('### Components and helpers'));
  const hooks = [...new Set([...table.matchAll(/`(use[A-Z][A-Za-z]*)`/g)].map((m) => m[1]))];

  it('finds the hooks table', () => {
    expect(hooks).toContain('useId');
    expect(hooks.length).toBeGreaterThan(10);
  });

  it.each(hooks)('exports %s', (name) => {
    expect(typeof (react as Record<string, unknown>)[name]).toBe('function');
  });

  it('useId returns distinct ids', () => {
    expect(react.useId()).not.toBe(react.useId());
  });
});
