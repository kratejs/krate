/**
 * React-style event system and form-control semantics.
 *
 * Handlers live in a per-element table (`el.__kh`) keyed by the React event
 * name (`click`, `change`, `focus`) plus `$` for the capture variant. Like
 * React 17+, delegated events get one capture-phase and one bubble-phase
 * listener on the root container (`#root`, else `<body>`), installed lazily the
 * first time a handler for that native type is registered. The capture
 * listener runs capture handlers top-down, the bubble listener runs bubble
 * handlers bottom-up, so React handlers interleave with native listeners the
 * same way they do in React: native listeners inside the root run before React
 * bubble handlers, listeners on `document` run after them, and
 * `stopPropagation()` stops both further React handlers and native propagation.
 *
 * Events React does not delegate (scroll, media, load/error, enter/leave, ...)
 * get a listener on the element itself.
 *
 * onChange follows React's ChangeEventPlugin: text inputs and textareas fire on
 * every `input`, checkboxes and radios on `click`, selects and file inputs on
 * `change`, with a value tracker so a change fires once per actual change.
 * After onChange handlers run, controlled inputs (bound with krateControl) are
 * restored to their state value, so a handler that does not update state keeps
 * the field unchanged, as in React.
 */
import { createEffect } from './signal.js';

/** A React-compatible synthetic event. */
export interface KrateSyntheticEvent<E extends Event = Event> {
  type: string;
  target: EventTarget | null;
  currentTarget: EventTarget | null;
  nativeEvent: E;
  defaultPrevented: boolean;
  readonly bubbles: boolean;
  readonly cancelable: boolean;
  readonly eventPhase: number;
  readonly isTrusted: boolean;
  readonly timeStamp: number;
  preventDefault(): void;
  stopPropagation(): void;
  isDefaultPrevented(): boolean;
  isPropagationStopped(): boolean;
  persist(): void;
  isPersistent(): boolean;
  [field: string]: unknown;
}

// eslint-disable-next-line @typescript-eslint/no-explicit-any
type Handler = (e: KrateSyntheticEvent<any>) => unknown;
type HTable = Record<string, Handler | undefined>;
type KNode = Node & { __kh?: HTable; __kd?: Record<string, 1> };
type FormEl = HTMLInputElement & {
  __kv?: string;
  __kc?: Record<string, (() => unknown) | undefined>;
  __kw?: 1;
};

// React event names whose native event has a different name.
const TO_NATIVE: Record<string, string> = { doubleclick: 'dblclick', focus: 'focusin', blur: 'focusout' };
const TO_REACT: Record<string, string> = { dblclick: 'doubleclick', focusin: 'focus', focusout: 'blur' };

// Synthetic event type for events whose React type differs from the native one.
const REACT_TYPE: Record<string, string> = { focusin: 'focus', focusout: 'blur' };

// Events React attaches to the element instead of the root (they do not bubble,
// or React emulates them per element).
const DIRECT = new Set(
  (
    'scroll scrollend load error abort cancel close invalid toggle beforetoggle ' +
    'mouseenter mouseleave pointerenter pointerleave ' +
    'canplay canplaythrough durationchange emptied encrypted ended loadeddata ' +
    'loadedmetadata loadstart pause play playing progress ratechange resize ' +
    'seeked seeking stalled suspend timeupdate volumechange waiting'
  ).split(' ')
);

// Native events each plugin event listens to.
const PLUGIN_DEPS: Record<string, string[]> = {
  change: ['change', 'input', 'click'],
  beforeinput: ['beforeinput', 'compositionend'],
};

const proto: Record<string, unknown> = {
  preventDefault(this: KrateSyntheticEvent) {
    this.defaultPrevented = true;
    this.nativeEvent.preventDefault();
  },
  stopPropagation(this: KrateSyntheticEvent & { _s?: boolean }) {
    this._s = true;
    this.nativeEvent.stopPropagation();
  },
  isDefaultPrevented(this: KrateSyntheticEvent) {
    return this.defaultPrevented;
  },
  isPropagationStopped(this: KrateSyntheticEvent & { _s?: boolean }) {
    return !!this._s;
  },
  persist() {},
  isPersistent() {
    return true;
  },
};
// Fields the synthetic event does not own (key, clientX, relatedTarget,
// deltaY, touches, dataTransfer, clipboardData, data, animationName, ...) are
// read from the native event, so every type-specific field is available.
const traps: ProxyHandler<Record<string | symbol, unknown>> = {
  get(t, k) {
    if (k in t) return t[k];
    const n = t.nativeEvent as Record<string | symbol, unknown>;
    const v = n[k];
    return typeof v === 'function' ? (v as (...a: unknown[]) => unknown).bind(n) : v;
  },
};

/** Wraps a native event in a React-style synthetic event. */
export function krateSynthetic<E extends Event>(native: E, type?: string, target?: EventTarget | null): KrateSyntheticEvent<E> {
  const e = Object.create(proto) as KrateSyntheticEvent<E>;
  let t = target === undefined ? native.target : target;
  // React reports the element for events targeted at text nodes.
  if (t && (t as Node).nodeType === 3) t = (t as Node).parentNode;
  e.type = type || REACT_TYPE[native.type] || native.type;
  e.target = t;
  e.currentTarget = null;
  e.nativeEvent = native;
  e.defaultPrevented = native.defaultPrevented;
  return new Proxy(e as unknown as Record<string | symbol, unknown>, traps) as unknown as KrateSyntheticEvent<E>;
}

// The first error thrown by a handler is rethrown after the dispatch finishes,
// so every handler still runs (React rethrows the same way).
let caught: unknown;
let hasCaught = false;

function call(h: Handler, e: KrateSyntheticEvent): void {
  try {
    h(e);
  } catch (err) {
    if (!hasCaught) {
      hasCaught = true;
      caught = err;
    }
  }
}

function rethrow(): void {
  if (hasCaught) {
    const err = caught;
    hasCaught = false;
    caught = undefined;
    throw err;
  }
}

// Phases for emit(): 1 = capture handlers, 2 = bubble handlers, 3 = both.
function emit(native: Event, name: string, target: Node | null, phases: number, type?: string): KrateSyntheticEvent | null {
  const cap: Array<Node | Handler> = [];
  const bub: Array<Node | Handler> = [];
  for (let n = target as KNode | null; n; n = n.parentNode as KNode | null) {
    const m = n.__kh;
    if (!m) continue;
    let h: Handler | undefined;
    if (phases & 1 && (h = m[name + '$'])) cap.unshift(n, h);
    if (phases & 2 && (h = m[name])) bub.push(n, h);
  }
  if (!cap.length && !bub.length) return null;
  const e = krateSynthetic(native, type, target);
  const list = cap.concat(bub);
  for (let i = 0; i < list.length; i += 2) {
    if ((e as { _s?: boolean })._s) break;
    e.currentTarget = list[i] as Node;
    call(list[i + 1] as Handler, e);
  }
  e.currentTarget = null;
  return e;
}

// Root containers with their installed native types.
const rootTypes = new WeakMap<Node, Set<string>>();
const roots = new Set<Node>();
const allTypes = new Set<string>();

function defaultRoot(): Node | null {
  if (typeof document === 'undefined') return null;
  return document.getElementById('root') || document.body;
}

function installOn(root: Node, type: string): void {
  let set = rootTypes.get(root);
  if (!set) {
    set = new Set();
    rootTypes.set(root, set);
  }
  if (set.has(type)) return;
  set.add(type);
  root.addEventListener(type, onRootCapture, true);
  root.addEventListener(type, onRootBubble, false);
}

function ensure(type: string): void {
  allTypes.add(type);
  const r = defaultRoot();
  if (r) {
    roots.add(r);
    installOn(r, type);
  }
  for (const extra of roots) {
    if (extra !== r && extra.isConnected) installOn(extra, type);
  }
}

/**
 * Makes `node` an additional delegation root (portals moved outside `#root`).
 * Every event type registered so far, and any registered later, is listened
 * to on it.
 */
export function krateEventRoot(node: Node): void {
  roots.add(node);
  for (const t of allTypes) installOn(node, t);
}

function onRootCapture(this: Node, native: Event): void {
  dispatchRoot(native, true);
}

function onRootBubble(this: Node, native: Event): void {
  dispatchRoot(native, false);
}

function dispatchRoot(native: Event, capture: boolean): void {
  // Several roots can be on the path (a portal inside the root); the first
  // listener to see the event handles the whole path.
  const flag = capture ? '__kdc' : '__kdb';
  const n = native as Event & Record<string, unknown>;
  if (n[flag]) return;
  n[flag] = 1;
  const t = native.type;
  const target = native.target as Node | null;
  if (t !== 'change' && t !== 'beforeinput') {
    emit(native, TO_REACT[t] || t, target, capture ? 1 : 2);
  }
  if (!capture) {
    if (t === 'change' || t === 'input' || t === 'click') changePlugin(native);
    else if (t === 'beforeinput' || t === 'compositionend') beforeInputPlugin(native);
  }
  rethrow();
}

function onDirect(this: Node, native: Event): void {
  directDispatch(this, native, false);
}

function onDirectCapture(this: Node, native: Event): void {
  directDispatch(this, native, true);
}

function directDispatch(el: Node, native: Event, capture: boolean): void {
  const m = (el as KNode).__kh;
  const h = m && m[native.type + (capture ? '$' : '')];
  if (!h) return;
  const e = krateSynthetic(native);
  e.currentTarget = el;
  call(h, e);
  e.currentTarget = null;
  rethrow();
}

/** Splits a React prop (`onClickCapture`) into its event name and phase. */
export function krateEventName(prop: string): [name: string, capture: boolean] {
  let n = prop.slice(2);
  let capture = false;
  if (n.length > 7 && n.slice(-7) === 'Capture' && !/^(Got|Lost)PointerCapture$/.test(n)) {
    n = n.slice(0, -7);
    capture = true;
  }
  return [n.toLowerCase(), capture];
}

/**
 * Sets (or with a non-function `fn`, removes) the React event handler `prop`
 * (`onClick`, `onChangeCapture`, ...) on `el`.
 */
export function krateOn(el: Element, prop: string, fn: ((e: KrateSyntheticEvent<any>) => unknown) | null | undefined): void {
  const [name, capture] = krateEventName(prop);
  const node = el as unknown as KNode;
  const table = node.__kh || (node.__kh = {});
  const key = name + (capture ? '$' : '');
  if (typeof fn !== 'function') {
    table[key] = undefined;
    return;
  }
  table[key] = fn as Handler;
  if (DIRECT.has(name)) {
    const done = node.__kd || (node.__kd = {});
    if (!done[key]) {
      done[key] = 1;
      el.addEventListener(name, capture ? onDirectCapture : onDirect, capture);
    }
    return;
  }
  const deps = PLUGIN_DEPS[name];
  if (deps) {
    for (const d of deps) ensure(d);
    if (name === 'change') track(el as FormEl);
  } else {
    ensure(TO_NATIVE[name] || name);
  }
}

// value tracking and onChange

const TEXT_TYPES = new Set(
  'color date datetime datetime-local email month number password range search tel text time url week'.split(' ')
);

function isCheckable(el: FormEl): boolean {
  return el.tagName === 'INPUT' && (el.type === 'checkbox' || el.type === 'radio');
}

function currentValue(el: FormEl): string {
  return isCheckable(el) ? String(el.checked) : String(el.value);
}

function track(el: FormEl): void {
  if (el.__kv === undefined && (el.tagName === 'INPUT' || el.tagName === 'TEXTAREA')) {
    el.__kv = currentValue(el);
  }
}

function updateTracker(el: FormEl): boolean {
  const v = currentValue(el);
  if (el.__kv === v) return false;
  el.__kv = v;
  return true;
}

function radioGroup(el: FormEl): FormEl[] {
  if (!el.name || !el.ownerDocument) return [el];
  const out: FormEl[] = [];
  const list = el.ownerDocument.getElementsByName(el.name);
  for (let i = 0; i < list.length; i++) {
    const o = list[i] as FormEl;
    if (o.tagName === 'INPUT' && o.type === 'radio' && o.form === el.form) out.push(o);
  }
  return out;
}

function changeTarget(native: Event): FormEl | null {
  const el = native.target as FormEl | null;
  if (!el || !el.tagName) return null;
  const t = native.type;
  const tag = el.tagName;
  if (tag === 'SELECT' || (tag === 'INPUT' && el.type === 'file')) return t === 'change' ? el : null;
  if (isCheckable(el)) return t === 'click' && updateTracker(el) ? el : null;
  if (tag === 'TEXTAREA' || (tag === 'INPUT' && TEXT_TYPES.has(el.type || 'text'))) {
    return t !== 'click' && updateTracker(el) ? el : null;
  }
  return null;
}

function changePlugin(native: Event): void {
  const el = changeTarget(native);
  if (!el) return;
  emit(native, 'change', el, 3, 'change');
  if (el.type === 'radio') {
    for (const o of radioGroup(el)) {
      updateTracker(o);
      restore(o);
    }
  } else {
    restore(el);
  }
}

function beforeInputPlugin(native: Event): void {
  const n = native as InputEvent;
  if (native.type === 'beforeinput' && (n.isComposing || typeof n.data !== 'string' || n.data === '')) return;
  if (native.type === 'compositionend' && !n.data) return;
  emit(native, 'beforeinput', native.target as Node | null, 3, 'beforeinput');
}

// controlled form controls

function warnOnce(el: FormEl, msg: string): void {
  if (el.__kw) return;
  el.__kw = 1;
  console.error(msg);
}

function setSelect(el: HTMLSelectElement, v: unknown, asDefault: boolean): void {
  const opts = el.options;
  if (el.multiple) {
    const want = new Set((Array.isArray(v) ? v : [v]).map(String));
    for (let i = 0; i < opts.length; i++) {
      const sel = want.has(opts[i].value);
      if (asDefault) opts[i].defaultSelected = sel;
      else if (opts[i].selected !== sel) opts[i].selected = sel;
    }
    return;
  }
  const s = String(v);
  let first: HTMLOptionElement | null = null;
  for (let i = 0; i < opts.length; i++) {
    if (opts[i].value === s) {
      if (asDefault) opts[i].defaultSelected = true;
      else opts[i].selected = true;
      return;
    }
    if (!first && !opts[i].disabled) first = opts[i];
  }
  // React selects the first enabled option when no option matches.
  if (first && !asDefault) first.selected = true;
}

/**
 * Applies one value of a form-control property with React semantics.
 * `value`/`checked` of null or undefined leave the DOM untouched (the control is
 * uncontrolled); `defaultValue`/`defaultChecked` only change the default.
 */
export function krateSetControl(el: Element, prop: string, v: unknown): void {
  const f = el as FormEl;
  switch (prop) {
    case 'value': {
      if (v == null) return;
      if (f.tagName === 'SELECT') {
        setSelect(f as unknown as HTMLSelectElement, v, false);
        return;
      }
      const s = String(v);
      if (f.value !== s) f.value = s;
      // React keeps the value attribute in sync, so form.reset() restores the
      // current controlled value. Skipped for a focused number input.
      if (f.type !== 'file' && !(f.type === 'number' && f.ownerDocument && f.ownerDocument.activeElement === f)) {
        if (f.defaultValue !== s) f.defaultValue = s;
      }
      if (!isCheckable(f)) f.__kv = s;
      return;
    }
    case 'checked': {
      if (v == null) return;
      const b = !!v;
      if (f.checked !== b) f.checked = b;
      f.__kv = String(b);
      return;
    }
    case 'defaultValue':
      if (f.tagName === 'SELECT') setSelect(f as unknown as HTMLSelectElement, v, true);
      else f.defaultValue = v == null ? '' : String(v);
      return;
    case 'defaultChecked':
      f.defaultChecked = !!v;
      return;
    default:
      (f as unknown as Record<string, unknown>)[prop] = !!v;
  }
}

function restore(el: FormEl): void {
  const c = el.__kc;
  if (!c) return;
  if (c.value) krateSetControl(el, 'value', c.value());
  if (c.checked) krateSetControl(el, 'checked', c.checked());
}

/**
 * Binds a reactive form-control property (`value`, `checked`, `defaultValue`,
 * ...) to `get`. `value`/`checked` make the control controlled: after onChange
 * the DOM is restored to `get()`. Switching between controlled (a defined
 * value) and uncontrolled (undefined) logs React's warning. Returns the
 * effect's dispose function.
 */
export function krateControl(el: Element, prop: string, get: () => unknown): () => void {
  const f = el as FormEl;
  const controlProp = prop === 'value' || prop === 'checked';
  if (controlProp) {
    (f.__kc || (f.__kc = {}))[prop] = get;
    // Restoring needs the change listeners even without an onChange handler.
    for (const d of PLUGIN_DEPS.change) ensure(d);
    track(f);
  }
  let was: boolean | undefined;
  return createEffect(() => {
    const v = get();
    if (controlProp) {
      const now = v != null;
      if (v === null && prop === 'value') {
        warnOnce(f, '`value` prop on `' + f.tagName.toLowerCase() + '` should not be null. Use an empty string to clear it or `undefined` for an uncontrolled field.');
      } else if (was !== undefined && was !== now) {
        warnOnce(
          f,
          'A component is changing ' + (was ? 'a controlled' : 'an uncontrolled') + ' input to be ' +
            (now ? 'controlled' : 'uncontrolled') +
            '. Decide between using a controlled or uncontrolled input element for the lifetime of the component. More info: https://react.dev/link/controlled-components'
        );
      }
      was = now;
    }
    krateSetControl(f, prop, v);
  });
}
