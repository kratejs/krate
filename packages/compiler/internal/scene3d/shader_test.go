package scene3d

import (
	"context"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/compiler/ast"
	"github.com/kratejs/krate/packages/compiler/internal/diag"
	"github.com/kratejs/krate/packages/compiler/internal/scene3d/matc"
)

// fakeMatc writes a matc stand-in that copies its input to the -o file and
// appends a line to a counter file per run.
func fakeMatc(t *testing.T) (opts matc.Options, runs func() int, dir string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("shell script stand-in for matc")
	}
	tmp := t.TempDir()
	counter := filepath.Join(tmp, "runs")
	script := filepath.Join(tmp, "matc")
	body := "#!/bin/sh\nout=\"\"\nin=\"\"\nwhile [ $# -gt 0 ]; do\n  if [ \"$1\" = \"-o\" ]; then out=\"$2\"; fi\n  in=\"$1\"\n  shift\ndone\necho run >> " + counter + "\ncat \"$in\" > \"$out\"\n"
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	opts = matc.Options{Getenv: func(k string) string {
		if k == matc.EnvMatc {
			return script
		}
		return ""
	}}
	runs = func() int {
		b, err := os.ReadFile(counter)
		if err != nil {
			return 0
		}
		return strings.Count(string(b), "run")
	}
	return opts, runs, filepath.Join(tmp, "out")
}

func shaderOption(o matc.Options, dir string, seen *[][]byte) func(*Options) {
	return func(opts *Options) {
		opts.Shader = func(mat []byte) (string, error) {
			if seen != nil {
				*seen = append(*seen, mat)
			}
			path, _, err := o.Compile(context.Background(), mat, dir)
			if err != nil {
				return "", err
			}
			return "/assets/3d/" + filepath.Base(path), nil
		}
	}
}

const shaderScene = `<Canvas>
  <mesh>
    <boxGeometry />
    <shaderMaterial
      fragmentShader={FRAG}
      vertexShader="material.worldPosition.y += materialParams.amp * sin(materialParams.t);"
      uniforms={{ amp: 0.2, tint: [1, 0.5, 0.25], t: { type: "float", value: rotY() } }}
    />
  </mesh>
  <mesh position={[2, 0, 0]}>
    <boxGeometry />
    <shaderMaterial
      fragmentShader={FRAG}
      vertexShader="material.worldPosition.y += materialParams.amp * sin(materialParams.t);"
      uniforms={{ amp: 0.2, tint: [1, 0.5, 0.25], t: { type: "float", value: rotY() } }}
    />
  </mesh>
</Canvas>`

const fragConst = "const FRAG = `material.baseColor = vec4(materialParams.tint, 1.0);`;"

func TestShaderMaterialCompilesAndCaches(t *testing.T) {
	o, runs, dir := fakeMatc(t)
	var seen [][]byte
	r := compile(t, page(shaderScene, fragConst), shaderOption(o, dir, &seen))
	if r.err != nil || !r.changed {
		t.Fatalf("changed=%v err=%v diags=%v\n%s", r.changed, r.err, r.diags, r.out)
	}
	mat := string(seen[0])
	for _, want := range []string{
		"shadingModel : unlit,", "blending : opaque,", "requires : [ uv0 ]",
		"{ type : float, name : \"amp\" }", "{ type : float3, name : \"tint\" }", "{ type : float, name : \"t\" }",
		"void materialVertex(inout MaterialVertexInputs material) {", "material.worldPosition.y += materialParams.amp * sin(materialParams.t);",
		"void material(inout MaterialInputs material) {", "prepareMaterial(material);", "material.baseColor = vec4(materialParams.tint, 1.0);",
	} {
		if !strings.Contains(mat, want) {
			t.Errorf("generated .mat lacks %q:\n%s", want, mat)
		}
	}
	hash := matc.ShaderHash(seen[0])
	r.has(`__scene.material("custom", { url: "/assets/3d/` + hash + `.filamat", params: { amp: 0.2, tint: [1, 0.5, 0.25] } })`)
	// The reactive uniform stays reactive and the materials with reactive params are separate handles.
	r.has(`__scene.setMaterialParam(__m0, "t", rotY())`, `__scene.setMaterialParam(__m1, "t", rotY())`)
	if _, err := os.Stat(filepath.Join(dir, hash+".filamat")); err != nil {
		t.Errorf("the compiled material is not in the cache dir: %v", err)
	}
	if runs() != 1 {
		t.Errorf("matc ran %d times for two identical sources, want 1", runs())
	}
	// A second build finds the hashed file and does not run matc again.
	compile(t, page(shaderScene, fragConst), shaderOption(o, dir, nil))
	if runs() != 1 {
		t.Errorf("matc ran again on a cache hit (%d runs)", runs())
	}
}

func TestShaderMaterialSharedWhenStatic(t *testing.T) {
	o, runs, dir := fakeMatc(t)
	r := compile(t, page(`<Canvas>
      <mesh><boxGeometry /><shaderMaterial fragmentShader="material.baseColor = vec4(1.0);" /></mesh>
      <mesh position={[1, 0, 0]}><boxGeometry /><shaderMaterial fragmentShader="material.baseColor = vec4(1.0);" /></mesh>
    </Canvas>`, ""), shaderOption(o, dir, nil))
	if n := r.count(`__scene.material("custom"`); n != 1 {
		t.Errorf("identical static shader materials should share a handle, got %d:\n%s", n, r.out)
	}
	r.has("__scene.instanced(__g0, __m0, 2,")
	if runs() != 1 {
		t.Errorf("matc runs = %d", runs())
	}
}

func TestShaderMaterialSourceVariants(t *testing.T) {
	src := string(buildMaterialSource("material.baseColor = vec4(1.0);", "", true, true, nil))
	for _, want := range []string{"shadingModel : lit,", "blending : transparent,", "requires : [ uv0 ]\n}"} {
		if !strings.Contains(src, want) {
			t.Errorf("missing %q:\n%s", want, src)
		}
	}
	if strings.Contains(src, "parameters") || strings.Contains(src, "vertex {") || strings.Contains(src, "*= material.baseColor.a") {
		t.Errorf("unexpected sections in lit transparent source:\n%s", src)
	}
	unlit := string(buildMaterialSource("x;", "", false, true, nil))
	if !strings.Contains(unlit, "material.baseColor.rgb *= material.baseColor.a;") {
		t.Errorf("unlit transparent shaders premultiply alpha:\n%s", unlit)
	}
}

func TestShaderMaterialTransparentOption(t *testing.T) {
	o, _, dir := fakeMatc(t)
	var seen [][]byte
	r := compile(t, page(`<Canvas><mesh><boxGeometry /><shaderMaterial transparent shadingModel="lit" fragmentShader="material.baseColor = vec4(1.0, 0.0, 0.0, 0.5);" /></mesh></Canvas>`, ""), shaderOption(o, dir, &seen))
	r.has("transparent: true")
	if !strings.Contains(string(seen[0]), "blending : transparent,") || !strings.Contains(string(seen[0]), "shadingModel : lit,") {
		t.Errorf("bad source:\n%s", seen[0])
	}
}

func TestShaderMaterialMatcMissingOfflineIsADiagnostic(t *testing.T) {
	o := matc.Options{HomeDir: t.TempDir(), Getenv: func(k string) string {
		if k == matc.EnvOffline {
			return "1"
		}
		return ""
	}}
	r := compile(t, page(`<Canvas><mesh><boxGeometry /><shaderMaterial fragmentShader="material.baseColor = vec4(1.0);" /></mesh></Canvas>`, ""),
		shaderOption(o, t.TempDir(), nil))
	if r.err == nil {
		t.Fatal("want a build error when matc is unavailable offline")
	}
	d, ok := r.err.(diag.Diagnostic)
	if !ok {
		t.Fatalf("error is %T, want a diag.Diagnostic: %v", r.err, r.err)
	}
	if d.File != "page.tsx" || d.Line == 0 || d.Hint == "" || !strings.Contains(d.Hint+d.Message, matc.EnvMatc) {
		t.Errorf("diagnostic lacks position or a hint about %s: %+v", matc.EnvMatc, d)
	}
}

func TestShaderMaterialWithoutCompilerIsADiagnostic(t *testing.T) {
	r := compile(t, page(`<Canvas><mesh><boxGeometry /><shaderMaterial fragmentShader="material.baseColor = vec4(1.0);" /></mesh></Canvas>`, ""))
	if d, ok := r.err.(diag.Diagnostic); !ok || d.Hint == "" {
		t.Fatalf("want a diagnostic with a hint, got %v", r.err)
	}
}

func TestShaderMaterialBadUniformsFallBack(t *testing.T) {
	for _, u := range []string{`{ "bad-name": 1 }`, `{ a: "str" }`, `{ a: [1, 2] }`, `{ a: { type: "mat4", value: 1 } }`, `{ ...o }`} {
		src := `<Canvas><mesh><boxGeometry /><shaderMaterial fragmentShader="x;" uniforms={` + u + `} /></mesh></Canvas>`
		r := compile(t, page(src, "declare const o: object;"), func(o *Options) {
			o.Shader = func([]byte) (string, error) { t.Errorf("compiled despite bad uniforms %s", u); return "/x", nil }
		})
		if r.err != nil {
			// A malformed object literal can fail to parse; that is not the pass's concern.
			continue
		}
		if r.changed || len(r.diags) != 1 {
			t.Errorf("uniforms %s: changed=%v diags=%v", u, r.changed, r.diags)
		}
	}
}

func TestFolderShadowBlocksModuleConst(t *testing.T) {
	f := newFolder(map[string]ast.Expr{"X": &ast.Literal{Kind: ast.NumberLit, Value: "3"}})
	if v, ok := f.fold(ident("X"), nil); !ok || v.n != 3 {
		t.Fatalf("module const not folded: %v %v", v, ok)
	}
	g := newFolder(f.consts)
	g.shadow = map[string]bool{"X": true}
	if _, ok := g.fold(ident("X"), nil); ok {
		t.Error("a shadowed name must not fold to the module const")
	}
	sc := &scope{vars: map[string]value{"X": num(9)}}
	if v, ok := g.fold(ident("X"), sc); !ok || v.n != 9 {
		t.Errorf("a loop variable wins over the shadow check: %v %v", v, ok)
	}
}

func TestFoldArithmetic(t *testing.T) {
	cases := map[string]float64{
		"Math.PI / 2":              math.Pi / 2,
		"-Math.PI":                 -math.Pi,
		"2 ** 3 + 1":               9,
		"Math.max(1, 4) * 2":       8,
		"[1, 2, 3][1]":             2,
		"0x10 + 1_000":             1016,
		"(1 + 2) * 3":              9,
		"true ? 5 : 6":             5,
		"Math.sqrt(16) % 3":        1,
		"Math.round(2.5)":          3,
		"'ab'.length":              2,
		"[1, 2, 3].length":         3,
		"Math.min(Math.PI, 3) / 3": 1,
	}
	for src, want := range cases {
		prog, errs := parseExpr(src)
		if len(errs) > 0 {
			t.Fatalf("%s: %v", src, errs)
		}
		v, ok := newFolder(nil).fold(prog, nil)
		if !ok || v.k != vNum || !approx(v.n, want) {
			t.Errorf("%s = %v (ok=%v), want %v", src, v, ok, want)
		}
	}
	for _, src := range []string{"foo()", "x + 1", "Math.random()", "a.b", "[...a]", "{ a: f() }"} {
		prog, _ := parseExpr(src)
		if _, ok := newFolder(nil).fold(prog, nil); ok {
			t.Errorf("%s must not fold", src)
		}
	}
}
