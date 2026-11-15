import { describe, expect, it } from 'vitest';
import { createReactCompatExtras } from './react-compat';

function setup() {
  const options: any = {};
  const Fragment = function Fragment() { return null; };
  const c: any = {
    useState: () => [], useRef: () => ({}), useEffect: () => {}, useCallback: (f: any) => f,
    useContext: () => ({}), createContext: () => ({}), createElement: () => ({}), Fragment,
  };
  createReactCompatExtras(c, options);
  const make = (type: any, ref: any) => {
    const vnode: any = { type, props: { id: 'x' }, ref };
    options.vnode(vnode);
    return vnode;
  };
  return { make, Fragment };
}

describe('react-compat ref as a prop', () => {
  it('moves the ref of a function component into props.ref', () => {
    const { make } = setup();
    const object = { current: null };
    const cb = () => {};
    function Button() { return null; }
    for (const ref of [object, cb]) {
      const v = make(Button, ref);
      expect(v.props.ref).toBe(ref);
      expect(v.ref).toBeNull();
    }
  });

  it('leaves class, forwardRef-style, DOM and Fragment vnodes alone', () => {
    const { make, Fragment } = setup();
    const ref = { current: null };
    class Cls { render() { return null; } }
    const forwarded: any = function Forwarded() { return null; };
    forwarded._forwarded = true;
    for (const type of [Cls, forwarded, 'button', Fragment]) {
      const v = make(type, ref);
      expect(v.ref).toBe(ref);
      expect(v.props.ref).toBeUndefined();
    }
  });

  it('does nothing when there is no ref', () => {
    const { make } = setup();
    function Button() { return null; }
    const v = make(Button, null);
    expect('ref' in v.props).toBe(false);
  });
});
