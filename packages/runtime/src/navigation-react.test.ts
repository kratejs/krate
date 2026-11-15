import { describe, expect, it } from 'vitest';
import { createValueNavigationHooks } from './navigation-react.js';

describe('value-returning router hooks', () => {
  it('return strings and objects, and notify subscribers on navigation', () => {
    let subscribed: (() => void) | null = null;
    const hooks = createValueNavigationHooks((subscribe, getSnapshot) => {
      subscribed = subscribe(() => {}) as unknown as () => void;
      return getSnapshot();
    });
    history.pushState({}, '', '/store/x?q=abc');
    expect(hooks.usePathname()).toBe('/store/x');
    const sp = hooks.useSearchParams();
    expect(sp.get('q')).toBe('abc');
    expect(hooks.useSearchParams()).toBe(sp);
    expect(typeof hooks.useRouter().push).toBe('function');
    expect(hooks.useSelectedLayoutSegments()).toEqual(['store', 'x']);
    expect(hooks.useSelectedLayoutSegment()).toBe('store');
    history.pushState({}, '', '/templates');
    expect(hooks.usePathname()).toBe('/templates');
    expect(typeof subscribed).toBe('function');
  });
});
