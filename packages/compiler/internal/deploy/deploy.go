// Package deploy writes host-specific adapter files (redirects, config) into a
// built site so it can be dropped onto a static host without manual setup.
package deploy

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/kratejs/krate/packages/compiler/internal/config"
	"github.com/kratejs/krate/packages/compiler/internal/fsutil"
	"github.com/kratejs/krate/packages/compiler/internal/gohost"
)

// Targets lists the supported deploy adapters.
var Targets = []string{"netlify", "vercel", "cloudflare", "gh-pages", "docker"}

// ServerFeatures lists the server-side features a built project uses. A static
// host serves none of them, so deploying such a project to a static target
// would publish pages whose dynamic parts silently do nothing.
func ServerFeatures(root, outDir string) []string {
	var out []string
	// Server-only output lives under dist/.krate/ (older builds wrote it to
	// dist/ directly).
	serverOut := func(name string) string {
		if p := filepath.Join(outDir, ".krate", name); fileExists(p) || dirExists(p) {
			return p
		}
		return filepath.Join(outDir, name)
	}
	if data, err := os.ReadFile(serverOut("server-manifest.json")); err == nil {
		var m struct {
			Pages   []json.RawMessage          `json:"pages"`
			Regions map[string]json.RawMessage `json:"regions"`
		}
		if json.Unmarshal(data, &m) == nil && (len(m.Pages) > 0 || len(m.Regions) > 0) {
			out = append(out, "SSR/ISR/streaming pages")
		}
	}
	hasFiles := func(dir string) bool {
		n := 0
		_ = filepath.WalkDir(dir, func(_ string, d os.DirEntry, err error) error {
			if err == nil && !d.IsDir() {
				n++
			}
			return nil
		})
		return n > 0
	}
	if hasFiles(serverOut("api")) || hasFiles(serverOut("api-node")) || fileExists(gohost.RoutesManifestPath(root)) {
		out = append(out, "API routes")
	}
	if data, err := os.ReadFile(filepath.Join(root, ".krate", "actions", "manifest.json")); err == nil {
		var m struct {
			Actions []json.RawMessage `json:"actions"`
		}
		if json.Unmarshal(data, &m) == nil && len(m.Actions) > 0 {
			out = append(out, "server actions")
		}
	}
	if fileExists(filepath.Join(root, ".krate", "middleware.js")) {
		out = append(out, "middleware")
	}
	return out
}

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

// Options tunes Emit.
type Options struct {
	// StaticOnly acknowledges that the project uses server features that a
	// static target will not run, and deploys the static output anyway.
	StaticOnly bool
}

// Emit writes the adapter files for target and returns their paths. root is the
// project directory (vercel.json and the Docker files live there); outDir is the
// build output.
func Emit(target string, cfg *config.Config, root, outDir string, opts ...Options) ([]string, error) {
	var opt Options
	if len(opts) > 0 {
		opt = opts[0]
	}
	if fi, err := os.Stat(outDir); err != nil || !fi.IsDir() {
		return nil, fmt.Errorf("output directory %q not found; run `krate build` first", outDir)
	}
	if target == "docker" {
		return emitDocker(cfg, root, outDir)
	}
	if features := ServerFeatures(root, outDir); len(features) > 0 && !opt.StaticOnly {
		return nil, fmt.Errorf("this project uses server features (%s) that the %s target cannot run: the deployed site would be an empty shell for them. Deploy with `krate deploy --target docker` (or run `krate serve` on a Node/Go host), or pass --static-only to publish only the static pages", strings.Join(features, ", "), target)
	}
	// Server-only artifacts (bundles, manifests, renderer, ISR cache) must never be uploaded
	// to a static host. Remove them before writing adapter files.
	internal := filepath.Join(outDir, ".krate")
	if err := os.RemoveAll(internal); err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("removing server-only artifacts %s: %w", internal, err)
	}
	// Builds from before server-only output moved under .krate left these in
	// the public root.
	for _, legacy := range []string{"api-node", "server-manifest.json"} {
		p := filepath.Join(outDir, legacy)
		if err := os.RemoveAll(p); err != nil && !os.IsNotExist(err) {
			return nil, fmt.Errorf("removing server-only artifacts %s: %w", p, err)
		}
	}
	switch target {
	case "netlify", "cloudflare":
		files, err := emitRedirects(cfg, outDir)
		if err != nil {
			return nil, err
		}
		hfiles, err := emitHeaders(cfg, outDir)
		if err != nil {
			return nil, err
		}
		return append(files, hfiles...), nil
	case "vercel":
		return emitVercel(cfg, root, outDir)
	case "gh-pages":
		return emitGitHubPages(outDir)
	default:
		return nil, fmt.Errorf("unknown deploy target %q (want one of: %s)", target, strings.Join(Targets, ", "))
	}
}

// emitRedirects writes a `_redirects` file (Netlify + Cloudflare Pages format).
func emitRedirects(cfg *config.Config, outDir string) ([]string, error) {
	if len(cfg.Redirects) == 0 && len(cfg.Rewrites) == 0 {
		return nil, nil
	}
	var b strings.Builder
	for _, r := range cfg.Redirects {
		status := "302"
		if r.Permanent {
			status = "301"
		}
		src, dst := translateRule(r.Source, r.Destination, false)
		fmt.Fprintf(&b, "%s\t%s\t%s\n", src, dst, status)
	}
	for _, r := range cfg.Rewrites {
		src, dst := translateRule(r.Source, r.Destination, false)
		fmt.Fprintf(&b, "%s\t%s\t200\n", src, dst)
	}
	p := filepath.Join(outDir, "_redirects")
	if err := fsutil.WriteFileAtomic(p, []byte(b.String()), 0644); err != nil {
		return nil, fmt.Errorf("writing _redirects: %w", err)
	}
	return []string{p}, nil
}

// emitHeaders writes a `_headers` file (Netlify + Cloudflare Pages format) from
// the config's `headers` rules. Only explicit rules are emitted; `security`
// defaults are left to `krate serve` so static-host behavior is not changed
// implicitly.
func emitHeaders(cfg *config.Config, outDir string) ([]string, error) {
	if len(cfg.Headers) == 0 {
		return nil, nil
	}
	var b strings.Builder
	for _, rule := range cfg.Headers {
		if rule.Source == "" || len(rule.Headers) == 0 {
			continue
		}
		b.WriteString(rule.Source)
		b.WriteByte('\n')
		keys := make([]string, 0, len(rule.Headers))
		for k := range rule.Headers {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			b.WriteString("  ")
			b.WriteString(k)
			b.WriteString(": ")
			b.WriteString(rule.Headers[k])
			b.WriteByte('\n')
		}
		b.WriteByte('\n')
	}
	if b.Len() == 0 {
		return nil, nil
	}
	p := filepath.Join(outDir, "_headers")
	if err := fsutil.WriteFileAtomic(p, []byte(b.String()), 0644); err != nil {
		return nil, fmt.Errorf("writing _headers: %w", err)
	}
	return []string{p}, nil
}

// translateRule converts a Krate redirect/rewrite pattern to a host's syntax.
// Krate's matcher treats a trailing "/*" and a trailing ":name*" as "the rest
// of the path". Netlify and Cloudflare spell that "/*" with ":splat"; Vercel
// (path-to-regexp) spells it ":path*". Single-segment ":name" parameters are
// common to all. A destination must name the splat explicitly (":splat"): the
// dev server's implicit "append the rest of the path" is not portable, so
// hosts receive the destination unchanged.
func translateRule(source, dest string, vercel bool) (string, string) {
	token := ":splat"
	if vercel {
		token = ":path*"
	}
	switch {
	case strings.HasSuffix(source, "/*"):
		base := strings.TrimSuffix(source, "/*")
		dest = strings.ReplaceAll(dest, ":splat", token)
		if vercel {
			source = base + "/:path*"
		}
	default:
		segs := strings.Split(source, "/")
		last := segs[len(segs)-1]
		if strings.HasPrefix(last, ":") && strings.HasSuffix(last, "*") && !vercel {
			name := strings.TrimSuffix(last[1:], "*")
			dest = strings.ReplaceAll(dest, ":"+name, ":splat")
			segs[len(segs)-1] = "*"
			source = strings.Join(segs, "/")
		}
	}
	return source, dest
}

const vercelSchema = "https://openapi.vercel.sh/vercel.json"

// emitVercel writes `vercel.json` at the project root, where a Git-integrated
// Vercel project reads it (a copy inside the output directory is ignored
// there). `outputDirectory` points Vercel at the build output.
func emitVercel(cfg *config.Config, root, outDir string) ([]string, error) {
	type vRedirect struct {
		Source      string `json:"source"`
		Destination string `json:"destination"`
		// Always written: Vercel treats a missing "permanent" as true (308).
		Permanent bool `json:"permanent"`
	}
	type vRewrite struct {
		Source      string `json:"source"`
		Destination string `json:"destination"`
	}
	doc := map[string]interface{}{"$schema": vercelSchema}
	if rel, err := filepath.Rel(root, outDir); err == nil && !strings.HasPrefix(rel, "..") {
		doc["outputDirectory"] = filepath.ToSlash(rel)
	}
	if len(cfg.Redirects) > 0 {
		rs := make([]vRedirect, 0, len(cfg.Redirects))
		for _, r := range cfg.Redirects {
			src, dst := translateRule(r.Source, r.Destination, true)
			rs = append(rs, vRedirect{Source: src, Destination: dst, Permanent: r.Permanent})
		}
		doc["redirects"] = rs
	}
	if len(cfg.Rewrites) > 0 {
		rw := make([]vRewrite, 0, len(cfg.Rewrites))
		for _, r := range cfg.Rewrites {
			src, dst := translateRule(r.Source, r.Destination, true)
			rw = append(rw, vRewrite{Source: src, Destination: dst})
		}
		doc["rewrites"] = rw
	}
	if len(cfg.Redirects) == 0 && len(cfg.Rewrites) == 0 {
		doc["cleanUrls"] = true
	}
	// Adapter options (config `deploy.options`).
	if v, ok := cfg.Deploy.Options["cleanUrls"]; ok {
		doc["cleanUrls"] = strings.EqualFold(v, "true")
	}
	if v, ok := cfg.Deploy.Options["trailingSlash"]; ok {
		doc["trailingSlash"] = strings.EqualFold(v, "true")
	}
	// Custom headers (config `headers`).
	if len(cfg.Headers) > 0 {
		type vHeader struct {
			Key   string `json:"key"`
			Value string `json:"value"`
		}
		type vHeaderRule struct {
			Source  string    `json:"source"`
			Headers []vHeader `json:"headers"`
		}
		rules := make([]vHeaderRule, 0, len(cfg.Headers))
		for _, r := range cfg.Headers {
			if r.Source == "" || len(r.Headers) == 0 {
				continue
			}
			keys := make([]string, 0, len(r.Headers))
			for k := range r.Headers {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			hh := make([]vHeader, 0, len(keys))
			for _, k := range keys {
				hh = append(hh, vHeader{Key: k, Value: r.Headers[k]})
			}
			src, _ := translateRule(r.Source, "", true)
			rules = append(rules, vHeaderRule{Source: src, Headers: hh})
		}
		if len(rules) > 0 {
			doc["headers"] = rules
		}
	}

	data, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return nil, err
	}
	p := filepath.Join(root, "vercel.json")
	// Never clobber a hand-written vercel.json: only a file krate wrote (it
	// carries the $schema marker) is regenerated.
	if existing, err := os.ReadFile(p); err == nil {
		var cur map[string]interface{}
		if json.Unmarshal(existing, &cur) != nil || cur["$schema"] != vercelSchema {
			return nil, fmt.Errorf("%s already exists and was not generated by krate; merge the redirects/headers by hand or remove it, then re-run `krate deploy --target vercel`", p)
		}
	}
	if err := fsutil.WriteFileAtomic(p, append(data, '\n'), 0644); err != nil {
		return nil, fmt.Errorf("writing vercel.json: %w", err)
	}
	// Any copy left in the output directory by an older krate is stale.
	if filepath.Clean(root) != filepath.Clean(outDir) {
		_ = os.Remove(filepath.Join(outDir, "vercel.json"))
	}
	return []string{p}, nil
}

// emitGitHubPages writes `.nojekyll` and a root `404.html`, which GitHub Pages
// requires for SPA-style/fallback routing and to skip Jekyll processing.
func emitGitHubPages(outDir string) ([]string, error) {
	var written []string

	nojekyll := filepath.Join(outDir, ".nojekyll")
	if err := fsutil.WriteFileAtomic(nojekyll, nil, 0644); err != nil {
		return nil, fmt.Errorf("writing .nojekyll: %w", err)
	}
	written = append(written, nojekyll)

	dest := filepath.Join(outDir, "404.html")
	if _, err := os.Stat(dest); os.IsNotExist(err) {
		src := filepath.Join(outDir, "404", "index.html")
		if data, err := os.ReadFile(src); err == nil {
			if err := fsutil.WriteFileAtomic(dest, data, 0644); err != nil {
				return nil, fmt.Errorf("writing 404.html: %w", err)
			}
			written = append(written, dest)
		}
	}
	return written, nil
}

// emitDocker writes a Dockerfile and .dockerignore at the project root. The
// image builds the site once and runs `krate start`, which serves the built
// output (SSR/ISR pages, API routes, actions and middleware that a static host
// cannot) without rebuilding.
func emitDocker(cfg *config.Config, root, outDir string) ([]string, error) {
	install := "RUN npm install"
	copyManifests := "COPY package.json package-lock.json* ./"
	switch {
	case fileExists(filepath.Join(root, "pnpm-lock.yaml")):
		install = "RUN corepack enable && pnpm install --frozen-lockfile"
		copyManifests = "COPY package.json pnpm-lock.yaml ./"
	case fileExists(filepath.Join(root, "yarn.lock")):
		install = "RUN corepack enable && yarn install --frozen-lockfile"
		copyManifests = "COPY package.json yarn.lock ./"
	case fileExists(filepath.Join(root, "package-lock.json")):
		install = "RUN npm ci"
	}
	port := cfg.ServerPort()
	var b strings.Builder
	b.WriteString("# Generated by `krate deploy --target docker`.\n")
	b.WriteString("FROM node:22-bookworm-slim\n")
	if fileExists(gohost.RoutesManifestPath(root)) {
		b.WriteString("# Go API routes and components compile with the Go toolchain.\n")
		b.WriteString("COPY --from=golang:1.26 /usr/local/go /usr/local/go\n")
		b.WriteString("ENV PATH=/usr/local/go/bin:$PATH\n")
	}
	b.WriteString("WORKDIR /app\n")
	b.WriteString(copyManifests + "\n")
	b.WriteString(install + "\n")
	b.WriteString("COPY . .\n")
	b.WriteString("ENV NODE_ENV=production\n")
	b.WriteString("RUN npx krate build\n")
	fmt.Fprintf(&b, "ENV PORT=%d\n", port)
	fmt.Fprintf(&b, "EXPOSE %d\n", port)
	b.WriteString("CMD [\"npx\", \"krate\", \"start\"]\n")

	dockerfile := filepath.Join(root, "Dockerfile")
	if existing, err := os.ReadFile(dockerfile); err == nil && !strings.Contains(string(existing), "krate deploy --target docker") {
		return nil, fmt.Errorf("%s already exists and was not generated by krate; remove it or merge by hand, then re-run `krate deploy --target docker`", dockerfile)
	}
	if err := fsutil.WriteFileAtomic(dockerfile, []byte(b.String()), 0644); err != nil {
		return nil, fmt.Errorf("writing Dockerfile: %w", err)
	}
	written := []string{dockerfile}
	// An existing .dockerignore is the user's; only create one when absent.
	dockerignore := filepath.Join(root, ".dockerignore")
	if !fileExists(dockerignore) {
		ignore := "node_modules\n.git\n.krate\ndist\n.env*\n*.log\nDockerfile\n.dockerignore\n"
		if err := fsutil.WriteFileAtomic(dockerignore, []byte(ignore), 0644); err != nil {
			return nil, fmt.Errorf("writing .dockerignore: %w", err)
		}
		written = append(written, dockerignore)
	}
	return written, nil
}

func dirExists(p string) bool {
	fi, err := os.Stat(p)
	return err == nil && fi.IsDir()
}
