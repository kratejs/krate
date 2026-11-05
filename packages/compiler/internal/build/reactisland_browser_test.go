package build

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestBrowserReactLibraryIslands runs React-library islands in a real browser:
// store hooks shared across islands, a form hook, a CommonJS hook library,
// client:visible deferral and a CSS-in-JS island. It needs KRATE_BROWSER like
// the other browser gates.
func TestBrowserReactLibraryIslands(t *testing.T) {
	browser := strings.ToLower(strings.TrimSpace(os.Getenv("KRATE_BROWSER")))
	if browser == "" {
		t.Skip("set KRATE_BROWSER=chromium|firefox|webkit to run browser checks")
	}
	if _, err := exec.LookPath("node"); err != nil {
		requireE2E(t, "node is not installed")
	}
	if err := exec.Command("node", "-e", "require.resolve('playwright')").Run(); err != nil {
		requireE2E(t, "playwright is not installed (run pnpm install)")
	}

	script, err := filepath.Abs(filepath.Join("testdata", "browser_react_islands.mjs"))
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	cfg := setupIslandProject(t, root, true)
	cfg.Minify = true
	repo, err := repoRootPath()
	if err != nil {
		t.Fatal(err)
	}
	// The pages hydrate with the built runtime package.
	runtimeSrc := filepath.Join(repo, "examples", "node_modules", "@krate", "runtime")
	if _, err := os.Stat(filepath.Join(runtimeSrc, "dist", "krate-runtime.js")); err != nil {
		requireE2E(t, "the runtime package is not built: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(root, "node_modules", "@krate"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(runtimeSrc, filepath.Join(root, "node_modules", "@krate", "runtime")); err != nil {
		t.Skipf("cannot link the runtime package: %v", err)
	}
	// The runtime bundle is found next to the working directory.
	t.Chdir(root)
	reactLibFixture(t, root)
	writeProjectFile(t, root, "src/store.ts", `
		import { create } from 'fake-store';
		export const useCount = create((set) => ({ n: 1, inc: () => set((s) => ({ n: s.n + 1 })) }));
	`)
	writeProjectFile(t, root, "src/components/Counter.tsx", `
		import { useCount } from '../store';
		export function Counter({ label }: { label: string }) {
			const n = useCount((s) => s.n);
			const inc = useCount((s) => s.inc);
			return <button id="inc" onClick={inc}>{label} {n}</button>;
		}
	`)
	writeProjectFile(t, root, "src/components/Display.tsx", `
		import { useCount } from '../store';
		export default function Display() { return <p id="disp">shown {useCount((s) => s.n)}</p>; }
	`)
	writeProjectFile(t, root, "src/components/Hello.tsx", `
		import { useGreeting } from 'fake-cjs';
		export default function Hello() { return <i id="hello">{useGreeting('krate')}</i>; }
	`)
	writeProjectFile(t, root, "src/components/Form.tsx", `
		import { useForm } from 'fake-form';
		export default function Form() {
			const f = useForm();
			return <div><input id="name" onInput={(e) => f.set((e.target as HTMLInputElement).value)} /><p id="echo">{f.value()}</p></div>;
		}
	`)
	writeProjectFile(t, root, "src/pages/index.tsx", `
		import { createSignal } from '@krate/runtime';
		import { Counter } from '../components/Counter';
		import Display from '../components/Display';
		import Hello from '../components/Hello';
		import Form from '../components/Form';
		export default function Page() {
			const [label, setLabel] = createSignal('count');
			return (
				<main>
					<button id="rename" onClick={() => setLabel('clicks')}>rename</button>
					<Counter label={label()} />
					<Display />
					<Hello />
					<Form />
				</main>
			);
		}
	`)
	writeProjectFile(t, root, "src/pages/vis.tsx", `
		import { Badge } from 'fake-lib';
		export default function Page() {
			return <div><div style={{ height: 2400 }}>spacer</div><Badge client:visible label="deferred" /></div>;
		}
	`)
	writeProjectFile(t, root, "src/pages/idle.tsx", `
		import { Badge } from 'fake-lib';
		export default function Page() { return <Badge client:idle label="idle" />; }
	`)
	writeProjectFile(t, root, "src/pages/styled.tsx", `
		import styled from 'styled-components';
		const Box = styled.div`+"`color: rgb(255, 0, 0);`"+`;
		export default function Page() { return <Box id="o">styled</Box>; }
	`)
	// The fake Badge renders <span class="badge">; give the deferred one an id.
	writeProjectFile(t, root, "node_modules/fake-lib/index.js", `
		import { jsx } from 'react/jsx-runtime';
		export function Badge({ label }) { return jsx('span', { id: 'deferred', className: 'badge', children: label }); }
	`)
	if _, err := captureOutput(func() error { return New(root, cfg).BuildAll() }); err != nil {
		t.Fatalf("BuildAll: %v", err)
	}
	cmd := exec.Command("node", script, cfg.OutDir, browser)
	out, err := cmd.CombinedOutput()
	if err != nil {
		if strings.Contains(string(out), "Executable doesn't exist") || strings.Contains(string(out), "browserType.launch") {
			requireE2E(t, "%s browser is not installed: %s", browser, out)
		}
		t.Fatalf("browser checks failed: %v\n%s", err, out)
	}
	t.Logf("%s", out)
}
