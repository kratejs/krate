import { describe, it, expect } from 'vitest';
import { resolveSearchHotkey } from '../../base-docs-theme/src/search';

describe('docs search hotkey', () => {
  it('defaults to mod+k when nothing is configured', () => {
    expect(resolveSearchHotkey(undefined)).toBe('mod+k');
  });

  it('keeps a configured shortcut', () => {
    expect(resolveSearchHotkey('/')).toBe('/');
    expect(resolveSearchHotkey('ctrl+p')).toBe('ctrl+p');
  });

  it('turns the shortcut off with off/none/empty', () => {
    expect(resolveSearchHotkey('off')).toBeUndefined();
    expect(resolveSearchHotkey('None')).toBeUndefined();
    expect(resolveSearchHotkey('')).toBeUndefined();
  });
});
