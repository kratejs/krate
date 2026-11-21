// A scene the compiler cannot analyse (a component inside the Canvas) stays on
// the runtime path and reports KR8701.
import { Canvas } from "@krate/3d";

function Ball() {
  return (
    <mesh>
      <sphereGeometry />
      <meshStandardMaterial color="red" />
    </mesh>
  );
}

export default function Page() {
  return (
    <Canvas id="fallback">
      <ambientLight intensity={1} />
      <Ball />
    </Canvas>
  );
}
