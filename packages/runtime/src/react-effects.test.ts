import { describe, it, expect, beforeEach } from 'vitest';
import {
  useEffect,
  useLayoutEffect,
  useInsertionEffect,
  useState,
  useReducer,
  useMemo,
  useCallback,
  useImperativeHandle,
  createElement,
} from './react';
import { disposeAll } from './signal';
import { disposeNode } from './dom';

const tick = () => new Promise((r) => setTimeout(r, 0));
async function settle(): Promise<void> {
  for (let i = 0; i < 3; i++) await tick();
}

describe('React compat hooks (unlowered code paths)', () => {
  beforeEach(() => disposeAll());

  it('runs insertion, layout and passive effects in React order', async () => {
    const log: string[] = [];
    const Child = () => {
      useEffect(() => void log.push('child passive'), []);
      useLayoutEffect(() => void log.push('child layout'), []);
      useInsertionEffect(() => void log.push('child insertion'), []);
      return createElement('i', null);
    };
    const Parent = () => {
      useEffect(() => void log.push('parent passive'), []);
      useLayoutEffect(() => void log.push('parent layout'), []);
      return createElement('div', null, createElement(Child, null));
    };
    createElement(Parent, null);
    await settle();
    expect(log).toEqual(['child insertion', 'child layout', 'parent layout', 'child passive', 'parent passive']);
  });

  it('honours dependency arrays whose elements are state getters', async () => {
    const log: number[] = [];
    let setCount!: (v: number | ((p: number) => number)) => void;
    let other!: (v: number) => void;
    const Comp = () => {
      const [count, set] = useState(0);
      const [, setOther] = useState(0);
      setCount = set;
      other = setOther;
      useEffect(() => void log.push(count()), [count]);
      return createElement('span', null);
    };
    createElement(Comp, null);
    await settle();
    other(1);
    await settle();
    expect(log).toEqual([0]);
    setCount(1);
    await settle();
    expect(log).toEqual([0, 1]);
  });

  it('runs cleanups when the component unmounts', async () => {
    const log: string[] = [];
    const Comp = () => {
      useEffect(() => () => void log.push('passive cleanup'), []);
      useLayoutEffect(() => () => void log.push('layout cleanup'), []);
      return createElement('span', null);
    };
    const node = createElement(Comp, null);
    await settle();
    disposeNode(node);
    await settle();
    expect(log).toEqual(['layout cleanup', 'passive cleanup']);
  });

  it('keeps useMemo and useCallback values stable for the component lifetime', () => {
    let calls = 0;
    const fn = () => 1;
    const memo = useMemo(() => ++calls, [1]);
    expect(memo).toBe(1);
    expect(useCallback(fn, [])).toBe(fn);
  });

  it('supports the useReducer lazy initializer and bail-out', async () => {
    const [state, dispatch] = useReducer((s: number, a: number) => (a === 0 ? s : s + a), 2, (n: number) => n * 3);
    expect(state()).toBe(6);
    let runs = 0;
    useEffect(() => void runs++, [state]);
    await settle();
    dispatch(0);
    await settle();
    expect(runs).toBe(1);
    dispatch(1);
    await settle();
    expect(runs).toBe(2);
  });

  it('attaches imperative handles in the layout phase and detaches on unmount', async () => {
    const ref: { current: { focus: () => string } | null } = { current: null };
    const Comp = () => {
      useImperativeHandle(ref, () => ({ focus: () => 'focused' }), []);
      return createElement('input', null);
    };
    const node = createElement(Comp, null);
    expect(ref.current).toBe(null);
    await Promise.resolve();
    expect(ref.current?.focus()).toBe('focused');
    disposeNode(node);
    expect(ref.current).toBe(null);
  });
});
