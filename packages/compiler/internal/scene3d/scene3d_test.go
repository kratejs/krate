package scene3d

import (
	"errors"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/astprint"
	"github.com/kratejs/krate/packages/compiler/internal/diag"
	"github.com/kratejs/krate/packages/compiler/internal/tsstrip"
)

func wantMatrix(t *testing.T, got, want [16]float64) {
	t.Helper()
	for i := range want {
		if !approx(got[i], want[i]) {
			t.Fatalf("matrix element %d = %v, want %v\ngot  %v\nwant %v", i, got[i], want[i], got, want)
		}
	}
}

func TestStaticFlatteningNestedGroups(t *testing.T) {
	r := compile(t, page(`<Canvas>
      <group position={[0, 1, 0]} rotation={[0, Math.PI / 2, 0]} scale={2}>
        <mesh position={[1, 0, 0]}><boxGeometry /><meshStandardMaterial /></mesh>
        <group position={[0, 0, H]}>
          <mesh><boxGeometry args={[2, 2, 2]} /><meshStandardMaterial /></mesh>
        </group>
      </group>
    </Canvas>`, "const H = 1;"))
	if r.err != nil || !r.changed || len(r.diags) != 0 {
		t.Fatalf("changed=%v diags=%v err=%v", r.changed, r.diags, r.err)
	}
	r.has("K3dCanvas", "setup={", "__k3d.scene(__canvas")
	r.lacks("<group", "<mesh", "<Canvas", "dynamicMesh")
	ms := staticMatrices(r.out)
	if len(ms) != 2 {
		t.Fatalf("want 2 static meshes, got %d:\n%s", len(ms), r.out)
	}
	// Rotation of 90 degrees about Y maps +x to -z. Group scale 2.
	wantMatrix(t, ms[0], [16]float64{0, 0, -2, 0, 0, 2, 0, 0, 2, 0, 0, 0, 0, 1, -2, 1})
	// The nested group moves along its own +z, which the parent turned into +x and scaled.
	wantMatrix(t, ms[1], [16]float64{0, 0, -2, 0, 0, 2, 0, 0, 2, 0, 0, 0, 2, 1, 0, 1})
}

func TestStaticFoldsModuleConstants(t *testing.T) {
	r := compile(t, page(`<Canvas><mesh position={[X * 2, -Y, 0]} rotation={[TURN / 4, 0, 0]}><boxGeometry /><meshBasicMaterial /></mesh></Canvas>`,
		"const X = 1.5;\nconst Y = X + 1;\nconst TURN = 2 * Math.PI;"))
	if !r.changed {
		t.Fatalf("not compiled: %v\n%s", r.diags, r.out)
	}
	m := staticMatrices(r.out)[0]
	wantMatrix(t, m, [16]float64{1, 0, 0, 0, 0, 0, 1, 0, 0, -1, 0, 0, 3, -2.5, 0, 1})
}

func TestShadowedConstIsNotFolded(t *testing.T) {
	r := compile(t, `
const X = 1;
export default function Page(props) {
  const X = props.x;
  return <Canvas><mesh position={[X, 0, 0]}><boxGeometry /><meshBasicMaterial /></mesh></Canvas>;
}`)
	r.has("dynamicMesh", "__scene.pos(0, ")
	r.lacks("staticMesh")
}

func TestReactiveTransformsGetSlotsAndEffects(t *testing.T) {
	r := compile(t, page(`<Canvas>
      <group position={[0, 1, 0]}>
        <mesh position={[2, 0, 0]}><boxGeometry /><meshStandardMaterial color="orange" /></mesh>
        <mesh rotation={[0, rotY(), 0]} position={[rotY(), 1, 0]} scale={2} onClick={() => setRotY(rotY() + 0.5)}>
          <sphereGeometry args={[1, 32, 16]} />
          <meshStandardMaterial color="#4488ff" roughness={0.4} />
        </mesh>
      </group>
    </Canvas>`, ""))
	if !r.changed {
		t.Fatalf("not compiled: %v", r.diags)
	}
	r.has(
		"slots: 1",
		"__scene.staticMesh(__g0, __m0, [1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1, 0, 2, 1, 0, 1]);",
		// The static ancestor (the group) is folded into the parent matrix; static scale is the initial value.
		"__scene.dynamicMesh(0, __g1, __m1, [1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1, 0, 0, 1, 0, 1], { scale: [2, 2, 2] });",
		"__scene.pos(0, rotY(), 1, 0)",
		"__scene.rot(0, 0, rotY(), 0)",
		`__scene.on(__e0, "click", () => setRotY(rotY() + 0.5))`,
	)
	if n := r.count("__k3d.effect("); n != 2 {
		t.Errorf("want one effect per reactive prop (2), got %d", n)
	}
	r.lacks("__scene.scale(")
}

func TestNonLiteralReactiveVectorPassedWhole(t *testing.T) {
	r := compile(t, page(`<Canvas><mesh position={pos()} scale={s()}><boxGeometry /><meshBasicMaterial /></mesh></Canvas>`, "declare const pos: () => number[]; declare const s: () => number;"))
	r.has("__scene.pos(0, pos())", "__scene.scale(0, s())")
}

func TestMixedStaticAndDynamicSiblings(t *testing.T) {
	r := compile(t, page(`<Canvas>
      <mesh position={[0, 0, 0]}><boxGeometry /><meshBasicMaterial /></mesh>
      <mesh position={[rotY(), 0, 0]}><boxGeometry /><meshBasicMaterial /></mesh>
      <mesh position={[0, 3, 0]}><boxGeometry /><meshBasicMaterial /></mesh>
    </Canvas>`, ""))
	// The two static siblings share geometry and material: one instanced call.
	r.has("__scene.instanced(__g0, __m0, 2, new Float32Array([", "__scene.dynamicMesh(0, __g0, __m0")
	if r.count("__scene.geometry(") != 1 || r.count("__scene.material(") != 1 {
		t.Errorf("geometry and material are not deduplicated:\n%s", r.out)
	}
}

func TestDedupe(t *testing.T) {
	r := compile(t, page(`<Canvas>
      <mesh position={[1, 0, 0]}><boxGeometry args={[1, 1, 1]} /><meshStandardMaterial color="red" roughness={0.5} /></mesh>
      <mesh position={[0, 1, 0]} onClick={f}><boxGeometry /><meshStandardMaterial roughness={0.5} color="#f00" /></mesh>
      <mesh position={[0, 0, 1]} onClick={f}><boxGeometry args={[2, 1, 1]} /><meshStandardMaterial color="red" roughness={0.6} /></mesh>
    </Canvas>`, "declare const f: () => void;"))
	// args [] and [1,1,1] differ as written, so two box geometries plus the [2,1,1] one.
	if n := r.count("__scene.geometry("); n != 3 {
		t.Errorf("want 3 geometries, got %d:\n%s", n, r.out)
	}
	// red/#f00 with the same roughness share one material; 0.6 is separate.
	if n := r.count("__scene.material("); n != 2 {
		t.Errorf("want 2 materials, got %d:\n%s", n, r.out)
	}
}

func TestAutoInstancingRepeatedJSX(t *testing.T) {
	r := compile(t, page(`<Canvas>
      <mesh position={[0, 0, 0]}><boxGeometry /><meshStandardMaterial color="red" /></mesh>
      <mesh position={[1, 0, 0]}><boxGeometry /><meshStandardMaterial color="red" /></mesh>
      <mesh position={[2, 0, 0]}><boxGeometry /><meshStandardMaterial color="red" /></mesh>
    </Canvas>`, ""))
	r.has("__scene.instanced(__g0, __m0, 3, new Float32Array([")
	r.lacks("staticMesh", "dynamicMesh")
	re := regexp.MustCompile(`new Float32Array\(\[([^\]]*)\]`)
	nums := strings.Split(re.FindStringSubmatch(r.out)[1], ",")
	if len(nums) != 48 {
		t.Fatalf("want 48 matrix numbers, got %d", len(nums))
	}
	for i, want := range []float64{0, 1, 2} {
		x, _ := strconv.ParseFloat(strings.TrimSpace(nums[i*16+12]), 64)
		if x != want {
			t.Errorf("instance %d translation x = %v, want %v", i, x, want)
		}
	}
}

func TestAutoInstancingForOverConstArray(t *testing.T) {
	r := compile(t, page(`<Canvas>
{GRID.map((p, i) => (
        <mesh position={[p.x, p.y * 2, i]} scale={0.5}><sphereGeometry args={[1, 8, 8]} /><meshStandardMaterial color="gold" /></mesh>
      ))}
    </Canvas>`, "const GRID = [{ x: 1, y: 1 }, { x: 2, y: 2 }, { x: 3, y: 3 }, { x: 4, y: 4 }];"))
	if !r.changed {
		t.Fatalf("not compiled: %v", r.diags)
	}
	r.has("__scene.instanced(__g0, __m0, 4, new Float32Array([")
	r.lacks(".map(", "staticMesh")
	if r.count("__scene.geometry(") != 1 {
		t.Errorf("loop body geometry should be shared:\n%s", r.out)
	}
	re := regexp.MustCompile(`new Float32Array\(\[([^\]]*)\]`)
	nums := strings.Split(re.FindStringSubmatch(r.out)[1], ",")
	// item 3: x=4, y=8, z=3
	for k, want := range []float64{4, 8, 3} {
		v, _ := strconv.ParseFloat(strings.TrimSpace(nums[3*16+12+k]), 64)
		if v != want {
			t.Errorf("item 3 translation[%d] = %v, want %v", k, v, want)
		}
	}
}

func TestEventsKeepMeshesIndividual(t *testing.T) {
	r := compile(t, page(`<Canvas>
      <mesh onClick={a} onPointerOver={b} onPointerOut={b} onPointerMove={b} onPointerDown={b} onPointerUp={b}><boxGeometry /><meshBasicMaterial /></mesh>
      <mesh position={[1, 0, 0]} onClick={a}><boxGeometry /><meshBasicMaterial /></mesh>
    </Canvas>`, "declare const a: () => void; declare const b: () => void;"))
	r.lacks("instanced")
	r.has("const __e0 = __scene.staticMesh(", `__scene.on(__e0, "click", a)`, `"pointerover", b`, `"pointerout", b`,
		`"pointermove", b`, `"pointerdown", b`, `"pointerup", b`, `__scene.on(__e1, "click", a)`)
}

func TestMaterialColors(t *testing.T) {
	cases := []struct {
		color string
		want  string
	}{
		{`"orange"`, "[1, 0.376262, 0, 1]"},
		{`"#4488ff"`, "[0.0578054, 0.246201, 1, 1]"},
		{`{0x4488ff}`, "[0.0578054, 0.246201, 1, 1]"},
		{`"#f80"`, "[1, 0.246201, 0, 1]"},
		{`"rgb(255, 0, 0)"`, "[1, 0, 0, 1]"},
		{`"nonsense"`, "[1, 1, 1, 1]"},
		{`"white"`, "[1, 1, 1, 1]"},
	}
	for _, c := range cases {
		r := compile(t, page(`<Canvas><mesh><boxGeometry /><meshStandardMaterial color=`+c.color+` /></mesh></Canvas>`, ""))
		r.has("baseColor: " + c.want)
	}
}

func TestMaterialParamsAndTransparency(t *testing.T) {
	r := compile(t, page(`<Canvas>
      <mesh><boxGeometry /><meshPhysicalMaterial color="black" emissive="red" metalness={0.2} clearcoat={1} opacity={0.5} /></mesh>
      <mesh position={[1, 0, 0]}><boxGeometry /><meshStandardMaterial transparent map="/t.png" /></mesh>
      <mesh position={[2, 0, 0]}><boxGeometry /><meshStandardMaterial color={c()} emissive={e()} roughness={rough()} opacity={o()} /></mesh>
    </Canvas>`, "declare const c: () => string; declare const e: () => string; declare const rough: () => number; declare const o: () => number;"))
	r.has(
		`__scene.material("physical", { baseColor: [0, 0, 0, 1], emissive: [1, 0, 0], metallic: 0.2, clearcoat: 1, opacity: 0.5, transparent: true })`,
		`__scene.material("standard", { baseColorMap: "/t.png", transparent: true })`,
		`__scene.material("standard", { transparent: true })`,
		`__scene.setMaterialParam(__m2, "baseColor", __k3d.color(c()).concat(1))`,
		`__scene.setMaterialParam(__m2, "emissive", __k3d.color(e()))`,
		`__scene.setMaterialParam(__m2, "roughness", rough())`,
		`__scene.setMaterialParam(__m2, "opacity", o())`,
	)
}

func TestLightCalibration(t *testing.T) {
	r := compile(t, page(`<Canvas shadows>
      <directionalLight intensity={1.5} position={[0, 4, 0]} color="#ffffff" castShadow />
      <pointLight intensity={2} position={[1, 2, 3]} distance={10} />
      <spotLight intensity={1} position={[0, 5, 0]} angle={0.5} penumbra={0.2} target={[0, 0, 0]} castShadow={false} />
      <ambientLight intensity={0.5} color="red" />
      <directionalLight />
    </Canvas>`, ""))
	point := strconv.FormatFloat(2*4*math.Pi*1000, 'g', -1, 64)
	spot := strconv.FormatFloat(4*math.Pi*1000, 'g', -1, 64)
	r.has(
		`__scene.light("directional", { intensity: 165000, color: [1, 1, 1], castShadows: true, position: [0, 4, 0], direction: [0, -1, 0] })`,
		`__scene.light("point", { intensity: `+point+`, position: [1, 2, 3], falloff: 10 })`,
		`__scene.light("spot", { intensity: `+spot+`, position: [0, 5, 0], direction: [0, -1, 0], outer: 0.5, inner: 0.4 })`,
		`__scene.light("ambient", { intensity: 15000, color: [1, 0, 0] })`,
		`__scene.light("directional", { intensity: 110000, position: [0, 0, 0], direction: [0, 0, 0] })`,
	)
}

func TestLightsWithoutShadowsIgnoreCastShadow(t *testing.T) {
	r := compile(t, page(`<Canvas><directionalLight position={[0, 1, 0]} castShadow /><mesh castShadow receiveShadow><boxGeometry /><meshBasicMaterial /></mesh></Canvas>`, ""))
	r.lacks("castShadow")
}

func TestShadowFlagsOnMeshes(t *testing.T) {
	r := compile(t, page(`<Canvas shadows><mesh castShadow receiveShadow><boxGeometry /><meshBasicMaterial /></mesh></Canvas>`, ""))
	r.has("{ castShadow: true, receiveShadow: true }")
}

func TestReactiveLightProps(t *testing.T) {
	r := compile(t, page(`<Canvas>
      <directionalLight intensity={rotY()} color={col()} position={[rotY(), 5, 0]} />
      <spotLight position={[0, 5, 0]} target={tgt()} />
    </Canvas>`, "declare const col: () => string; declare const tgt: () => number[];"))
	r.has(
		`__scene.setLight(__l0, "intensity", __k3d.intensity("directional", rotY()))`,
		`__scene.setLight(__l0, "color", __k3d.color(col()))`,
		`__k3d.aimLight(__scene, __l0, "directional", [rotY(), 5, 0], null, null)`,
		`__k3d.aimLight(__scene, __l1, "spot", [0, 5, 0], tgt(), null)`,
	)
}

func TestCanvasProps(t *testing.T) {
	r := compile(t, page(`<Canvas shadows bloom ssao={{ radius: 1 }} antialias="taa" toneMapping="filmic" exposure={0.5}
        camera={{ position: [0, 2, 6], target: [0, 1, 0], fov: 50 }} class="stage" style={{ height: "400px" }} id="c"
        pixelRatio={1} onCreated={ready}>
      <mesh><boxGeometry /><meshBasicMaterial /></mesh>
    </Canvas>`, "declare const ready: (s: unknown) => void;"))
	r.has(
		`class="stage"`, `id="c"`, `style={{ height: "400px" }}`,
		`view: { shadows: true, bloom: true, ssao: { radius: 1 }, antialias: "taa", toneMapping: "filmic" }`,
		`camera: { aperture: 16, shutter: 0.008, iso: 100, position: [0, 2, 6], target: [0, 1, 0], fov: 50, exposure: 0.5 }`,
		`pixelRatio: 1`, `ready(__scene)`,
	)
}

func TestCanvasDefaultsAndControls(t *testing.T) {
	r := compile(t, page(`<Canvas><OrbitControls autoRotate target={[0, 1, 0]} /><Environment preset="studio" intensity={rotY()} /></Canvas>`, ""))
	r.has(`camera: { aperture: 16, shutter: 0.008, iso: 100 }`, "OrbitControls({ autoRotate: true, target: [0, 1, 0] })", `Environment({ preset: "studio", intensity: rotY() })`)
	r.lacks("slots:")
}

func TestOnlyKrate3DCanvasIsCompiled(t *testing.T) {
	src := `import { Canvas } from "@krate/three";
export default function P() { return <Canvas><mesh position={[0,0,0]}/></Canvas>; }`
	prog, _ := tsstrip.Parse("p.tsx", src, tsstrip.Options{})
	before := astprint.Print(prog)
	changed, diags, err := Rewrite(prog, Options{})
	if changed || len(diags) != 0 || err != nil || astprint.Print(prog) != before {
		t.Fatalf("a non-3d Canvas was touched: %v %v %v", changed, diags, err)
	}
}

func TestAliasedImportAndCanvasImportPruned(t *testing.T) {
	src := `import { Canvas as Stage } from "@krate/3d/runtime";
export default function P() { return <Stage><mesh><boxGeometry /><meshBasicMaterial /></mesh></Stage>; }`
	prog, _ := tsstrip.Parse("p.tsx", src, tsstrip.Options{})
	changed, _, err := Rewrite(prog, Options{})
	out := astprint.Print(prog)
	if !changed || err != nil {
		t.Fatalf("not compiled: %v %v", changed, err)
	}
	if strings.Contains(out, "@krate/3d/runtime\"") && !strings.Contains(out, "compiled") {
		t.Errorf("unexpected import left: %s", out)
	}
	if strings.Contains(out, `from "@krate/3d/runtime";`) {
		t.Errorf("the unused Canvas import should be dropped:\n%s", out)
	}
}

func TestFallbacks(t *testing.T) {
	cases := []struct {
		name, scene, pre, reason string
	}{
		{"spread on mesh", `<Canvas><mesh {...p}><boxGeometry /><meshBasicMaterial /></mesh></Canvas>`, "declare const p: object;", "spread prop on <mesh>"},
		{"spread on canvas", `<Canvas {...p}><mesh><boxGeometry /><meshBasicMaterial /></mesh></Canvas>`, "declare const p: object;", "spread prop on <Canvas>"},
		{"unknown component", `<Canvas><Robot /></Canvas>`, "declare const Robot: () => null;", "component <Robot>"},
		{"unknown element", `<Canvas><torus /></Canvas>`, "", "<torus>"},
		{"reactive group", `<Canvas><group position={[rotY(), 0, 0]}><mesh><boxGeometry /><meshBasicMaterial /></mesh></group></Canvas>`, "", "reactive <group> transform"},
		{"map over signal", `<Canvas>{list().map((p) => <mesh position={[p, 0, 0]}><boxGeometry /><meshBasicMaterial /></mesh>)}</Canvas>`, "declare const list: () => number[];", "not a constant array"},
		{"for over signal", `<Canvas><For each={list()}>{(p) => <mesh position={[p, 0, 0]}><boxGeometry /><meshBasicMaterial /></mesh>}</For></Canvas>`, "declare const list: () => number[];", "not a constant array"},
		{"reactive inside for", `<Canvas><For each={[1, 2]}>{(p) => <mesh position={[p, rotY(), 0]}><boxGeometry /><meshBasicMaterial /></mesh>}</For></Canvas>`, "", "inside a list callback"},
		{"reactive geometry args", `<Canvas><mesh><boxGeometry args={[rotY(), 1, 1]} /><meshBasicMaterial /></mesh></Canvas>`, "", "reactive geometry args"},
		{"expression child", `<Canvas>{rotY() > 1 && <mesh><boxGeometry /><meshBasicMaterial /></mesh>}</Canvas>`, "", "expression child"},
		{"map reactive inside", `<Canvas>{LIST.map((x) => <mesh position={[x, rotY(), 0]}><boxGeometry /><meshBasicMaterial /></mesh>)}</Canvas>`, "const LIST = [1, 2];", "inside a list callback"},
		{"map with block body", `<Canvas>{LIST.map((x) => { const y = x; return <mesh position={[y, 0, 0]}><boxGeometry /><meshBasicMaterial /></mesh>; })}</Canvas>`, "const LIST = [1, 2];", "returns one scene element"},
		{"mesh without material", `<Canvas><mesh><boxGeometry /></mesh></Canvas>`, "", "without both a geometry and a material"},
		{"unknown canvas prop", `<Canvas frameloop="never"><mesh><boxGeometry /><meshBasicMaterial /></mesh></Canvas>`, "", `"frameloop"`},
		{"dynamic shader source", `<Canvas><mesh><boxGeometry /><shaderMaterial fragmentShader={src()} /></mesh></Canvas>`, "declare const src: () => string;", "not a static string"},
		{"nested mesh", `<Canvas><mesh><boxGeometry /><meshBasicMaterial /><mesh><boxGeometry /><meshBasicMaterial /></mesh></mesh></Canvas>`, "", "inside <mesh>"},
		{"bad vector", `<Canvas><mesh position={[1, 2]}><boxGeometry /><meshBasicMaterial /></mesh></Canvas>`, "", "not a [x, y, z] array"},
		{"reactive spot angle", `<Canvas><spotLight angle={rotY()} /></Canvas>`, "", "reactive spotLight angle"},
		{"text child", `<Canvas>hello</Canvas>`, "", "text inside"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			compile(t, page(c.scene, c.pre)).fallback(c.reason)
		})
	}
}

func TestFallbackOnlyAffectsThatCanvas(t *testing.T) {
	r := compile(t, `
export function A() { return <Canvas><mesh><boxGeometry /><meshBasicMaterial /></mesh></Canvas>; }
export function B() { return <Canvas><Robot /></Canvas>; }
`)
	if !r.changed || len(r.diags) != 1 {
		t.Fatalf("changed=%v diags=%v", r.changed, r.diags)
	}
	r.has("K3dCanvas", "<Canvas>", `import { Canvas, OrbitControls, Environment } from "@krate/3d"`)
	if !strings.Contains(r.diags[0].Source, "Robot") {
		t.Errorf("the diagnostic should show the Canvas line, got %q", r.diags[0].Source)
	}
}

func TestForComponentOverConstArray(t *testing.T) {
	r := compile(t, page(`<Canvas><For each={[1, 2, 3]}>{(x) => <mesh position={[x, 0, 0]}><boxGeometry /><meshBasicMaterial /></mesh>}</For></Canvas>`, ""))
	r.has("__scene.instanced(__g0, __m0, 3,")
	r.lacks("<For")
}

func TestForNestedAndFragments(t *testing.T) {
	r := compile(t, page(`<Canvas>
      <For each={[0, 1]}>{(i) => (
        <For each={[0, 1]}>{(j) => <><mesh position={[i, j, 0]}><boxGeometry /><meshBasicMaterial /></mesh></>}</For>
      )}</For>
    </Canvas>`, ""))
	r.has("__scene.instanced(__g0, __m0, 4,")
}

// calibration.ts and color.ts are the runtime side of the compile-time
// constants; these tests parse them so the two cannot drift apart.

func readRuntime(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "..", "..", "3d", "src", "runtime", name))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func tsConst(t *testing.T, src, name string) string {
	t.Helper()
	m := regexp.MustCompile(`export const ` + name + `(?:\s*:[^=]+)? = ([^;]+);`).FindStringSubmatch(src)
	if m == nil {
		t.Fatalf("calibration.ts has no const %s", name)
	}
	return strings.TrimSpace(m[1])
}

func TestCalibrationMatchesRuntime(t *testing.T) {
	src := readRuntime(t, "calibration.ts")
	if got := tsConst(t, src, "DIRECTIONAL_LUX_PER_UNIT"); got != strconv.FormatFloat(DirectionalLuxPerUnit, 'f', -1, 64) {
		t.Errorf("DIRECTIONAL_LUX_PER_UNIT: ts %s, go %v", got, DirectionalLuxPerUnit)
	}
	if got := tsConst(t, src, "AMBIENT_IBL_PER_UNIT"); got != strconv.FormatFloat(AmbientIBLPerUnit, 'f', -1, 64) {
		t.Errorf("AMBIENT_IBL_PER_UNIT: ts %s, go %v", got, AmbientIBLPerUnit)
	}
	if got := tsConst(t, src, "POINT_LUMENS_PER_UNIT"); got != "4 * Math.PI * 1000" || PointLumensPerUnit != 4*math.Pi*1000 {
		t.Errorf("POINT_LUMENS_PER_UNIT: ts %q, go %v", got, PointLumensPerUnit)
	}
	exp := tsConst(t, src, "DEFAULT_EXPOSURE")
	for name, want := range map[string]float64{"aperture": DefaultAperture, "iso": DefaultISO} {
		m := regexp.MustCompile(name + `: ([0-9.]+)`).FindStringSubmatch(exp)
		if m == nil {
			t.Fatalf("DEFAULT_EXPOSURE has no %s: %s", name, exp)
		}
		if v, _ := strconv.ParseFloat(m[1], 64); v != want {
			t.Errorf("%s: ts %v, go %v", name, v, want)
		}
	}
	m := regexp.MustCompile(`shutter: 1 / ([0-9.]+)`).FindStringSubmatch(exp)
	if m == nil {
		t.Fatalf("DEFAULT_EXPOSURE shutter is not 1 / N: %s", exp)
	}
	if v, _ := strconv.ParseFloat(m[1], 64); 1/v != DefaultShutter {
		t.Errorf("shutter: ts 1/%v, go %v", v, DefaultShutter)
	}
	// The switch in lightIntensity has the same three cases as LightIntensity.
	for kind, want := range map[LightKind]float64{LightDirectional: 110000, LightAmbient: 30000, LightPoint: PointLumensPerUnit, LightSpot: PointLumensPerUnit} {
		if got := LightIntensity(kind, 1); got != want {
			t.Errorf("LightIntensity(%s, 1) = %v, want %v", kind, got, want)
		}
	}
}

func TestNamedColorsMatchRuntime(t *testing.T) {
	src := readRuntime(t, "color.ts")
	body := regexp.MustCompile(`(?s)const NAMED: Record<string, number> = \{(.*?)\n\};`).FindStringSubmatch(src)
	if body == nil {
		t.Fatal("color.ts has no NAMED table")
	}
	entries := regexp.MustCompile(`(\w+): 0x([0-9A-Fa-f]{6})`).FindAllStringSubmatch(body[1], -1)
	if len(entries) != len(namedColors) {
		t.Errorf("ts has %d named colors, go has %d", len(entries), len(namedColors))
	}
	for _, e := range entries {
		want, _ := strconv.ParseUint(e[2], 16, 32)
		if got, ok := namedColors[e[1]]; !ok || uint64(got) != want {
			t.Errorf("color %s: ts 0x%s, go 0x%06X (present=%v)", e[1], e[2], got, ok)
		}
	}
}

func TestDiagCodeRegistered(t *testing.T) {
	if _, ok := diag.Lookup(diag.CodeScene3DFallback); !ok {
		t.Fatal("KR8701 is not registered")
	}
}

func assetsOption(a Assets, err error) func(*Options) {
	return func(o *Options) { o.Assets = func() (Assets, error) { return a, err } }
}

func TestAssetsReachTheScene(t *testing.T) {
	a := Assets{FilamentURL: "/base/assets/3d/filament-1.53.4/filament.js", MaterialBase: "/base/assets/3d/materials-1.53.4"}
	r := compile(t, page(`<Canvas><mesh><boxGeometry /><meshStandardMaterial /></mesh></Canvas>`, ""), assetsOption(a, nil))
	if r.err != nil || !r.changed {
		t.Fatalf("changed=%v err=%v", r.changed, r.err)
	}
	r.has(`filamentUrl: "/base/assets/3d/filament-1.53.4/filament.js"`, `materialBase: "/base/assets/3d/materials-1.53.4"`)

	// A Canvas left to the runtime gets the same URLs as props.
	fb := compile(t, page(`<Canvas><Unknown /></Canvas>`, ""), assetsOption(a, nil))
	fb.has(`filamentUrl="/base/assets/3d/filament-1.53.4/filament.js"`, `materialBase="/base/assets/3d/materials-1.53.4"`)
}

func TestMissingFilamentWarns(t *testing.T) {
	r := compile(t, page(`<Canvas><mesh><boxGeometry /><meshStandardMaterial /></mesh></Canvas>`, ""),
		assetsOption(Assets{}, errors.New("the `filament` package is not installed")))
	if r.err != nil || !r.changed {
		t.Fatalf("a missing filament must not fail the build: changed=%v err=%v", r.changed, r.err)
	}
	if len(r.diags) != 1 || r.diags[0].Code != diag.CodeScene3DAssets || !r.diags[0].IsWarning() || !strings.Contains(r.diags[0].Hint, "install filament") {
		t.Fatalf("want one KR8702 warning with an install hint, got %+v", r.diags)
	}
	r.lacks("filamentUrl")
}
