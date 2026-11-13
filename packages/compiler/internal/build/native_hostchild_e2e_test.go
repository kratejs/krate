package build

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Host children, end to end through the real shell: TypeScript host code on
// Node, Bun and Deno, started by krate-shell as the app.json says, called
// from the page through the real protocol. The fixture has no SDK calls
// (the SDK areas are not wired into the children), only host functions, a
// host signal, a stream and an error, which is what the child runtime
// implements. On Windows the transport is loopback TCP with a token, so this
// is also the test that runs the Windows host child path.

const hostChildHost = `import { hostSignal } from '@krate/app/host';

export const ticks = hostSignal(0);
let total = 0;

export function whoami(): string {
  const g = globalThis as any;
  if (g.Deno) return 'deno ' + g.Deno.version.deno;
  if (g.Bun) return 'bun ' + g.Bun.version;
  return 'node ' + g.process.versions.node;
}

export async function bump(by: number): Promise<number> {
  total += by;
  ticks.set(total);
  return total;
}

export async function* count(to: number) {
  for (let i = 1; i <= to; i++) yield i;
}

export function fail(): never {
  const e = new Error('nope') as Error & { code: string };
  e.code = 'custom_code';
  throw e;
}

export function echo(text: string, bytes: Uint8Array): string {
  return text + '|' + bytes.length + '|' + bytes[bytes.length - 1];
}
`

const hostChildPage = `import { createSignal } from '@krate/runtime';
import { ticks, whoami, bump, count, fail, echo } from '../lib/child.host';

export default function P() {
  const [who, setWho] = createSignal('');
  const [bumped, setBumped] = createSignal('');
  const [sum, setSum] = createSignal('');
  const [error, setError] = createSignal('');
  const [echoed, setEchoed] = createSignal('');
  return (
    <main>
      <p id="ticks">Ticks: {ticks()}</p>
      <button id="who" onClick={async () => setWho(await whoami())}>who</button>
      <p id="whoami">{who()}</p>
      <button id="bump" onClick={async () => setBumped(String(await bump(2)))}>bump</button>
      <p id="bumped">{bumped()}</p>
      <button id="count" onClick={async () => { let s = 0; for await (const n of count(4)) s += n; setSum(String(s)); }}>count</button>
      <p id="sum">{sum()}</p>
      <button id="fail" onClick={async () => { try { await fail(); } catch (e) { setError(e.code + ': ' + e.message); } }}>fail</button>
      <p id="error">{error()}</p>
      <button id="echo" onClick={async () => setEchoed(await echo('héllo 世界', new Uint8Array([1, 2, 255])))}>echo</button>
      <p id="echoed">{echoed()}</p>
    </main>
  );
}
`

// requireHostRuntime finds a runtime on PATH. A runtime with no build for
// this machine (Bun and Deno have no Windows on ARM builds, for example) is
// named in KRATE_SKIP_RUNTIMES by the CI job, which skips just that runtime;
// KRATE_REQUIRE_E2E does not turn that into a failure. Any other missing
// runtime is a failure under KRATE_REQUIRE_E2E.
func requireHostRuntime(t *testing.T, name string) {
	t.Helper()
	if _, err := exec.LookPath(name); err == nil {
		return
	}
	for _, s := range strings.FieldsFunc(os.Getenv("KRATE_SKIP_RUNTIMES"), func(r rune) bool { return r == ',' || r == ' ' }) {
		if s == name {
			t.Skipf("%s is not available on this machine and KRATE_SKIP_RUNTIMES names it", name)
		}
	}
	requireE2E(t, "%s is not installed", name)
}

// hostChildProject writes the fixture inside examples/native-tasks so that it
// resolves @krate/app and @krate/runtime from that example's node_modules.
func hostChildProject(t *testing.T, ex, runtime string) string {
	t.Helper()
	root, err := os.MkdirTemp(ex, "hostchild-e2e-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(root) })
	writeTree(t, root, map[string]string{
		"krate.config.ts": `import { defineConfig } from '@krate/core';
export default defineConfig({
  entry: 'src/pages/index.tsx',
  pagesDir: 'src/pages',
  outDir: 'dist',
  native: {
    appId: 'com.example.hostchild',
    productName: 'Host Child',
    version: '1.0.0',
    window: { width: 640, height: 480 },
    host: { runtime: '` + runtime + `' },
  },
});
`,
		"src/lib/child.host.ts": hostChildHost,
		"src/pages/index.tsx":   hostChildPage,
	})
	return root
}

func TestNativeHostChildGUI(t *testing.T) {
	if os.Getenv("KRATE_NATIVE_GUI") != "1" {
		t.Skip("set KRATE_NATIVE_GUI=1 to run the host child end-to-end test")
	}
	ex := e2eRequireDeps(t)
	work := t.TempDir()
	bin := e2eBuildShell(t, work)
	for _, rt := range []string{"node", "bun", "deno"} {
		rt := rt
		t.Run(rt, func(t *testing.T) {
			requireHostRuntime(t, rt)
			root := hostChildProject(t, ex, rt)
			appDir := e2eBuildNative(t, root, filepath.Join(t.TempDir(), "dist-native"))

			s := e2eStartShell(t, bin, appDir)
			s.waitReady(90 * time.Second)

			flow := strings.ReplaceAll(e2eReadFlow(t, "flow_hostchild.js"), "__RUNTIME__", rt)
			var steps []e2eStep
			if err := s.query(flow, &steps); err != nil {
				t.Fatalf("flow: %v", err)
			}
			e2eCheckSteps(t, steps, 8)

			// The page threw nothing the flow did not expect.
			var errs []string
			if err := s.query(`(globalThis.__krateErrs || []).map(String)`, &errs); err == nil && len(errs) > 0 {
				t.Errorf("page errors: %v", errs)
			}
			if _, err := s.send("quit", 15*time.Second); err != nil {
				t.Errorf("quit: %v", err)
			}
		})
	}
}
