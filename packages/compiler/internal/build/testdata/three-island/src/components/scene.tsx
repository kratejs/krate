// The scene of the island viewer: drei helpers, a picked mesh, a suspending
// glTF model and a frame callback that takes over rendering.
import { Environment, Lightformer, OrbitControls, useGLTF } from "@krate/three/drei";
import { useFrame, useThree } from "@krate/three";
import * as React from "react";
import * as THREE from "three";

function Model({ url }: { url: string }) {
  const gltf = useGLTF(url, false, true);
  const scene = React.useMemo(() => gltf.scene.clone(true), [gltf]);
  React.useEffect(() => { (window as any).__gltf = gltf; }, [gltf]);
  return <primitive object={scene} />;
}

export function Scene({ url, near, onPick }: { url: string; near: number; onPick: (n: string) => void }) {
  const aspect = useThree((s) => s.size.width / Math.max(1, s.size.height));
  const hasControls = useThree((s) => !!s.controls);
  const [hits, setHits] = React.useState(0);
  const box = React.useRef<THREE.Mesh>(null);
  // DOM picking on the canvas, as an app can do next to OrbitControls: the
  // controls must not capture the pointer, or the pointerup never comes here.
  const gl = useThree((s) => s.gl);
  React.useEffect(() => {
    const el = gl.domElement;
    const up = () => ((window as any).__canvasUps = ((window as any).__canvasUps ?? 0) + 1);
    el.addEventListener("pointerup", up);
    return () => el.removeEventListener("pointerup", up);
  }, [gl]);
  React.useEffect(() => { (window as any).__aspect = aspect; (window as any).__hasControls = hasControls; (window as any).__box = box.current; }, [aspect, hasControls]);
  useFrame((state) => { (window as any).__frames = ((window as any).__frames ?? 0) + 1; state.gl.render(state.scene, state.camera); }, 1);
  return (
    <>
      <ambientLight intensity={0.35} />
      <Environment resolution={32} background={false}>
        <Lightformer ref={(m: unknown) => ((window as any).__envTop = m)} form="rect" intensity={2.2} position={[0, 5, 0]} rotation-x={Math.PI / 2} scale={[12, 12, 1]} />
        <Lightformer form="ring" intensity={0.8} position={[0, 1, -6]} scale={6} />
      </Environment>
      <mesh ref={box} name="box" onClick={(e) => { e.stopPropagation(); setHits(hits + 1); onPick("box"); }} scale={hits > 0 ? 1.5 : 1}>
        <boxGeometry />
        <meshStandardMaterial color={hits > 0 ? "hotpink" : "orange"} />
      </mesh>
      <React.Suspense fallback={null}>
        <Model url={url} />
      </React.Suspense>
      <OrbitControls makeDefault enableDamping dampingFactor={0.1} minDistance={near} maxDistance={50} />
    </>
  );
}
