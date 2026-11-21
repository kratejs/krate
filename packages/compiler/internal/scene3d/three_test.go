package scene3d

import (
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/astprint"
	"github.com/kratejs/krate/packages/compiler/internal/tsstrip"
)

const threeHeader = `import { Canvas, useFrame } from "@krate/three";
import { OrbitControls } from "@krate/three/drei";
import { createSignal } from "@krate/runtime";
`

type threeResult struct {
	t       *testing.T
	out     string
	orig    string
	changed bool
}

func compileThree(t *testing.T, scene string, pre ...string) threeResult {
	t.Helper()
	src := threeHeader + strings.Join(pre, "\n") + "\nexport default function Page() {\n  const [rotY, setRotY] = createSignal(0);\n  let ref;\n  return (" + scene + ");\n}\n"
	prog, errs := tsstrip.Parse("page.tsx", src, tsstrip.Options{})
	if len(errs) > 0 {
		t.Fatalf("parse: %v", errs)
	}
	orig := astprint.Print(prog)
	changed := RewriteThree(prog)
	return threeResult{t: t, out: astprint.Print(prog), orig: orig, changed: changed}
}

// squash makes printed code comparable: no spaces, single quotes.
func squash(s string) string {
	return strings.ReplaceAll(strings.ReplaceAll(s, " ", ""), `"`, "'")
}

func (r threeResult) has(sub ...string) {
	r.t.Helper()
	for _, s := range sub {
		if !strings.Contains(squash(r.out), squash(s)) {
			r.t.Errorf("output does not contain %q:\n%s", s, r.out)
		}
	}
}

func (r threeResult) lacks(sub ...string) {
	r.t.Helper()
	for _, s := range sub {
		if strings.Contains(squash(r.out), squash(s)) {
			r.t.Errorf("output contains %q:\n%s", s, r.out)
		}
	}
}

func (r threeResult) count(sub string) int { return strings.Count(squash(r.out), squash(sub)) }

// untouched asserts the Canvas (and the module) was left exactly as written.
func (r threeResult) untouched() {
	r.t.Helper()
	if r.changed || r.out != r.orig {
		r.t.Errorf("expected no change:\n%s", r.out)
	}
}

var numArray = regexp.MustCompile(`\[(-?[0-9.e+-]+(?:, ?-?[0-9.e+-]+){15})\]`)

// matrices returns every 16-number array literal of the output, in order.
func matrices(out string) [][16]float64 {
	var res [][16]float64
	for _, m := range numArray.FindAllStringSubmatch(out, -1) {
		var a [16]float64
		for i, s := range strings.Split(m[1], ",") {
			a[i], _ = strconv.ParseFloat(strings.TrimSpace(s), 64)
		}
		res = append(res, a)
	}
	return res
}

func box(extra string) string {
	return `<mesh ` + extra + `><boxGeometry args={[1, 1, 1]} /><meshStandardMaterial color="orange" roughness={0.4} /></mesh>`
}

func TestThreeSharesGeometryAndMaterial(t *testing.T) {
	r := compileThree(t, `<Canvas>`+box(`name="a" position={[1, 0, 0]}`)+box(`name="b" position={[2, 0, 0]}`)+
		`<mesh name="c"><sphereGeometry args={[1, 8, 8]} /><meshStandardMaterial color="orange" roughness={0.4} /></mesh></Canvas>`)
	if !r.changed {
		t.Fatal("the scene was not compiled")
	}
	// Named meshes are not instanced; they share one box geometry and one
	// material (also with the sphere).
	r.has("K3CompiledScene", "__t.geo('box', [1,1,1])", "__t.geo('sphere', [1,8,8])")
	if n := r.count("__t.geo("); n != 2 {
		t.Errorf("%d geometries, want 2", n)
	}
	if n := r.count("__t.mat("); n != 1 {
		t.Errorf("%d materials, want 1:\n%s", n, r.out)
	}
	if n := r.count("__t.mesh("); n != 3 {
		t.Errorf("%d meshes, want 3", n)
	}
	r.lacks("<mesh", "boxGeometry", "<Canvas>\n<K3")
}

func TestThreeStaticMatrices(t *testing.T) {
	r := compileThree(t, `<Canvas>
      <group name="g" position={[0, 1, 0]} rotation={[0, Math.PI / 2, 0]} scale={2}>
        <mesh name="m" position={[1, 0, 0]}><boxGeometry /><meshBasicMaterial /></mesh>
      </group>
    </Canvas>`)
	if !r.changed {
		t.Fatal("not compiled")
	}
	ms := matrices(r.out)
	if len(ms) != 3 {
		t.Fatalf("want 3 matrices (group local, mesh local, mesh world), got %d:\n%s", len(ms), r.out)
	}
	// Group: rotated 90 degrees about y, scaled by 2, moved up 1.
	wantMatrix(t, ms[0], [16]float64{0, 0, -2, 0, 0, 2, 0, 0, 2, 0, 0, 0, 0, 1, 0, 1})
	// The mesh's own matrix is local; the world one folds the group in.
	wantMatrix(t, ms[1], [16]float64{1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1, 0, 1, 0, 0, 1})
	wantMatrix(t, ms[2], [16]float64{0, 0, -2, 0, 0, 2, 0, 0, 2, 0, 0, 0, 0, 1, -2, 1})
	// The group's own world matrix equals its local one and is left out.
	r.has("__t.group(__t.root, [", "{name:'g',position:[0,1,0],rotation:[0,1.5707963267948966,0],scale:[2,2,2]}")
	r.has("<K3CompiledScene top build")
}

func TestThreeInstancesRepeatedMeshes(t *testing.T) {
	r := compileThree(t, `<Canvas>`+box(`position={[1, 0, 0]}`)+box(`position={[0, 2, 0]}`)+box(`position={[0, 0, 3]} scale={2}`)+
		`<mesh><sphereGeometry /><meshStandardMaterial /></mesh></Canvas>`)
	r.has("__t.mesh(__t.root, __g0, __m0, true)", "__t.instances(__t.root, __g1, __m1, new Float32Array([")
	if n := r.count("__t.instances("); n != 1 {
		t.Errorf("%d instanced groups, want 1", n)
	}
	// 48 numbers in one Float32Array.
	flat := regexp.MustCompile(`new Float32Array\(\[([^\]]*)\]`).FindStringSubmatch(r.out)
	if flat == nil || len(strings.Split(flat[1], ",")) != 48 {
		t.Fatalf("instance matrices: %v", flat)
	}
	nums := strings.Split(squash(flat[1]), ",")
	if nums[12] != "1" || nums[29] != "2" || nums[32] != "2" || nums[46] != "3" {
		t.Errorf("instance translations/scales wrong: %v", nums)
	}
}

func TestThreeInstancesMapOverConstArray(t *testing.T) {
	r := compileThree(t, `<Canvas>{GRID.map((p, i) => (
      <mesh position={[p.x, 0, p.z * 2]}><boxGeometry args={[1, 1, 1]} /><meshStandardMaterial color={i > 100 ? "red" : "blue"} /></mesh>
    ))}</Canvas>`, "const GRID = [{ x: 0, z: 0 }, { x: 1, z: 1 }, { x: 2, z: 2 }, { x: 3, z: 3 }];")
	r.has("__t.instances(", "new Float32Array([")
	r.lacks("GRID.map", "__t.mesh(", "dynamicInstances")
	flat := regexp.MustCompile(`new Float32Array\(\[([^\]]*)\]`).FindStringSubmatch(r.out)
	nums := strings.Split(squash(flat[1]), ",")
	if len(nums) != 64 || nums[12+16*3] != "3" || nums[14+16*3] != "6" {
		t.Errorf("mapped instance matrices wrong: %v", nums)
	}
}

func TestThreeNotInstancedWhenItWouldChangeBehavior(t *testing.T) {
	cases := map[string]string{
		"handler":     box(`onClick={() => {}}`) + box(`onClick={() => {}}`),
		"name":        box(`name="a"`) + box(`name="b"`),
		"transparent": strings.ReplaceAll(box(``), `roughness={0.4}`, `transparent opacity={0.5}`) + strings.ReplaceAll(box(``), `roughness={0.4}`, `transparent opacity={0.5}`),
		"reactive":    box(`rotation={[0, rotY(), 0]}`) + box(`rotation={[0, rotY(), 0]}`),
		"named group": `<group name="g">` + box(``) + box(``) + `</group>`,
		"one":         box(``),
	}
	for name, scene := range cases {
		t.Run(name, func(t *testing.T) {
			r := compileThree(t, `<Canvas>`+scene+`</Canvas>`)
			if !r.changed {
				t.Fatal("not compiled")
			}
			r.lacks("__t.instances(", "dynamicInstances")
			if r.count("__t.mesh(") < 1 {
				t.Errorf("no individual meshes:\n%s", r.out)
			}
		})
	}
}

func TestThreeReactiveTransformsAreDirectWrites(t *testing.T) {
	r := compileThree(t, `<Canvas>
      <mesh name="m" position={[x(), 1, 0]} rotation={[0, rotY(), 0]} scale={s()}><boxGeometry /><meshBasicMaterial /></mesh>
    </Canvas>`, "const x = () => 1, s = () => 2;")
	r.has("__t.fx(()=>{", "__o0.position.set(x(), 1, 0)", "__o0.rotation.set(0, rotY(), 0)", "__t.vec(__o0.scale, s())")
	// A reactive object keeps three's own matrix update: no matrix arrays.
	r.has("__t.mesh(__t.root, __g0, __m0, null")
	r.lacks("matrixAutoUpdate")
	if len(matrices(r.out)) != 0 {
		t.Errorf("a reactive mesh got a static matrix:\n%s", r.out)
	}
}

func TestThreeReactiveInstanceMatrices(t *testing.T) {
	r := compileThree(t, `<Canvas>
      <group position={[0, 2, 0]}>{RING.map((a) => (
        <mesh position={[Math.cos(a) * 2, 0, Math.sin(a) * 2]} rotation={[0, rotY() + a, 0]}>
          <boxGeometry args={[0.3, 0.3, 0.3]} /><meshStandardMaterial color="purple" />
        </mesh>
      ))}</group>
    </Canvas>`, "const RING = [0, 1, 2, 3];")
	// The instances are composed against the group's matrix and written one
	// item at a time into the instance buffer; the emptied group is dropped.
	r.has("__t.dynamicInstances(__t.root, __g0, __m0, 4, [", "RING.forEach((a, __i)=>", "__w0.setv(__i, Math.cos(a) * 2, 0, Math.sin(a) * 2, 0, rotY() + a, 0, 1, 1, 1)")
	r.lacks("RING.map", "<mesh", "__t.group(")
	ms := matrices(r.out)
	if len(ms) != 1 || !approx(ms[0][13], 2) {
		t.Errorf("parent matrix: %v", ms)
	}
}

func TestThreeReactiveInstanceWithScalarScale(t *testing.T) {
	r := compileThree(t, `<Canvas>{RING.map((a) => (
        <mesh position={[a, 0, 0]} scale={0.5 + wave()}><boxGeometry /><meshBasicMaterial /></mesh>
      ))}</Canvas>`, "const RING = [0, 1];\nconst wave = () => 0;")
	r.has("__w0.set(__i, [a, 0, 0], undefined, 0.5 + wave())")
}

func TestThreeEventsKeepMeshesIndividual(t *testing.T) {
	r := compileThree(t, `<Canvas>`+box(`position={[1, 0, 0]} onClick={() => setRotY(1)} onPointerOver={over}`)+box(`position={[2, 0, 0]}`)+`</Canvas>`, "const over = () => {};")
	r.has("{onClick:()=>setRotY(1),onPointerOver:over}")
	r.lacks("__t.instances(")
	if n := r.count("__t.mesh("); n != 2 {
		t.Errorf("%d meshes, want 2", n)
	}
}

func TestThreeIneligibleStaysJSX(t *testing.T) {
	cases := map[string]string{
		"ref":              box(`ref={ref}`),
		"spread props":     box(`{...props}`),
		"unknown element":  `<Thing />`,
		"drei component":   `<OrbitControls />`,
		"component child":  `<group><Thing /></group>`,
		"attach":           `<mesh><boxGeometry attach="geometry" /><meshStandardMaterial /></mesh>`,
		"primitive":        `<primitive object={obj} />`,
		"dispose null":     `<mesh><boxGeometry /><meshStandardMaterial dispose={null} /></mesh>`,
		"reactive args":    `<mesh><boxGeometry args={[w(), 1, 1]} /><meshStandardMaterial /></mesh>`,
		"reactive color":   `<mesh><boxGeometry /><meshStandardMaterial color={c()} /></mesh>`,
		"texture map":      `<mesh><boxGeometry /><meshStandardMaterial map={tex} /></mesh>`,
		"extra child":      `<mesh><boxGeometry /><meshStandardMaterial /><pointLight /></mesh>`,
		"two geometries":   `<mesh><boxGeometry /><sphereGeometry /><meshStandardMaterial /></mesh>`,
		"no material":      `<mesh><boxGeometry /></mesh>`,
		"unknown prop":     box(`userData={{ a: 1 }}`),
		"pierced prop":     box(`position-x={1}`),
		"object prop":      `<mesh geometry={geo} material={mat} />`,
		"light":            `<ambientLight intensity={1} />`,
		"number position":  box(`position={1}`),
		"signal list":      `{items().map((i) => ` + box(``) + `)}`,
		"conditional":      `{show() && ` + box(``) + `}`,
		"text":             `text`,
		"reactive in list": `{RING.map((a) => <mesh position={[a, 0, 0]} onClick={() => go(a)}><boxGeometry /><meshStandardMaterial /></mesh>)}`,
	}
	pre := "const obj = {}, geo = {}, mat = {}, tex = {}, RING = [1, 2], items = () => [1], show = () => true, c = () => 'red', w = () => 1, go = () => {};\nconst Thing = () => null;"
	for name, scene := range cases {
		t.Run(name, func(t *testing.T) {
			compileThree(t, `<Canvas>`+scene+`</Canvas>`, pre).untouched()
		})
	}
}

func TestThreeCompilesOnlyTheEligiblePart(t *testing.T) {
	r := compileThree(t, `<Canvas>
      <ambientLight intensity={1} />
      `+box(`name="plain"`)+`
      `+box(`name="byref" ref={ref}`)+`
      <Thing />
      <group name="holder" ref={ref}>`+box(`name="inner"`)+`</group>
    </Canvas>`, "const Thing = () => null;")
	if !r.changed {
		t.Fatal("nothing compiled")
	}
	// The mesh with a ref, the component, the light and the group with a ref
	// stay JSX; the plain mesh and the plain mesh inside the group compile.
	r.has("<ambientLight", "<Thing", "ref={ref}", "name=\"byref\"", "name=\"holder\"")
	if n := r.count("<K3CompiledScene"); n != 2 {
		t.Errorf("%d compiled elements, want 2 (one in the Canvas, one in the group)", n)
	}
	r.has("{name:'plain'", "{name:'inner'")
	// Only the compiled subtree sitting directly in the Canvas gets `top`.
	if n := r.count("top"); n != 1 {
		t.Errorf("top appears %d times, want 1:\n%s", n, r.out)
	}
}

func TestThreeGroupWithHandlerStaysJSXButItsChildrenCompile(t *testing.T) {
	r := compileThree(t, `<Canvas><group onClick={() => {}}>`+box(`name="kid"`)+`</group></Canvas>`)
	r.has("<group onClick", "<K3CompiledScene build")
	// Not directly in the scene: the subtree must not freeze its world matrices.
	r.lacks("top")
}

func TestThreeCompileProp(t *testing.T) {
	scene := func(attr string) string { return `<Canvas ` + attr + `>` + box(``) + `</Canvas>` }
	compileThree(t, scene(`compile={false}`)).untouched()
	compileThree(t, scene(`compile={flag()}`), "const flag = () => false;").untouched()
	compileThree(t, scene(`compile={OFF}`), "const OFF = false;").untouched()
	for _, attr := range []string{``, `compile`, `compile={true}`} {
		if r := compileThree(t, scene(attr)); !r.changed {
			t.Errorf("Canvas %q was not compiled", attr)
		}
	}
}

func TestThreeOnlyKrateThreeCanvas(t *testing.T) {
	src := `import { Canvas } from "@react-three/fiber";
export default function P() { return <Canvas>` + box(``) + `</Canvas>; }`
	prog, errs := tsstrip.Parse("page.tsx", src, tsstrip.Options{})
	if len(errs) > 0 {
		t.Fatal(errs)
	}
	if RewriteThree(prog) {
		t.Error("a Canvas of another package was compiled")
	}
	src = `import * as K from "@krate/three";
export default function P() { return <K.Canvas>` + box(``) + `</K.Canvas>; }`
	prog, errs = tsstrip.Parse("page.tsx", src, tsstrip.Options{})
	if len(errs) > 0 {
		t.Fatal(errs)
	}
	if !RewriteThree(prog) {
		t.Error("a namespace import of the Canvas was not compiled")
	}
}

func TestThreeImportAndIdempotence(t *testing.T) {
	r := compileThree(t, `<Canvas>`+box(``)+`</Canvas>`)
	r.has(`import { CompiledScene as K3CompiledScene } from "@krate/three/engine/compiled"`)
	// A module without a compilable Canvas gets no import.
	compileThree(t, `<Canvas><Thing /></Canvas>`, "const Thing = () => null;").untouched()
}

func TestThreeMixedSiblingsMerge(t *testing.T) {
	r := compileThree(t, `<Canvas>`+box(`position={[1, 0, 0]}`)+`<ambientLight />`+box(`position={[2, 0, 0]}`)+`</Canvas>`)
	// Both meshes end up in one compiled element, so they instance together.
	if n := r.count("<K3CompiledScene"); n != 1 {
		t.Errorf("%d compiled elements, want 1", n)
	}
	r.has("__t.instances(", "<ambientLight")
}
