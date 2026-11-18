package migrate

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

const threeViewer = `'use client';
import { Canvas, useThree, type ThreeEvent } from '@react-three/fiber';
import * as React from 'react';
import { VehicleScene } from './scene';

function Resume({ running }: { running: boolean }) {
  const invalidate = useThree((s) => s.invalidate);
  React.useEffect(() => { if (running) invalidate(); }, [running, invalidate]);
  return null;
}

export default function Viewer({ url }: { url: string }) {
  return (
    <Canvas frameloop="demand" camera={{ fov: 35, position: [4, 2, 4] }}>
      <VehicleScene url={url} />
      <Resume running />
    </Canvas>
  );
}
`

const threeScene = `import { Environment, Lightformer, OrbitControls, useGLTF } from "@react-three/drei";
import {
  useFrame,
  useThree,
} from "@react-three/fiber";
import * as React from "react";
import * as THREE from "three";
import { MeshoptDecoder } from "three/examples/jsm/libs/meshopt_decoder.module.js";
import type { RootState } from "@react-three/fiber";

const extendLoader = (loader: { setMeshoptDecoder?: (d: unknown) => unknown }) => loader.setMeshoptDecoder?.(MeshoptDecoder);

function Model({ url }: { url: string }) {
  const gltf = useGLTF(url, false, false, extendLoader as never);
  useFrame((state: RootState) => state.invalidate());
  return <primitive object={gltf.scene} />;
}

export function VehicleScene({ url }: { url: string }) {
  const aspect = useThree((s) => s.size.width / s.size.height);
  return (
    <>
      <Environment resolution={128} background={false}>
        <Lightformer form="rect" intensity={2.2} position={[0, 5, 0]} />
      </Environment>
      <React.Suspense fallback={null}><Model url={url} /></React.Suspense>
      <OrbitControls makeDefault minDistance={aspect} />
    </>
  );
}
useGLTF.preload("/car.glb");
`

func threeProject(t *testing.T, scene string, extra map[string]string) string {
	t.Helper()
	files := map[string]string{
		"package.json":                       `{"dependencies":{"next":"16.0.0","react":"19.0.0","@react-three/fiber":"9.8.2","@react-three/drei":"10.7.9","three":"0.186.1"}}`,
		"tsconfig.json":                      `{"compilerOptions":{"baseUrl":".","paths":{"@/*":["src/*"]}}}`,
		"src/components/viewer/viewer.tsx":   threeViewer,
		"src/components/viewer/scene.tsx":    scene,
		"src/app/page.tsx":                   "import Viewer from '@/components/viewer/viewer';\nexport default function P() { return <main><Viewer url=\"/car.glb\" /></main>; }\n",
		"src/app/layout.tsx":                 "export default function L({ children }: { children: any }) { return <html><body>{children}</body></html>; }\n",
		"src/components/unrelated/plain.tsx": "export const Plain = () => <p>plain</p>;\n",
	}
	for k, v := range extra {
		files[k] = v
	}
	root := t.TempDir()
	for rel, body := range files {
		writeNextFile(t, root, rel, body)
	}
	return root
}

func plannedBody(t *testing.T, rep *Report, source string) string {
	t.Helper()
	for _, f := range rep.Files {
		if f.Source == source {
			return string(f.Content)
		}
	}
	t.Fatalf("no planned file for %s", source)
	return ""
}

func TestMigrateReactThreeFiberToKrateThree(t *testing.T) {
	root := threeProject(t, threeScene, nil)
	rep, err := ScanNextWithOptions(root, ScanOptions{})
	if err != nil {
		t.Fatal(err)
	}
	viewer := plannedBody(t, rep, "src/components/viewer/viewer.tsx")
	if !strings.Contains(viewer, `import { Canvas, useThree, type ThreeEvent } from '@krate/three';`) || strings.Contains(viewer, "@react-three") {
		t.Errorf("viewer fiber import not rewritten:\n%s", viewer)
	}
	scene := plannedBody(t, rep, "src/components/viewer/scene.tsx")
	for _, want := range []string{
		`import { Environment, Lightformer, OrbitControls, useGLTF } from "@krate/three/drei";`,
		"  useThree,\n} from \"@krate/three\";",
		`import type { RootState } from "@krate/three";`,
		`from "three/examples/jsm/libs/meshopt_decoder.module.js"`,
	} {
		if !strings.Contains(scene, want) {
			t.Errorf("scene missing %q:\n%s", want, scene)
		}
	}
	if strings.Contains(scene, "@react-three") {
		t.Errorf("scene still imports @react-three:\n%s", scene)
	}
	for _, f := range rep.Files {
		if f.Source == "src/components/viewer/scene.tsx" && !strings.Contains(strings.Join(f.Rewrites, "\n"), "useGLTF") {
			t.Errorf("scene rewrite note does not name the APIs: %v", f.Rewrites)
		}
	}
	var pkg struct {
		Dependencies map[string]string `json:"dependencies"`
	}
	if err := json.Unmarshal([]byte(plannedBody(t, rep, "package.json")), &pkg); err != nil {
		t.Fatal(err)
	}
	if pkg.Dependencies["@krate/three"] == "" {
		t.Errorf("@krate/three was not added: %v", pkg.Dependencies)
	}
	for _, m := range rep.Manual {
		if strings.Contains(m.Reason, "@react-three") {
			t.Errorf("unexpected manual item %+v", m)
		}
	}
	if !warned(rep, "moved 2 modules from @react-three/fiber to @krate/three and Environment, Lightformer, OrbitControls, useGLTF from @react-three/drei to @krate/three/drei") {
		t.Errorf("missing summary warning: %v", rep.Warnings)
	}
}

func TestMigrateReactThreeWithoutPackageUpdateAsksForDependency(t *testing.T) {
	rep, err := ScanNextWithOptions(threeProject(t, threeScene, nil), ScanOptions{NoUpdatePackage: true})
	if err != nil {
		t.Fatal(err)
	}
	if !hasManual(rep, "package.json", "@krate/three, which is not a dependency yet") {
		t.Errorf("expected a manual item for the @krate/three dependency: %+v", rep.Manual)
	}
}

func TestMigrateReactThreeKeepsFiberForUnsupportedAPIs(t *testing.T) {
	cases := []struct {
		name   string
		scene  string
		extra  map[string]string
		source string
		reason string
	}{
		{
			name:   "drei component",
			scene:  strings.Replace(threeScene, "OrbitControls, useGLTF }", "OrbitControls, useGLTF, Text }", 1),
			source: "src/components/viewer/scene.tsx",
			reason: "uses Text from @react-three/drei, which @krate/three/drei does not provide",
		},
		{
			name:   "environment preset",
			scene:  strings.Replace(threeScene, "<Environment resolution={128}", `<Environment preset="city" resolution={128}`, 1),
			source: "src/components/viewer/scene.tsx",
			reason: "uses <Environment preset>",
		},
		{
			name:   "fiber reconciler",
			scene:  strings.Replace(threeScene, "  useFrame,\n", "  useFrame,\n  reconciler,\n", 1),
			source: "src/components/viewer/scene.tsx",
			reason: "uses reconciler from @react-three/fiber: @krate/three has no React reconciler",
		},
		{
			name:  "fiber-based package",
			scene: threeScene,
			extra: map[string]string{
				"node_modules/@react-spring/three/package.json": `{"name":"@react-spring/three","peerDependencies":{"@react-three/fiber":">=6"}}`,
				"src/components/viewer/spring.tsx":              "import { animated } from '@react-spring/three';\nexport const Spin = () => <animated.mesh />;\n",
				"src/app/spin/page.tsx":                         "import { Spin } from '@/components/viewer/spring';\nexport default function P() { return <Spin />; }\n",
			},
			source: "src/components/viewer/spring.tsx",
			reason: "imports @react-spring/three, which is built on @react-three/fiber",
		},
		{
			name:   "drei subpath",
			scene:  strings.Replace(threeScene, `import * as React from "react";`, "import * as React from \"react\";\nimport { Html } from \"@react-three/drei/web/Html\";", 1),
			source: "src/components/viewer/scene.tsx",
			reason: "imports a @react-three/drei subpath",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rep, err := ScanNextWithOptions(threeProject(t, tc.scene, tc.extra), ScanOptions{})
			if err != nil {
				t.Fatal(err)
			}
			if !hasManual(rep, tc.source, tc.reason) {
				t.Fatalf("expected manual item %q on %s, got %+v", tc.reason, tc.source, rep.Manual)
			}
			// All or nothing: no module moves, so the canvas and its scene keep one renderer.
			for _, f := range rep.Files {
				if strings.Contains(string(f.Content), "@krate/three") {
					t.Errorf("%s was rewritten to @krate/three although the project is blocked:\n%s", f.Source, f.Content)
				}
			}
			if !warned(rep, "kept @react-three/fiber") {
				t.Errorf("missing kept-fiber warning: %v", rep.Warnings)
			}
		})
	}
}

func TestMigrateReactThreeLeavesProjectsWithoutFiberAlone(t *testing.T) {
	root := gapsProject(t, map[string]string{
		"src/app/page.tsx": "import * as THREE from 'three';\nexport default function P() { return <p>{String(new THREE.Vector3().x)}</p>; }\n",
	})
	rep, err := ScanNextWithOptions(root, ScanOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(plannedBody(t, rep, "package.json"), "@krate/three") || warned(rep, "@krate/three") {
		t.Errorf("a project without fiber got @krate/three: %v", rep.Warnings)
	}
}

func TestThreeClauseNames(t *testing.T) {
	src := "F.useFrame(); F.Canvas; G.x"
	for clause, want := range map[string]string{
		"{ Canvas, useThree as useT, type ThreeEvent }": "Canvas,useThree",
		"type { RootState }":                            "",
		"* as F":                                        "Canvas,useFrame",
		"Fiber, { Canvas }":                             "Canvas,default",
		"{\n  useFrame,\n  useThree,\n}":                "useFrame,useThree",
	} {
		names, _ := threeClauseNames(src, clause)
		sort.Strings(names)
		if got := strings.Join(names, ","); got != want {
			t.Errorf("threeClauseNames(%q) = %q, want %q", clause, got, want)
		}
	}
}

func warned(rep *Report, substring string) bool {
	for _, w := range rep.Warnings {
		if strings.Contains(w, substring) {
			return true
		}
	}
	return false
}

// TestKrateThreeFiberNamesExist checks that every name the migrator treats as
// provided is exported by packages/three, so a rewritten import resolves.
func TestKrateThreeFiberNamesExist(t *testing.T) {
	repo := filepath.Join("..", "..", "..", "three", "src")
	var src strings.Builder
	for _, dir := range []string{repo, filepath.Join(repo, "engine")} {
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Skipf("packages/three not found: %v", err)
		}
		for _, e := range entries {
			if e.IsDir() || strings.Contains(e.Name(), ".test.") {
				continue
			}
			data, err := os.ReadFile(filepath.Join(dir, e.Name()))
			if err != nil {
				t.Fatal(err)
			}
			src.Write(data)
			src.WriteByte('\n')
		}
	}
	all := src.String()
	exported := func(name string) bool {
		decl := regexp.MustCompile(`export\s+(?:declare\s+)?(?:const|function|class|interface|type|namespace|let)\s+` + regexp.QuoteMeta(name) + `\b`)
		listed := regexp.MustCompile(`export\s+(?:type\s+)?(?:\*\s+as\s+` + regexp.QuoteMeta(name) + `\b|\{[^}]*\b` + regexp.QuoteMeta(name) + `\b[^}]*\})`)
		return decl.MatchString(all) || listed.MatchString(all)
	}
	for name := range krateThreeFiberNames {
		if !exported(name) {
			t.Errorf("@krate/three does not export %s", name)
		}
	}
	dreiSrc, err := os.ReadFile(filepath.Join(repo, "drei.tsx"))
	if err != nil {
		t.Fatal(err)
	}
	for name := range krateDreiNames {
		if !regexp.MustCompile(`export\s+(?:const|function|interface|type)\s+` + name + `\b`).Match(dreiSrc) {
			t.Errorf("@krate/three/drei does not export %s", name)
		}
	}
}
