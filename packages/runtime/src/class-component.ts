// Runtime for lowered React class components.
//
// The compiler lowers `class X extends Component` into a function component
// whose render reads a committed-state signal and a gated props object. This
// module supplies the instance behind `this`: batched setState with callbacks,
// forceUpdate, getDerivedStateFromProps, shouldComponentUpdate /
// PureComponent, getSnapshotBeforeUpdate, the mount/update/unmount
// lifecycles, legacy UNSAFE_ hooks and error boundaries.
//
// Updates run in two phases, like React's render and commit:
//
//   render  (after the current effect flush): every dirty instance, parents
//           first, computes its next props/state, consults
//           shouldComponentUpdate, takes the snapshot, and writes the
//           committed signals the DOM bindings read;
//   commit  (after those DOM writes flush): componentDidMount /
//           componentDidUpdate / componentDidCatch and setState callbacks,
//           children before parents.

import { createEffect, createSignal, krateOnSettle, krateRenderPhase, krateRequestFlush, untrack } from './signal.js';
import { krateSetRef, type KrateRef } from './refs.js';
import { krateOnLayoutCommit } from './effects.js';
import {
  getOwner,
  handleError,
  inCommitPhaseError,
  mountOwner,
  unmountOwner,
  onOwnerCleanup,
  remountChildren,
  runWithOwner,
  setBoundary,
  unmountChildren,
  type ErrorInfo,
  type Owner,
} from './boundary.js';

type Obj = Record<string, unknown>;

/** Lifecycle methods and statics of a lowered class, as closures. */
export interface ClassSpec {
  name?: string;
  /** PureComponent: shallow-compare props and state. */
  pure?: boolean;
  defaultProps?: Obj | null;
  /** Reads the value of `static contextType`. */
  context?: () => unknown;
  /** Writes the committed context the lowered render reads. */
  setContext?: (value: unknown) => void;
  getDerivedStateFromProps?: (props: Obj, state: Obj) => Obj | null | undefined;
  getDerivedStateFromError?: (error: unknown) => Obj | null | undefined;
  shouldComponentUpdate?: (nextProps: Obj, nextState: Obj, nextContext: unknown) => unknown;
  getSnapshotBeforeUpdate?: (prevProps: Obj, prevState: Obj) => unknown;
  componentDidMount?: () => void;
  componentDidUpdate?: (prevProps: Obj, prevState: Obj, snapshot: unknown) => void;
  componentWillUnmount?: () => void;
  componentDidCatch?: (error: unknown, info: ErrorInfo) => void;
  componentWillReceiveProps?: (nextProps: Obj, nextContext: unknown) => void;
  componentWillUpdate?: (nextProps: Obj, nextState: Obj, nextContext: unknown) => void;
  /** Public methods to put on the instance (read once, at mount). */
  expose?: () => Obj;
  /** An error a descendant threw while the page was built (see build-time boundaries). */
  buildError?: { name?: string; message?: string } | null;
}

/** The object lowered code uses as `this`. Instance fields are added freely. */
export interface ClassInstance {
  readonly props: Obj;
  state: Obj;
  readonly context: unknown;
  setState(update: Obj | ((state: Obj, props: Obj) => Obj | null | undefined) | null, callback?: () => void): void;
  forceUpdate(callback?: () => void): void;
  [field: string]: unknown;
}

interface Update {
  partial: unknown;
  callback?: () => void;
}

let seq = 0;

// Where the parent's original prop getters are kept on a gated props object.
const RAW_GETTERS = Symbol.for('krate.class.rawProps');

class Controller {
  readonly inst: ClassInstance;
  readonly owner: Owner | null;
  readonly spec: ClassSpec;
  readonly seq = seq++;
  private readonly getState: () => Obj;
  private readonly setStateSignal: (v: Obj) => void;
  private readonly raw: Obj;
  private readonly rawGetters: Array<[string, () => unknown]> = [];
  private readonly setRendered: (v: Obj) => void;
  private queue: Update[] = [];
  private forced = false;
  private parentRender = false;
  mounted = false;
  disposed = false;
  private catching = false;
  private key: unknown;
  private detachRef: (() => void) | null = null;
  private errorCommitPending = false;
  private props: Obj;
  private state: Obj;
  private context: unknown;

  constructor(raw: Obj | null | undefined, getState: () => Obj, setState: (v: Obj) => void, spec: ClassSpec) {
    this.spec = spec || {};
    this.owner = getOwner();
    this.raw = raw && typeof raw === 'object' ? raw : {};
    this.getState = getState;
    this.setStateSignal = setState;
    // A remounted instance reuses the registry object an earlier instance
    // gated; read the parent's original getters from where it saved them.
    const holder = this.raw as { [RAW_GETTERS]?: Array<[string, () => unknown]> };
    let saved = holder[RAW_GETTERS];
    if (!saved) {
      saved = [];
      for (const key of Object.keys(this.raw)) {
        const desc = Object.getOwnPropertyDescriptor(this.raw, key);
        if (desc && desc.get) {
          const get = desc.get;
          const obj = this.raw;
          saved.push([key, () => get.call(obj)]);
        }
      }
      try {
        Object.defineProperty(this.raw, RAW_GETTERS, { value: saved, enumerable: false, configurable: true });
      } catch {
        // A frozen registry cannot be gated either.
      }
    }
    this.rawGetters.push(...saved);
    this.props = this.readProps();
    this.key = this.readKey();
    this.state = untrack(getState) || {};
    this.context = this.readContext();
    const [rendered, setRendered] = createSignal<Obj>(this.props);
    this.setRendered = setRendered;
    this.gateProps(rendered);

    const self = this;
    const inst: ClassInstance = {
      get props() {
        return self.props;
      },
      get state() {
        return self.state;
      },
      set state(v: Obj) {
        // `this.state = ...` outside the constructor replaces instance state
        // without rendering, as in React.
        self.state = v;
      },
      get context() {
        return self.context;
      },
      setState(update, callback) {
        self.enqueue({ partial: update, callback });
      },
      forceUpdate(callback) {
        self.forced = true;
        self.enqueue({ partial: null, callback });
      },
    };
    this.inst = inst;

    if (this.owner) {
      this.owner.data.classController = this;
      onOwnerCleanup(this.owner, () => this.unmount());
      if (this.spec.getDerivedStateFromError || this.spec.componentDidCatch) {
        setBoundary(this.owner, (err, info) => this.catchError(err, info));
      }
    }

    // Track the parent's prop getters: a change schedules an update.
    if (this.spec.setContext) this.spec.setContext(this.context);
    if (this.rawGetters.length > 0 || this.spec.context) {
      let first = true;
      createEffect(() => {
        for (const [, get] of this.rawGetters) get();
        this.trackContext();
        if (first) {
          first = false;
          return;
        }
        if (!this.disposed) {
          renderQueue.add(this);
          schedule();
        }
      });
    }

    commit(this, () => {
      // Public methods live as locals in the lowered scope; put them on the
      // instance so refs (`ref.current.focus()`) can call them.
      const exposed = this.spec.expose ? untrack(this.spec.expose) : null;
      if (exposed) {
        for (const key of Object.keys(exposed)) {
          if (!(key in this.inst)) (this.inst as Obj)[key] = exposed[key];
        }
      }
      this.mounted = true;
      if (this.spec.componentDidMount) this.spec.componentDidMount.call(this.inst);
      // `<MyClass ref={r}/>` receives the instance after componentDidMount,
      // before the parent's componentDidMount (children commit first).
      const ref = untrack(() => this.raw.ref) as KrateRef;
      if (ref) this.detachRef = krateSetRef(ref, this.inst);
    });
    const buildError = this.spec.buildError;
    if (buildError && this.spec.componentDidCatch) {
      const err = new Error(buildError.message || 'Error');
      if (buildError.name) err.name = buildError.name;
      if (this.owner) this.owner.inError = true;
      commit(this, () => {
        this.spec.componentDidCatch!.call(this.inst, err, { componentStack: '\n    at ' + (this.spec.name || 'Component') + ' (build)' });
      });
    } else if (buildError && this.owner) {
      this.owner.inError = true;
    }
  }

  private readProps(): Obj {
    const out: Obj = {};
    untrack(() => {
      for (const key of Object.keys(this.raw)) {
        // React keeps `ref` (and `key`) out of a class's props.
        if (key.startsWith('__krate') || key === 'ref' || key === 'key') continue;
        const getter = this.rawGetters.find((g) => g[0] === key);
        out[key] = getter ? getter[1]() : this.raw[key];
      }
    });
    const defaults = this.spec.defaultProps;
    if (defaults) {
      for (const key of Object.keys(defaults)) {
        if (out[key] === undefined) out[key] = defaults[key];
      }
    }
    return out;
  }

  private readContext(): unknown {
    return untrack(() => this.trackContext());
  }

  // A dynamic provider value reaches the component as a thunk prop
  // (`__krate_ctx_<Name>`); call it to get the value (tracked when called
  // from the props-tracking effect).
  private trackContext(): unknown {
    const read = this.spec.context;
    if (!read) return undefined;
    const v = read();
    if (typeof v === 'function') {
      for (const key of Object.keys(this.raw)) {
        if (key.startsWith('__krate_ctx_') && this.raw[key] === v) return (v as () => unknown)();
      }
    }
    return v;
  }

  // Render bindings read props through the registry object. Redefine its
  // getters to read the committed props, so a skipped update
  // (shouldComponentUpdate false) leaves the DOM unchanged and defaultProps
  // apply at runtime.
  private gateProps(rendered: () => Obj): void {
    const keys = new Set<string>();
    for (const [key] of this.rawGetters) keys.add(key);
    const defaults = this.spec.defaultProps;
    if (defaults) {
      for (const key of Object.keys(defaults)) {
        if (this.raw[key] === undefined) keys.add(key);
      }
    }
    for (const key of keys) {
      if (key.startsWith('__krate') || key === 'ref' || key === 'key') continue;
      try {
        Object.defineProperty(this.raw, key, {
          get: () => rendered()[key],
          enumerable: true,
          configurable: true,
        });
      } catch {
        // A frozen registry keeps live props.
      }
    }
  }

  enqueue(update: Update): void {
    if (this.disposed) return;
    this.queue.push(update);
    renderQueue.add(this);
    schedule();
  }

  /** Marks the instance for a re-render because its creator re-rendered. */
  markParentRender(): void {
    if (this.disposed || !this.mounted) return;
    this.parentRender = true;
    renderQueue.add(this);
  }

  performUpdate(): void {
    if (this.disposed || (this.owner && this.owner.state !== 'mounted')) return;
    if (this.owner) runWithOwner(this.owner, () => this.update());
    else {
      try {
        this.update();
      } catch (err) {
        console.error(err);
      }
    }
  }

  private readKey(): unknown {
    const getter = this.rawGetters.find((g) => g[0] === '__krate_key');
    return untrack(() => (getter ? getter[1]() : this.raw.__krate_key));
  }

  private update(): void {
    const spec = this.spec;
    const inst = this.inst;
    // A new key is a new component: unmount this instance and mount a fresh
    // one (the owner's setup runs again).
    if (!Object.is(this.readKey(), this.key) && this.owner) {
      const owner = this.owner;
      unmountOwner(owner);
      owner.inError = false;
      mountOwner(owner);
      remountChildren(owner);
      return;
    }
    const nextProps = this.readProps();
    const nextContext = this.readContext();
    const propsChanged = !shallowEqual(nextProps, this.props) || nextContext !== this.context;
    const queue = this.queue;
    this.queue = [];
    const forced = this.forced;
    this.forced = false;
    const parentRender = this.parentRender;
    this.parentRender = false;
    if (queue.length === 0 && !forced && !propsChanged && !parentRender) return;

    const legacy = !spec.getDerivedStateFromProps && !spec.getSnapshotBeforeUpdate;
    if (legacy && (propsChanged || parentRender) && spec.componentWillReceiveProps) {
      spec.componentWillReceiveProps.call(inst, nextProps, nextContext);
      // setState inside componentWillReceiveProps joins this update.
      queue.push(...this.queue);
      this.queue = [];
    }

    const prevProps = this.props;
    const prevState = this.state;
    let nextState = prevState;
    const callbacks: Array<() => void> = [];
    for (const u of queue) {
      const partial =
        typeof u.partial === 'function'
          ? (u.partial as (s: Obj, p: Obj) => unknown).call(inst, nextState, nextProps)
          : u.partial;
      if (partial != null && typeof partial === 'object') nextState = { ...nextState, ...(partial as Obj) };
      if (typeof u.callback === 'function') callbacks.push(u.callback);
    }
    if (spec.getDerivedStateFromProps) {
      const derived = spec.getDerivedStateFromProps(nextProps, nextState);
      if (derived != null) nextState = { ...nextState, ...derived };
    }

    let shouldUpdate = true;
    if (!forced) {
      if (spec.shouldComponentUpdate) {
        shouldUpdate = !!spec.shouldComponentUpdate.call(inst, nextProps, nextState, nextContext);
      } else if (spec.pure) {
        shouldUpdate = !shallowEqual(prevProps, nextProps) || !shallowEqual(prevState, nextState);
      }
    }
    if (shouldUpdate && legacy && spec.componentWillUpdate) {
      spec.componentWillUpdate.call(inst, nextProps, nextState, nextContext);
    }

    // React updates the instance even when the render is skipped.
    this.props = nextProps;
    this.state = nextState;
    this.context = nextContext;

    if (!shouldUpdate) {
      for (const cb of callbacks) commit(this, () => cb.call(inst));
      return;
    }

    const snapshot = spec.getSnapshotBeforeUpdate
      ? spec.getSnapshotBeforeUpdate.call(inst, prevProps, prevState)
      : undefined;
    this.setRendered(nextProps);
    if (spec.setContext) spec.setContext(nextContext);
    // A forced update re-runs every binding, even when state is unchanged.
    this.setStateSignal(forced && nextState === prevState ? { ...nextState } : nextState);
    if (forced && nextState === prevState) this.state = untrack(this.getState);

    const recovering = this.owner && this.owner.inError && !this.errorCommitPending;
    if (recovering && this.owner) {
      this.owner.inError = false;
      remountQueue.add(this.owner);
    }
    commit(this, () => {
      if (spec.componentDidUpdate) spec.componentDidUpdate.call(inst, prevProps, prevState, snapshot);
    });
    for (const cb of callbacks) commit(this, () => cb.call(inst));
    this.cascade();
  }

  // A re-render re-renders the class components this one's render created
  // (directly or through function components), unless they bail out.
  private cascade(): void {
    const owner = this.owner;
    if (!owner) return;
    const reaches = (o: Owner | null): boolean => {
      for (let c = o ? o.creator : null; c; c = c.creator) {
        if (c === owner) return true;
        if (c.data.classController) return false;
      }
      return false;
    };
    const walk = (o: Owner): void => {
      for (const child of o.children) {
        if (child.state !== 'mounted') continue;
        const ctrl = child.data.classController as Controller | undefined;
        if (ctrl && reaches(child)) ctrl.markParentRender();
        walk(child);
      }
    };
    walk(owner);
  }

  catchError(err: unknown, info: ErrorInfo): boolean {
    const spec = this.spec;
    const owner = this.owner;
    if (this.disposed || !owner || owner.state !== 'mounted') return false;
    if (this.catching) return true;
    this.catching = true;
    try {
      // React logs every error a boundary catches.
      if (typeof console !== 'undefined') {
        console.error(err);
        console.error(
          'The above error occurred in the <' + (info.componentStack.match(/at (\S+)/)?.[1] || 'Unknown') +
            '> component:' + info.componentStack +
            '\n\nReact will try to recreate this component tree from scratch using the error boundary you provided, ' +
            (spec.name || 'Component') + '.',
        );
      }
      owner.inError = true;
      // The children that were rendered when the error happened stay down
      // until the boundary renders again; dormant ones (a hidden fallback
      // branch) may still mount.
      const failed = owner.children.filter((c) => c.state === 'mounted');
      unmountChildren(owner);
      for (const child of failed) child.blocked = true;
      let next = this.state;
      if (spec.getDerivedStateFromError) {
        // React 18 retries a render that threw during an update once before
        // committing the fallback, so getDerivedStateFromError runs twice.
        if (this.mounted && !inCommitPhase && !inCommitPhaseError()) spec.getDerivedStateFromError(err);
        const partial = spec.getDerivedStateFromError(err);
        if (partial != null) next = { ...next, ...partial };
      } else if (typeof console !== 'undefined') {
        console.error(
          (spec.name || 'Component') +
            ': Error boundaries should implement getDerivedStateFromError(). ' +
            'In that method, return a state update to display an error message or fallback UI.',
        );
      }
      const prevProps = this.props;
      const prevState = this.state;
      const wasMounted = this.mounted;
      this.state = next;
      this.errorCommitPending = true;
      this.setStateSignal(next);
      commit(this, () => {
        this.errorCommitPending = false;
        if (wasMounted && next !== prevState && spec.componentDidUpdate) {
          spec.componentDidUpdate.call(this.inst, prevProps, prevState, undefined);
        }
        if (spec.componentDidCatch) spec.componentDidCatch.call(this.inst, err, info);
      });
    } finally {
      this.catching = false;
    }
    return true;
  }

  unmount(): void {
    if (this.disposed) return;
    this.disposed = true;
    renderQueue.delete(this);
    // React detaches the ref before componentWillUnmount.
    const detach = this.detachRef;
    this.detachRef = null;
    if (detach) detach();
    if (this.mounted && this.spec.componentWillUnmount) {
      this.spec.componentWillUnmount.call(this.inst);
    }
    this.mounted = false;
  }

  runLifecycle(fn: () => void): void {
    if (this.disposed) return;
    if (this.owner) runWithOwner(this.owner, fn);
    else {
      try {
        fn();
      } catch (err) {
        console.error(err);
      }
    }
  }
}

function shallowEqual(a: Obj | null | undefined, b: Obj | null | undefined): boolean {
  if (a === b) return true;
  if (!a || !b) return false;
  const ka = Object.keys(a);
  const kb = Object.keys(b);
  if (ka.length !== kb.length) return false;
  for (const k of ka) {
    if (!Object.prototype.hasOwnProperty.call(b, k) || !Object.is(a[k], b[k])) return false;
  }
  return true;
}

// Scheduler.

const renderQueue = new Set<Controller>();
const remountQueue = new Set<Owner>();
const commitQueue: Array<{ c: Controller; fn: () => void; n: number }> = [];
let commitSeq = 0;
let commitScheduled = false;

// Render requests are processed when the current flush settles (krateOnSettle),
// so class DOM writes land in the same pass as other bindings.
function schedule(): void {
  krateRequestFlush();
}

// Lifecycles run after the pass's DOM writes and the effects.ts layout commit.
function commit(c: Controller, fn: () => void): void {
  commitQueue.push({ c, fn, n: commitSeq++ });
  if (!commitScheduled) {
    commitScheduled = true;
    krateRequestFlush();
  }
}

function preOrder(a: Controller, b: Controller): number {
  if (a.owner && b.owner) return a.owner.order - b.owner.order;
  return a.seq - b.seq;
}

function isAncestor(a: Owner, b: Owner): boolean {
  for (let p = b.parent; p; p = p.parent) if (p === a) return true;
  return false;
}

// Children commit before their parents; unrelated owners keep tree order.
function postOrder(a: { c: Controller; n: number }, b: { c: Controller; n: number }): number {
  const oa = a.c.owner;
  const ob = b.c.owner;
  if (oa && ob && oa !== ob) {
    if (isAncestor(oa, ob)) return 1;
    if (isAncestor(ob, oa)) return -1;
    return oa.order - ob.order;
  }
  if (oa === ob) return a.n - b.n;
  return a.c.seq - b.c.seq || a.n - b.n;
}

function runRender(): void {
  for (let guard = 0; renderQueue.size > 0 && guard < 1000; guard++) {
    const list = Array.from(renderQueue).sort(preOrder);
    renderQueue.clear();
    krateRenderPhase(() => {
      for (const c of list) c.performUpdate();
    });
  }
}

// Set while lifecycles run: errors there are commit-phase errors, which React
// does not retry.
let inCommitPhase = false;

function runCommit(): void {
  commitScheduled = false;
  inCommitPhase = true;
  try {
    runCommitEntries();
  } finally {
    inCommitPhase = false;
  }
}

function runCommitEntries(): void {
  if (remountQueue.size > 0) {
    const owners = Array.from(remountQueue).sort((a, b) => a.order - b.order);
    remountQueue.clear();
    for (const o of owners) remountChildren(o);
  }
  if (commitQueue.length > 0) {
    const list = commitQueue.splice(0).sort(postOrder);
    for (const entry of list) entry.c.runLifecycle(entry.fn);
  }
}

krateOnSettle(runRender);
// Lifecycles commit in the layout phase: after layout effects, before passive.
krateOnLayoutCommit(() => {
  if (commitScheduled || commitQueue.length > 0 || remountQueue.size > 0) runCommit();
});

/**
 * Creates the instance behind a lowered class component. `props` is the
 * component's props registry object, `getState`/`setState` the committed-state
 * signal its render reads.
 */
export function __krateClass(
  props: Obj | null | undefined,
  getState: () => Obj,
  setState: (v: Obj) => void,
  spec: ClassSpec,
): ClassInstance {
  return new Controller(props, getState, setState, spec).inst;
}

/** Shape of a runtime (not compiler-lowered) class component instance. */
interface RuntimeClassInstance {
  props?: Obj;
  state?: Obj | null;
  context?: unknown;
  render(): unknown;
  setState?: unknown;
  forceUpdate?: unknown;
  [method: string]: unknown;
}

type RuntimeClass = (new (props: Obj) => RuntimeClassInstance) & Record<string, unknown>;

const hooks = [
  'shouldComponentUpdate',
  'getSnapshotBeforeUpdate',
  'componentDidMount',
  'componentDidUpdate',
  'componentWillUnmount',
  'componentDidCatch',
  'componentWillReceiveProps',
  'componentWillUpdate',
] as const;

/**
 * Mounts a class component at runtime (React.createElement(MyClass) in code
 * the compiler did not lower). The instance's setState/forceUpdate go through
 * the same controller as lowered classes; each committed update re-runs
 * render() and replaces the host's content.
 */
export function mountClassElement(Klass: RuntimeClass, props: Obj, toNode: (v: unknown) => Node): Node {
  const instance = new Klass(props);
  const [state, setState] = createSignal<Obj>((instance.state as Obj) || {});
  const spec: ClassSpec = {
    name: (Klass.displayName as string) || Klass.name,
    pure: !!(Klass.prototype as { isPureReactComponent?: boolean }).isPureReactComponent,
    defaultProps: (Klass.defaultProps as Obj) || null,
    getDerivedStateFromProps: Klass.getDerivedStateFromProps as ClassSpec['getDerivedStateFromProps'],
    getDerivedStateFromError: Klass.getDerivedStateFromError as ClassSpec['getDerivedStateFromError'],
  };
  for (const name of hooks) {
    const fn = instance[name] ?? instance['UNSAFE_' + name];
    if (typeof fn === 'function') {
      (spec as Record<string, unknown>)[name] = (...args: unknown[]) => (fn as (...a: unknown[]) => unknown).apply(instance, args);
    }
  }
  const inst = __krateClass(props, state, setState, spec);
  Object.defineProperty(instance, 'props', { get: () => inst.props, configurable: true });
  Object.defineProperty(instance, 'state', {
    get: () => inst.state,
    set: (v: Obj) => {
      inst.state = v;
    },
    configurable: true,
  });
  instance.setState = inst.setState;
  instance.forceUpdate = inst.forceUpdate;
  const host = document.createElement('div');
  host.style.display = 'contents';
  createEffect(() => {
    state();
    const node = untrack(() => toNode(instance.render()));
    host.replaceChildren(node);
  });
  return host;
}

/** Reports an error from lowered class code to the nearest boundary. */
export function __krateClassError(err: unknown): void {
  handleError(err, getOwner());
}
