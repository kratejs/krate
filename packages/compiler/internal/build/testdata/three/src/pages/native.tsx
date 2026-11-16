// Krate-style scene: signals and getters, a ref variable, and the
// @krate/three hooks read as getters.
import { createSignal } from "@krate/runtime";
import { Canvas, useFrame, useThree } from "@krate/three";
import type { Mesh } from "three";

function Spinner(props: { speed: number }) {
  let mesh: Mesh | undefined;
  const camera = useThree((s) => s.camera);
  useFrame((_state, delta) => {
    if (mesh) mesh.rotation.x += delta * props.speed;
    (window as any).__camera = camera();
  });
  return (
    <mesh ref={mesh} name="spinner">
      <torusGeometry args={[1, 0.3, 16, 32]} />
      <meshNormalMaterial />
    </mesh>
  );
}

export default function Native() {
  const [x, setX] = createSignal(0);
  return (
    <main>
      <button id="move" onClick={() => setX(x() + 1)}>move</button>
      <div style={{ width: "300px", height: "200px" }}>
        <Canvas frameloop="demand" camera={{ position: [0, 0, 6], fov: 50 }} onCreated={(s) => ((window as any).__three = s)}>
          <group name="mover" position-x={x()}>
            <Spinner speed={2} />
          </group>
        </Canvas>
      </div>
    </main>
  );
}
