// A scene the compile-time optimizer handles: shared geometry and materials,
// static matrices, instanced meshes (written out and mapped over a constant
// array), reactive transforms written directly, a mesh with a handler, and
// runtime-path pieces (a ref, a component using useFrame, lights) beside them.
import { createSignal } from "@krate/runtime";
import { Canvas, useFrame } from "@krate/three";

const GRID = [
  { x: -2, z: -1 }, { x: 0, z: -1 }, { x: 2, z: -1 },
  { x: -2, z: 1 }, { x: 0, z: 1 }, { x: 2, z: 1 },
  { x: -2, z: 3 }, { x: 0, z: 3 }, { x: 2, z: 3 },
];
const RING = [0, 1, 2, 3, 4, 5];

function Probe() {
  useFrame(() => {
    const w = window as any;
    w.__frames = (w.__frames || 0) + 1;
  });
  return null;
}

export default function Page() {
  const [spin, setSpin] = createSignal(0);
  let viaRef: any;
  return (
    <main>
      <button id="spin" onClick={() => setSpin(spin() + 0.5)}>spin</button>
      <div style={{ width: "400px", height: "300px" }}>
        <Canvas id="scene" camera={{ position: [0, 5, 9] }} onCreated={(s) => ((window as any).__three = s)}>
          <ambientLight intensity={1.5} />
          <directionalLight position={[3, 5, 2]} intensity={2} />
          {GRID.map((p) => (
            <mesh position={[p.x, 0, p.z]}>
              <boxGeometry args={[1, 1, 1]} />
              <meshStandardMaterial color="orange" roughness={0.4} />
            </mesh>
          ))}
          <mesh position={[-3, 2, 0]}>
            <sphereGeometry args={[0.5, 16, 16]} />
            <meshStandardMaterial color="steelblue" />
          </mesh>
          <mesh position={[3, 2, 0]}>
            <sphereGeometry args={[0.5, 16, 16]} />
            <meshStandardMaterial color="steelblue" />
          </mesh>
          <mesh name="clickable" position={[0, 2, 0]} onClick={() => ((window as any).__clicks = ((window as any).__clicks || 0) + 1)}>
            <sphereGeometry args={[0.5, 16, 16]} />
            <meshStandardMaterial color="steelblue" />
          </mesh>
          <group name="outer" position={[0, 0.5, -3]} scale={1.5}>
            <mesh name="inner" position={[0.5, 0, 0]}>
              <boxGeometry args={[0.5, 0.5, 0.5]} />
              <meshStandardMaterial color="green" />
            </mesh>
          </group>
          <group name="spinner" position={[0, 3, 0]} rotation={[0, spin(), 0]}>
            <mesh name="arm" position={[1, 0, 0]}>
              <boxGeometry args={[1, 0.2, 0.2]} />
              <meshStandardMaterial color="hotpink" />
            </mesh>
          </group>
          {RING.map((a) => (
            <mesh position={[Math.cos(a) * 3, 1, Math.sin(a) * 3 - 3]} rotation={[0, spin() + a, 0]}>
              <boxGeometry args={[0.3, 0.3, 0.3]} />
              <meshStandardMaterial color="purple" />
            </mesh>
          ))}
          <mesh name="byref" ref={viaRef} position={[0, -1, 0]}>
            <boxGeometry args={[1, 0.2, 1]} />
            <meshStandardMaterial color="white" />
          </mesh>
          <Probe />
        </Canvas>
      </div>
    </main>
  );
}
