package build

import (
	"bufio"
	"bytes"
	"context"
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

func TestNativeBuildHostChildrenManifest(t *testing.T) {
	root := runtimesProject(t)
	res := buildRuntimesApp(t, root, config.NativeHost{Runtime: "node"})
	m := readNativeManifest(t, res.AppDir)
	if m["hostjs"] != nil {
		t.Errorf("a node project has no hostjs: %v", m["hostjs"])
	}
	hc := m["hostchildren"].([]any)
	if len(hc) != 1 {
		t.Fatalf("hostchildren = %v", hc)
	}
	c := hc[0].(map[string]any)
	if c["runtime"] != "node" || c["entry"] != "host/node/host.mjs" || c["map"] != "host/node/host.mjs.map" {
		t.Errorf("child = %v", c)
	}
	if got := strings.Join(nativeStrings(c["command"]), " "); got != "node --enable-source-maps {entry}" {
		t.Errorf("command = %q", got)
	}
	if got := strings.Join(nativeStrings(c["funcs"]), ","); got != "src/lib/files.host.runtime,src/lib/files.host.sizeOf,src/lib/plain.host.hello" {
		t.Errorf("funcs = %s", got)
	}
	if got := strings.Join(nativeStrings(c["streams"]), ","); got != "src/lib/files.host.ticks" {
		t.Errorf("streams = %s", got)
	}
	if _, err := os.Stat(filepath.Join(res.AppDir, "host", "node", "host.mjs")); err != nil {
		t.Error(err)
	}
	if len(res.HostGroups) != 1 || res.HostGroups[0].Runtime != "node" {
		t.Errorf("HostGroups = %+v", res.HostGroups)
	}

	// With an island the QuickJS bundle stays and the island becomes a child.
	root = runtimesProject(t)
	res = buildRuntimesApp(t, root, config.NativeHost{Islands: map[string]string{"src/lib/files.host.ts": "bun"}})
	m = readNativeManifest(t, res.AppDir)
	if m["hostjs"] == nil {
		t.Error("the QuickJS part is missing")
	}
	c = m["hostchildren"].([]any)[0].(map[string]any)
	if c["runtime"] != "bun" || strings.Join(nativeStrings(c["command"]), " ") != "bun run {entry}" {
		t.Errorf("island child = %v", c)
	}
	if got := strings.Join(nativeStrings(c["funcs"]), ","); got != "src/lib/files.host.runtime,src/lib/files.host.sizeOf" {
		t.Errorf("island funcs = %s", got)
	}
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

// TestNativeDevReloadsHostChild edits the module of a Node island while
// `krate native dev` runs: the shell restarts the child and the next call sees
// the new code, while the QuickJS part keeps running.
func TestNativeDevReloadsHostChild(t *testing.T) {
	if testing.Short() {
		t.Skip("starts a shell and several builds")
	}
	for _, tool := range []string{"node", "go"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skip(tool + " is not on PATH")
		}
	}
	if !hasGoHostPackage(t) {
		t.Skip("no native module in this checkout")
	}
	root := runtimesProject(t)
	shellBin := filepath.Join(t.TempDir(), ShellFileName(runtime.GOOS))
	cmd := exec.Command("go", "build", "-o", shellBin, "./cmd/krate-shell")
	cmd.Dir = filepath.Join(repoRoot(t), "packages", "native")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Skipf("cannot build the shell: %v\n%s", err, out)
	}
	cfgLoad := func() (string, *config.Config, map[string]string, error) {
		cfg := config.Default()
		cfg.PagesDir = filepath.Join(root, "src", "pages")
		cfg.OutDir = filepath.Join(root, "dist")
		cfg.Native = &config.NativeConfig{AppID: "com.acme.rt", ProductName: "RT", Version: "1.0.0",
			Host: config.NativeHost{Islands: map[string]string{"src/lib/files.host.ts": "node"}}}
		cfg.Resolve(root)
		return root, cfg, nil, nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	urlc := make(chan string, 1)
	done := make(chan error, 1)
	stderr := &lockedWriter{w: &bytes.Buffer{}}
	go func() {
		done <- RunNativeDev(NativeDevOptions{
			LoadConfig: cfgLoad, Browser: true, Shell: shellBin, Ctx: ctx,
			Stderr: stderr, OnURL: func(u string) { urlc <- u },
		})
	}()
	defer func() {
		cancel()
		select {
		case <-done:
		case <-time.After(30 * time.Second):
			t.Error("RunNativeDev did not stop")
		}
	}()
	var u *url.URL
	select {
	case s := <-urlc:
		u, _ = url.Parse(s)
	case err := <-done:
		done <- err
		t.Fatalf("native dev ended early: %v\n%s", err, stderr.String())
	case <-time.After(180 * time.Second):
		t.Fatalf("no url; shell output:\n%s", stderr.String())
	}
	c := &devFrameClient{t: t, base: "http://" + u.Host, token: u.Query().Get("krate_token")}
	c.waitCall("src/lib/files.host.runtime", `[]`, `"ok":"node string"`, 60*time.Second)
	c.waitCall("src/lib/plain.host.hello", `["w"]`, `"ok":"hello w"`, 30*time.Second)

	replaceInFile(t, filepath.Join(root, "src", "lib", "files.host.ts"), `return "node " +`, `return "node2 " +`)
	c.waitCall("src/lib/files.host.runtime", `[]`, `"ok":"node2 string"`, 90*time.Second)
	c.waitCall("src/lib/plain.host.hello", `["w"]`, `"ok":"hello w"`, 30*time.Second)

	// The QuickJS part reloads in place too.
	replaceInFile(t, filepath.Join(root, "src", "lib", "plain.host.ts"), `"hello "`, `"hi "`)
	c.waitCall("src/lib/plain.host.hello", `["w"]`, `"ok":"hi w"`, 90*time.Second)
	c.waitCall("src/lib/files.host.runtime", `[]`, `"ok":"node2 string"`, 30*time.Second)
}
