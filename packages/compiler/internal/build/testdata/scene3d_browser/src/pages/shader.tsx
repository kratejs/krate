// A build-compiled shaderMaterial on the real engine: a flat magenta plane
// whose brightness is a reactive float uniform.
import { createSignal } from "@krate/runtime";
import { Canvas } from "@krate/3d";

const FRAG = `
  material.baseColor = vec4(materialParams.tint * materialParams.k, 1.0);
`;

export default function Page() {
  const [k, setK] = createSignal(1);
  return (
    <main>
      <button id="dim" onClick={() => setK(0)}>dim</button>
      <div style={{ width: "400px", height: "300px" }}>
        <Canvas
          id="scene"
          camera={{ position: [0, 0, 4] }}
          onCreated={(s) => {
            window.__scene = s;
          }}
        >
          <mesh>
            <planeGeometry args={[3, 3]} />
            <shaderMaterial fragmentShader={FRAG} uniforms={{ tint: [1, 0, 1], k: { type: "float", value: k() } }} />
          </mesh>
        </Canvas>
      </div>
    </main>
  );
}
