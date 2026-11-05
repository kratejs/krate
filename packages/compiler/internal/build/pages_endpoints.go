package build

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/evanw/esbuild/pkg/api"
	"github.com/kratejs/krate/packages/compiler/internal/bundler"
	"github.com/kratejs/krate/packages/compiler/internal/diag"
	"github.com/kratejs/krate/packages/compiler/internal/environ"
	"github.com/kratejs/krate/packages/compiler/internal/execx"
	"github.com/kratejs/krate/packages/compiler/internal/fsutil"
	"github.com/kratejs/krate/packages/compiler/internal/gohost"
	"github.com/kratejs/krate/packages/compiler/internal/jseval"
)

// Prerendered endpoints: src/pages/data.json.ts, feed.xml.ts, robots.txt.ts.
// A module under the pages directory whose name is <name>.<data extension>.ts
// is not a page. It exports GET, which runs once at build time, and the response
// body is written to the output as <name>.<data extension>.

// pagesEndpointExts are the document and data extensions that mark a .ts or .js
// file under the pages directory as a prerendered endpoint.
var pagesEndpointExts = map[string]bool{
	"json": true, "xml": true, "txt": true, "csv": true, "rss": true, "atom": true,
	"webmanifest": true, "ics": true, "yml": true, "yaml": true,
}

// isPagesEndpointFile reports whether a file name under the pages directory
// is a prerendered endpoint (data.json.ts, feed.xml.js).
func isPagesEndpointFile(base string) bool {
	ext := filepath.Ext(base)
	if ext != ".ts" && ext != ".js" {
		return false
	}
	inner := strings.TrimSuffix(base, ext)
	dot := strings.LastIndexByte(inner, '.')
	if dot <= 0 || strings.HasPrefix(inner, "_") {
		return false
	}
	return pagesEndpointExts[strings.ToLower(inner[dot+1:])]
}

// isStrayAPIFile reports whether rel (slash path relative to the pages
// directory) is a .ts or .js file under api/: the Next.js Pages Router location
// for API routes, which Krate does not serve from there.
func isStrayAPIFile(rel string) bool {
	if !strings.HasPrefix(rel, "api/") {
		return false
	}
	ext := filepath.Ext(rel)
	return ext == ".ts" || ext == ".js"
}

// pagesStrays are the non-page script files found under the pages directory.
type pagesStrays struct {
	endpoints []string // absolute paths of prerendered endpoints
	apiFiles  []string // absolute paths of src/pages/api/*.ts|js
}

// findPagesStrays lists the script files under the pages directory that are not
// pages: prerendered endpoints and files in a Pages Router style api/ folder.
func findPagesStrays(dir string) pagesStrays {
	var out pagesStrays
	_ = fsutil.WalkExt(dir, map[string]bool{".ts": true, ".js": true}, nil, func(path string, _ os.FileInfo) error {
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return nil
		}
		rel = filepath.ToSlash(rel)
		base := filepath.Base(path)
		if strings.HasPrefix(base, "_") {
			return nil
		}
		switch {
		case isStrayAPIFile(rel):
			out.apiFiles = append(out.apiFiles, path)
		case isPagesEndpointFile(base):
			out.endpoints = append(out.endpoints, path)
		}
		return nil
	})
	return out
}

// strayAPIWarnings turns files under src/pages/api into warnings: they used to
// build into empty pages while /api/... answered 502.
func (b *Builder) strayAPIWarnings(files []string) []diag.Diagnostic {
	var out []diag.Diagnostic
	for _, f := range files {
		rel := f
		if r, err := filepath.Rel(b.Root, f); err == nil {
			rel = filepath.ToSlash(r)
		}
		inAPI := filepath.Base(f)
		if r, err := filepath.Rel(filepath.Join(b.Cfg.PagesDir, "api"), f); err == nil {
			inAPI = filepath.ToSlash(r)
		}
		out = append(out, diag.Diagnostic{
			File:     rel,
			Severity: diag.SeverityWarning,
			Code:     diag.CodeAPIRouteInPages,
			Message:  "this file is in the pages directory, so it is not served as an API route (/api/... answers 502)",
			Hint:     "API routes live in " + gohost.DefaultAPIDir + "/; move it to " + gohost.DefaultAPIDir + "/" + inAPI,
		})
	}
	return out
}

// buildPagesEndpoints runs each prerendered endpoint once and writes its
// response body into the output directory. It returns one error per failure.
func (b *Builder) buildPagesEndpoints(files []string) []error {
	if len(files) == 0 {
		return nil
	}
	bnd := b.newBundler()
	var errs []error
	for _, f := range files {
		if err := b.buildPagesEndpoint(f, bnd); err != nil {
			errs = append(errs, err)
		}
	}
	return errs
}

func (b *Builder) buildPagesEndpoint(file string, bnd *bundler.Bundler) error {
	rel, err := filepath.Rel(b.Cfg.PagesDir, file)
	if err != nil {
		return err
	}
	rel = filepath.ToSlash(rel)
	display := rel
	if r, err := filepath.Rel(b.Root, file); err == nil {
		display = filepath.ToSlash(r)
	}
	fail := func(msg, hint string) error {
		return diag.Diagnostic{File: display, Code: diag.CodePagesEndpoint, Message: msg, Hint: hint}
	}
	outRel := strings.TrimSuffix(rel, filepath.Ext(rel))
	if strings.ContainsAny(outRel, "[]") {
		return fail("a prerendered endpoint cannot have a dynamic segment", "prerendered endpoints are built once; use an API route in src/api/ for a path that depends on a parameter")
	}
	abs, err := filepath.Abs(file)
	if err != nil {
		return err
	}

	body, err := b.runEndpointBundle(abs, "/"+outRel, bnd)
	if err != nil {
		var ee *endpointExit
		if errors.As(err, &ee) && ee.noGET {
			return fail("no GET export", "export `async function GET() { return Response.json(...) }`; it runs once at build time and its body becomes "+outRel)
		}
		return fail("running GET failed: "+err.Error(), "the endpoint runs on Node at build time with the project's environment; fix the error above, or move it to src/api/ to compute it per request")
	}
	var res struct {
		Status int    `json:"status"`
		Body   string `json:"body"`
	}
	if err := json.Unmarshal(body, &res); err != nil {
		return fail("unreadable endpoint result: "+err.Error(), "")
	}
	if res.Status < 200 || res.Status > 299 {
		return fail(fmt.Sprintf("GET returned status %d", res.Status), "a prerendered endpoint must answer 2xx: its response is written to the output as a file")
	}
	data, err := base64.StdEncoding.DecodeString(res.Body)
	if err != nil {
		return fail("unreadable endpoint body: "+err.Error(), "")
	}
	dest := filepath.Join(b.Cfg.OutDir, filepath.FromSlash(outRel))
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return err
	}
	if err := fsutil.WriteFileIfChanged(dest, data, 0o644); err != nil {
		return err
	}
	printf("    %s+%s %s\n", cGreen, cReset, outRel)
	return nil
}

// endpointExit is a failed run of the endpoint bootstrap.
type endpointExit struct {
	noGET bool
	msg   string
}

func (e *endpointExit) Error() string { return e.msg }

// endpointBootstrapJS imports the endpoint, calls GET and prints the response
// as JSON (status plus the base64 body).
func endpointBootstrapJS(importPath, route string) string {
	return fmt.Sprintf(`import * as mod from %q;
const GET = mod.GET || (mod.default && mod.default.GET);
if (typeof GET !== 'function') {
  process.stderr.write('KRATE_NO_GET\n');
  process.exit(3);
}
const res = await GET(new Request('http://localhost' + %q), { params: {} });
if (!res || typeof res.arrayBuffer !== 'function') {
  process.stderr.write('GET must return a Response\n');
  process.exit(4);
}
const body = Buffer.from(await res.arrayBuffer());
process.stdout.write(JSON.stringify({ status: res.status, body: body.toString('base64') }));
`, importPath, route)
}

// runEndpointBundle bundles the bootstrap to ESM and runs it with the
// configured runtime, like generateStaticParams does for Node-only code.
func (b *Builder) runEndpointBundle(abs, route string, bnd *bundler.Bundler) ([]byte, error) {
	resolve := func(importer, spec string) (string, bool) {
		if bnd != nil {
			if p := bnd.Resolve(importer, spec); p != "" {
				return p, true
			}
		}
		return "", false
	}
	js, err := jseval.Bundle(jseval.Options{
		Source:        endpointBootstrapJS(filepath.ToSlash(abs), route),
		ResolveDir:    filepath.Dir(abs),
		Sourcefile:    "krate-endpoint-bootstrap.ts",
		Loader:        api.LoaderTS,
		ResolveImport: resolve,
	})
	if err != nil {
		return nil, fmt.Errorf("bundling: %w", err)
	}
	genDir := filepath.Join(b.Root, ".krate", "gen")
	if err := os.MkdirAll(genDir, 0o755); err != nil {
		return nil, err
	}
	path := filepath.Join(genDir, "endpoint-"+hashContent([]byte(js))+".mjs")
	if err := fsutil.WriteFileIfChanged(path, []byte(js), 0o644); err != nil {
		return nil, err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmdName, args := "node", []string{path}
	switch b.gspRuntime() {
	case "bun":
		cmdName, args = "bun", []string{"run", path}
	case "deno":
		cmdName, args = "deno", []string{"run", "--allow-net", "--allow-read", "--allow-env", "--allow-sys", path}
	}
	cmd := execx.Command(ctx, cmdName, args...)
	cmd.Dir = filepath.Dir(abs)
	if len(b.Env) > 0 {
		cmd.Env = append(os.Environ(), environ.KVList(b.Env)...)
	}
	out, err := cmd.Output()
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return nil, errors.New("timed out after 30s")
		}
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			stderr := strings.TrimSpace(string(exitErr.Stderr))
			if strings.Contains(stderr, "KRATE_NO_GET") {
				return nil, &endpointExit{noGET: true, msg: "no GET export"}
			}
			return nil, &endpointExit{msg: stderr}
		}
		return nil, fmt.Errorf("%s is required to build prerendered endpoints: %w", cmdName, err)
	}
	return out, nil
}
