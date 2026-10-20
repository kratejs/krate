import { describe, expect, it } from 'vitest';
import {
  Children,
  Fragment,
  cloneElement,
  createContext,
  createElement,
  createRef,
  isValidElement,
  useContext,
  useOptimistic,
  useFormStatus,
} from './react.js';

describe('react compat runtime', () => {
  it('createElement builds DOM elements with props and children', () => {
    const el = createElement('div', { class: 'x', id: 'a' }, 'hi') as Element;
    expect(el.tagName).toBe('DIV');
    expect(el.getAttribute('class')).toBe('x');
    expect(el.getAttribute('id')).toBe('a');
    expect(el.textContent).toBe('hi');
  });

  it('createElement(Fragment) flattens children', () => {
    const frag = createElement(
      Fragment,
      null,
      createElement('a', null),
      createElement('b', null)
    ) as DocumentFragment;
    expect(frag.childNodes.length).toBe(2);
  });

  it('Children helpers flatten and map', () => {
    expect(Children.map([1, 2, 3], (n) => (n as number) * 2)).toEqual([2, 4, 6]);
    expect(Children.count([1, [2, 3]])).toBe(3);
    expect(Children.toArray([1, [2, 3]])).toEqual([1, 2, 3]);
    expect(() => Children.only([1, 2])).toThrow();
  });

  it('cloneElement applies props and replaces children', () => {
    const el = createElement('span', { id: 'a' }, 'old') as Element;
    const cloned = cloneElement(el, { id: 'b' }, 'new') as Element;
    expect(cloned.getAttribute('id')).toBe('b');
    expect(cloned.textContent).toBe('new');
  });

  it('isValidElement distinguishes elements', () => {
    expect(isValidElement(createElement('div', null))).toBe(true);
    expect(isValidElement(null)).toBe(false);
  });

  it('createRef starts empty', () => {
    expect(createRef<number>().current).toBeNull();
  });

  it('createContext falls back to its default', () => {
    const Ctx = createContext('fallback');
    expect(useContext(Ctx)).toBe('fallback');
    expect(Ctx.defaultValue).toBe('fallback');
  });
});

describe('react 19 compat shims', () => {
  it('useOptimistic updates its getter', () => {
    const [state, add] = useOptimistic(1);
    expect(state()).toBe(1);
    add(2);
    expect(state()).toBe(2);
  });

  it('useFormStatus exposes an idle status', () => {
    expect(useFormStatus()).toEqual({ pending: false, data: null, method: null, action: null });
  });
});