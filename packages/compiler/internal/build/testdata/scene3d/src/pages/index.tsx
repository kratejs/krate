// Compiled @krate/3d scene: static flattening, a signal-driven mesh, constant
// <For> instancing, calibrated lights and a custom shaderMaterial.
import { createSignal } from "@krate/runtime";
import { Canvas, OrbitControls } from "@krate/3d";

const GRID = [
  [-2, 0],
  [-1, 0],
  [0, 0],
  [1, 0],
];
const TURN = Math.PI / 2;

const WAVE = `
  float w = 0.5 + 0.5 * sin(materialParams.speed * 3.0);
  material.baseColor = vec4(materialParams.tint * w, 1.0);
`;

export default function Page() {
  const [rotY, setRotY] = createSignal(0);
  return (
    <main>
      <button id="spin" onClick={() => setRotY(rotY() + 0.5)}>spin</button>
      <div style={{ width: "400px", height: "300px" }}>
        <Canvas id="scene" shadows camera={{ position: [0, 2, 6] }}>
          <group position={[0, 1, 0]} rotation={[0, TURN, 0]}>
            <mesh position={[2, 0, 0]}>
              <boxGeometry />
              <meshStandardMaterial color="orange" roughness={0.4} />
            </mesh>
            <mesh rotation={[0, rotY(), 0]} castShadow onClick={() => setRotY(rotY() + 0.5)}>
              <sphereGeometry args={[1, 32, 16]} />
              <meshStandardMaterial color="#4488ff" />
            </mesh>
          </group>
          {GRID.map((p) => (
            <mesh position={[p[0], -1, p[1]]} scale={0.4}>
              <boxGeometry />
              <meshStandardMaterial color="gold" />
            </mesh>
          ))}
          <mesh position={[0, 0, -2]}>
            <planeGeometry args={[2, 2]} />
            <shaderMaterial fragmentShader={WAVE} uniforms={{ speed: 1, tint: [1, 0.5, 0.2] }} />
          </mesh>
          <directionalLight intensity={1.5} position={[5, 5, 5]} castShadow />
          <ambientLight intensity={0.2} />
          <OrbitControls />
        </Canvas>
      </div>
    </main>
  );
}
