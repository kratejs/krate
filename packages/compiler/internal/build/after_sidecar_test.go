package build

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/evanw/esbuild/pkg/api"
)

const afterRoute = `
import { after } from "RUNTIME_AFTER";
import { writeFileSync } from "node:fs";
export async function GET(request) {
  const marker = request.nextUrl.searchParams.get("marker");
  after(async () => {
    await new Promise((r) => setTimeout(r, 150));
    writeFileSync(marker, "ran");
  });
  after(() => { throw new Error("boom"); });
  after(() => writeFileSync(marker + ".second", "ran"));
  return Response.json({ path: request.nextUrl.pathname, base: JSON.stringify(request.nextUrl.basePath) });
}
`

func TestAPISidecarAfterRunsAfterResponse(t *testing.T) {
	for _, runtime := range []string{"node", "bun"} {
		t.Run(runtime, func(t *testing.T) {
			if _, err := exec.LookPath(runtime); err != nil {
				if runtime == "node" {
					requireE2E(t, "node is not installed")
				}
				t.Skipf("%s is not installed", runtime)
			}
			repoRoot, err := repoRootPath()
			if err != nil {
				t.Fatal(err)
			}
			afterTS := filepath.Join(repoRoot, "packages", "runtime", "src", "after.ts")
			if _, err := os.Stat(afterTS); err != nil {
				requireE2E(t, "@krate/runtime source not found: %v", err)
			}
			dir := t.TempDir()
			apiDir := filepath.Join(dir, "api")
			if err := os.MkdirAll(apiDir, 0755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dir, "package.json"), []byte(`{"type":"module"}`), 0644); err != nil {
				t.Fatal(err)
			}
			res := api.Build(api.BuildOptions{
				Bundle:   true,
				Format:   api.FormatESModule,
				Platform: api.PlatformNode,
				Outfile:  filepath.Join(apiDir, "later.js"),
				Write:    true,
				External: []string{"node:*"},
				Stdin: &api.StdinOptions{
					Loader:     api.LoaderTS,
					Contents:   strings.ReplaceAll(afterRoute, "RUNTIME_AFTER", filepath.ToSlash(afterTS)),
					Sourcefile: "later.ts",
					ResolveDir: dir,
				},
			})
			if len(res.Errors) > 0 {
				t.Fatalf("bundle: %s", res.Errors[0].Text)
			}

			script := apiServerScriptContent
			args := []string{}
			if runtime == "bun" {
				script = apiServerBunScriptContent
				args = append(args, "run")
			}
			scriptPath := filepath.Join(dir, "sidecar.mjs")
			if err := os.WriteFile(scriptPath, []byte(script), 0644); err != nil {
				t.Fatal(err)
			}
			port := freePort(t)
			args = append(args, scriptPath, apiDir, fmt.Sprint(port), "", filepath.Join(dir, "actions"))
			cmd := exec.Command(runtime, args...)
			cmd.Env = append(os.Environ(), "KRATE_BASE_PATH=/docs")
			var logs syncBuffer
			cmd.Stdout = &logs
			cmd.Stderr = &logs
			if err := cmd.Start(); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				_ = cmd.Process.Kill()
				_, _ = cmd.Process.Wait()
				if t.Failed() {
					t.Logf("%s sidecar output:\n%s", runtime, logs.String())
				}
			})

			marker := filepath.Join(dir, "marker")
			url := fmt.Sprintf("http://127.0.0.1:%d/api/later?marker=%s", port, marker)
			var body string
			deadline := time.Now().Add(15 * time.Second)
			for {
				resp, err := http.Get(url)
				if err == nil {
					b, _ := io.ReadAll(resp.Body)
					resp.Body.Close()
					body = string(b)
					break
				}
				if time.Now().After(deadline) {
					t.Fatalf("%s sidecar did not answer: %v", runtime, err)
				}
				time.Sleep(50 * time.Millisecond)
			}
			if !strings.Contains(body, `"path":"/api/later"`) || !strings.Contains(body, `base`) || !strings.Contains(body, `/docs`) {
				t.Fatalf("nextUrl in the handler: %s", body)
			}
			if _, err := os.Stat(marker); err == nil {
				t.Fatal("after() task ran before the response was sent")
			}
			for _, f := range []string{marker, marker + ".second"} {
				ok := false
				for i := 0; i < 100; i++ {
					if _, err := os.Stat(f); err == nil {
						ok = true
						break
					}
					time.Sleep(50 * time.Millisecond)
				}
				if !ok {
					t.Fatalf("after() task never wrote %s (a failing task must not stop the rest)", filepath.Base(f))
				}
			}
		})
	}
}
