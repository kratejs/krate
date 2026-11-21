// glTF loading through Filament's gltfio. KTX2 textures are decoded by the
// Ktx2Provider that filament.js wires into loadResources, so no extra setup.

import { getCurrentScene } from './context.js';
import type { FAsset, FEntity } from './filament.js';
import { defaultFetch } from './materials.js';
import { rt } from './rt.js';
import type { Scene3D } from './scene.js';

export interface GLTFState {
  status: 'loading' | 'ready' | 'error';
  asset: FAsset | null;
  entities: FEntity[];
  error: unknown;
}

export interface GLTFResult {
  readonly status: GLTFState['status'];
  readonly asset: FAsset | null;
  readonly entities: FEntity[];
  readonly error: unknown;
}

export interface UseGLTFOptions {
  /** Add the loaded entities to the scene (default true). */
  add?: boolean;
  scene?: Scene3D;
}

/** Loads a .gltf/.glb into the current scene. Fields are reactive: read them inside an effect. */
export function useGLTF(url: string, options: UseGLTFOptions = {}): GLTFResult {
  const [state, setState] = rt().createSignal<GLTFState>({ status: 'loading', asset: null, entities: [], error: null });
  const fail = (error: unknown): void => setState({ status: 'error', asset: null, entities: [], error });
  const scene = options.scene ?? getCurrentScene();
  if (!scene) {
    fail(new Error('@krate/3d: useGLTF needs a <Canvas> scene'));
  } else {
    scene.whenReady(() => {
      const fetchBytes = scene.opts.fetchBytes ?? defaultFetch;
      void fetchBytes(url).then((bytes) => {
        if (scene.disposed) return;
        const loader = scene.getLoader();
        const asset = loader.createAsset(bytes);
        if (!asset) throw new Error(`@krate/3d: could not parse ${url}`);
        const base = new URL(url, typeof document !== 'undefined' ? document.baseURI : 'http://localhost/').href;
        asset.loadResources(
          () => {
            if (scene.disposed) return;
            const entities = asset.getEntities();
            if (options.add !== false) scene.fscene.addEntities(entities);
            scene.track(() => {
              scene.fscene.removeEntities(entities);
              loader.destroyAsset(asset);
            });
            setState({ status: 'ready', asset, entities, error: null });
          },
          null,
          base
        );
      }).catch(fail);
    });
  }
  return {
    get status() {
      return state().status;
    },
    get asset() {
      return state().asset;
    },
    get entities() {
      return state().entities;
    },
    get error() {
      return state().error;
    },
  };
}
