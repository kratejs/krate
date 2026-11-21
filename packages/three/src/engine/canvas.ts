// What <Canvas> does once it is in the document: measure its container,
// create and configure the root, render the scene and connect events. The
// behavior follows @react-three/fiber's web Canvas (MIT, Copyright (c)
// 2019-2025 Poimandres), see NOTICE.

import type { RenderProps } from './configuration.js';
import type { EventManager } from './events.js';
import { createRoot, type ReconcilerRoot } from './root.js';
import { rt, setRef } from './runtime.js';
import type { RootState, RootStore, Size } from './store.js';
import { isRef } from './utils.js';
import { createPointerEvents } from './web-events.js';

/** Options for measuring the canvas container (react-use-measure compatible). */
export interface ResizeOptions {
  /** Debounce resize/scroll updates in ms, or per kind */
  debounce?: number | { scroll: number; resize: number };
  /** React to scroll changes (updates top/left) */
  scroll?: boolean;
  /** A ResizeObserver polyfill */
  polyfill?: { new (cb: ResizeObserverCallback): ResizeObserver };
  /** Use offsetWidth/offsetHeight instead of the bounding rect (ignores transforms) */
  offsetSize?: boolean;
}

export interface CanvasProps extends Omit<RenderProps<HTMLCanvasElement>, 'size'> {
  /** Scene description (compiled children) */
  children?: unknown;
  /** Canvas element ref */
  ref?: unknown;
  /**
   * `false` keeps the whole scene on the runtime reconciler: the compiler does
   * not share geometries and materials, fold static matrices, instance repeated
   * meshes or write reactive transforms directly.
   */
  compile?: boolean;
  /** Canvas fallback content, similar to img's alt prop */
  fallback?: unknown;
  /** Options for measuring the container */
  resize?: ResizeOptions;
  /** The target where events are being subscribed to, default: the div that wraps canvas */
  eventSource?: HTMLElement | { current: HTMLElement | null };
  /** The event prefix that is cast into canvas pointer x/y events, default: "offset" */
  eventPrefix?: 'offset' | 'client' | 'page' | 'layer' | 'screen';
  [key: string]: unknown;
}

export interface CanvasElements {
  /** The outer div (event source by default) */
  div: HTMLDivElement;
  /** The measured container */
  container: HTMLDivElement;
  canvas: HTMLCanvasElement;
}

const CONFIG_KEYS = [
  'gl',
  'scene',
  'events',
  'shadows',
  'linear',
  'flat',
  'legacy',
  'orthographic',
  'frameloop',
  'dpr',
  'performance',
  'raycaster',
  'camera',
] as const;

function measure(el: HTMLElement, offsetSize?: boolean): Size {
  const rect = el.getBoundingClientRect();
  if (offsetSize) return { width: el.offsetWidth, height: el.offsetHeight, top: rect.top, left: rect.left };
  return { width: rect.width, height: rect.height, top: rect.top, left: rect.left };
}

/**
 * Calls fn in a task of its own (a MessageChannel message), once for any
 * number of calls made before it runs.
 *
 * A resize must not reach the renderer from a ResizeObserver callback: those
 * run after the frame's requestAnimationFrame callbacks and before its paint,
 * and gl.setSize clears the drawing buffer, so the browser painted the cleared
 * canvas before the next frame drew it (blank frames while the Genesis builder's
 * split divider is dragged). @react-three/fiber gets the size from
 * react-use-measure, whose observer sets React state, and React renders that
 * update in a scheduler task: after the paint, before the next frame's render.
 */
function inTask(fn: () => void): (() => void) & { cancel: () => void } {
  if (typeof MessageChannel === 'undefined') return debounced(fn, 1);
  let pending = false;
  const channel = new MessageChannel();
  channel.port1.onmessage = () => {
    pending = false;
    fn();
  };
  const run = (): void => {
    if (pending) return;
    pending = true;
    channel.port2.postMessage(null);
  };
  return Object.assign(run, {
    cancel: () => {
      pending = false;
      channel.port1.onmessage = null;
      channel.port1.close();
    },
  });
}

/**
 * fn after ms of quiet, or (ms 0, the resize default) in a task of its own
 * (see inTask). Shared by the island Canvas.
 */
export function debounced(fn: () => void, ms: number): (() => void) & { cancel: () => void } {
  if (!ms) return inTask(fn);
  let t: ReturnType<typeof setTimeout> | undefined;
  const run = (): void => {
    if (t) clearTimeout(t);
    t = setTimeout(fn, ms);
  };
  return Object.assign(run, { cancel: () => t && clearTimeout(t) });
}

/**
 * Mounts a canvas: `getProps` returns the current Canvas props (reading it
 * subscribes to the props that come from signals, so a changed prop
 * reconfigures the root). Returns the teardown.
 */
export function mountCanvas(els: CanvasElements, getProps: () => CanvasProps): () => void {
  const runtime = rt();
  let root: ReconcilerRoot<HTMLCanvasElement> | null = null;
  let rootState: RootState | null = null;
  let rendered = false;
  let disposed = false;
  const [size, setSize] = runtime.createSignal<Size>(measure(els.container, getProps().resize?.offsetSize));

  const eventTarget = (props: CanvasProps): HTMLElement => {
    const source = props.eventSource;
    if (!source) return els.div;
    return (isRef(source) ? source.current : source) ?? els.div;
  };

  const releaseRef = setRef(runtime.untrack(() => getProps().ref), els.canvas);

  // Measure the container like react-use-measure (scroll updates top/left).
  const resize = runtime.untrack(() => getProps().resize) ?? {};
  const debounce = resize.debounce ?? { scroll: 50, resize: 0 };
  const resizeMs = typeof debounce === 'number' ? debounce : debounce.resize;
  const scrollMs = typeof debounce === 'number' ? debounce : debounce.scroll;
  const update = (): void => {
    if (disposed) return;
    const next = measure(els.container, resize.offsetSize);
    const prev = size();
    if (next.width !== prev.width || next.height !== prev.height || next.top !== prev.top || next.left !== prev.left) setSize(next);
  };
  const onResize = debounced(update, resizeMs);
  const onScroll = debounced(update, scrollMs);
  const RO = resize.polyfill ?? (typeof ResizeObserver !== 'undefined' ? ResizeObserver : undefined);
  const observer = RO ? new RO(() => onResize()) : null;
  observer?.observe(els.container);
  if (typeof window !== 'undefined') {
    window.addEventListener('resize', onResize);
    if (resize.scroll !== false) window.addEventListener('scroll', onScroll, { capture: true, passive: true });
  }

  const reportError = (err: unknown): void => {
    els.div.setAttribute('data-krate-three-error', '');
    console.error(err);
  };

  const dispose = runtime.createRoot((d) => {
    runtime.createEffect(() => {
      const props = getProps();
      const rect = size();
      const config: RenderProps<HTMLCanvasElement> = {};
      for (const key of CONFIG_KEYS) {
        const v = props[key];
        if (v !== undefined) (config as Record<string, unknown>)[key] = v;
      }
      if (!config.events) config.events = createPointerEvents as (store: RootStore) => EventManager<HTMLElement>;
      const onPointerMissed = props.onPointerMissed;
      const onCreated = props.onCreated;
      const eventPrefix = props.eventPrefix;
      const children = props.children;
      runtime.untrack(() => {
        if (disposed || !(rect.width > 0 && rect.height > 0)) return;
        if (!root) root = createRoot(els.canvas);
        const r = root;
        r.configure({
          ...config,
          size: rect,
          onPointerMissed: (event: MouseEvent) => onPointerMissed?.(event),
          onCreated: (state: RootState) => {
            rootState = state;
            state.events.connect?.(eventTarget(props));
            if (eventPrefix) {
              state.setEvents({
                compute: (event, s) => {
                  const x = (event as unknown as Record<string, number>)[eventPrefix + 'X'];
                  const y = (event as unknown as Record<string, number>)[eventPrefix + 'Y'];
                  s.pointer.set((x / s.size.width) * 2 - 1, -(y / s.size.height) * 2 + 1);
                  s.raycaster.setFromCamera(s.pointer, s.camera);
                },
              });
            }
            onCreated?.(state);
          },
        }).then(() => {
          if (disposed || rendered) return;
          rendered = true;
          try {
            r.render(children);
          } catch (err) {
            reportError(err);
          }
        }, reportError);
        // Keep the event target in sync (an eventSource ref set later).
        const state = rootState?.get();
        const target = eventTarget(props);
        if (state && target && state.events.connected !== target) state.events.connect?.(target);
      });
    });
    return d;
  });

  return () => {
    disposed = true;
    dispose();
    observer?.disconnect();
    onResize.cancel();
    onScroll.cancel();
    if (typeof window !== 'undefined') {
      window.removeEventListener('resize', onResize);
      window.removeEventListener('scroll', onScroll, { capture: true } as EventListenerOptions);
    }
    releaseRef();
    root?.unmount();
    root = null;
  };
}
