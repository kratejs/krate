package build

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// threeResizeFiles is a page with three @krate/three canvases, one per
// frameloop, whose containers the page resizes continuously, as the Genesis
// builder's split divider does while it is dragged.
var threeResizeFiles = map[string]string{
	"src/components/Resizing.tsx": `'use client';
import { Canvas } from '@krate/three';

// Logs every renderer resize ("S", which clears the drawing buffer) and render
// ("R"). A ResizeObserver made after the canvas's own runs right after it,
// before the browser paints: a resize with no render after it at that point is
// a cleared canvas about to be shown.
function watch(name: string, s: any) {
  const w = window as any;
  const log: string[] = ((w.__events ||= {})[name] = []);
  (w.__violations ||= {})[name] = 0;
  const gl = s.gl, setSize = gl.setSize.bind(gl), render = gl.render.bind(gl);
  gl.setSize = (...a: any[]) => { log.push('S'); return setSize(...a); };
  gl.render = (...a: any[]) => { log.push('R'); return render(...a); };
  new ResizeObserver(() => { if (log[log.length - 1] === 'S') w.__violations[name]++; }).observe(gl.domElement.parentElement.parentElement);
}

function Viewer({ frameloop }: { frameloop: 'always' | 'demand' | 'never' }) {
  return (
    <div id={'box-' + frameloop} style={{ width: '320px', height: '200px' }}>
      <Canvas frameloop={frameloop} onCreated={(s) => { (s as any).gl.render((s as any).scene, (s as any).camera); watch(frameloop, s); }}>
        <mesh rotation={[0.4, 0.6, 0]}>
          <boxGeometry args={[1, 1, 1]} />
          <meshNormalMaterial />
        </mesh>
      </Canvas>
    </div>
  );
}

export default function Resizing() {
  return <div><Viewer frameloop="always" /><Viewer frameloop="demand" /><Viewer frameloop="never" /></div>;
}
`,
	"src/pages/index.tsx": `import Resizing from '../components/Resizing';
export default async function Page() { await Promise.resolve(); return <main><Resizing /></main>; }
`,
}

// TestUseClientIslandBrowserThreeResize resizes @krate/three canvases
// continuously (frameloop always, demand and never) and checks that a
// renderer resize never comes last before a paint: gl.setSize clears the
// drawing buffer, and the canvas went blank for frames while the Genesis
// builder's split divider was dragged. It also checks that every resize is
// followed by a render and that the drawing buffer ends at the final size.
func TestUseClientIslandBrowserThreeResize(t *testing.T) {
	t.Parallel()
	e2eEnv(t)
	repo, err := repoRootPath()
	if err != nil {
		t.Fatal(err)
	}
	threePkg := filepath.Join(repo, "packages", "three")
	threeLib := filepath.Join(threePkg, "node_modules", "three")
	if _, err := os.Stat(filepath.Join(threeLib, "package.json")); err != nil {
		requireE2E(t, "three is not installed for packages/three (run pnpm install): %v", err)
	}
	b, root := scProject(t, threeResizeFiles)
	withPreact(t, root)
	if err := linkRuntimePackage(t, filepath.Join(root, "node_modules", "@krate")); err != nil {
		t.Fatal(err)
	}
	for link, target := range map[string]string{
		filepath.Join(root, "node_modules", "@krate", "three"): threePkg,
		filepath.Join(root, "node_modules", "three"):           threeLib,
	} {
		if err := os.Symlink(target, link); err != nil {
			t.Skipf("cannot link %s: %v", target, err)
		}
	}
	if out, err := captureBuild(b); err != nil {
		t.Fatalf("BuildAll: %v\n%s", err, out)
	}
	browser := strings.ToLower(strings.TrimSpace(os.Getenv("KRATE_BROWSER")))
	if browser == "" {
		t.Skip("set KRATE_BROWSER to run")
	}
	if err := exec.Command("node", "-e", "require.resolve('playwright')").Run(); err != nil {
		requireE2E(t, "playwright is not installed (run pnpm install)")
	}
	base, err := startKrateServer(t, root, b.Cfg)
	if err != nil {
		t.Fatalf("serve: %v", err)
	}
	script, err := filepath.Abs(filepath.Join("testdata", "browser_three_resize.mjs"))
	if err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command("node", script, browser, base).CombinedOutput()
	if strings.Contains(string(out), "NO_WEBGL") {
		t.Skipf("%s has no WebGL here", browser)
	}
	if err != nil {
		if strings.Contains(string(out), "Executable doesn't exist") || strings.Contains(string(out), "browserType.launch") {
			requireE2E(t, "%s browser is not installed: %s", browser, out)
		}
		t.Fatalf("browser checks failed: %v\n%s", err, out)
	}
	t.Logf("%s", out)
}
