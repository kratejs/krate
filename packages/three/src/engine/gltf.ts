// useGLTF of @krate/three/drei, in its own module so that pages which do not
// load glTF files do not bundle the loader and its decoders. Behavior follows
// @react-three/drei 10 (MIT, Copyright (c) 2020 Poimandres), see NOTICE.

import { DRACOLoader } from 'three/examples/jsm/loaders/DRACOLoader.js';
import { GLTFLoader, type GLTF } from 'three/examples/jsm/loaders/GLTFLoader.js';
import { MeshoptDecoder } from 'three/examples/jsm/libs/meshopt_decoder.module.js';
import { useLoader } from './hooks.js';
import type { ObjectMap } from './utils.js';

let dracoLoader: DRACOLoader | null = null;
let decoderPath = 'https://www.gstatic.com/draco/versioned/decoders/1.5.5/';

type GLTFExtend = (loader: GLTFLoader) => void;

function gltfExtensions(useDraco: boolean | string = true, useMeshopt = true, extendLoader?: GLTFExtend) {
  return (loader: GLTFLoader): void => {
    if (extendLoader) extendLoader(loader);
    if (useDraco) {
      if (!dracoLoader) dracoLoader = new DRACOLoader();
      dracoLoader.setDecoderPath(typeof useDraco === 'string' ? useDraco : decoderPath);
      loader.setDRACOLoader(dracoLoader);
    }
    if (useMeshopt) {
      const decoder = MeshoptDecoder as unknown;
      loader.setMeshoptDecoder((typeof decoder === 'function' ? (decoder as () => unknown)() : decoder) as never);
    }
  };
}

/** A loaded glTF with the named nodes, materials and meshes of its scene. */
export type GLTFResult = GLTF & ObjectMap;

interface UseGLTF {
  <T extends string | string[]>(
    path: T,
    useDraco?: boolean | string,
    useMeshopt?: boolean,
    extendLoader?: GLTFExtend,
  ): T extends string[] ? GLTFResult[] : GLTFResult;
  preload(path: string | string[], useDraco?: boolean | string, useMeshopt?: boolean, extendLoader?: GLTFExtend): void;
  clear(path: string | string[]): void;
  setDecoderPath(path: string): void;
}

/**
 * Loads a glTF with three's GLTFLoader, cached like useLoader, with a Draco
 * decoder (`useDraco`, from the gstatic CDN unless a path is given) and the
 * Meshopt decoder (`useMeshopt`), as @react-three/drei's useGLTF does.
 */
export const useGLTF = ((path: string | string[], useDraco?: boolean | string, useMeshopt?: boolean, extendLoader?: GLTFExtend) =>
  useLoader(GLTFLoader, path as string, gltfExtensions(useDraco, useMeshopt, extendLoader) as never)) as unknown as UseGLTF;

useGLTF.preload = (path, useDraco, useMeshopt, extendLoader) =>
  useLoader.preload(GLTFLoader, path as string, gltfExtensions(useDraco, useMeshopt, extendLoader) as never);
useGLTF.clear = (path) => useLoader.clear(GLTFLoader, path as string);
useGLTF.setDecoderPath = (path) => {
  decoderPath = path;
};
