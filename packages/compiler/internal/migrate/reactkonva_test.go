package migrate

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// konvaOverlay and konvaHandles follow a real canvas editor: a Stage with an
// aliased Layer, a Transformer driven through a ref, and a second module whose
// shapes render inside that Stage. The type imports name konva itself.
const konvaOverlay = `"use client";

import type Konva from "konva";
import type { KonvaEventObject } from "konva/lib/Node";
import { useEffect, useRef } from "react";
import { Circle, Group, Layer as KLayer, Line, Rect, Stage, Text, Transformer } from "react-konva";
import { Handles } from "./handles";

export default function Overlay({ width, height }: { width: number; height: number }) {
  const stageRef = useRef<Konva.Stage>(null);
  const trRef = useRef<Konva.Transformer>(null);
  const boxRef = useRef<Konva.Rect>(null);
  useEffect(() => {
    trRef.current?.nodes(boxRef.current ? [boxRef.current] : []);
  }, []);
  const down = (e: KonvaEventObject<PointerEvent>) => {
    if (e.target !== e.target.getStage()) return;
  };
  return (
    <Stage ref={stageRef} width={width} height={height} onPointerDown={down}>
      <KLayer>
        <Group listening={false}>
          <Line points={[0, 0, 10, 10]} dash={[4, 4]} stroke="#000" />
          <Text text="label" />
        </Group>
        <Rect ref={boxRef} width={10} height={10} hitStrokeWidth={12} draggable onDragMove={() => {}} />
        <Circle radius={4} shadowBlur={2} onMouseEnter={() => {}} onMouseLeave={() => {}} />
        <Handles />
        <Transformer ref={trRef} keepRatio rotateEnabled anchorSize={8} ignoreStroke boundBoxFunc={(o, n) => n} />
      </KLayer>
    </Stage>
  );
}
`

const konvaHandles = `"use client";

import type { KonvaEventObject } from "konva/lib/Node";
import { Circle, Group, Path } from 'react-konva';

export function Handles() {
  return (
    <Group>
      <Path data="M0 0 L10 10" />
      <Circle radius={3} draggable onDragMove={(e: KonvaEventObject<DragEvent>) => e.target.x()} />
    </Group>
  );
}
`

func konvaProject(t *testing.T, handles string, extra map[string]string) string {
	t.Helper()
	files := map[string]string{
		"package.json":                        `{"dependencies":{"next":"16.0.0","react":"19.3.1","react-dom":"19.3.1","konva":"10.9.0","react-konva":"19.3.2","@krate/runtime":"0.5.0"}}`,
		"tsconfig.json":                       `{"compilerOptions":{"baseUrl":".","paths":{"@/*":["src/*"]}}}`,
		"src/components/builder/overlay.tsx":  konvaOverlay,
		"src/components/builder/handles.tsx":  handles,
		"src/app/page.tsx":                    "import Overlay from '@/components/builder/overlay';\nexport default function P() { return <main><Overlay width={10} height={10} /></main>; }\n",
		"src/app/layout.tsx":                  "export default function L({ children }: { children: any }) { return <html><body>{children}</body></html>; }\n",
		"src/components/unrelated/plain.tsx":  "export const Plain = () => <p>plain</p>;\n",
		"src/components/builder/geometry.ts":  "import Konva from 'konva';\nexport const box = (n: Konva.Node) => n.getClientRect();\n",
		"src/components/builder/use-shape.ts": "import type { ShapeConfig } from 'konva/lib/Shape';\nexport const shape = (c: ShapeConfig) => c.x;\n",
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

func TestMigrateReactKonvaToKrateKonva(t *testing.T) {
	rep, err := ScanNextWithOptions(konvaProject(t, konvaHandles, nil), ScanOptions{})
	if err != nil {
		t.Fatal(err)
	}
	overlay := plannedBody(t, rep, "src/components/builder/overlay.tsx")
	for _, want := range []string{
		`import { Circle, Group, Layer as KLayer, Line, Rect, Stage, Text, Transformer } from "@krate/konva";`,
		`import type Konva from "konva";`,
		`import type { KonvaEventObject } from "konva/lib/Node";`,
	} {
		if !strings.Contains(overlay, want) {
			t.Errorf("overlay missing %q:\n%s", want, overlay)
		}
	}
	if strings.Contains(overlay, "react-konva") {
		t.Errorf("overlay still imports react-konva:\n%s", overlay)
	}
	handles := plannedBody(t, rep, "src/components/builder/handles.tsx")
	if !strings.Contains(handles, `import { Circle, Group, Path } from '@krate/konva';`) || !strings.Contains(handles, `from "konva/lib/Node"`) {
		t.Errorf("handles not rewritten:\n%s", handles)
	}
	for _, f := range rep.Files {
		if f.Source == "src/components/builder/overlay.tsx" && !strings.Contains(strings.Join(f.Rewrites, "\n"), "Transformer") {
			t.Errorf("overlay rewrite note does not name the APIs: %v", f.Rewrites)
		}
		if strings.HasSuffix(f.Source, "geometry.ts") || strings.HasSuffix(f.Source, "use-shape.ts") {
			if strings.Contains(string(f.Content), "@krate/konva") {
				t.Errorf("%s only uses konva and must not change:\n%s", f.Source, f.Content)
			}
		}
	}
	var pkg struct {
		Dependencies map[string]string `json:"dependencies"`
	}
	if err := json.Unmarshal([]byte(plannedBody(t, rep, "package.json")), &pkg); err != nil {
		t.Fatal(err)
	}
	if pkg.Dependencies["@krate/konva"] == "" {
		t.Errorf("@krate/konva was not added: %v", pkg.Dependencies)
	}
	if pkg.Dependencies["konva"] == "" {
		t.Errorf("konva must stay a dependency: %v", pkg.Dependencies)
	}
	for _, m := range rep.Manual {
		if strings.Contains(m.Reason, "konva") {
			t.Errorf("unexpected manual item %+v", m)
		}
	}
	if !warned(rep, "moved 2 modules from react-konva to @krate/konva (Circle, Group, Layer, Line, Path, Rect, Stage, Text, Transformer)") {
		t.Errorf("missing summary warning: %v", rep.Warnings)
	}
}

func TestMigrateReactKonvaWithoutPackageUpdateAsksForDependency(t *testing.T) {
	rep, err := ScanNextWithOptions(konvaProject(t, konvaHandles, nil), ScanOptions{NoUpdatePackage: true})
	if err != nil {
		t.Fatal(err)
	}
	if !hasManual(rep, "package.json", "@krate/konva, which is not a dependency yet") {
		t.Errorf("expected a manual item for the @krate/konva dependency: %+v", rep.Manual)
	}
}

func TestMigrateReactKonvaCoreEntry(t *testing.T) {
	handles := strings.Replace(konvaHandles, `from 'react-konva'`, `from 'react-konva/lib/ReactKonvaCore'`, 1)
	rep, err := ScanNextWithOptions(konvaProject(t, handles, nil), ScanOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if got := plannedBody(t, rep, "src/components/builder/handles.tsx"); !strings.Contains(got, `from '@krate/konva'`) {
		t.Errorf("the core entry was not rewritten:\n%s", got)
	}
}

func TestMigrateReactKonvaKeepsReactKonvaForUnsupportedAPIs(t *testing.T) {
	cases := []struct {
		name    string
		handles string
		extra   map[string]string
		source  string
		reason  string
	}{
		{
			name:    "reconciler",
			handles: strings.Replace(konvaHandles, "Circle, Group, Path }", "Circle, Group, Path, KonvaRenderer }", 1),
			source:  "src/components/builder/handles.tsx",
			reason:  "uses KonvaRenderer from react-konva: @krate/konva has no React reconciler",
		},
		{
			name:    "internal subpath",
			handles: konvaHandles + "import { applyNodeProps } from 'react-konva/es/makeUpdates.js';\nexport const apply = applyNodeProps;\n",
			source:  "src/components/builder/handles.tsx",
			reason:  "imports react-konva/es/makeUpdates.js",
		},
		{
			name:    "package built on react-konva",
			handles: konvaHandles,
			extra: map[string]string{
				"node_modules/react-konva-utils/package.json": `{"name":"react-konva-utils","peerDependencies":{"react-konva":">=18"}}`,
				"src/components/builder/html.tsx":             "import { Html } from 'react-konva-utils';\nexport const Label = () => <Html><p>hi</p></Html>;\n",
				"src/app/label/page.tsx":                      "import { Label } from '@/components/builder/html';\nexport default function P() { return <Label />; }\n",
			},
			source: "src/components/builder/html.tsx",
			reason: "imports react-konva-utils, which is built on react-konva",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rep, err := ScanNextWithOptions(konvaProject(t, tc.handles, tc.extra), ScanOptions{})
			if err != nil {
				t.Fatal(err)
			}
			if !hasManual(rep, tc.source, tc.reason) {
				t.Fatalf("expected manual item %q on %s, got %+v", tc.reason, tc.source, rep.Manual)
			}
			for _, m := range rep.Manual {
				if strings.Contains(m.Reason, tc.reason) && !strings.Contains(m.Reason, "react-konva stays") {
					t.Errorf("the manual item must say react-konva stays: %+v", m)
				}
			}
			// All or nothing: the Stage and the shapes it renders keep one renderer.
			for _, f := range rep.Files {
				if strings.Contains(string(f.Content), "@krate/konva") {
					t.Errorf("%s was rewritten to @krate/konva although the project is blocked:\n%s", f.Source, f.Content)
				}
			}
			if !warned(rep, "kept react-konva in") {
				t.Errorf("missing kept-react-konva warning: %v", rep.Warnings)
			}
		})
	}
}

func TestMigrateReactKonvaLeavesProjectsWithoutReactKonvaAlone(t *testing.T) {
	root := gapsProject(t, map[string]string{
		"src/app/page.tsx": "import Konva from 'konva';\nexport default function P() { return <p>{Konva.version}</p>; }\n",
	})
	rep, err := ScanNextWithOptions(root, ScanOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(plannedBody(t, rep, "package.json"), "@krate/konva") || warned(rep, "@krate/konva") || warned(rep, "react-konva") {
		t.Errorf("a project without react-konva got @krate/konva: %v", rep.Warnings)
	}
}

// TestKrateKonvaNamesExist checks that every name the migrator treats as
// provided is exported by packages/konva (its Krate-source entry and its
// island build), so a rewritten import resolves in pages and islands alike.
func TestKrateKonvaNamesExist(t *testing.T) {
	pkg := filepath.Join("..", "..", "..", "konva", "src")
	read := func(dirs ...string) string {
		var src strings.Builder
		for _, dir := range dirs {
			entries, err := os.ReadDir(dir)
			if err != nil {
				t.Skipf("packages/konva not found: %v", err)
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
		return src.String()
	}
	exported := func(all, name string) bool {
		decl := regexp.MustCompile(`export\s+(?:declare\s+)?(?:const|function|class|interface|type|namespace|let|var)\s+` + regexp.QuoteMeta(name) + `\b`)
		listed := regexp.MustCompile(`export\s+(?:type\s+)?(?:\*\s+as\s+` + regexp.QuoteMeta(name) + `\b|\{[^}]*\b` + regexp.QuoteMeta(name) + `\b[^}]*\})`)
		return decl.MatchString(all) || listed.MatchString(all)
	}
	krateEntry := read(pkg, filepath.Join(pkg, "engine"))
	island := read(filepath.Join(pkg, "island"), filepath.Join(pkg, "engine"))
	for name := range krateKonvaNames {
		if !exported(krateEntry, name) {
			t.Errorf("@krate/konva does not export %s", name)
		}
		if !exported(island, name) {
			t.Errorf("@krate/konva/island does not export %s", name)
		}
	}
}
