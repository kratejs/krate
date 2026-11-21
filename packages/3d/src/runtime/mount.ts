// Browser mount of a <Canvas>: scene creation plus the fallback tree walker.

import type { CanvasProps } from '../types.js';
import { setCurrentScene } from './context.js';
import type { FilamentSource } from './filament.js';
import { Scene3D, type SceneOptions } from './scene.js';
import { canvasStyle } from './canvas_style.js';
import { mountSceneTree } from './tree.js';

export { canvasStyle };

export interface CanvasMountProps extends CanvasProps {
  /** An already loaded Filament module or loader (tests, custom wasm location). */
  filament?: FilamentSource;
  /** URL of the filament.js the build copied into the site. */
  filamentUrl?: string;
  /** Base URL of the compiled .filamat files. */
  materialBase?: string;
  pixelRatio?: number;
  fetchBytes?: SceneOptions['fetchBytes'];
  onCreated?: (scene: Scene3D) => void;
}

export function sceneOptions(props: CanvasMountProps): SceneOptions {
  return {
    view: {
      shadows: props.shadows,
      bloom: props.bloom,
      ssao: props.ssao,
      antialias: props.antialias,
      toneMapping: props.toneMapping,
    },
    camera: { ...props.camera, exposure: props.exposure },
    filament: props.filament,
    filamentUrl: props.filamentUrl,
    materialBase: props.materialBase,
    pixelRatio: props.pixelRatio,
    fetchBytes: props.fetchBytes,
  };
}

/** Creates the scene on a canvas and mounts the children; returns the teardown. */
export function mountCanvas(canvas: HTMLCanvasElement, props: CanvasMountProps): () => void {
  const scene = new Scene3D(canvas, sceneOptions(props));
  setCurrentScene(scene);
  const unmount = mountSceneTree(scene, props.children);
  props.onCreated?.(scene);
  return () => {
    unmount();
    scene.dispose();
  };
}
