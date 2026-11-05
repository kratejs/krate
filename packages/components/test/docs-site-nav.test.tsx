import { describe, it, expect } from 'vitest';
import { activeNavLink, normalizeNavPath } from '../../base-docs-theme/src/nav';
import { PLATFORM_PREPAINT } from '../../base-docs-theme/src/platform';
import { hotkeyKeys, hotkeyLabel } from '../../base-docs-theme/src/search';

const nav = [
  { href: '/docs/' },
  { href: '/docs/features/plugins/' },
  { href: '/blog/' },
  { href: 'https://github.com/kratejs/krate', external: true },
  { href: '/about', match: 'exact' as const },
  { href: '/' },
];

describe('site nav active link', () => {
  it('marks the page itself as an exact match', () => {
    expect(activeNavLink(nav, '/blog/')).toEqual({ index: 2, exact: true });
    expect(activeNavLink(nav, '/blog')).toEqual({ index: 2, exact: true });
    expect(activeNavLink(nav, '/docs/features/plugins/')).toEqual({ index: 1, exact: true });
  });

  it('marks the section a deeper page is in, preferring the most specific link', () => {
    expect(activeNavLink(nav, '/docs/getting-started/')).toEqual({ index: 0, exact: false });
    expect(activeNavLink(nav, '/docs/features/plugins/hooks/')).toEqual({ index: 1, exact: false });
    expect(activeNavLink(nav, '/blog/why-krate/')).toEqual({ index: 2, exact: false });
  });

  it('does not match on a partial segment', () => {
    expect(activeNavLink(nav, '/blogger/')).toBeNull();
    expect(activeNavLink(nav, '/documents/')).toBeNull();
  });

  it('marks "/" only on the home page and honours match: exact', () => {
    expect(activeNavLink(nav, '/')).toEqual({ index: 5, exact: true });
    expect(activeNavLink(nav, '/roadmap/')).toBeNull();
    expect(activeNavLink(nav, '/about/team')).toBeNull();
  });

  it('ignores external links and strips the base path, query and hash', () => {
    expect(activeNavLink(nav, '/krate/blog/?x=1#top', '/krate')).toEqual({ index: 2, exact: true });
    expect(normalizeNavPath('/krate', '/krate')).toBe('/');
    expect(normalizeNavPath('/a//', '')).toBe('/a');
  });
});

describe('shortcut labels by platform', () => {
  it('names the modifier for the platform instead of relying on the command glyph', () => {
    expect(hotkeyLabel('mod+k', 'other')).toBe('Ctrl K');
    expect(hotkeyLabel('mod+k', 'mac')).toBe('⌘ K');
    expect(hotkeyKeys('alt+p', 'other')).toEqual(['Alt', 'P']);
    expect(hotkeyLabel('/', 'other')).toBe('/');
    expect(hotkeyLabel('/', 'mac')).toBe('/');
  });

  it('detects Apple platforms in the pre-paint script', () => {
    const run = (platform: string) => {
      const attrs: Record<string, string> = {};
      const fn = new Function('navigator', 'document', PLATFORM_PREPAINT);
      fn({ platform }, { documentElement: { setAttribute: (k: string, v: string) => (attrs[k] = v) } });
      return attrs['data-platform'];
    };
    expect(run('MacIntel')).toBe('mac');
    expect(run('iPhone')).toBe('mac');
    expect(run('Linux x86_64')).toBe('other');
    expect(run('Win32')).toBe('other');
  });
});
