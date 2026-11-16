// Krate-style scene built with signals and the create* primitives:
// createFrame, createThree, createLoader and createGraph return accessors,
// and signals from outside the canvas drive props, conditionals and lists.
import { createSignal, Suspense } from "@krate/runtime";
import { Canvas, createFrame, createGraph, createLoader, createThree } from "@krate/three";
import * as THREE from "three";

function Spinner(props: { speed: number }) {
  let mesh: THREE.Mesh | undefined;
  const width = createThree((s) => s.size.width);
  const [spins, setSpins] = createSignal(0);
  const stop = createFrame((_state, delta) => {
    if (!mesh) return;
    mesh.rotation.y += delta * props.speed;
    setSpins(spins() + 1);
    (window as any).__spins = spins();
  });
  (window as any).__stopSpinner = stop;
  return (
    <mesh ref={mesh} name="k-spinner" scale={width() / 300}>
      <boxGeometry />
      <meshBasicMaterial color={spins() > 0 ? "orange" : "gray"} />
    </mesh>
  );
}

function Loaded(props: { url: string }) {
  const text = createLoader(THREE.FileLoader, () => props.url);
  return <group name={"k-loaded:" + String(text()).trim()} />;
}

function Graph() {
  const model = new THREE.Group();
  model.add(Object.assign(new THREE.Object3D(), { name: "wheel" }));
  model.add(Object.assign(new THREE.Object3D(), { name: "body" }));
  const graph = createGraph(model);
  return <group name={"k-graph:" + Object.keys(graph().nodes).sort().join("+")} />;
}

export default function Signals() {
  const [x, setX] = createSignal(0);
  const [speed, setSpeed] = createSignal(1);
  const [show, setShow] = createSignal(true);
  const [items, setItems] = createSignal(["a", "b"]);
  const [file, setFile] = createSignal("/data.txt");
  return (
    <main>
      <button id="k-move" onClick={() => setX(x() + 1)}>move</button>
      <button id="k-fast" onClick={() => setSpeed(speed() * 10)}>faster</button>
      <button id="k-toggle" onClick={() => setShow(!show())}>toggle</button>
      <button id="k-add" onClick={() => setItems([...items(), "c"])}>add</button>
      <button id="k-file" onClick={() => setFile("/other.txt")}>other file</button>
      <div style={{ width: "300px", height: "200px" }}>
        <Canvas onCreated={(s) => ((window as any).__three = s)}>
          <group name="k-mover" position-x={x()}>
            <Spinner speed={speed()} />
          </group>
          {show() && <mesh name="k-extra" />}
          {items().map((item) => (
            <group key={item} name={"k-item-" + item} />
          ))}
          <Suspense fallback={<group name="k-loading" />}>
            <Loaded url={file()} />
          </Suspense>
          <Graph />
        </Canvas>
      </div>
    </main>
  );
}
