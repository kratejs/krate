// Image-based lighting from a Filament KTX1 cubemap (cmgen output).

import { getCurrentScene } from './context.js';
import { defaultFetch } from './materials.js';
import type { Scene3D } from './scene.js';
import type { EnvironmentProps } from '../types.js';

const presets: Record<string, string> = {};

/** Maps preset names (`<Environment preset="studio" />`) to KTX URLs you host. */
export function setEnvironmentPresets(map: Record<string, string>): void {
  Object.assign(presets, map);
}

export const DEFAULT_IBL_INTENSITY = 30000;

/** Loads the IBL (and, with `background`, the matching `_skybox.ktx`) into the scene. */
export function createEnvironment(scene: Scene3D, props: EnvironmentProps): void {
  const url = props.files ?? (props.preset ? presets[props.preset] : undefined);
  if (!url) {
    throw new Error(
      props.preset
        ? `@krate/3d: no URL registered for Environment preset "${props.preset}"; call setEnvironmentPresets() or pass files`
        : '@krate/3d: <Environment> needs `files` or `preset`'
    );
  }
  if (/\.ktx2(\?|$)/i.test(url)) {
    throw new Error('@krate/3d: Environment needs a KTX1 cubemap (.ktx from Filament cmgen); KTX2 is not supported for IBL');
  }
  scene.whenReady(() => {
    const fetchBytes = scene.opts.fetchBytes ?? defaultFetch;
    const skyUrl = url.replace(/_ibl\.ktx/i, '_skybox.ktx');
    void Promise.all([fetchBytes(url), props.background && skyUrl !== url ? fetchBytes(skyUrl) : undefined])
      .then(([iblBytes, skyBytes]) => {
        if (scene.disposed) return;
        const ibl = scene.engine.createIblFromKtx1(iblBytes);
        ibl.setIntensity(props.intensity ?? DEFAULT_IBL_INTENSITY);
        scene.setEnvironment(ibl, skyBytes ? scene.engine.createSkyFromKtx1(skyBytes) : undefined);
      })
      .catch((err) => (scene.opts.onError ?? console.error)(err));
  });
}

/** `<Environment />` inside a Canvas. */
export function Environment(props: EnvironmentProps = {}): null {
  const scene = getCurrentScene();
  if (!scene) throw new Error('@krate/3d: <Environment> must be used inside a <Canvas>');
  createEnvironment(scene, props);
  return null;
}
