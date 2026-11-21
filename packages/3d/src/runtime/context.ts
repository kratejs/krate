// The scene hooks such as useGLTF and useFrame attach to.

import type { Scene3D } from './scene.js';

let current: Scene3D | undefined;

export function setCurrentScene(scene: Scene3D | undefined): void {
  current = scene;
}

/** The most recently created live scene. */
export function getCurrentScene(): Scene3D | undefined {
  return current && !current.disposed ? current : undefined;
}
