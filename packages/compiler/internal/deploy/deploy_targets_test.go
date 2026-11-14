package deploy

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/config"
)

func TestServerFeaturesBlockStaticTargets(t *testing.T) {
	root := t.TempDir()
	out := filepath.Join(root, "dist")
	_ = os.MkdirAll(filepath.Join(root, ".krate"), 0o755)
	_ = os.MkdirAll(out, 0o755)
	if err := os.WriteFile(filepath.Join(root, ".krate", "middleware.js"), []byte("//"), 0o644); err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(filepath.Join(out, "server-manifest.json"), []byte(`{"pages":[{"route":"/x","mode":"ssr"}]}`), 0o644)

	feats := ServerFeatures(root, out)
	if len(feats) != 2 {
		t.Fatalf("features = %v, want SSR + middleware", feats)
	}
	_, err := Emit("gh-pages", config.Default(), root, out)
	if err == nil || !strings.Contains(err.Error(), "server features") || !strings.Contains(err.Error(), "--static-only") {
		t.Errorf("gh-pages: expected a server-features error, got %v", err)
	}
	// The platform targets run the server, which needs the bundle from the build.
	for _, target := range []string{"netlify", "vercel", "cloudflare"} {
		if _, err := Emit(target, config.Default(), root, out); err == nil || !strings.Contains(err.Error(), "server bundle") {
			t.Errorf("%s: expected a missing server bundle error, got %v", target, err)
		}
	}
	if _, err := Emit("netlify", config.Default(), root, out, Options{StaticOnly: true}); err != nil {
		t.Fatalf("--static-only must allow the deploy: %v", err)
	}
	if _, err := Emit("docker", config.Default(), root, out); err != nil {
		t.Fatalf("docker target must accept server features: %v", err)
	}
}

func TestEmitDockerfileCopiesTraceIncludes(t *testing.T) {
	root := t.TempDir()
	out := filepath.Join(root, "dist")
	_ = os.MkdirAll(out, 0o755)
	cfg := config.Default()
	cfg.OutputFileTracingIncludes = map[string][]string{
		"/api/og": {"assets/fonts/**", "workers/image.js"},
		"*":       {"node_modules/@resvg/resvg-js-*/**", "**/*.wasm"},
	}
	if _, err := Emit("docker", cfg, root, out); err != nil {
		t.Fatal(err)
	}
	df, _ := os.ReadFile(filepath.Join(root, "Dockerfile"))
	text := string(df)
	if !strings.Contains(text, `for p in 'assets/fonts' 'workers/image.js'; do`) || !strings.Contains(text, `cp -a --parents "$p" /tmp/runtime/`) {
		t.Errorf("runtime-read files must be copied into the runtime stage:\n%s", text)
	}
	if strings.Contains(text, "'node_modules") {
		t.Errorf("node_modules globs are covered by the installed packages:\n%s", text)
	}
	if !strings.Contains(text, `glob "**/*.wasm" has no leading directory`) {
		t.Errorf("a glob without a leading directory must be flagged:\n%s", text)
	}
}

// A robots.ts or sitemap.ts that runs per request needs a server: a static
// target refuses it and names the cause instead of publishing no robots.txt.
func TestPerRequestMetadataRoutesBlockStaticTargets(t *testing.T) {
	root := t.TempDir()
	out := filepath.Join(root, "dist")
	meta := filepath.Join(out, ".krate", "api-node", "__krate_meta")
	if err := os.MkdirAll(meta, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(meta, "robots.js"), []byte("//"), 0o644); err != nil {
		t.Fatal(err)
	}
	feats := ServerFeatures(root, out)
	if len(feats) != 1 || !strings.Contains(feats[0], "metadata routes") {
		t.Fatalf("features = %v, want only the metadata routes", feats)
	}
	if _, err := Emit("gh-pages", config.Default(), root, out); err == nil || !strings.Contains(err.Error(), "metadata routes") {
		t.Errorf("gh-pages: expected the error to name the metadata routes, got %v", err)
	}
}

func TestEmitDockerfile(t *testing.T) {
	root := t.TempDir()
	out := filepath.Join(root, "dist")
	_ = os.MkdirAll(out, 0o755)
	_ = os.WriteFile(filepath.Join(root, "pnpm-lock.yaml"), []byte(""), 0o644)
	cfg := config.Default()
	cfg.Server.Port = 4321
	files, err := Emit("docker", cfg, root, out)
	if err != nil || len(files) != 2 {
		t.Fatalf("files=%v err=%v", files, err)
	}
	df, _ := os.ReadFile(filepath.Join(root, "Dockerfile"))
	text := string(df)
	for _, want := range []string{
		"FROM node:22-bookworm-slim AS build", "RUN corepack enable\n", "pnpm install --frozen-lockfile", "npx krate build", "pnpm prune --prod",
		"EXPOSE 4321", "USER node", "COPY --chown=node:node . .", "HEALTHCHECK", "/healthz",
		"COPY --from=build --chown=node:node /app/node_modules ./node_modules", "COPY --from=build --chown=node:node /tmp/runtime/ ./",
		`CMD ["node", "node_modules/@krate/core/bin/krate.js", "start"]`, "ARG KEEP_DEV_DEPS=false",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("Dockerfile missing %q:\n%s", want, df)
		}
	}
	// Two stages: the runtime stage starts from a clean base, copies the build
	// output and runs as the unprivileged user, with no toolchain and no sources.
	stages := strings.Split(text, "\nFROM ")
	if len(stages) != 3 { // the header comment, then the two stages
		t.Fatalf("want a build stage and a runtime stage, got %d FROMs:\n%s", len(stages)-1, df)
	}
	runtime := stages[2]
	for _, bad := range []string{"npx", "pnpm", "COPY . .", "golang", "src"} {
		if strings.Contains(runtime, bad) {
			t.Errorf("runtime stage must not contain %q:\n%s", bad, runtime)
		}
	}
	if !strings.Contains(runtime, "USER node") || !strings.Contains(runtime, "HEALTHCHECK") {
		t.Errorf("runtime stage must run as node with a healthcheck:\n%s", runtime)
	}
	// corepack needs root, so it comes before the switch to the unprivileged user,
	// and the build runs as that user.
	if !(strings.Index(text, "corepack enable") < strings.Index(text, "USER node") && strings.Index(text, "USER node") < strings.Index(text, "RUN npx krate build")) {
		t.Errorf("expected corepack enable, then USER node, then the build:\n%s", df)
	}
	if strings.Contains(text, "corepack enable && ") {
		t.Errorf("root-only corepack step must not run after USER node:\n%s", df)
	}
	// The build-only parts of .krate are dropped; the server's parts stay.
	for _, dropped := range []string{"/tmp/runtime/.krate/cache", "/tmp/runtime/.krate/types", "/tmp/runtime/.krate/segments", "/tmp/runtime/.krate/goext"} {
		if !strings.Contains(text, dropped) {
			t.Errorf("Dockerfile should drop %s from the runtime image:\n%s", dropped, df)
		}
	}
	if strings.Contains(text, "rm -rf /tmp/runtime/.krate/actions") || strings.Contains(text, "/tmp/runtime/.krate/middleware") {
		t.Errorf("actions and middleware are runtime files:\n%s", df)
	}
	di, _ := os.ReadFile(filepath.Join(root, ".dockerignore"))
	if !strings.Contains(string(di), ".krate") || !strings.Contains(string(di), ".env*") {
		t.Errorf(".dockerignore = %q", di)
	}
	// A user's Dockerfile is never overwritten.
	_ = os.WriteFile(filepath.Join(root, "Dockerfile"), []byte("FROM scratch\n"), 0o644)
	if _, err := Emit("docker", cfg, root, out); err == nil {
		t.Fatal("expected refusal to overwrite a hand-written Dockerfile")
	}
}
