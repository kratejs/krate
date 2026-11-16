// @krate/three inside a Preact island.
//
// A "use client" component of a server-rendered page runs as a Preact island
// (React semantics on preact/compat), and the build points its @krate/three
// imports here. The API is the same as @krate/three's React style, which is
// react-three-fiber's: <Canvas> renders its children with Preact into the
// scene through the engine's DOM-like host (engine/host.ts), hooks are
// Preact hooks, and everything else (root, renderer, events, frame loop,
// loaders) is the shared engine. Behavior follows @react-three/fiber (MIT,
// Copyright (c) 2019-2025 Poimandres), see NOTICE.

import { Component, createContext, createElement, options, render, type ComponentChildren, type VNode } from 'preact';
import { forwardRef, Suspense } from 'preact/compat';
import { useContext, useLayoutEffect, useMemo, useReducer, useRef, useState } from 'preact/hooks';
import * as engine from '@krate/three/engine';
import type { CanvasProps as EngineCanvasProps, Instance, RootState, RootStore } from '@krate/three/engine';
import type * as THREE from 'three';

export type * from '@krate/three/engine';
export type * as ReactThreeFiber from '@krate/three/engine';
export type * as KrateThree from '@krate/three/engine';

const { SceneNode, SCENE_NS, commitSceneProps, createSceneRoot, createPortalRoot } = engine;
type SceneNode = InstanceType<typeof SceneNode>;

// Preact creates scene elements with createElementNS in the scene namespace
// (a child inherits its parent's namespace, and the scene root has it), and
// diffs their props: the props of a scene element are applied to its object
// once Preact has diffed it, and its ref receives the object.
type HostVNode = VNode & { __e?: unknown };

const g = globalThis as { __krateThreeIsland?: boolean };
if (typeof document !== 'undefined' && !g.__krateThreeIsland) {
  g.__krateThreeIsland = true;
  const createNS = document.createElementNS;
  document.createElementNS = function (this: Document, ns: string | null, name: string, opts?: unknown) {
    if (ns === SCENE_NS) return new SceneNode(name) as unknown as Element;
    return (createNS as (...a: unknown[]) => Element).call(this, ns, name, opts);
  } as typeof document.createElementNS;
  const diffed = options.diffed;
  options.diffed = (vnode: HostVNode) => {
    const dom = vnode.__e;
    if (dom instanceof SceneNode && typeof vnode.type === 'string') {
      // The host passes the object (not the node) to the ref.
      dom.ref = vnode.ref ?? null;
      (vnode as { ref?: unknown }).ref = null;
      commitSceneProps(dom, vnode.props as Record<string, unknown>);
    }
    diffed?.(vnode);
  };
}

const StoreContext = createContext<RootStore | null>(null);

// Passes the providers above a component (Preact's context object) to a tree
// rendered into another container, as compat's portals do.
function ContextProvider(this: { getChildContext?: () => unknown }, props: { context: unknown; children?: ComponentChildren }) {
  this.getChildContext = () => props.context;
  return props.children as VNode;
}

/** Returns the root store of the scene the calling component renders in. */
export function useStore(): RootStore {
  const store = useContext(StoreContext);
  if (!store) throw new Error('@krate/three: Hooks can only be used within the Canvas component!');
  return store;
}

/**
 * Accesses the root state (renderer, camera, scene, size, ...). With a
 * selector, the component re-renders when the selected value changes.
 */
export function useThree<T = RootState>(
  selector: (state: RootState) => T = (state) => state as unknown as T,
  equalityFn?: (a: T, b: T) => boolean,
): T {
  const store = useStore();
  const [, force] = useReducer((n: number) => n + 1, 0);
  const slice = selector(store.getState());
  const latest = useRef({ selector, equalityFn, slice });
  latest.current = { selector, equalityFn, slice };
  useLayoutEffect(
    () =>
      store.subscribe(() => {
        const { selector: select, equalityFn: equal, slice: prev } = latest.current;
        let next: T;
        try {
          next = select(store.getState());
        } catch {
          force(0);
          return;
        }
        if (!(equal ?? Object.is)(prev, next)) force(0);
      }),
    [store],
  );
  return slice;
}

/**
 * Runs `callback` every frame before rendering. A positive `renderPriority`
 * takes over rendering.
 */
export function useFrame(callback: engine.RenderCallback, renderPriority = 0): null {
  const store = useStore();
  const ref = useRef(callback);
  useLayoutEffect(() => {
    ref.current = callback;
  }, [callback]);
  useLayoutEffect(() => store.getState().internal.subscribe(ref, renderPriority, store), [renderPriority, store]);
  return null;
}

/** Returns the named nodes, materials and meshes of an object tree. */
export function useGraph(object: THREE.Object3D): engine.ObjectMap {
  return useMemo(() => engine.buildGraph(object), [object]);
}

export const useLoader = engine.useLoader;
export const useInstanceHandle = engine.useInstanceHandle;

/** Renders children into another object (and, with `state`, a layered root state). */
export function createPortal(children: ComponentChildren, container: THREE.Object3D, state?: Record<string, unknown>): VNode {
  return createElement(Portal as never, { container, state, children }) as VNode;
}

function Portal(props: { container: THREE.Object3D; state?: Record<string, unknown>; children?: ComponentChildren }, context: unknown) {
  const parent = useStore();
  const portal = useMemo(() => {
    const layered = props.state ? engine.createPortalState(parent, props.container, props.state) : null;
    const store = layered?.store ?? parent;
    return { node: createPortalRoot(props.container, store), store, unsubscribe: layered?.unsubscribe };
  }, [parent, props.container]);
  useLayoutEffect(() => {
    render(
      createElement(ContextProvider, { context }, createElement(StoreContext.Provider, { value: portal.store }, props.children as VNode)),
      portal.node as unknown as Element,
    );
  });
  useLayoutEffect(
    () => () => {
      render(null, portal.node as unknown as Element);
      portal.unsubscribe?.();
    },
    [portal],
  );
  return null;
}

// Engine exports
export const extend = engine.extend;
export const createRoot = engine.createRoot;
export const unmountComponentAtNode = engine.unmountComponentAtNode;
export const applyProps = engine.applyProps;
export const getRootState = engine.getRootState;
export const dispose = engine.dispose;
export const buildGraph = engine.buildGraph;
export const flushSync = engine.flushSync;
export const act = engine.act;
export const _roots = engine._roots;
export const invalidate = engine.invalidate;
export const advance = engine.advance;
export const addEffect = engine.addEffect;
export const addAfterEffect = engine.addAfterEffect;
export const addTail = engine.addTail;
export const flushGlobalEffects = engine.flushGlobalEffects;
export const events = engine.events;
export const createEvents = engine.createEvents;
export const createPointerEvents = engine.createPointerEvents;
/** The scene store as a React context (`useContext(context)`). */
export const context = StoreContext;

/** Props of <Canvas>: renderer options plus the attributes of its wrapping div. */
export interface CanvasProps extends EngineCanvasProps {
  id?: string;
  className?: string;
  style?: Record<string, string | number>;
  children?: ComponentChildren;
}

const CANVAS_OWN = new Set([
  'children',
  'fallback',
  'resize',
  'style',
  'gl',
  'events',
  'eventSource',
  'eventPrefix',
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
  'scene',
  'onPointerMissed',
  'onCreated',
  'ref',
]);

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

// Catches what the scene throws and hands it to <Canvas>, which throws it in
// the page's tree (a scene error reaches the error boundary around <Canvas>).
class SceneErrorBoundary extends Component<{ set: (e: unknown) => void; children?: ComponentChildren }, { error: boolean }> {
  state = { error: false };
  componentDidCatch(error: unknown) {
    this.setState({ error: true });
    this.props.set(error);
  }
  render() {
    return this.state.error ? null : (this.props.children as VNode);
  }
}

function measure(el: HTMLElement, offsetSize?: boolean): engine.Size {
  const rect = el.getBoundingClientRect();
  if (offsetSize) return { width: el.offsetWidth, height: el.offsetHeight, top: rect.top, left: rect.left };
  return { width: rect.width, height: rect.height, top: rect.top, left: rect.left };
}

function debounced(fn: () => void, ms: number): (() => void) & { cancel: () => void } {
  if (!ms) return Object.assign(fn, { cancel: () => {} });
  let t: ReturnType<typeof setTimeout> | undefined;
  return Object.assign(
    () => {
      if (t) clearTimeout(t);
      t = setTimeout(fn, ms);
    },
    { cancel: () => t && clearTimeout(t) },
  );
}

const sameSize = (a: engine.Size | null, b: engine.Size): boolean =>
  !!a && a.width === b.width && a.height === b.height && a.top === b.top && a.left === b.left;

function CanvasImpl(props: CanvasProps & { forwardedRef?: unknown }, context: unknown) {
  const div = useRef<HTMLDivElement>(null);
  const container = useRef<HTMLDivElement>(null);
  const canvas = useRef<HTMLCanvasElement>(null);
  const [error, setError] = useState<unknown>(null);
  if (error) throw error;
  const [size, setSize] = useState<engine.Size | null>(null);
  const [root] = useState(() => ({
    current: null as engine.ReconcilerRoot<HTMLCanvasElement> | null,
    scene: null as null | { node: SceneNode; store: RootStore },
    rendered: false,
    disposed: false,
  }));
  const live = useRef({ props, context });
  live.current = { props, context };

  const renderScene = (): void => {
    const s = root.scene;
    if (!s) return;
    const { props: p, context: c } = live.current;
    render(
      createElement(
        ContextProvider,
        { context: c },
        createElement(
          StoreContext.Provider,
          { value: s.store },
          createElement(SceneErrorBoundary, { set: setError }, createElement(Suspense, { fallback: null }, p.children as VNode)),
        ),
      ),
      s.node as unknown as Element,
    );
  };

  // Measure the container like react-use-measure (scroll updates top/left).
  useLayoutEffect(() => {
    const el = container.current!;
    const resize = live.current.props.resize ?? {};
    const debounce = resize.debounce ?? { scroll: 50, resize: 0 };
    const update = (): void => {
      if (root.disposed) return;
      const next = measure(el, resize.offsetSize);
      setSize((prev) => (sameSize(prev, next) ? prev : next));
    };
    const onResize = debounced(update, typeof debounce === 'number' ? debounce : debounce.resize);
    const onScroll = debounced(update, typeof debounce === 'number' ? debounce : debounce.scroll);
    const RO = resize.polyfill ?? (typeof ResizeObserver !== 'undefined' ? ResizeObserver : undefined);
    const observer = RO ? new RO(() => onResize()) : null;
    observer?.observe(el);
    window.addEventListener('resize', onResize);
    if (resize.scroll !== false) window.addEventListener('scroll', onScroll, { capture: true, passive: true });
    update();
    return () => {
      observer?.disconnect();
      onResize.cancel();
      onScroll.cancel();
      window.removeEventListener('resize', onResize);
      window.removeEventListener('scroll', onScroll, { capture: true } as EventListenerOptions);
    };
  }, []);

  const eventTarget = (p: CanvasProps): HTMLElement => {
    const source = p.eventSource;
    if (!source) return div.current!;
    return (engine.isRef(source) ? source.current : source) ?? div.current!;
  };

  // Configure the root on every render (only changed inputs are applied) and
  // render the scene once it is configured; later renders re-render it.
  useLayoutEffect(() => {
    if (!size || !(size.width > 0 && size.height > 0)) return;
    if (!root.current) root.current = engine.createRoot(canvas.current!);
    const r = root.current;
    const p = props;
    const config: Record<string, unknown> = {};
    for (const key of CONFIG_KEYS) if (p[key] !== undefined) config[key] = p[key];
    if (!config.events) config.events = engine.createPointerEvents;
    const prefix = p.eventPrefix;
    r.configure({
      ...config,
      size,
      onPointerMissed: (event: MouseEvent) => live.current.props.onPointerMissed?.(event),
      onCreated: (state: RootState) => {
        state.events.connect?.(eventTarget(live.current.props));
        if (prefix) {
          state.setEvents({
            compute: (event, s) => {
              const x = (event as unknown as Record<string, number>)[prefix + 'X'];
              const y = (event as unknown as Record<string, number>)[prefix + 'Y'];
              s.pointer.set((x / s.size.width) * 2 - 1, -(y / s.size.height) * 2 + 1);
              s.raycaster.setFromCamera(s.pointer, s.camera);
            },
          });
        }
        live.current.props.onCreated?.(state);
      },
    } as never).then(
      () => {
        if (root.disposed) return;
        if (!root.rendered) {
          root.rendered = true;
          r.render({
            __k3mount: (scene: Instance, store: RootStore) => {
              const node = createSceneRoot(scene, store);
              root.scene = { node, store };
              renderScene();
              return () => {
                render(null, node as unknown as Element);
                if (root.scene?.node === node) root.scene = null;
              };
            },
          });
        } else {
          renderScene();
        }
        const state = root.scene?.store.getState();
        const target = eventTarget(live.current.props);
        if (state && target && state.events.connected !== target) state.events.connect?.(target);
      },
      (err: unknown) => setError(err ?? new Error('@krate/three: the renderer could not be created')),
    );
  });

  // The canvas ref, and teardown.
  useLayoutEffect(() => {
    const release = engine.setRef(live.current.props.forwardedRef, canvas.current);
    return () => {
      root.disposed = true;
      release();
      root.current?.unmount();
      root.current = null;
    };
  }, []);

  const rest: Record<string, unknown> = {};
  for (const key in props) if (!CANVAS_OWN.has(key) && key !== 'forwardedRef') rest[key] = (props as Record<string, unknown>)[key];
  return createElement(
    'div',
    {
      ...rest,
      ref: div,
      style: {
        position: 'relative',
        width: '100%',
        height: '100%',
        overflow: 'hidden',
        pointerEvents: props.eventSource ? 'none' : 'auto',
        ...props.style,
      },
    },
    createElement(
      'div',
      { ref: container, style: { width: '100%', height: '100%' } },
      createElement('canvas', { ref: canvas, style: { display: 'block' } }, props.fallback as VNode),
    ),
  );
}

/**
 * A DOM canvas that renders its three.js children. The scene mounts in the
 * browser once the canvas has a size; on the server only the wrapper and the
 * canvas element render.
 */
export const Canvas = forwardRef<HTMLCanvasElement, CanvasProps>((props, ref) =>
  createElement(CanvasImpl as never, { ...props, forwardedRef: ref }),
);
