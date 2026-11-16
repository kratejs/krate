import { Scene } from 'three';

export interface Root { render(children: () => unknown[]): void }

export function createRoot(canvas: HTMLCanvasElement): Root {
  const scene: Scene = new Scene();
  return { render(children) { console.log('ENGINE_MARK', canvas, scene, children()); } };
}
