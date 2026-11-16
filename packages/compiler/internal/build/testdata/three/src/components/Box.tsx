import { useRef, useState } from "react";
import { useFrame } from "@krate/three";
import type { Mesh } from "three";

export default function Box(props: { name: string; position: [number, number, number]; color: string }) {
  const ref = useRef<Mesh>(null);
  const [hovered, setHover] = useState(false);
  const [clicks, setClicks] = useState(0);
  useFrame((_state, delta) => {
    ref.current!.rotation.y += delta;
  });
  return (
    <mesh
      name={props.name}
      ref={ref}
      position={props.position}
      scale={clicks > 0 ? 1.5 : 1}
      onClick={(e) => {
        e.stopPropagation();
        setClicks(clicks + 1);
        (window as any).__clicks = clicks + 1;
      }}
      onPointerOver={() => setHover(true)}
      onPointerOut={() => setHover(false)}
    >
      <boxGeometry args={[1, 1, 1]} />
      <meshBasicMaterial color={hovered ? "hotpink" : props.color} />
    </mesh>
  );
}
