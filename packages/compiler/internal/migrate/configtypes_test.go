package migrate

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// richNextConfig touches every next.config key the migrator turns into a
// krate.config.ts setting.
const richNextConfig = `import type { NextConfig } from 'next';

const nextConfig: NextConfig = {
  output: 'standalone',
  basePath: '/app',
  trailingSlash: true,
  compress: false,
  serverExternalPackages: ['sharp'],
  images: {
    formats: ['image/avif', 'image/webp'],
    remotePatterns: [{ protocol: 'https', hostname: 'cdn.example.com', pathname: '/img/**' }],
    deviceSizes: [640, 1080],
  },
  async rewrites() {
    return [
      { source: '/docs/:path*', destination: 'https://docs.example.com/:path*' },
      { source: '/old', has: [{ type: 'header', key: 'x-legacy' }], destination: '/new' },
    ];
  },
  async redirects() {
    return [{ source: '/a', destination: '/b', permanent: false }];
  },
  async headers() {
    return [{ source: '/:path*', headers: [{ key: 'X-Frame-Options', value: 'DENY' }] }];
  },
};
export default nextConfig;
`

// tscFor returns the TypeScript compiler installed for @krate/core.
func tscFor(t *testing.T) string {
	t.Helper()
	name := "tsc"
	if runtime.GOOS == "windows" {
		name = "tsc.cmd"
	}
	tsc, _ := filepath.Abs(filepath.Join("..", "..", "..", "core", "node_modules", ".bin", name))
	if _, err := os.Stat(tsc); err != nil {
		if os.Getenv("KRATE_REQUIRE_E2E") != "" {
			t.Fatalf("typescript is not installed for @krate/core (run pnpm install): %v", err)
		}
		t.Skip("typescript is not installed (run pnpm install)")
	}
	return tsc
}

// typecheckKrateConfig writes the planned krate.config.ts into root and runs
// tsc on it with @krate/core resolved to its sources.
func typecheckKrateConfig(t *testing.T, tsc, root string, rep *Report) {
	t.Helper()
	cfg := findPlan(rep, "krate.config.ts")
	if cfg == nil {
		t.Fatal("krate.config.ts not planned")
	}
	if err := os.WriteFile(filepath.Join(root, "krate.config.ts"), cfg.Content, 0o644); err != nil {
		t.Fatal(err)
	}
	core, _ := filepath.Abs(filepath.Join("..", "..", "..", "core"))
	tsconfig := map[string]any{
		"compilerOptions": map[string]any{
			"strict": true, "noEmit": true, "target": "ES2022", "module": "ESNext",
			"moduleResolution": "bundler", "skipLibCheck": true, "allowImportingTsExtensions": true,
			"lib":       []string{"ES2022", "DOM"},
			"types":     []string{"node"},
			"typeRoots": []string{filepath.ToSlash(filepath.Join(core, "node_modules", "@types"))},
			"paths":     map[string][]string{"@krate/core": {filepath.ToSlash(filepath.Join(core, "src", "index.ts"))}},
		},
		"files": []string{"krate.config.ts"},
	}
	data, _ := json.Marshal(tsconfig)
	if err := os.WriteFile(filepath.Join(root, "tsconfig.typecheck.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(tsc, "-p", filepath.Join(root, "tsconfig.typecheck.json"))
	cmd.Dir = root
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("tsc rejects the migrated krate.config.ts: %v\n%s\n%s", err, out, cfg.Content)
	}
}

// TestMigratedKrateConfigTypechecks: the krate.config.ts the migrator writes
// typechecks against @krate/core's KrateConfig, including deploy.target
// "node" for output: 'standalone'.
func TestMigratedKrateConfigTypechecks(t *testing.T) {
	tsc := tscFor(t)
	cases := map[string]func(t *testing.T) (string, *Report){
		"standalone monorepo": func(t *testing.T) (string, *Report) {
			app := monorepoApp(t, monorepoConfig, nil)
			return app, scanApp(t, app, ScanOptions{})
		},
		"redirects with conditions": func(t *testing.T) (string, *Report) {
			return scanConfigRoot(t, map[string]string{"next.config.ts": ddNextConfig})
		},
		"code pieces": func(t *testing.T) (string, *Report) {
			return scanConfigRoot(t, map[string]string{
				"next.config.ts":                 appNextConfig,
				"src/config/security-headers.ts": appSecurityHeaders,
				"src/config/cache-headers.ts":    appCacheHeaders,
			})
		},
		"every translated key": func(t *testing.T) (string, *Report) {
			return scanConfigRoot(t, map[string]string{
				"next.config.ts": richNextConfig,
				".env":           "NEXT_PUBLIC_API=https://api.example.com\n",
				"app/env.tsx":    `export function Api() { return <p>{process.env.NEXT_PUBLIC_API}</p>; }`,
				"middleware.ts":  "import fs from 'node:fs';\nexport function middleware() { fs.existsSync('x'); }\n",
			})
		},
	}
	for name, run := range cases {
		t.Run(name, func(t *testing.T) {
			root, rep := run(t)
			if name == "standalone monorepo" && !strings.Contains(configBody(t, rep), `"target": "node"`) {
				t.Fatalf("expected deploy.target node:\n%s", configBody(t, rep))
			}
			typecheckKrateConfig(t, tsc, root, rep)
		})
	}
}

func scanConfigRoot(t *testing.T, files map[string]string) (string, *Report) {
	t.Helper()
	root := t.TempDir()
	writeNextFile(t, root, "app/page.tsx", `export default function Page() { return <h1>Home</h1>; }`)
	for name, body := range files {
		writeNextFile(t, root, name, body)
	}
	rep, err := ScanNext(root)
	if err != nil {
		t.Fatal(err)
	}
	return root, rep
}
