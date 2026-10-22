import { describe, it, expect } from 'vitest';
import { createContext } from './context';
import { h } from './dom';

// Runtime providers push their value while their children are read; compiled
// source components resolve context at build time instead. The tests pass
// lazy children (getters) so the children render under the provider.
describe('createContext (runtime)', () => {
  it('reads the default without a provider', () => {
    const Theme = createContext({ color: 'black' });
    expect(Theme.useContext()).toEqual({ color: 'black' });
    expect(Theme.use()).toEqual({ color: 'black' });
  });

  it('nested providers shadow outer ones and restore on exit', () => {
    const Depth = createContext(0);
    const seen: number[] = [];
    const read = () => {
      seen.push(Depth.useContext());
      return document.createTextNode(String(Depth.useContext()));
    };
    const outer = Depth.Provider({
      value: 1,
      get children() {
        const first = read();
        const inner = Depth.Provider({
          value: 2,
          get children() {
            return [read()];
          },
        } as { value: number; children: Node[] });
        return [first, inner, read()];
      },
    } as { value: number; children: Node[] });
    expect(seen).toEqual([1, 2, 1]);
    expect(outer.textContent).toBe('121');
    expect(Depth.useContext()).toBe(0);
  });

  it('works as a React 19 provider and with Consumer', () => {
    const Density = createContext('comfortable');
    let inside = '';
    const node = Density({
      value: 'compact',
      get children() {
        inside = Density.useContext();
        return [Density.Consumer({ children: (d: string) => h('span', null, d) })];
      },
    } as { value: string; children: Node[] });
    expect(inside).toBe('compact');
    expect((node as Element).querySelector('span')!.textContent).toBe('compact');
    expect(Density.useContext()).toBe('comfortable');
  });

  it('Consumer renders text, arrays and nothing', () => {
    const Name = createContext('ada');
    expect(Name.Consumer({ children: (n: string) => 'hi ' + n }).textContent).toBe('hi ada');
    expect(Name.Consumer({ children: [(n: string) => [n, '!']] }).textContent).toBe('ada!');
    expect(Name.Consumer({ children: () => null }).textContent).toBe('');
  });
});
