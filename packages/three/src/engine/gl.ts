// Renderer creation and release. Ported from @react-three/fiber (MIT,
// Copyright (c) 2019-2025 Poimandres), see NOTICE.

import * as THREE from 'three';
import type { DefaultGLProps, GLProps } from './configuration.js';
import { isPromiseLike } from './promise.js';
import type { Renderer } from './store.js';

export const isRenderer = (def: any): def is Renderer => !!def?.render;

/** The renderer to configure, or a promise for one from an async factory. */
export function createRenderer(canvas: DefaultGLProps['canvas'], gl?: GLProps): Renderer | PromiseLike<Renderer> {
  const defaults: DefaultGLProps = { canvas, powerPreference: 'high-performance', antialias: true, alpha: true };
  if (typeof gl === 'function') return (gl as (d: DefaultGLProps) => Renderer | PromiseLike<Renderer>)(defaults);
  if (isRenderer(gl)) return gl;
  return new THREE.WebGLRenderer({ ...defaults, ...(gl as THREE.WebGLRendererParameters) } as THREE.WebGLRendererParameters);
}

function attempt<T>(step: () => T): T | undefined {
  try {
    return step();
  } catch (error) {
    console.warn('[@krate/three] Error while unmounting root; teardown may be incomplete:', error);
  }
}

// WebGLRenderer.dispose() keeps its context until garbage collection, and
// browsers cap live contexts, so the context is released explicitly.
function disposeRenderer(gl: any): void | Promise<void> {
  if (gl.hasInitialized?.() === false) return;
  const disposed = attempt(() => (gl.dispose ? gl.dispose() : gl.renderLists?.dispose?.()));
  if (isPromiseLike(disposed)) {
    return Promise.resolve(disposed)
      .catch((error) => console.warn('[@krate/three] Error disposing renderer', error))
      .then(() => void attempt(() => gl.forceContextLoss?.()));
  }
  attempt(() => gl.forceContextLoss?.());
}

/** Owned renderers are disposed. Caller-provided renderers release caches and their context. */
export function releaseRenderer(gl: THREE.WebGLRenderer | undefined, owned: boolean): void | Promise<void> {
  if (!gl) return;
  if (owned) return disposeRenderer(gl);
  attempt(() => (gl as any).renderLists?.dispose?.());
  attempt(() => (gl as any).forceContextLoss?.());
}
