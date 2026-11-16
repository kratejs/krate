# @krate/three

A [three.js](https://threejs.org) renderer for [Krate](https://krate.js.org).
Scenes are written in JSX, with the API of
[react-three-fiber](https://github.com/pmndrs/react-three-fiber): `<Canvas>`,
every three class as an element, `useFrame`, `useThree`, `useLoader`,
pointer events, portals, `extend` and `createRoot`.

```sh
pnpm add @krate/three three
```

```tsx
import { useRef, useState } from 'react';
import { Canvas, useFrame } from '@krate/three';
import type { Mesh } from 'three';

function Box(props: { position: [number, number, number] }) {
  const ref = useRef<Mesh>(null);
  const [hovered, setHover] = useState(false);
  const [active, setActive] = useState(false);
  useFrame((_state, delta) => (ref.current!.rotation.x += delta));
  return (
    <mesh
      {...props}
      ref={ref}
      scale={active ? 1.5 : 1}
      onClick={() => setActive(!active)}
      onPointerOver={() => setHover(true)}
      onPointerOut={() => setHover(false)}
    >
      <boxGeometry args={[1, 1, 1]} />
      <meshStandardMaterial color={hovered ? 'hotpink' : 'orange'} />
    </mesh>
  );
}

export default function Page() {
  return (
    <Canvas>
      <ambientLight intensity={Math.PI / 2} />
      <pointLight position={[10, 10, 10]} decay={0} intensity={Math.PI} />
      <Box position={[-1.2, 0, 0]} />
      <Box position={[1.2, 0, 0]} />
    </Canvas>
  );
}
```

The scene is not part of the page's HTML: `<Canvas>` renders its wrapper and
`<canvas>` element on the server, and the scene mounts in the browser.

Documentation: https://krate.js.org/docs/features/three/

Parts of this package are ported from react-three-fiber (MIT, Copyright (c)
2019-2025 Poimandres); see [NOTICE](./NOTICE).
