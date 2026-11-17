// @krate/konva inside a Preact island.
//
// A "use client" component of a server-rendered page runs as a Preact island
// (React semantics on preact/compat), and the build points its @krate/konva
// imports here. The API is react-konva's: <Stage> renders its children with
// Preact into the Konva.Stage through the engine's DOM-like host
// (engine/host.ts), and the elements are the Konva class names, as in
// react-konva. Behavior follows react-konva (MIT, Copyright (c) 2017-present
// Lavrenov Anton), see NOTICE.

import { createElement, options, render, type ComponentChildren, type VNode } from 'preact';
import { forwardRef } from 'preact/compat';
import { useImperativeHandle, useLayoutEffect, useRef } from 'preact/hooks';
import {
  applyNodeProps,
  commitHostProps,
  createHostRoot,
  EVENTS_NAMESPACE,
  Konva as KonvaClasses,
  KONVA_NS,
  KonvaHostNode,
  konvaRef,
  toggleStrictMode,
  unmountHost,
  version as engineVersion,
} from '@krate/konva/engine';
import type { KonvaNodeComponent, KonvaRef, StageProps as EngineStageProps } from '@krate/konva/engine';
import type Konva from 'konva';

export type { KonvaNodeEvents, KonvaNodeComponent, KonvaRef } from '@krate/konva/engine';

// Preact creates stage elements with createElementNS in the konva namespace
// (a child inherits its parent's namespace, and the stage root has it). Once
// Preact has diffed an element its props are applied to its Konva node, and
// its ref is pointed at the Konva node; an unmounted element is destroyed.
type HostVNode = VNode & { __e?: unknown };

const g = globalThis as { __krateKonvaIsland?: boolean };
if (typeof document !== 'undefined' && !g.__krateKonvaIsland) {
  g.__krateKonvaIsland = true;
  const createNS = document.createElementNS;
  document.createElementNS = function (this: Document, ns: string | null, name: string, opts?: unknown) {
    if (ns === KONVA_NS) return new KonvaHostNode(name) as unknown as Element;
    return (createNS as (...a: unknown[]) => Element).call(this, ns, name, opts);
  } as typeof document.createElementNS;
  const diffed = options.diffed;
  options.diffed = (vnode: HostVNode) => {
    const dom = vnode.__e;
    if (dom instanceof KonvaHostNode && typeof vnode.type === 'string') {
      (vnode as { ref?: unknown }).ref = konvaRef(vnode.ref) as never;
      commitHostProps(dom, vnode.props as Record<string, unknown>);
    }
    diffed?.(vnode);
  };
  const unmount = options.unmount;
  options.unmount = (vnode: HostVNode) => {
    const dom = vnode.__e;
    if (dom instanceof KonvaHostNode && typeof vnode.type === 'string') unmountHost(dom);
    unmount?.(vnode);
  };
}

// Passes the providers above the Stage (Preact's context object) to the tree
// rendered into the stage, as compat's portals do.
function ContextProvider(this: { getChildContext?: () => unknown }, props: { context: unknown; children?: ComponentChildren }) {
  this.getChildContext = () => props.context;
  return props.children as VNode;
}

export const Layer = 'Layer' as unknown as KonvaNodeComponent<Konva.Layer, Konva.LayerConfig>;
export const FastLayer = 'FastLayer' as unknown as KonvaNodeComponent<Konva.FastLayer, Konva.LayerConfig>;
export const Group = 'Group' as unknown as KonvaNodeComponent<Konva.Group, Konva.ContainerConfig>;
export const Label = 'Label' as unknown as KonvaNodeComponent<Konva.Label, Konva.LabelConfig>;
export const Rect = 'Rect' as unknown as KonvaNodeComponent<Konva.Rect, Konva.RectConfig>;
export const Circle = 'Circle' as unknown as KonvaNodeComponent<Konva.Circle, Konva.CircleConfig>;
export const Ellipse = 'Ellipse' as unknown as KonvaNodeComponent<Konva.Ellipse, Konva.EllipseConfig>;
export const Wedge = 'Wedge' as unknown as KonvaNodeComponent<Konva.Wedge, Konva.WedgeConfig>;
export const Transformer = 'Transformer' as unknown as KonvaNodeComponent<Konva.Transformer, Konva.TransformerConfig>;
export const Line = 'Line' as unknown as KonvaNodeComponent<Konva.Line, Konva.LineConfig>;
export const Sprite = 'Sprite' as unknown as KonvaNodeComponent<Konva.Sprite, Konva.SpriteConfig>;
export const Image = 'Image' as unknown as KonvaNodeComponent<Konva.Image, Konva.ImageConfig>;
export const Text = 'Text' as unknown as KonvaNodeComponent<Konva.Text, Konva.TextConfig>;
export const TextPath = 'TextPath' as unknown as KonvaNodeComponent<Konva.TextPath, Konva.TextPathConfig>;
export const Star = 'Star' as unknown as KonvaNodeComponent<Konva.Star, Konva.StarConfig>;
export const Ring = 'Ring' as unknown as KonvaNodeComponent<Konva.Ring, Konva.RingConfig>;
export const Arc = 'Arc' as unknown as KonvaNodeComponent<Konva.Arc, Konva.ArcConfig>;
export const Tag = 'Tag' as unknown as KonvaNodeComponent<Konva.Tag, Konva.TagConfig>;
export const Path = 'Path' as unknown as KonvaNodeComponent<Konva.Path, Konva.PathConfig>;
export const RegularPolygon = 'RegularPolygon' as unknown as KonvaNodeComponent<Konva.RegularPolygon, Konva.RegularPolygonConfig>;
export const Arrow = 'Arrow' as unknown as KonvaNodeComponent<Konva.Arrow, Konva.ArrowConfig>;
export const Shape = 'Shape' as unknown as KonvaNodeComponent<Konva.Shape, Konva.ShapeConfig>;

/** See react-konva: compare props with node attributes on every update. */
export const useStrictMode: (useStrictMode: boolean) => void = toggleStrictMode;

export const version: string = engineVersion;

function Bridge(props: { children?: ComponentChildren }) {
  return props.children as VNode;
}

/**
 * react-konva (its-fine) API: returns a component that passes the context
 * around the Stage to its children. Stage already passes it, so it renders
 * its children as they are.
 */
export function useContextBridge(): (props: { children?: unknown }) => any {
  return Bridge as (props: { children?: unknown }) => any;
}

/** Props of <Stage>: the Konva.Stage config and events plus the attributes of its container div. */
export interface StageProps extends EngineStageProps {
  ref?: KonvaRef<Konva.Stage>;
}

const DOM_PROPS = new Set(['className', 'class', 'id', 'role', 'tabIndex', 'title', 'accessKey']);

function StageWrap(props: StageProps & { forwardedRef?: unknown }, context: unknown) {
  const container = useRef<HTMLDivElement>(null);
  const state = useRef({
    stage: null as Konva.Stage | null,
    root: null as InstanceType<typeof KonvaHostNode> | null,
    prev: {} as Record<string, unknown>,
    batch: undefined as unknown,
  }).current;

  useLayoutEffect(() => {
    const stage = new KonvaClasses.Stage({ width: props.width!, height: props.height!, container: container.current! });
    state.stage = stage;
    state.root = createHostRoot(stage as unknown as Konva.Container);
    return () => {
      render(null, state.root as unknown as Element);
      stage.destroy();
      stage.off(EVENTS_NAMESPACE);
      state.stage = null;
      state.root = null;
      state.prev = {};
    };
  }, []);

  useImperativeHandle(props.forwardedRef as never, () => state.stage, []);

  useLayoutEffect(() => {
    const stage = state.stage!;
    const next: Record<string, unknown> = {};
    for (const key in props) if (key !== 'forwardedRef' && !DOM_PROPS.has(key)) next[key] = (props as Record<string, unknown>)[key];
    applyNodeProps(stage, next, state.prev);
    state.prev = next;
    if (props.eventBatchFunc !== state.batch) {
      state.batch = props.eventBatchFunc;
      const s = stage as unknown as { eventBatchFunc?: (f?: unknown) => unknown };
      if (typeof s.eventBatchFunc === 'function') s.eventBatchFunc(props.eventBatchFunc);
    }
    // Synchronous, so the parent's layout effects see the nodes and refs.
    render(createElement(ContextProvider, { context }, props.children as VNode), state.root as unknown as Element);
  });

  return createElement('div', {
    ref: container,
    id: props.id,
    accessKey: props.accessKey,
    className: props.className ?? props.class,
    role: props.role,
    style: props.style,
    tabIndex: props.tabIndex,
    title: props.title,
  });
}

/**
 * A DOM container holding a Konva.Stage that renders its children. Its ref
 * receives the Konva.Stage. On the server only the container div renders.
 */
export const Stage = forwardRef<Konva.Stage, StageProps>((props, ref) =>
  createElement(StageWrap as never, { ...props, forwardedRef: ref }),
) as unknown as KonvaNodeComponent<Konva.Stage, StageProps>;
