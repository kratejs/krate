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
	"github.com/kratejs/krate/packages/compiler/internal/sitepath"
)

// Targets lists the supported deploy adapters.
var Targets = []string{"netlify", "vercel", "cloudflare", "gh-pages", "docker", "node"}

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
	// metaDir holds the per-request robots.ts/sitemap.ts/manifest.ts routes.
	const metaDir = "__krate_meta"
	hasFiles := func(dir string) bool {
		n := 0
		_ = filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
			if err == nil && d.IsDir() && d.Name() == metaDir {
				return filepath.SkipDir
			}
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
	if dirExists(filepath.Join(serverOut("api-node"), metaDir)) {
		out = append(out, "per-request metadata routes (robots.ts, sitemap.ts or manifest.ts with dynamic or revalidate)")
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
	// Server is the bundled portable server (build.BuildServerBundle). It is
	// required for the node target and for platform targets of projects with
	// server features (see WantsServer).
	Server *ServerPlan
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
	features := ServerFeatures(root, outDir)
	if WantsServer(target, features, opt.StaticOnly) {
		return emitServer(target, cfg, root, outDir, opt.Server)
	}
	if len(features) > 0 && !opt.StaticOnly {
		return nil, fmt.Errorf("this project uses server features (%s) that the %s target cannot run: the deployed site would be an empty shell for them. Deploy with `krate deploy --target docker`, `--target node`, vercel, netlify or cloudflare (they run the server), or pass --static-only to publish only the static pages", strings.Join(features, ", "), target)
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

// ruleBase is the base path a redirect rule lives under: none for a
// basePath: false rule, which is written exactly as configured.
func ruleBase(base string, r config.Redirect) string {
	if !r.InBasePath() {
		return ""
	}
	return base
}

// emitRedirects writes a `_redirects` file (Netlify + Cloudflare Pages format).
func emitRedirects(cfg *config.Config, outDir string) ([]string, error) {
	if len(cfg.Redirects) == 0 && len(cfg.Rewrites) == 0 {
		return nil, nil
	}
	base := cfg.BaseURLPath()
	var b strings.Builder
	for _, r := range cfg.Redirects {
		status := "302"
		if r.Permanent {
			status = "301"
		}
		src, dst := translateRule(r.Source, r.Destination, false)
		if rb := ruleBase(base, r); rb != "" {
			src, dst = sitepath.Join(rb, src), sitepath.Join(rb, dst)
		}
		fmt.Fprintf(&b, "%s\t%s\t%s\n", src, dst, status)
	}
	for _, r := range cfg.Rewrites {
		src, dst := translateRule(r.Source, r.Destination, false)
		fmt.Fprintf(&b, "%s\t%s\t200\n", sitepath.Join(base, src), sitepath.Join(base, dst))
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
	base := cfg.BaseURLPath()
	var b strings.Builder
	for _, rule := range cfg.Headers {
		if rule.Source == "" || len(rule.Headers) == 0 {
			continue
		}
		b.WriteString(sitepath.Join(base, rule.Source))
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
	base := cfg.BaseURLPath()
	doc := map[string]interface{}{"$schema": vercelSchema}
	if rel, err := filepath.Rel(root, outDir); err == nil && !strings.HasPrefix(rel, "..") {
		doc["outputDirectory"] = filepath.ToSlash(rel)
	}
	if len(cfg.Redirects) > 0 {
		rs := make([]vRedirect, 0, len(cfg.Redirects))
		for _, r := range cfg.Redirects {
			src, dst := translateRule(r.Source, r.Destination, true)
			rb := ruleBase(base, r)
			rs = append(rs, vRedirect{Source: sitepath.Join(rb, src), Destination: sitepath.Join(rb, dst), Permanent: r.Permanent})
		}
		doc["redirects"] = rs
	}
	if len(cfg.Rewrites) > 0 {
		rw := make([]vRewrite, 0, len(cfg.Rewrites))
		for _, r := range cfg.Rewrites {
			src, dst := translateRule(r.Source, r.Destination, true)
			rw = append(rw, vRewrite{Source: sitepath.Join(base, src), Destination: sitepath.Join(base, dst)})
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
			rules = append(rules, vHeaderRule{Source: sitepath.Join(base, src), Headers: hh})
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

// GitHubPagesWarnings flags a base path on a site with a custom domain. GitHub
// Pages serves a repository with a CNAME at the domain root, so every prefixed
// URL would miss.
func GitHubPagesWarnings(cfg *config.Config, outDir string) []string {
	base := cfg.BaseURLPath()
	if base == "" {
		return nil
	}
	if _, err := os.Stat(filepath.Join(outDir, "CNAME")); err != nil {
		return nil
	}
	return []string{fmt.Sprintf("this site has a CNAME file and the base path %q, but GitHub Pages serves a custom domain at its root, so its URLs will 404: remove basePath and any path in site.url", base)}
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

// emitDocker writes a multi-stage Dockerfile and .dockerignore at the project
// root. The build stage installs everything, builds the site and drops the dev
// dependencies; the runtime stage holds only what `krate start` reads:
//
//   - the build output (dist, with its .krate server bundles and sidecar scripts),
//   - the project's .krate directory without its build-only parts (actions,
//     middleware, the Go host binary and route manifests stay; the build cache,
//     generated types, config schema, segment sources and vendored goext go),
//   - krate.config.* (krate start evaluates it, imports and all),
//   - package.json and node_modules without dev dependencies. @krate/core
//     (config imports and the binary) and the other @krate packages are moved
//     to dependencies first when the project lists them as devDependencies,
//     because server bundles import @krate/runtime and the config imports
//     @krate/core at runtime.
//
// No source files, no Go toolchain, no compilers are copied. The server runs
// as the image's unprivileged node user and the image has a HEALTHCHECK.
func emitDocker(cfg *config.Config, root, outDir string) ([]string, error) {
	rel, err := filepath.Rel(root, outDir)
	if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return nil, fmt.Errorf("the Docker image copies the build output from inside the project, but outDir is %s (project root %s): set outDir to a folder under the project", outDir, root)
	}
	rel = filepath.ToSlash(rel)
	install := "RUN npm install"
	copyManifests := "COPY package.json package-lock.json* ./"
	prune := "npm prune --omit=dev"
	switch {
	case fileExists(filepath.Join(root, "pnpm-lock.yaml")):
		install = "RUN corepack enable && pnpm install --frozen-lockfile"
		copyManifests = "COPY package.json pnpm-lock.yaml ./"
		prune = "pnpm prune --prod"
	case fileExists(filepath.Join(root, "yarn.lock")):
		install = "RUN corepack enable && yarn install --frozen-lockfile"
		copyManifests = "COPY package.json yarn.lock ./"
		prune = "yarn install --production --frozen-lockfile --ignore-scripts"
	case fileExists(filepath.Join(root, "package-lock.json")):
		install = "RUN npm ci"
	}
	port := cfg.ServerPort()
	var b strings.Builder
	b.WriteString("# Generated by `krate deploy --target docker`.\n")
	b.WriteString("# Stage 1 builds the site; stage 2 holds only what `krate start` reads at runtime.\n")
	b.WriteString("FROM node:22-bookworm-slim AS build\n")
	if fileExists(gohost.RoutesManifestPath(root)) {
		b.WriteString("# Go API routes and components compile with the Go toolchain (build stage only).\n")
		b.WriteString("COPY --from=golang:1.26 /usr/local/go /usr/local/go\n")
		b.WriteString("ENV PATH=/usr/local/go/bin:$PATH\n")
	}
	b.WriteString("WORKDIR /app\n")
	if strings.Contains(install, "corepack") {
		b.WriteString("RUN corepack enable\n")
		install = strings.Replace(install, "corepack enable && ", "", 1)
	}
	// The build does not need root either: the app directory belongs to the
	// image's unprivileged node user.
	b.WriteString("RUN chown node:node /app\n")
	b.WriteString("USER node\n")
	b.WriteString(strings.Replace(copyManifests, "COPY ", "COPY --chown=node:node ", 1) + "\n")
	b.WriteString(install + "\n")
	b.WriteString("COPY --chown=node:node . .\n")
	b.WriteString("ENV NODE_ENV=production\n")
	b.WriteString("RUN npx krate build\n")
	b.WriteString("# Drop the dev dependencies. The @krate packages are needed at runtime (server bundles import\n")
	b.WriteString("# @krate/runtime, krate.config.* imports @krate/core and the binary lives next to it), so a\n")
	b.WriteString("# devDependency on them is promoted first. Packages that krate.config.* or your pages import\n")
	b.WriteString("# must be in dependencies, or build with --build-arg KEEP_DEV_DEPS=true.\n")
	b.WriteString("ARG KEEP_DEV_DEPS=false\n")
	b.WriteString(`RUN node -e "const fs=require('fs');const p=JSON.parse(fs.readFileSync('package.json','utf8'));` +
		`p.dependencies=p.dependencies||{};for(const n of Object.keys(p.devDependencies||{})){` +
		`if(n.startsWith('@krate/')&&n!=='@krate/ts-plugin'){p.dependencies[n]=p.devDependencies[n];delete p.devDependencies[n];}}` +
		`fs.writeFileSync('package.json',JSON.stringify(p,null,2))"` + "\n")
	b.WriteString("RUN if [ \"$KEEP_DEV_DEPS\" != \"true\" ]; then " + prune + "; fi\n")
	b.WriteString("# What the server reads: the build output, .krate without its build-only parts, the config.\n")
	b.WriteString("RUN mkdir -p /tmp/runtime && cp package.json /tmp/runtime/ \\\n")
	b.WriteString(" && for f in krate.config.*; do [ -e \"$f\" ] && cp \"$f\" /tmp/runtime/; done; \\\n")
	fmt.Fprintf(&b, "    mkdir -p \"/tmp/runtime/%s\" && cp -a \"%s/.\" \"/tmp/runtime/%s/\" \\\n", rel, rel, rel)
	b.WriteString(" && if [ -d .krate ]; then mkdir -p /tmp/runtime/.krate && cp -a .krate/. /tmp/runtime/.krate/ \\\n")
	b.WriteString("      && rm -rf /tmp/runtime/.krate/cache /tmp/runtime/.krate/types /tmp/runtime/.krate/config.schema.json /tmp/runtime/.krate/segments /tmp/runtime/.krate/goext; fi\n")
	if dirs, skipped := traceIncludeDirs(cfg.OutputFileTracingIncludes); len(dirs) > 0 || len(skipped) > 0 {
		b.WriteString("# outputFileTracingIncludes: runtime-read project files (the leading directory of each glob).\n")
		for _, g := range skipped {
			fmt.Fprintf(&b, "# glob %q has no leading directory: copy what it matches into the runtime stage yourself.\n", g)
		}
		if len(dirs) > 0 {
			b.WriteString("RUN for p in " + strings.Join(dirs, " ") + "; do if [ -e \"$p\" ]; then cp -a --parents \"$p\" /tmp/runtime/; fi; done\n")
		}
	}
	b.WriteString("\n")
	b.WriteString("FROM node:22-bookworm-slim\n")
	b.WriteString("ENV NODE_ENV=production\n")
	b.WriteString("WORKDIR /app\n")
	b.WriteString("RUN chown node:node /app\n")
	b.WriteString("USER node\n")
	b.WriteString("COPY --from=build --chown=node:node /app/node_modules ./node_modules\n")
	b.WriteString("COPY --from=build --chown=node:node /tmp/runtime/ ./\n")
	fmt.Fprintf(&b, "ENV PORT=%d\n", port)
	fmt.Fprintf(&b, "EXPOSE %d\n", port)
	// /healthz is the liveness probe krate start serves at the root, base path or not.
	b.WriteString("HEALTHCHECK --interval=30s --timeout=3s --start-period=30s --retries=3 \\\n")
	b.WriteString("  CMD node -e \"fetch('http://127.0.0.1:'+(process.env.PORT||" + fmt.Sprint(port) + ")+'/healthz').then(r=>process.exit(r.ok?0:1),()=>process.exit(1))\"\n")
	b.WriteString("CMD [\"node\", \"node_modules/@krate/core/bin/krate.js\", \"start\"]\n")

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
