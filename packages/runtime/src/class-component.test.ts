import { afterEach, describe, expect, it, vi } from 'vitest';
import { createEffect, createSignal, disposeAll } from './signal.js';
import { __krateOwn, __krateOwner, __krateCondPre, __krateCondPost, __krateOnError } from './boundary.js';
import { __krateClass, type ClassInstance, type ClassSpec } from './class-component.js';

type Obj = Record<string, unknown>;

// Lets the effect flush, the render phase and the commit phase run.
const settle = (): Promise<void> => new Promise((r) => setTimeout(r, 0));

afterEach(() => {
  disposeAll();
});

interface Mounted {
  inst: ClassInstance;
  state: () => Obj;
}

// Simulates the hydration scope the compiler emits for a lowered class: a
// committed-state signal, the instance, and the render bindings.
function classScope(
  props: Obj,
  initial: Obj,
  spec: ClassSpec,
  render?: (state: () => Obj, props: Obj) => void,
): Mounted {
  const [state, setState] = createSignal<Obj>(initial);
  const inst = __krateClass(props, state, setState, spec);
  if (render) render(state, props);
  return { inst, state };
}

describe('class lifecycles', () => {
  it('mounts children before parents and updates child before parent', async () => {
    const log: string[] = [];
    let parent!: Mounted;
    const text = { p: '', c: '' };
    const childProps: Obj = {};
    __krateOwn('P', { n: 'Parent' }, () => {
      parent = classScope({}, { n: 1 }, {
        componentDidMount: () => log.push('P didMount'),
        componentDidUpdate: (pp, ps) => log.push('P didUpdate ' + ps.n + '>' + parent.inst.state.n),
      }, (st) => {
        Object.defineProperty(childProps, 'n', { get: () => st().n, enumerable: true, configurable: true });
        createEffect(() => {
          text.p = String(st().n);
        });
      });
    });
    __krateOwn('C', { p: 'P', n: 'Child', k: 'P' }, () => {
      const c = classScope(childProps, {}, {
        componentDidMount: () => log.push('C didMount ' + c.inst.props.n),
        componentDidUpdate: (pp) => log.push('C didUpdate ' + pp.n + '>' + c.inst.props.n + ' dom=' + text.c),
      }, (_st, props) => {
        createEffect(() => {
          text.c = String(props.n);
        });
      });
    });
    await settle();
    expect(log).toEqual(['C didMount 1', 'P didMount']);
    log.length = 0;
    parent.inst.setState({ n: 2 });
    await settle();
    expect(log).toEqual(['C didUpdate 1>2 dom=2', 'P didUpdate 1>2']);
    expect(text).toEqual({ p: '2', c: '2' });
  });

  it('re-renders child classes when the creator re-renders with equal props', async () => {
    const log: string[] = [];
    let parent!: Mounted;
    __krateOwn('P', { n: 'Parent' }, () => {
      parent = classScope({}, { n: 1, other: 0 }, { componentDidUpdate: () => log.push('P') });
    });
    __krateOwn('C', { p: 'P', n: 'Child', k: 'P' }, () => {
      classScope({ label: 'x' }, {}, { componentDidUpdate: () => log.push('C') });
    });
    __krateOwn('Pure', { p: 'P', n: 'Pure', k: 'P' }, () => {
      classScope({ label: 'x' }, {}, { pure: true, componentDidUpdate: () => log.push('Pure') });
    });
    // Passed as children from outside: not re-rendered by P.
    __krateOwn('Passed', { p: 'P', n: 'Passed' }, () => {
      classScope({}, {}, { componentDidUpdate: () => log.push('Passed') });
    });
    await settle();
    parent.inst.setState({ other: 1 });
    await settle();
    expect(log).toEqual(['C', 'P']);
  });

  it('batches setState, applies updaters in order and runs callbacks after didUpdate', async () => {
    const log: string[] = [];
    let c!: Mounted;
    __krateOwn('A', { n: 'A' }, () => {
      c = classScope({}, { n: 0 }, {
        componentDidUpdate: (_pp, ps) => log.push('didUpdate ' + ps.n + '>' + c.inst.state.n),
      });
    });
    await settle();
    c.inst.setState((s) => ({ n: (s.n as number) + 1 }), () => log.push('cb1 ' + c.inst.state.n));
    c.inst.setState((s) => ({ n: (s.n as number) + 1 }), () => log.push('cb2'));
    c.inst.setState({ extra: true });
    // setState is asynchronous: this.state is unchanged until the update runs.
    expect(c.inst.state.n).toBe(0);
    await settle();
    expect(log).toEqual(['didUpdate 0>2', 'cb1 2', 'cb2']);
    expect(c.state()).toEqual({ n: 2, extra: true });
  });

  it('shouldComponentUpdate false skips the render and didUpdate but updates the instance', async () => {
    const log: string[] = [];
    let c!: Mounted;
    let dom = '';
    __krateOwn('A', { n: 'A' }, () => {
      c = classScope({}, { n: 0 }, {
        shouldComponentUpdate: (_np, ns) => (ns.n as number) % 2 === 0,
        componentDidUpdate: () => log.push('didUpdate ' + c.inst.state.n),
      }, (st) => {
        createEffect(() => {
          dom = String(st().n);
        });
      });
    });
    await settle();
    c.inst.setState({ n: 1 }, () => log.push('cb ' + c.inst.state.n));
    await settle();
    expect(dom).toBe('0');
    expect(c.inst.state.n).toBe(1);
    expect(log).toEqual(['cb 1']);
    c.inst.setState({ n: 2 });
    await settle();
    expect(dom).toBe('2');
    expect(log).toEqual(['cb 1', 'didUpdate 2']);
    // forceUpdate bypasses shouldComponentUpdate.
    c.inst.setState({ n: 3 });
    await settle();
    expect(dom).toBe('2');
    c.inst.forceUpdate(() => log.push('forced'));
    await settle();
    expect(dom).toBe('3');
    expect(log.slice(-2)).toEqual(['didUpdate 3', 'forced']);
  });

  it('PureComponent skips shallow-equal updates', async () => {
    const log: string[] = [];
    let c!: Mounted;
    __krateOwn('A', { n: 'A' }, () => {
      c = classScope({}, { n: 0 }, { pure: true, componentDidUpdate: () => log.push('u') });
    });
    await settle();
    c.inst.setState({ n: 0 });
    await settle();
    expect(log).toEqual([]);
    c.inst.setState({ n: 1 });
    await settle();
    expect(log).toEqual(['u']);
  });

  it('runs getDerivedStateFromProps on prop changes and passes the snapshot to didUpdate', async () => {
    const log: string[] = [];
    const [x, setX] = createSignal(1);
    const props: Obj = {};
    Object.defineProperty(props, 'x', { get: () => x(), enumerable: true, configurable: true });
    let dom = '';
    __krateOwn('A', { n: 'A' }, () => {
      classScope(props, { doubled: 2 }, {
        getDerivedStateFromProps: (p, s) => {
          log.push('gDSFP ' + p.x + ' ' + s.doubled);
          return { doubled: (p.x as number) * 2 };
        },
        getSnapshotBeforeUpdate: (pp) => {
          log.push('snapshot dom=' + dom + ' prev=' + pp.x);
          return 'snap';
        },
        componentDidUpdate: (pp, ps, snap) => log.push('didUpdate ' + pp.x + ' ' + ps.doubled + ' ' + snap + ' dom=' + dom),
      }, (st, p) => {
        createEffect(() => {
          dom = p.x + ':' + st().doubled;
        });
      });
    });
    await settle();
    expect(dom).toBe('1:2');
    setX(5);
    await settle();
    expect(log).toEqual(['gDSFP 5 2', 'snapshot dom=1:2 prev=1', 'didUpdate 1 2 snap dom=5:10']);
  });

  it('applies defaultProps to this.props and gated reads', async () => {
    let c!: Mounted;
    const props: Obj = { given: 'g' };
    __krateOwn('A', { n: 'A' }, () => {
      c = classScope(props, {}, { defaultProps: { color: 'blue', given: 'no' } });
    });
    await settle();
    expect(c.inst.props).toEqual({ given: 'g', color: 'blue' });
    expect(props.color).toBe('blue');
  });

  it('runs componentWillUnmount parent-first when a branch is hidden and remounts fresh state', async () => {
    const log: string[] = [];
    let setups = 0;
    __krateOwn('Page', { n: 'Page' }, () => {
      __krateCondPost('5', true);
    });
    __krateOwn('P', { p: 'Page', n: 'P', c: '5', b: 'c' }, () => {
      setups++;
      classScope({}, {}, {
        componentDidMount: () => log.push('P mount'),
        componentWillUnmount: () => log.push('P unmount'),
      });
    });
    __krateOwn('C', { p: 'P', n: 'C' }, () => {
      classScope({}, {}, {
        componentDidMount: () => log.push('C mount'),
        componentWillUnmount: () => log.push('C unmount'),
      });
    });
    await settle();
    expect(log).toEqual(['C mount', 'P mount']);
    __krateCondPre('5', false);
    __krateCondPost('5', false);
    expect(log.slice(2)).toEqual(['P unmount', 'C unmount']);
    expect(__krateOwner('P')!.state).toBe('dormant');
    __krateCondPre('5', true);
    __krateCondPost('5', true);
    await settle();
    expect(setups).toBe(2);
    expect(log.slice(4)).toEqual(['C mount', 'P mount']);
  });

  it('keeps components in an initially hidden branch dormant', async () => {
    let setups = 0;
    __krateOwn('Page', { n: 'Page' }, () => {
      __krateCondPost('7', false);
    });
    __krateOwn('X', { p: 'Page', n: 'X', c: '7', b: 'c' }, () => {
      setups++;
    });
    expect(setups).toBe(0);
    __krateCondPost('7', true);
    expect(setups).toBe(1);
  });
});

describe('error boundaries', () => {
  function boundary(id: string, meta: Obj, log: string[], name = 'Boundary'): { get: () => Mounted } {
    let b!: Mounted;
    __krateOwn(id, { n: name, ...meta }, () => {
      b = classScope({}, { error: null }, {
        name,
        getDerivedStateFromError: (error) => {
          log.push(name + ' gDSFE ' + (error as Error).message);
          return { error: (error as Error).message };
        },
        componentDidCatch: (error, info) => log.push(name + ' didCatch ' + (error as Error).message + info.componentStack),
      });
    });
    return { get: () => b };
  }

  it('catches a descendant setup error, unmounts the subtree and recovers on reset', async () => {
    const log: string[] = [];
    const b = boundary('B', {}, log);
    let attempts = 0;
    let shouldThrow = true;
    __krateOwn('Ok', { p: 'B', n: 'Ok' }, () => {
      classScope({}, {}, {
        componentDidMount: () => log.push('Ok mount'),
        componentWillUnmount: () => log.push('Ok unmount'),
      });
    });
    __krateOwn('Buggy', { p: 'B', n: 'Buggy' }, () => {
      attempts++;
      if (shouldThrow) throw new Error('boom');
    });
    __krateOwn('Later', { p: 'B', n: 'Later' }, () => {
      log.push('Later setup');
    });
    await settle();
    expect(log).toEqual(['Boundary gDSFE boom', 'Boundary didCatch boom\n    at Buggy\n    at Boundary']);
    expect(b.get().state()).toEqual({ error: 'boom' });
    expect(__krateOwner('Ok')!.state).toBe('dormant');
    expect(__krateOwner('Later')!.state).toBe('dormant');
    log.length = 0;
    shouldThrow = false;
    b.get().inst.setState({ error: null });
    await settle();
    expect(attempts).toBe(2);
    expect(log).toEqual(['Later setup', 'Ok mount']);
    expect(__krateOwner('Buggy')!.state).toBe('mounted');
  });

  it('catches errors in reactive re-runs and lifecycles, not in event handlers', async () => {
    const log: string[] = [];
    boundary('B', {}, log);
    const [n, setN] = createSignal(0);
    let handler!: () => void;
    __krateOwn('Child', { p: 'B', n: 'Child' }, () => {
      createEffect(() => {
        if (n() === 2) throw new Error('render ' + n());
      });
      handler = () => {
        throw new Error('handler');
      };
    });
    await settle();
    expect(() => handler()).toThrow('handler');
    expect(log).toEqual([]);
    setN(2);
    await settle();
    expect(log[0]).toBe('Boundary gDSFE render 2');
    expect(__krateOwner('Child')!.state).toBe('dormant');

    const log2: string[] = [];
    disposeAll();
    boundary('B2', {}, log2);
    __krateOwn('Life', { p: 'B2', n: 'Life' }, () => {
      classScope({}, {}, {
        componentDidMount: () => {
          throw new Error('mount');
        },
      });
    });
    await settle();
    expect(log2[0]).toBe('Boundary gDSFE mount');
  });

  it('lets the innermost boundary catch and sends fallback errors to the next one', async () => {
    const log: string[] = [];
    boundary('Outer', {}, log, 'Outer');
    boundary('Inner', { p: 'Outer' }, log, 'Inner');
    __krateOwn('Leaf', { p: 'Inner', n: 'Leaf' }, () => {
      throw new Error('leaf');
    });
    await settle();
    expect(log.filter((l) => l.includes('gDSFE'))).toEqual(['Inner gDSFE leaf']);

    // An error thrown by the boundary's own render goes to the boundary above.
    const log2: string[] = [];
    disposeAll();
    boundary('O', {}, log2, 'O');
    __krateOwn('I', { p: 'O', n: 'I' }, () => {
      const [st, setSt] = createSignal<Obj>({ error: null });
      __krateClass({}, st, setSt, {
        getDerivedStateFromError: () => ({ error: 'x' }),
      });
      createEffect(() => {
        if (st().error) throw new Error('fallback broke');
      });
    });
    __krateOwn('Deep', { p: 'I', n: 'Deep' }, () => {
      throw new Error('deep');
    });
    await settle();
    expect(log2.filter((l) => l.includes('gDSFE'))).toEqual(['O gDSFE fallback broke']);
  });

  it('reports uncaught errors and unmounts only the failing component', async () => {
    const spy = vi.spyOn(console, 'error').mockImplementation(() => {});
    let siblingRan = false;
    __krateOwn('Root', { n: 'Root' }, () => {});
    __krateOwn('Bad', { p: 'Root', n: 'Bad' }, () => {
      throw new Error('nobody catches');
    });
    __krateOwn('Fine', { p: 'Root', n: 'Fine' }, () => {
      siblingRan = true;
    });
    expect(siblingRan).toBe(true);
    expect(__krateOwner('Bad')!.state).toBe('dormant');
    expect(spy.mock.calls.some((c) => String(c[0]).includes('The above error occurred in the <Bad> component'))).toBe(true);
    spy.mockRestore();
  });

  it('ignores errors from bindings outside the document', () => {
    const detached = document.createElement('div');
    let routed = false;
    __krateOwn('Root', { n: 'Root' }, () => {
      routed = __krateOnError(new Error('hidden'), detached);
    });
    expect(routed).toBe(false);
  });
});
