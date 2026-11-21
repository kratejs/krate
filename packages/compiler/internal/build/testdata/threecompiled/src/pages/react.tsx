// React-style state (useState) driving a compiled group and a compiled mesh's
// handler: the compiled closures must read the current state.
import { useState } from "react";
import { Canvas } from "@krate/three";

export default function ReactStyle() {
  const [angle, setAngle] = useState(0);
  return (
    <main>
      <button id="turn" onClick={() => setAngle(angle + 0.5)}>turn</button>
      <div style={{ width: "300px", height: "200px" }}>
        <Canvas id="scene" onCreated={(s) => ((window as any).__three = s)}>
          <group name="turner" rotation={[0, angle, 0]}>
            <mesh name="hit" onClick={() => setAngle(angle + 1)}>
              <boxGeometry args={[2, 2, 2]} />
              <meshBasicMaterial color="red" />
            </mesh>
          </group>
        </Canvas>
      </div>
    </main>
  );
}
