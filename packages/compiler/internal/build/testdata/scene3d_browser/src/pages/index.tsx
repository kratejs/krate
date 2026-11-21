// Compiled @krate/3d scene for the real-engine browser test: a static box, a
// signal-driven clickable sphere, an instanced grid, a physical material and
// a transparent one, lit by a directional and an ambient light.
import { createSignal } from "@krate/runtime";
import { Canvas } from "@krate/3d";

const GRID = [
  [-2, 0],
  [-1, 0],
  [0, 0],
  [1, 0],
];

export default function Page() {
  const [rotY, setRotY] = createSignal(0);
  return (
    <main>
      <button id="spin" onClick={() => setRotY(rotY() + 0.5)}>spin</button>
      <div style={{ width: "400px", height: "300px" }}>
        <Canvas
          id="scene"
          shadows
          camera={{ position: [0, 0, 6] }}
          onCreated={(s) => {
            window.__scene = s;
          }}
        >
          <mesh position={[-2, 0.8, 0]}>
            <boxGeometry />
            <meshStandardMaterial color="orange" roughness={0.4} />
          </mesh>
          <mesh
            rotation={[0, rotY(), 0]}
            onClick={() => {
              window.__clicks = (window.__clicks || 0) + 1;
            }}
          >
            <sphereGeometry args={[1, 32, 16]} />
            <meshStandardMaterial color="#4488ff" />
          </mesh>
          <mesh position={[2, 0.8, 0]}>
            <sphereGeometry args={[0.6, 24, 12]} />
            <meshPhysicalMaterial color="white" clearcoat={1} metalness={0.8} roughness={0.2} />
          </mesh>
          <mesh position={[0, 1.6, 0]}>
            <planeGeometry args={[1, 1]} />
            <meshStandardMaterial color="red" transparent opacity={0.5} />
          </mesh>
          {GRID.map((p) => (
            <mesh position={[p[0], -1.6, p[1]]} scale={0.4}>
              <boxGeometry />
              <meshStandardMaterial color="gold" />
            </mesh>
          ))}
          <directionalLight intensity={1.5} position={[5, 5, 5]} castShadow />
          <ambientLight intensity={0.3} />
        </Canvas>
      </div>
    </main>
  );
}
