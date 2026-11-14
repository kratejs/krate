package build

import (
	"bufio"
	"bytes"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/kratejs/krate/packages/compiler/internal/config"
)

const nodeHostModule = `import { readFileSync } from "node:fs";
export function sizeOf(path: string): number {
  return readFileSync(path, "utf8").length;
}
export function runtime(): string {
  return "node " + (typeof process.versions.node);
}
export async function* ticks(n: number) {
  for (let i = 0; i < n; i++) yield i;
}
`

const nodeRuntimePage = `import { sizeOf, runtime, ticks } from "../lib/files.host";
import { hello } from "../lib/plain.host";
export default function P() {
  return <main onClick={() => { sizeOf("x"); runtime(); ticks(2); hello("w"); }}>files</main>;
}
`

func runtimesProject(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	writeTree(t, root, fakeKrateModules)
	writeTree(t, root, map[string]string{
		"package.json":          `{"name":"t","type":"module"}`,
		"src/lib/files.host.ts": nodeHostModule,
		"src/lib/plain.host.ts": `export function hello(n: string) { return "hello " + n; }`,
		"src/pages/index.tsx":   nodeRuntimePage,
	})
	return root
}

func buildRuntimesApp(t *testing.T, root string, host config.NativeHost) *NativeAppResult {
	t.Helper()
	b := newNativeBuilder(t, root)
	b.Cfg.Native = &config.NativeConfig{AppID: "com.acme.rt", ProductName: "RT", Version: "1.0.0", Host: host}
	res, err := b.BuildNativeApp(NativeAppOptions{SkipGoCompile: true})
	if err != nil {
		t.Fatalf("native build: %v", err)
	}
	return res
}

func TestNativeHostChildNodeEndToEnd(t *testing.T) {
	if testing.Short() {
		t.Skip("starts a shell")
	}
	for _, tool := range []string{"node", "go"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skip(tool + " is not on PATH")
		}
	}
	if !hasGoHostPackage(t) {
		t.Skip("no native module in this checkout")
	}
	shellBin := filepath.Join(t.TempDir(), ShellFileName(runtime.GOOS))
	cmd := exec.Command("go", "build", "-o", shellBin, "./cmd/krate-shell")
	cmd.Dir = filepath.Join(repoRoot(t), "packages", "native")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Skipf("cannot build the shell: %v\n%s", err, out)
	}

	cases := map[string]config.NativeHost{
		"runtime node":      {Runtime: "node"},
		"island on quickjs": {Islands: map[string]string{"src/lib/files.host.ts": "node"}},
	}
	for name, host := range cases {
		t.Run(name, func(t *testing.T) {
			root := runtimesProject(t)
			res := buildRuntimesApp(t, root, host)
			data := filepath.Join(t.TempDir(), "data.txt")
			if err := os.WriteFile(data, []byte("twelve bytes"), 0o644); err != nil {
				t.Fatal(err)
			}

			sh := exec.Command(shellBin, "--browser", res.AppDir)
			stdout, err := sh.StdoutPipe()
			if err != nil {
				t.Fatal(err)
			}
			stderr := &lockedWriter{w: &bytes.Buffer{}}
			sh.Stderr = stderr
			if err := sh.Start(); err != nil {
				t.Fatal(err)
			}
			defer func() {
				_ = sh.Process.Kill()
				_ = sh.Wait()
			}()
			urls := make(chan string, 1)
			go func() {
				sc := bufio.NewScanner(stdout)
				for sc.Scan() {
					if u, ok := strings.CutPrefix(sc.Text(), "url "); ok {
						urls <- u
					}
				}
			}()
			var u *url.URL
			select {
			case s := <-urls:
				u, _ = url.Parse(s)
			case <-time.After(120 * time.Second):
				t.Fatalf("no url from the shell; stderr:\n%s", stderr.String())
			}
			c := &devFrameClient{t: t, base: "http://" + u.Host, token: u.Query().Get("krate_token")}
			c.waitCall("src/lib/files.host.sizeOf", `["`+filepath.ToSlash(data)+`"]`, `"ok":12`, 30*time.Second)
			c.waitCall("src/lib/files.host.runtime", `[]`, `"ok":"node string"`, 30*time.Second)
			if name == "island on quickjs" {
				// The module that needs no Node API runs in the embedded runtime.
				c.waitCall("src/lib/plain.host.hello", `["w"]`, `"ok":"hello w"`, 30*time.Second)
			}
		})
	}
}
