// @krate/three: a three.js renderer for Krate with the react-three-fiber API.
//
// This module is Krate source: <Canvas> compiles with the page. Everything
// else is implemented by the browser engine (`@krate/three/engine`), which
// is bundled like an npm dependency so that the app and the engine share one
// copy of three.

import * as engine from '@krate/three/engine';
import type { ThreeElements as EngineThreeElements } from '@krate/three/engine';

export { Canvas } from './canvas.js';
export type { CanvasProps } from './canvas.js';
export type * from '@krate/three/engine';
export type * as ReactThreeFiber from '@krate/three/engine';
export type * as KrateThree from '@krate/three/engine';

// Hooks (react-three-fiber names)
export const useThree = engine.useThree;
export const useFrame = engine.useFrame;
export const useLoader = engine.useLoader;
export const useGraph = engine.useGraph;
export const useStore = engine.useStore;
export const useInstanceHandle = engine.useInstanceHandle;

// Krate-style primitives: accessors instead of hook values
export const createThree = engine.createThree;
export const createFrame = engine.createFrame;
export const createLoader = engine.createLoader;
export const createGraph = engine.createGraph;

// Rendering
export const extend = engine.extend;
export const createPortal = engine.createPortal;
export const createRoot = engine.createRoot;
export const unmountComponentAtNode = engine.unmountComponentAtNode;
export const applyProps = engine.applyProps;
export const getRootState = engine.getRootState;
export const dispose = engine.dispose;
export const buildGraph = engine.buildGraph;
export const flushSync = engine.flushSync;
export const act = engine.act;
export const context = engine.context;
export const _roots = engine._roots;

// Frame loop
export const invalidate = engine.invalidate;
export const advance = engine.advance;
export const addEffect = engine.addEffect;
export const addAfterEffect = engine.addAfterEffect;
export const addTail = engine.addTail;
export const flushGlobalEffects = engine.flushGlobalEffects;

// Events
export const events = engine.events;
export const createEvents = engine.createEvents;
export const createPointerEvents = engine.createPointerEvents;

/**
 * The scene elements. Augment it to type elements registered with extend():
 * `declare module '@krate/three' { interface ThreeElements { orbitControls: ThreeElement<typeof OrbitControls> } }`
 */
export interface ThreeElements extends EngineThreeElements {}

declare global {
  namespace JSX {
    interface IntrinsicElements extends ThreeElements {}
  }
}
