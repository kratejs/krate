// The rest of the documented surface: spread props, destructured useThree,
// extend with a three/examples class, Suspense with useLoader, and portals.
import { createContext, Suspense, useContext, useState } from "react";
import { Canvas, createPortal, extend, useLoader, useThree } from "@krate/three";
import * as THREE from "three";
import { OrbitControls } from "three/examples/jsm/controls/OrbitControls.js";

extend({ OrbitControls });

const Theme = createContext("default");

function Themed() {
  const theme = useContext(Theme);
  return <group name={"theme:" + theme} />;
}

function Controls() {
  const { camera, gl } = useThree();
  return <orbitControls ref={(c: unknown) => ((window as any).__controls = c)} args={[camera, gl.domElement]} enableDamping={false} />;
}

function Labeled(props: { name: string; position: [number, number, number] }) {
  return (
    <mesh {...props}>
      <boxGeometry />
    </mesh>
  );
}

function Text() {
  const text = useLoader(THREE.FileLoader, "/data.txt");
  return <group name={"loaded:" + String(text)} />;
}

function Portal() {
  const [target] = useState(() => {
    const s = new THREE.Scene();
    s.name = "portal-target";
    (window as any).__portal = s;
    return s;
  });
  return createPortal(<mesh name="in-portal" />, target);
}

export default function Extras() {
  const [theme, setTheme] = useState("red");
  return (
    <Theme.Provider value={theme}>
    <button id="theme" onClick={() => setTheme("blue")}>theme</button>
    <div style={{ width: "300px", height: "200px" }}>
      <Canvas onCreated={(s) => ((window as any).__three = s)}>
        <Controls />
        <Labeled name="spread" position={[1, 2, 3]} />
        <Suspense fallback={<group name="loading" />}>
          <Text />
        </Suspense>
        <Portal />
        <Themed />
      </Canvas>
    </div>
    </Theme.Provider>
  );
}
