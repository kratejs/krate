// Generates the benchmark scene as Krate pages for the three variants.
//
// The scene is identical in all variants as far as the APIs allow:
//   - 1000 boxes (40 x 25) with one shared material, in a static grid
//   - 4 differently colored static meshes
//   - 100 spheres in a ring driven by a signal (animated pages) or fixed
//   - a ground plane, one directional light, one ambient light
//   - no shadows, ACES tone mapping, no antialiasing, 1280x720 canvas
// Generated sources have literal constant arrays because the compilers fold
// only module-level constants.

export const VARIANTS = {
  filament: { pkg: "@krate/3d", compile: true },
  "three-compiled": { pkg: "@krate/three", compile: true },
  "three-runtime": { pkg: "@krate/three", compile: false },
};

export const SCENE = {
  width: 1280,
  height: 720,
  fov: 50,
  camera: [0, 26, 30],
  target: [0, 0, 0],
  gridX: 40,
  gridZ: 25,
  ringCount: 100,
  ringRadius: 12,
  ringY: 4,
  directional: 2,
  ambient: 0.5,
};

const r = (n) => Math.round(n * 1e4) / 1e4;

function gridLiteral() {
  const rows = [];
  for (let iz = 0; iz < SCENE.gridZ; iz++) {
    const row = [];
    for (let ix = 0; ix < SCENE.gridX; ix++) {
      row.push(`[${r(ix - (SCENE.gridX - 1) / 2)},${r(iz - (SCENE.gridZ - 1) / 2)}]`);
    }
    rows.push("  " + row.join(","));
  }
  return `const GRID = [\n${rows.join(",\n")},\n];`;
}

function ringAngles() {
  return Array.from({ length: SCENE.ringCount }, (_, i) => r((i / SCENE.ringCount) * Math.PI * 2));
}

function ringStatic() {
  const out = ringAngles().map((a) => `[${r(Math.cos(a) * SCENE.ringRadius)},${r(SCENE.ringY + Math.sin(a * 3))},${r(Math.sin(a) * SCENE.ringRadius)}]`);
  return `const RING = [\n  ${out.join(",\n  ")},\n];`;
}

// Four static meshes with their own geometry/material pairs, so not everything
// is instanced.
const SPECIAL = `
      <mesh position={[-6, 1.5, 0]}>
        <boxGeometry args={[3, 3, 3]} />
        <meshStandardMaterial color="#e63946" roughness={0.5} />
      </mesh>
      <mesh position={[0, 2, 0]}>
        <sphereGeometry args={[2, 32, 16]} />
        <meshStandardMaterial color="#2a9d8f" roughness={0.3} />
      </mesh>
      <mesh position={[6, 1.5, 0]}>
        <boxGeometry args={[2, 3, 2]} />
        <meshStandardMaterial color="#4361ee" roughness={0.7} />
      </mesh>
      <mesh position={[0, 0.6, 7]}>
        <sphereGeometry args={[1.2, 24, 12]} />
        <meshStandardMaterial color="#ffd166" roughness={0.4} />
      </mesh>`;

function frameHooks(engine) {
  // Each variant marks the first frame the engine renders through its own
  // frame hook, and counts engine frames.
  if (engine === "filament") {
    return `
          onCreated={(s: any) => {
            const w = window as any;
            w.__created = performance.now();
            s.ready.then(() => { w.__ready = performance.now(); });
            s.onFrame(() => {
              w.__engineFrames = (w.__engineFrames || 0) + 1;
              if (!w.__firstFrame) w.__firstFrame = performance.now();
            });
          }}`;
  }
  return `
          onCreated={(state: any) => {
            const w = window as any;
            w.__created = performance.now();
            w.__ready = w.__created;
            state.camera.lookAt(${SCENE.target.join(", ")});
            addAfterEffect(() => {
              w.__engineFrames = (w.__engineFrames || 0) + 1;
              if (!w.__firstFrame) w.__firstFrame = performance.now();
            });
          }}`;
}

export function page(variant, animated, opts = {}) {
  const variantMap = !!opts.map;
  const { compile } = VARIANTS[variant];
  const engine = variant === "filament" ? "filament" : "three";
  const imports =
    engine === "filament"
      ? `import { Canvas } from "@krate/3d";`
      : `import { Canvas, addAfterEffect } from "@krate/three";`;
  const canvasProps =
    engine === "filament"
      ? `id="scene" antialias="none" toneMapping="aces" camera={{ position: [${SCENE.camera}], target: [${SCENE.target}], fov: ${SCENE.fov} }}`
      : `id="scene" ${compile ? "" : "compile={false} "}gl={{ antialias: false }} dpr={1} camera={{ position: [${SCENE.camera}], fov: ${SCENE.fov} }}`;
  const explicit = animated && engine === "filament" && !variantMap;
  const ringData = explicit
    ? ""
    : animated
    ? `const RING = [${ringAngles().join(", ")}];`
    : ringStatic();
  // @krate/3d compiles a .map() only when every item prop folds to a constant,
  // so its animated ring is written out (generated), one mesh per sphere.
  const ring = explicit
    ? ringAngles()
        .map(
          (a) => `
      <mesh position={[Math.cos(${a} + t()) * ${SCENE.ringRadius}, ${SCENE.ringY} + Math.sin(${r(a * 3)} + t() * 2), Math.sin(${a} + t()) * ${SCENE.ringRadius}]}>
        <sphereGeometry args={[0.5, 16, 12]} />
        <meshStandardMaterial color="#f4a261" roughness={0.4} />
      </mesh>`,
        )
        .join("")
    : animated
    ? `
      {RING.map((a) => (
        <mesh position={[Math.cos(a + t()) * ${SCENE.ringRadius}, ${SCENE.ringY} + Math.sin(a * 3 + t() * 2), Math.sin(a + t()) * ${SCENE.ringRadius}]}>
          <sphereGeometry args={[0.5, 16, 12]} />
          <meshStandardMaterial color="#f4a261" roughness={0.4} />
        </mesh>
      ))}`
    : `
      {RING.map((p) => (
        <mesh position={[p[0], p[1], p[2]]}>
          <sphereGeometry args={[0.5, 16, 12]} />
          <meshStandardMaterial color="#f4a261" roughness={0.4} />
        </mesh>
      ))}`;
  const tick = animated
    ? `
  const [t, setT] = createSignal(0);
  let frozen = false;
  onMount(() => {
    // The signal advances by a fixed step per animation frame, so animated
    // runs are deterministic. __freeze(v) stops it at time v.
    let n = 0;
    const loop = () => {
      if (!frozen) setT((n++) / 60);
      requestAnimationFrame(loop);
    };
    requestAnimationFrame(loop);
    (window as any).__freeze = (v: number) => {
      frozen = true;
      setT(v);
    };
  });`
    : `
  onMount(() => {
    (window as any).__freeze = () => {};
  });`;
  return `// Generated by packages/benchmark/3d/scene.mjs (${variant}, ${animated ? "animated" : "static"}).
import { createSignal, onMount } from "@krate/runtime";
${imports}

${gridLiteral()}
${ringData}

export default function Page() {${tick}
  return (
    <main style={{ position: "fixed", top: "0", left: "0", margin: "0", background: "#1b1d2a" }}>
      <div style={{ width: "${SCENE.width}px", height: "${SCENE.height}px", margin: "0", overflow: "hidden" }}>
        <Canvas
          ${canvasProps}${frameHooks(engine)}
        >
          <ambientLight intensity={${SCENE.ambient}} />
          <directionalLight intensity={${SCENE.directional}} position={[10, 20, 10]} />
          <mesh rotation={[-Math.PI / 2, 0, 0]}>
            <planeGeometry args={[60, 40]} />
            <meshStandardMaterial color="#8d99ae" roughness={0.9} />
          </mesh>
          {GRID.map((p) => (
            <mesh position={[p[0], 0.4, p[1]]}>
              <boxGeometry args={[0.8, 0.8, 0.8]} />
              <meshStandardMaterial color="#cdd5e0" roughness={0.6} />
            </mesh>
          ))}${SPECIAL}${ring}
        </Canvas>
      </div>
    </main>
  );
}
`;
}

// A Canvas with no children: the baseline JS every scene of the variant ships.
export function emptyPage(variant) {
  const { compile } = VARIANTS[variant];
  const imports = variant === "filament" ? `import { Canvas } from "@krate/3d";` : `import { Canvas } from "@krate/three";`;
  const extra = variant === "filament" ? "" : compile ? "" : " compile={false}";
  return `// Generated by packages/benchmark/3d/scene.mjs (${variant}, empty).
${imports}

export default function Page() {
  return (
    <main>
      <div style={{ width: "${SCENE.width}px", height: "${SCENE.height}px" }}>
        <Canvas id="scene"${extra} />
      </div>
    </main>
  );
}
`;
}
