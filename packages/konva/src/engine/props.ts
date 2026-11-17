// Node creation and prop updates, ported from react-konva (makeUpdates.js and
// ReactKonvaHostConfig.js, MIT, Copyright (c) 2017-present Lavrenov Anton),
// see NOTICE. Both the Krate reconciler and the Preact island host use them.

import type KonvaType from 'konva';
import { Konva } from './konva.js';

export type KonvaNode = KonvaType.Node;
export type KonvaContainer = KonvaType.Container;
type Props = Record<string, unknown>;

const propsToSkip: Record<string, true> = {
  children: true,
  ref: true,
  key: true,
  style: true,
  forwardedRef: true,
  eventBatchFunc: true,
  unstable_applyCache: true,
  unstable_applyDrawHitFromCache: true,
};

/** The namespace of the Konva listeners the renderer binds. */
export const EVENTS_NAMESPACE = '.react-konva-event';

let zIndexWarningShowed = false;
let dragWarningShowed = false;
let strictMode = false;

/**
 * Strict mode: props are compared with the node's attributes, not only with
 * the previous props, so a node changed by drag or transform is reset to the
 * props on the next update.
 */
export function toggleStrictMode(value: boolean): void {
  strictMode = value;
}

const DRAGGABLE_WARNING = `ReactKonva: You have a Konva node with draggable = true and position defined but no onDragMove or onDragEnd events are handled.
Position of a node will be changed during drag&drop, so you should update state of the react app as well.
Consider to add onDragMove or onDragEnd events.
For more info see: https://github.com/konvajs/react-konva/issues/256
`;

const Z_INDEX_WARNING = `ReactKonva: You are using "zIndex" attribute for a Konva node.
react-konva may get confused with ordering. Just define correct order of elements in your render function of a component.
For more info see: https://github.com/konvajs/react-konva/issues/194
`;

const EMPTY: Props = {};

const isEvent = (key: string): boolean => key.slice(0, 2) === 'on';

/** The Konva event of an `on*` prop: onDragMove -> dragmove, onContentClick -> contentClick. */
export function eventName(key: string): string {
  const name = key.slice(2).toLowerCase();
  return name.slice(0, 7) === 'content' ? 'content' + name.charAt(7).toUpperCase() + name.slice(8) : name;
}

/**
 * Applies `props` to a node, given the props applied before: changed and
 * removed attributes are set (removed ones to undefined), then changed event
 * handlers are rebound in the renderer's namespace.
 */
export function applyNodeProps(node: KonvaNode, props: Props, oldProps: Props = EMPTY): void {
  if (!zIndexWarningShowed && 'zIndex' in props) {
    console.warn(Z_INDEX_WARNING);
    zIndexWarningShowed = true;
  }
  if (!dragWarningShowed && props.draggable) {
    const hasPosition = props.x !== undefined || props.y !== undefined;
    const hasEvents = props.onDragEnd || props.onDragMove;
    if (hasPosition && !hasEvents) {
      console.warn(DRAGGABLE_WARNING);
      dragWarningShowed = true;
    }
  }
  const updated: Props = {};
  let hasUpdates = false;
  for (const key in oldProps) {
    if (propsToSkip[key]) continue;
    const event = isEvent(key);
    if (event && oldProps[key] !== props[key]) node.off(eventName(key) + EVENTS_NAMESPACE);
    if (!event && !Object.prototype.hasOwnProperty.call(props, key)) {
      updated[key] = undefined;
      hasUpdates = true;
    }
  }
  const strict = strictMode || !!props._useStrictMode;
  const events: Record<string, (e: unknown) => void> = {};
  for (const key in props) {
    if (propsToSkip[key]) continue;
    const value = props[key];
    if (isEvent(key)) {
      if (oldProps[key] !== value && value) events[eventName(key)] = value as (e: unknown) => void;
    } else if (value !== oldProps[key] || (strict && value !== node.getAttr(key))) {
      hasUpdates = true;
      updated[key] = value;
    }
  }
  if (hasUpdates) {
    node.setAttrs(updated);
    updatePicture(node);
  }
  // Events after attributes: setting attributes can add listeners (draggable).
  for (const name in events) {
    node.off(name + EVENTS_NAMESPACE);
    node.on(name + EVENTS_NAMESPACE, events[name] as never);
  }
}

/** Redraws the node's layer when Konva does not draw automatically. */
export function updatePicture(node: KonvaNode): void {
  if (!Konva.autoDrawEnabled) {
    const drawing = node.getLayer() || node.getStage();
    drawing?.batchDraw();
  }
}

/** Creates the node of an element type (Rect, Layer, ...) with its props. */
export function createNode(type: string, props: Props): KonvaNode {
  let NodeClass = (Konva as unknown as Record<string, new (config: Props) => KonvaNode>)[type];
  if (!NodeClass) {
    console.error(
      `Konva has no node with the type ${type}. Group will be used instead. If you use minimal version of react-konva, just import required nodes into Konva: "import "konva/lib/shapes/${type}"  If you want to render DOM elements as part of canvas tree take a look into this demo: https://konvajs.github.io/docs/react/DOM_Portal.html`,
    );
    NodeClass = Konva.Group as unknown as new (config: Props) => KonvaNode;
  }
  const attrs: Props = {};
  const events: Props = {};
  for (const key in props) {
    if (key === 'ref' || key === 'children' || key === 'key') continue;
    if (isEvent(key)) events[key] = props[key];
    else attrs[key] = props[key];
  }
  const node = new NodeClass(attrs);
  applyNodeProps(node, events);
  return node;
}

/**
 * Adds `child` to `parent`, before `before` when given (react-konva's
 * appendChild and insertBefore).
 */
export function insertNode(parent: KonvaContainer, child: KonvaNode, before: KonvaNode | null): void {
  if (before && before.parent === parent && before !== child) {
    // _remove keeps a drag going, unlike remove().
    if (child.parent) (child as unknown as { _remove(): void })._remove();
    parent.add(child as never);
    child.setZIndex(before.getZIndex());
  } else if (child.parent === parent) {
    child.moveToTop();
  } else {
    parent.add(child as never);
  }
  updatePicture(parent);
}

/** Destroys a removed node and its listeners. */
export function destroyNode(child: KonvaNode): void {
  const parent = child.parent;
  offTree(child);
  child.destroy();
  if (parent) updatePicture(parent);
}

function offTree(node: KonvaNode): void {
  node.off(EVENTS_NAMESPACE);
  const kids = (node as KonvaContainer).children;
  if (kids) for (const k of kids) offTree(k);
}

/** Hides a node (a suspended subtree). */
export function hideNode(node: KonvaNode): void {
  node.hide();
  updatePicture(node);
}

/** Shows a hidden node again unless its props hide it. */
export function unhideNode(node: KonvaNode, props: Props): void {
  if (props.visible == null || props.visible) {
    node.show();
    updatePicture(node);
  }
}
