// @krate/three/drei in a React-style scene, as `krate migrate nextjs` writes
// it from @react-three/drei: OrbitControls with makeDefault and a prop that
// follows state, Environment rendered from Lightformer children, and useGLTF.
import * as React from "react";
import { Canvas, useThree } from "@krate/three";
import { Environment, Lightformer, OrbitControls, useGLTF } from "@krate/three/drei";

function Model({ url }: { url: string }) {
  const gltf = useGLTF(url, false, true, (loader) => ((window as any).__extended = typeof (loader as any).setMeshoptDecoder));
  React.useEffect(() => {
    (window as any).__gltf = gltf;
  }, [gltf]);
  return <primitive object={gltf.scene} />;
}

function Rig({ near }: { near: number }) {
  const controls = useThree((s) => s.controls);
  React.useEffect(() => {
    (window as any).__controls = controls;
  }, [controls]);
  return (
    <OrbitControls
      makeDefault
      enableDamping
      dampingFactor={0.1}
      minDistance={near}
      maxDistance={50}
      onStart={() => ((window as any).__started = ((window as any).__started ?? 0) + 1)}
    />
  );
}

export default function Drei() {
  const [near, setNear] = React.useState(1);
  return (
    <main>
      <button id="near" onClick={() => setNear(near + 1)}>near</button>
      <div id="scene" style={{ width: "400px", height: "300px" }}>
        <Canvas camera={{ position: [0, 0, 5] }} onCreated={(s) => ((window as any).__three = s)}>
          <Environment resolution={32} background={false}>
            <Lightformer ref={(m: unknown) => ((window as any).__envTop = m)} name="top" form="rect" intensity={2} position={[0, 5, 0]} rotation-x={Math.PI / 2} scale={[12, 12]} />
            <Lightformer name="ring" form="ring" intensity={0.5} color="red" position={[0, 1, -6]} scale={6} />
          </Environment>
          <mesh name="ball">
            <sphereGeometry args={[1, 32, 32]} />
            <meshStandardMaterial metalness={1} roughness={0} />
          </mesh>
          <React.Suspense fallback={null}>
            <Model url="/triangle.glb" />
          </React.Suspense>
          <Rig near={near} />
        </Canvas>
      </div>
    </main>
  );
}
