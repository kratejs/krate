// The browser engine of @krate/konva. It is bundled for the page like an npm
// dependency (package.json "krate.vendor"), so it shares one `konva` with the
// app and uses the page's Krate runtime.

export type { KonvaNodeEvents, KonvaNodeComponent, KonvaRef, KonvaElementAttributes, StageProps } from './types.js';
export { Konva } from './konva.js';
export {
  applyNodeProps,
  updatePicture,
  createNode,
  insertNode,
  destroyNode,
  hideNode,
  unhideNode,
  eventName,
  toggleStrictMode,
  EVENTS_NAMESPACE,
} from './props.js';
export type { KonvaNode, KonvaContainer } from './props.js';
export { mountTree } from './reconciler.js';
export type { MountedTree } from './reconciler.js';
export { mountStage, STAGE_DOM_PROPS } from './stage.js';
export { KonvaHostNode, KONVA_NS, createHostRoot, commitHostProps, unmountHost, konvaRef } from './host.js';
export { setRef } from './runtime.js';

/** react-konva's version this package follows. */
export const version = '19.3.2';
