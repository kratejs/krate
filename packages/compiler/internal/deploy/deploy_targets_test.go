package deploy

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/config"
)

func TestTranslateRule(t *testing.T) {
	cases := []struct {
		src, dst     string
		vercel       bool
		wantS, wantD string
	}{
		{"/legacy/*", "/docs/:splat", false, "/legacy/*", "/docs/:splat"},
		{"/legacy/*", "/docs/:splat", true, "/legacy/:path*", "/docs/:path*"},
		{"/legacy/*", "/docs", false, "/legacy/*", "/docs"},
		{"/legacy/*", "/docs", true, "/legacy/:path*", "/docs"},
		{"/blog/:slug", "/posts/:slug", true, "/blog/:slug", "/posts/:slug"},
		{"/blog/:slug", "/posts/:slug", false, "/blog/:slug", "/posts/:slug"},
		{"/files/:path*", "/static/:path", false, "/files/*", "/static/:splat"},
		{"/files/:path*", "/static/:path*", true, "/files/:path*", "/static/:path*"},
		{"/old", "/new", true, "/old", "/new"},
	}
	for _, c := range cases {
		gs, gd := translateRule(c.src, c.dst, c.vercel)
		if gs != c.wantS || gd != c.wantD {
			t.Errorf("translateRule(%q,%q,vercel=%v) = %q,%q want %q,%q", c.src, c.dst, c.vercel, gs, gd, c.wantS, c.wantD)
		}
	}
}

func TestVercelWritesPermanentFalseAndLandsAtProjectRoot(t *testing.T) {
	root := t.TempDir()
	out := filepath.Join(root, "dist")
	if err := os.MkdirAll(out, 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()
	cfg.Redirects = []config.Redirect{{Source: "/a", Destination: "/b", Permanent: false}, {Source: "/legacy/*", Destination: "/docs/:splat", Permanent: true}}
	files, err := Emit("vercel", cfg, root, out)
	if err != nil {
		t.Fatal(err)
	}
	if files[0] != filepath.Join(root, "vercel.json") {
		t.Fatalf("vercel.json written to %s, want project root", files[0])
	}
	if _, err := os.Stat(filepath.Join(out, "vercel.json")); err == nil {
		t.Fatal("vercel.json must not be written inside the output directory")
	}
	data, _ := os.ReadFile(files[0])
	if !strings.Contains(string(data), `"permanent": false`) {
		t.Fatalf("permanent:false must be explicit (Vercel defaults to true):\n%s", data)
	}
	var doc struct {
		OutputDirectory string `json:"outputDirectory"`
		Redirects       []struct{ Source, Destination string }
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatal(err)
	}
	if doc.OutputDirectory != "dist" {
		t.Errorf("outputDirectory = %q", doc.OutputDirectory)
	}
	if doc.Redirects[1].Source != "/legacy/:path*" || doc.Redirects[1].Destination != "/docs/:path*" {
		t.Errorf("wildcard not translated: %+v", doc.Redirects[1])
	}
}

func TestVercelDoesNotClobberHandWrittenFile(t *testing.T) {
	root := t.TempDir()
	out := filepath.Join(root, "dist")
	_ = os.MkdirAll(out, 0o755)
	mine := `{"buildCommand":"x"}`
	if err := os.WriteFile(filepath.Join(root, "vercel.json"), []byte(mine), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Emit("vercel", config.Default(), root, out); err == nil {
		t.Fatal("expected refusal to overwrite a hand-written vercel.json")
	}
	if got, _ := os.ReadFile(filepath.Join(root, "vercel.json")); string(got) != mine {
		t.Fatalf("file was modified: %s", got)
	}
	// A file krate wrote itself is regenerated.
	_ = os.Remove(filepath.Join(root, "vercel.json"))
	if _, err := Emit("vercel", config.Default(), root, out); err != nil {
		t.Fatal(err)
	}
	if _, err := Emit("vercel", config.Default(), root, out); err != nil {
		t.Fatalf("regenerating its own file failed: %v", err)
	}
}

func TestNetlifyTranslatesNamedRestParam(t *testing.T) {
	out := t.TempDir()
	cfg := config.Default()
	cfg.Redirects = []config.Redirect{{Source: "/files/:path*", Destination: "/static/:path", Permanent: true}, {Source: "/legacy/*", Destination: "/docs/:splat"}}
	if _, err := Emit("netlify", cfg, out, out); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(filepath.Join(out, "_redirects"))
	for _, want := range []string{"/files/*\t/static/:splat\t301", "/legacy/*\t/docs/:splat\t302"} {
		if !strings.Contains(string(data), want) {
			t.Errorf("missing %q in:\n%s", want, data)
		}
	}
}

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

func TestStaticProjectHasNoServerFeatures(t *testing.T) {
	root := t.TempDir()
	out := filepath.Join(root, "dist")
	_ = os.MkdirAll(out, 0o755)
	if f := ServerFeatures(root, out); len(f) != 0 {
		t.Fatalf("features = %v", f)
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
	for _, want := range []string{"RUN corepack enable\n", "pnpm install --frozen-lockfile", "npx krate build", "EXPOSE 4321", "USER node", "COPY --chown=node:node . .", "HEALTHCHECK", "/healthz", `CMD ["npx", "krate", "start"]`} {
		if !strings.Contains(string(df), want) {
			t.Errorf("Dockerfile missing %q:\n%s", want, df)
		}
	}
	// corepack needs root, so it comes before the switch to the unprivileged user,
	// and the build and server both run as that user.
	text := string(df)
	if !(strings.Index(text, "corepack enable") < strings.Index(text, "USER node") && strings.Index(text, "USER node") < strings.Index(text, "RUN npx krate build")) {
		t.Errorf("expected corepack enable, then USER node, then the build:\n%s", df)
	}
	if strings.Contains(text, "corepack enable && ") {
		t.Errorf("root-only corepack step must not run after USER node:\n%s", df)
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

func TestRuleRegexForVercelRoutes(t *testing.T) {
	tests := []struct{ source, dest, wantSrc, wantDest string }{
		{"/old", "/new", `^/old$`, "/new"},
		{"/legacy/:slug", "/blog/:slug", `^/legacy/(?<slug>[^/]+)$`, "/blog/$slug"},
		{"/old/*", "/new/:splat", `^/old(?:/(?<splat>.*))?$`, "/new/$splat"},
		{"/files/:path*", "/static/:path", `^/files(?:/(?<path>.*))?$`, "/static/$path"},
	}
	for _, tt := range tests {
		src, dst := ruleRegex(tt.source, tt.dest)
		if src != tt.wantSrc || dst != tt.wantDest {
			t.Errorf("ruleRegex(%q, %q) = %q, %q; want %q, %q", tt.source, tt.dest, src, dst, tt.wantSrc, tt.wantDest)
		}
	}
}

func TestWantsServer(t *testing.T) {
	feats := []string{"middleware"}
	if !WantsServer("node", nil, false) || !WantsServer("vercel", feats, false) {
		t.Error("node always, platform targets with features")
	}
	if WantsServer("vercel", nil, false) || WantsServer("vercel", feats, true) || WantsServer("gh-pages", feats, false) || WantsServer("docker", feats, false) {
		t.Error("static projects, --static-only, gh-pages and docker keep their own path")
	}
}
