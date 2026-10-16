package build

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/evanw/esbuild/pkg/api"
)

// TestSSRDynamicRouteParamsE2E boots the staged SSR renderer driver against a
// dynamic route ([id]) manifest and verifies that params extracted by the Go
// server are injected into the page component's props at render time - not
// dropped as the placeholder/officially-empty props.
func TestSSRDynamicRouteParamsE2E(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		requireE2E(t, "node not available: %v", err)
	}

	repoRoot, err := repoRootPath()
	if err != nil {
		t.Fatal(err)
	}
	runtimeSrc := filepath.Join(repoRoot, "packages", "runtime", "src")
	serverTS := filepath.Join(runtimeSrc, "server.ts")
	if _, err := os.Stat(serverTS); err != nil {
		requireE2E(t, "@krate/runtime server source not found: %v", err)
	}

	fakeRoot := t.TempDir()
	outDir := filepath.Join(fakeRoot, "dist")

	// Page bundle: a dynamic SSR page that renders its `params.id`.
	pageSrc := `
export default function VideoPage(props) {
  return <div class="page">video-{props.params ? props.params.id : "missing"}</div>;
}
`
	bundleDir := filepath.Join(outDir, ".krate", "server-bundles")
	if err := os.MkdirAll(bundleDir, 0755); err != nil {
		t.Fatal(err)
	}
	bundlePath := filepath.Join(bundleDir, "video.test.server.mjs")

	res := api.Build(api.BuildOptions{
		AbsWorkingDir:   repoRoot,
		Bundle:          true,
		Format:          api.FormatESModule,
		Platform:        api.PlatformNode,
		Outfile:         bundlePath,
		Write:           true,
		JSX:             api.JSXAutomatic,
		JSXSideEffects:  false,
		JSXImportSource: "@krate/runtime/server",
		Plugins: []api.Plugin{{
			Name: "krate-ssr-e2e-alias",
			Setup: func(build api.PluginBuild) {
				build.OnResolve(api.OnResolveOptions{Filter: `^@krate/runtime/server(/|$)`},
					func(args api.OnResolveArgs) (api.OnResolveResult, error) {
						spec := strings.TrimPrefix(args.Path, "@krate/runtime/server/")
						if spec == args.Path || spec == "" {
							// bare `@krate/runtime/server`
							return api.OnResolveResult{Path: serverTS}, nil
						}
						// subpath like "jsx-runtime" -> try server-<name>.ts then <name>.ts
						candidates := []string{
							filepath.Join(runtimeSrc, "server-"+spec+".ts"),
							filepath.Join(runtimeSrc, spec+".ts"),
						}
						for _, c := range candidates {
							if _, err := os.Stat(c); err == nil {
								return api.OnResolveResult{Path: c}, nil
							}
						}
						return api.OnResolveResult{}, fmt.Errorf("alias: %s not found", args.Path)
					})
			},
		}},
		Stdin: &api.StdinOptions{
			Loader:     api.LoaderTSX,
			Contents:   pageSrc,
			Sourcefile: "video/[id].tsx",
		},
	})
	if len(res.Errors) > 0 {
		t.Fatalf("page bundle failed: %s", res.Errors[0].Text)
	}

	// server-manifest.json in the shape the renderer expects.
	man := ServerManifest{
		Pages: []ManifestPage{{
			Route:      "/video/[id]",
			Source:     filepath.Join(fakeRoot, "video", "[id].tsx"),
			Mode:       "ssr",
			BundlePath: ".krate/server-bundles/video.test.server.mjs",
		}},
	}
	mData, _ := json.Marshal(man)
	if err := os.WriteFile(filepath.Join(outDir, "server-manifest.json"), mData, 0644); err != nil {
		t.Fatal(err)
	}

	staged := stageServerRenderer(repoRoot, outDir)
	if staged == "" {
		t.Fatal("stageServerRenderer returned empty path")
	}

	port := freePort(t)
	cmd := exec.Command(node, staged)
	cmd.Env = append(os.Environ(),
		fmt.Sprintf("KRATE_SSR_PORT=%d", port),
		fmt.Sprintf("KRATE_MANIFEST=%s", filepath.Join(outDir, "server-manifest.json")),
		fmt.Sprintf("KRATE_ROOT=%s", fakeRoot),
	)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		t.Fatalf("starting renderer: %v", err)
	}
	defer func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	}()

	base := fmt.Sprintf("http://localhost:%d", port)
	ready := false
	for i := 0; i < 50; i++ {
		resp, err := http.Get(base + "/__krate/ssr/health")
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == 200 {
				ready = true
				break
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	if !ready {
		t.Fatal("renderer did not become ready")
	}

	// Render /video/abc123 with the Go-forwarded params.
	html := renderForParams(t, base, "/video/[id]", "video/abc123", map[string]string{"id": "abc123"}, nil)
	if !strings.Contains(html, "video-abc123") {
		t.Errorf("expected param id rendered, got HTML: %q", html)
	}

	// Without params the page must still render (no crash) and signal it.
	html = renderForParams(t, base, "/video/[id]", "video/xyz", nil, nil)
	if !strings.Contains(html, "video-missing") {
		t.Errorf("expected props fallback without params, got HTML: %q", html)
	}
}

func renderForParams(t *testing.T, base, route, url string, params, query map[string]string) string {
	t.Helper()
	body := map[string]interface{}{
		"route":   route,
		"url":     url,
		"method":  "GET",
		"params":  params,
		"query":   query,
		"regions": []map[string]string{{"id": "page", "kind": "page"}},
	}
	data, _ := json.Marshal(body)
	resp, err := http.Post(base+"/__krate/regions", "application/json", bytes.NewReader(data))
	if err != nil {
		t.Fatalf("regions request: %v", err)
	}
	defer resp.Body.Close()
	out, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		t.Fatalf("regions status %d: %s", resp.StatusCode, out)
	}
	// NDJSON: one region frame + one end frame.
	var frame struct {
		Type string `json:"type"`
		ID   string `json:"id,omitempty"`
		HTML string `json:"html,omitempty"`
		Err  string `json:"error,omitempty"`
	}
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if err := json.Unmarshal([]byte(line), &frame); err != nil {
			t.Fatalf("unmarshal region frame: %v\nline: %s", err, line)
		}
		if frame.Type == "region" && frame.ID == "page" {
			return frame.HTML
		}
		if frame.Type == "error" {
			t.Fatalf("region error: %s", frame.Err)
		}
	}
	t.Fatalf("no page region frame in response:\n%s", out)
	return ""
}

func freePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}
