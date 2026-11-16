import { createSignal, Suspense } from '@krate/runtime';
import { Vector3 } from 'three';
import { Canvas } from '@krate/fakethree';
import { Box } from '../components/Box';

export default function Page() {
  const [n, setN] = createSignal(1);
  const [items] = createSignal([1, 2]);
  const origin = new Vector3(0, 0, 0);
  return (
    <main>
      <h1 id="title">scene</h1>
      <Canvas frameloop="demand">
        <ambientLight intensity={0.5} />
        <mesh position={[n(), 0, 0]} onClick={() => setN(n() + 1)} onPointerMissed={() => setN(0)} attach={(parent, self) => () => {}}>
          <boxGeometry args={[1, 1, 1]} />
          <meshStandardMaterial color={n() > 2 ? 'hotpink' : 'orange'} />
        </mesh>
        <group position={origin} rotation-y={Math.PI} />
        {n() > 3 && <pointLight position={[2, 2, 2]} />}
        {items().map((i) => <Box key={i} x={i} />)}
        <Box x={9} />
        <Suspense fallback={<mesh name="loading" scale={n()} />}>
          <Box x={3} />
        </Suspense>
      </Canvas>
    </main>
  );
}
