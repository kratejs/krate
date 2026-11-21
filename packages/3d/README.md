# @krate/3d

Physically based 3D for Krate, rendered by Google's [Filament](https://github.com/google/filament)
engine (WebGL2). You write Three.js-style JSX; the Krate compiler flattens
static transforms, batches identical meshes and turns reactive props into
direct engine writes.

## Install

```sh
pnpm add @krate/3d filament
```

`filament` provides the WebAssembly engine and is a peer dependency, so it is
never bundled into `@krate/3d` or the `krate` binary: the Krate build copies its
`filament.js` and `filament.wasm` into `dist/assets/3d/` and the page loads them
with a script tag. Its version must match `materials/VERSION`, because the
pre-compiled materials are version-locked (the build warns, KR8702, otherwise).

## Example

```tsx
import { createSignal } from '@krate/runtime';
import { Canvas } from '@krate/3d';

export default function Page() {
  const [rotY, setRotY] = createSignal(0);
  return (
    <Canvas shadows bloom>
      <mesh rotation={[0, rotY(), 0]} onClick={() => setRotY(rotY() + 0.5)}>
        <boxGeometry />
        <meshStandardMaterial color="orange" roughness={0.4} />
      </mesh>
      <directionalLight intensity={1.5} position={[5, 5, 5]} castShadow />
    </Canvas>
  );
}
```

## Materials

`materials/*.filamat` are compiled from `materials/src/*.mat`. Rebuild them with
`pnpm run build:materials` (needs Filament's `matc`, found through
`KRATE_FILAMENT_MATC` or `~/.krate/tools/filament/<version>/bin/matc`).

See [DESIGN.md](./DESIGN.md) for the architecture.
