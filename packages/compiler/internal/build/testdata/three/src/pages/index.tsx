// React-style scene: hooks from react, a component from another module, a
// same-file component, conditionals and a keyed list inside the canvas.
import { useState } from "react";
import { Canvas, useFrame, useThree } from "@krate/three";
import Box from "../components/Box";

function Probe() {
  const width = useThree((s) => s.size.width);
  useFrame(() => {
    const w = window as any;
    w.__frames = (w.__frames || 0) + 1;
    w.__width = width;
  });
  return null;
}

export default function Page() {
  const [count, setCount] = useState(0);
  const [show, setShow] = useState(true);
  const [items, setItems] = useState([1, 2]);
  return (
    <main>
      <button id="inc" onClick={() => setCount(count + 1)}>{count}</button>
      <button id="toggle" onClick={() => setShow(!show)}>toggle</button>
      <button id="add" onClick={() => setItems([...items, items.length + 1])}>add</button>
      <div style={{ width: "400px", height: "300px" }}>
        <Canvas id="scene" fallback={<span>WebGL is not available</span>} onCreated={(s) => ((window as any).__three = s)}>
          <ambientLight intensity={1} />
          <Box name="center" position={[0, 0, 0]} color={count > 0 ? "red" : "blue"} />
          {show && (
            <mesh name="extra" position={[2.5, 0, 0]}>
              <sphereGeometry args={[0.5]} />
              <meshBasicMaterial color="green" />
            </mesh>
          )}
          {items.map((i) => (
            <group key={i} name={"item-" + i} position-x={i} />
          ))}
          <Probe />
        </Canvas>
      </div>
    </main>
  );
}
