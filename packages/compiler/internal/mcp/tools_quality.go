package mcp

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/kratejs/krate/packages/compiler/internal/build"
	"github.com/kratejs/krate/packages/compiler/internal/check"
	"github.com/kratejs/krate/packages/compiler/internal/diag"
	"github.com/kratejs/krate/packages/compiler/internal/execx"
	"github.com/kratejs/krate/packages/compiler/internal/fix"
	"github.com/kratejs/krate/packages/compiler/internal/migrate"
	"github.com/kratejs/krate/packages/compiler/internal/typecheck"
	krateversion "github.com/kratejs/krate/packages/compiler/internal/version"
)

func isScriptFile(p string) bool {
	switch strings.ToLower(filepath.Ext(p)) {
	case ".tsx", ".ts", ".mts", ".jsx", ".js", ".mjs", ".cjs":
		return true
	}
	return false
}

// --- audit_page ---

type auditFinding struct {
	Rule     string `json:"rule"`
	Category string `json:"category"`
	Severity string `json:"severity"`
	Route    string `json:"route,omitempty"`
	File     string `json:"file,omitempty"`
	Line     int    `json:"line,omitempty"`
	Col      int    `json:"col,omitempty"`
	// Approximate marks a position found by searching the source.
	Approximate bool   `json:"approximate,omitempty"`
	Message     string `json:"message"`
	Hint        string `json:"hint,omitempty"`
}

// runChecks evaluates the quality gates, building first when there is no
// output, exactly like the check tool.
func (s *Service) runChecks(ctx context.Context) ([]check.Finding, check.Config, error) {
	if _, err := os.Stat(build.ResolveManifestPath(s.cfg.OutDir, "manifest.json")); err != nil {
		if out, buildErr := s.runBuild(ctx); buildErr != nil {
			return nil, check.Config{}, fmt.Errorf("build failed: %v\n\n%s", buildErr, out)
		}
	}
	var findings []check.Finding
	var cfg check.Config
	chk := newBuilder(Options{Root: s.root, Cfg: s.cfg, Env: s.env, Verbose: s.verbose})
	chk.SetContext(ctx)
	s.runMu.Lock()
	_, err := captureStdout(func() error {
		var e error
		findings, cfg, e = chk.CheckSite(true)
		return e
	})
	s.runMu.Unlock()
	return findings, cfg, err
}

func (s *Service) toolAuditPage(ctx context.Context, args map[string]any) (ToolResult, *rpcError) {
	category := argString(args, "category")
	switch category {
	case "", "all", check.CategoryA11y, check.CategorySEO, check.CategoryPerf, "links":
	default:
		return ErrorResult("category must be one of: all, a11y, seo, perf, links"), nil
	}
	severity := argString(args, "severity")
	switch severity {
	case "", "all", "error", "warning":
	default:
		return ErrorResult("severity must be one of: all, error, warning"), nil
	}
	route := argString(args, "route")
	if route != "" {
		if _, err := s.findPage(route); err != nil {
			return ErrorResult(err.Error()), nil
		}
	}
	reportProgress(ctx, 0, 1, "running quality checks")
	findings, cfg, err := s.runChecks(ctx)
	if err != nil {
		return ErrorResult("running checks: " + err.Error()), nil
	}
	reportProgress(ctx, 1, 1, "checks complete")

	var kept []auditFinding
	var matched []check.Finding
	locate := s.newFindingLocator()
	byCat := map[string]int{}
	byRule := map[string]int{}
	for _, f := range findings {
		sev := f.Severity.String()
		cat := f.Category
		if strings.HasPrefix(f.Rule, "links") || strings.Contains(f.Rule, "link") {
			if category == "links" {
				cat = "links"
			}
		}
		if category != "" && category != "all" && cat != category && f.Category != category {
			continue
		}
		if severity != "" && severity != "all" && !strings.EqualFold(sev, severity) {
			continue
		}
		if route != "" && f.Route != route {
			continue
		}
		byCat[f.Category]++
		byRule[f.Rule]++
		matched = append(matched, f)
		line, col, approx := f.Line, f.Col, false
		file := filepath.ToSlash(f.File)
		if line == 0 {
			line, col, approx = locate.next(f.Rule, file)
		}
		kept = append(kept, auditFinding{Rule: f.Rule, Category: f.Category, Severity: sev, Route: f.Route,
			File: file, Line: line, Col: col, Approximate: approx, Message: f.Message, Hint: f.Hint})
	}
	// Counts follow the filters, so a page's audit reports that page.
	errs, warns := check.Counts(matched)
	page, next, rerr := paginate(kept, args, 30, 300)
	if rerr != nil {
		return ToolResult{}, rerr
	}
	return JSONResult(map[string]any{
		"total": len(kept), "errors": errs, "warnings": warns, "failOn": cfg.FailOn.String(),
		"failing":    check.Failing(findings, cfg.FailOn),
		"byCategory": byCat, "byRule": byRule,
		"findings": page, "nextCursor": next,
		"knownRules": check.KnownRuleIDs(),
		"note":       "file:line:col point into the source when the rule can locate it; route-level findings have only a route",
	})
}

// --- explain_diagnostic ---

var safeFixCodes = map[string]string{
	"KR5002": "lowercase event handler names",
	"KR5005": "ignored form default values",
	"KR5007": "contentEditable onChange",
	"KR7125": "tsconfig baseUrl/paths mismatch",
}

func (s *Service) toolExplainDiagnostic(ctx context.Context, args map[string]any) (ToolResult, *rpcError) {
	code := strings.ToUpper(strings.TrimSpace(argString(args, "code")))
	query := strings.ToLower(strings.TrimSpace(argString(args, "query")))
	if code == "" && query == "" {
		return ErrorResult("provide code (e.g. KR7115) or query (words from the message)"), nil
	}
	type view struct {
		Code        string `json:"code"`
		Title       string `json:"title"`
		Explanation string `json:"explanation"`
		SafeFix     string `json:"safeFix,omitempty"`
		Docs        string `json:"docs"`
	}
	mk := func(i diag.CodeInfo) view {
		v := view{Code: string(i.Code), Title: i.Title, Explanation: i.Explanation,
			Docs: "krate://docs/reference/diagnostics (section " + strings.ToLower(string(i.Code)) + ")"}
		if f, ok := safeFixCodes[string(i.Code)]; ok {
			v.SafeFix = "apply_safe_fixes with codes [\"" + string(i.Code) + "\"] rewrites this (" + f + ")"
		}
		return v
	}
	if code != "" {
		if info, ok := diag.Lookup(diag.Code(code)); ok {
			res := map[string]any{"match": mk(info)}
			// Where the current project reports it, from the last check/build is
			// not cached, so point at the tools instead.
			res["next"] = "run get_diagnostics (or get_errors on a running dev server) to see where " + code + " occurs, then fix the file:line it names"
			return JSONResult(res)
		}
		var near []string
		for _, i := range diag.Codes() {
			if strings.HasPrefix(string(i.Code), code[:min(len(code), 4)]) && len(near) < 8 {
				near = append(near, string(i.Code)+" "+i.Title)
			}
		}
		if len(near) == 0 {
			return ErrorResult("no registered diagnostic " + code + "; search with query, or see krate://docs/reference/diagnostics"), nil
		}
		return ErrorResult(fmt.Sprintf("no registered diagnostic %s. Codes in the same range: %s", code, strings.Join(near, "; "))), nil
	}
	var matches []view
	for _, i := range diag.Codes() {
		hay := strings.ToLower(string(i.Code) + " " + i.Title + " " + i.Explanation)
		ok := true
		for _, w := range strings.Fields(query) {
			if !strings.Contains(hay, w) {
				ok = false
				break
			}
		}
		if ok {
			matches = append(matches, mk(i))
		}
	}
	page, next, rerr := paginate(matches, args, 10, 50)
	if rerr != nil {
		return ToolResult{}, rerr
	}
	return JSONResult(map[string]any{"total": len(matches), "matches": page, "nextCursor": next})
}

// --- typecheck ---

var tscLineRe = regexp.MustCompile(`^(.+?)\((\d+),(\d+)\): (error|warning) (TS\d+): (.*)$`)

type tsDiag struct {
	File     string `json:"file"`
	Line     int    `json:"line"`
	Col      int    `json:"col"`
	Severity string `json:"severity"`
	Code     string `json:"code"`
	Message  string `json:"message"`
}

func parseTSC(out string) ([]tsDiag, []string) {
	var diags []tsDiag
	var other []string
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimRight(line, "\r")
		if m := tscLineRe.FindStringSubmatch(line); m != nil {
			l, _ := strconv.Atoi(m[2])
			c, _ := strconv.Atoi(m[3])
			diags = append(diags, tsDiag{File: filepath.ToSlash(m[1]), Line: l, Col: c, Severity: m[4], Code: m[5], Message: m[6]})
		} else if strings.TrimSpace(line) != "" {
			// Continuation lines belong to the previous diagnostic.
			if len(diags) > 0 && strings.HasPrefix(line, " ") {
				diags[len(diags)-1].Message += "\n" + strings.TrimSpace(line)
			} else {
				other = append(other, line)
			}
		}
	}
	return diags, other
}

func (s *Service) toolTypecheck(ctx context.Context, args map[string]any) (ToolResult, *rpcError) {
	if r := s.denyIfReadOnly("typecheck"); r != nil {
		return *r, nil
	}
	if !s.taskAllowed("typecheck") {
		return ErrorResult(`typecheck runs the project's TypeScript compiler; add "typecheck" to allowTasks in .krate/mcp.json to enable it`), nil
	}
	tsc := typecheck.FindTSC(s.root)
	if tsc == "" {
		return ErrorResult("typescript is not installed in this project (npm install -D typescript)"), nil
	}
	if errs := build.GenerateTypes(s.root, s.cfg); len(errs) > 0 {
		return ErrorResult("generating route types: " + errs[0].Error()), nil
	}
	reportProgress(ctx, 0, 1, "running tsc")
	runCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	cmd := execx.Command(runCtx, "node", tsc, "--noEmit", "--pretty", "false")
	cmd.Dir = s.root
	out, err := cmd.CombinedOutput()
	if err != nil && len(out) == 0 {
		return ErrorResult("running tsc: " + err.Error()), nil
	}
	kept, _, dropped := typecheck.Filter(string(out), func(p string) (string, error) {
		if !filepath.IsAbs(p) {
			p = filepath.Join(s.root, p)
		}
		data, e := os.ReadFile(p)
		return string(data), e
	})
	reportProgress(ctx, 1, 1, "tsc complete")
	diags, other := parseTSC(kept)
	counts := map[string]int{}
	for _, d := range diags {
		counts[d.Severity]++
	}
	if f := argString(args, "file"); f != "" {
		f = filepath.ToSlash(f)
		var sel []tsDiag
		for _, d := range diags {
			if d.File == f {
				sel = append(sel, d)
			}
		}
		diags = sel
	}
	page, next, rerr := paginate(diags, args, 50, 500)
	if rerr != nil {
		return ToolResult{}, rerr
	}
	return JSONResult(map[string]any{
		"ok": counts["error"] == 0, "errors": counts["error"], "warnings": counts["warning"],
		"ignoredTierDiagnostics": dropped,
		"diagnostics":            page, "nextCursor": next, "other": other,
		"note": "tsc --noEmit after generating route types; errors about tier decorators (@server, @client) that the compiler removes are filtered out",
	})
}

// --- upgrade ---

type upgradeStep struct {
	ID       string         `json:"id"`
	Title    string         `json:"title"`
	Status   string         `json:"status"` // ok, action, manual, info
	Detail   string         `json:"detail,omitempty"`
	Evidence []string       `json:"evidence,omitempty"`
	Fix      map[string]any `json:"fix,omitempty"`
}

func versionMinor(v string) string {
	v = strings.TrimLeft(v, "^~>=v ")
	parts := strings.SplitN(v, ".", 3)
	if len(parts) < 2 {
		return ""
	}
	return parts[0] + "." + parts[1]
}

func (s *Service) grepProjectFiles(patterns []*regexp.Regexp, files []string, max int) []string {
	var hits []string
	for _, rel := range files {
		abs, _, err := s.resolveProjectPath(rel)
		if err != nil {
			continue
		}
		info, err := os.Stat(abs)
		if err != nil || info.IsDir() || info.Size() > 512<<10 {
			continue
		}
		if s.policy != nil && s.policy.denied(rel) {
			continue
		}
		data, err := os.ReadFile(abs)
		if err != nil {
			continue
		}
		for i, line := range strings.Split(string(data), "\n") {
			for _, re := range patterns {
				if re.MatchString(line) && len(hits) < max {
					hits = append(hits, fmt.Sprintf("%s:%d", rel, i+1))
					break
				}
			}
		}
	}
	return hits
}

func (s *Service) opsFiles() []string {
	files := []string{"package.json", "Dockerfile", "docker-compose.yml", "docker-compose.yaml", "Procfile", "netlify.toml", "vercel.json", ".devcontainer/devcontainer.json"}
	for _, dir := range []string{".github/workflows", "scripts", ".gitlab"} {
		abs, _, err := s.resolveProjectPath(dir)
		if err != nil {
			continue
		}
		_ = filepath.WalkDir(abs, func(p string, d fs.DirEntry, err error) error {
			if err == nil && !d.IsDir() {
				if rel := s.relToRoot(p); rel != "" {
					files = append(files, rel)
				}
			}
			return nil
		})
	}
	return files
}

func (s *Service) toolUpgrade(ctx context.Context, args map[string]any) (ToolResult, *rpcError) {
	apply := argBool(args, "apply", false)
	runBuild := argBool(args, "build", true)
	var steps []upgradeStep
	to := versionMinor(krateversion.Value)

	// Versions: the framework packages must sit on one minor.
	installed := map[string]string{}
	var from string
	for _, pkg := range []string{"@krate/core", "@krate/runtime", "@krate/components"} {
		if v := s.installedVersion(pkg); v != "" {
			installed[pkg] = v
		}
	}
	if v, ok := installed["@krate/core"]; ok {
		from = v
	}
	minors := map[string]bool{}
	for _, v := range installed {
		minors[versionMinor(v)] = true
	}
	st := upgradeStep{ID: "versions", Title: "Keep @krate/core, @krate/runtime and @krate/components on one minor"}
	switch {
	case len(installed) == 0:
		st.Status, st.Detail = "info", "no @krate/* packages found in node_modules; install dependencies first"
	case len(minors) > 1:
		st.Status = "action"
		st.Detail = "the packages are on different minors"
		for k, v := range installed {
			st.Evidence = append(st.Evidence, k+"@"+v)
		}
		sort.Strings(st.Evidence)
		st.Fix = map[string]any{"command": s.packageManager() + " add @krate/core@latest @krate/runtime@latest @krate/components@latest", "runBy": "you (changes dependencies)"}
	default:
		st.Status, st.Detail = "ok", "all installed @krate/* packages are on "+versionMinor(from)
	}
	steps = append(steps, st)

	// 1: tsconfig baseUrl and the other safe codemods.
	files, changes, skipped, ferr := fix.Plan(s.root)
	st = upgradeStep{ID: "safe-fixes", Title: "tsconfig baseUrl and conservative codemods (krate fix)"}
	switch {
	case ferr != nil:
		st.Status, st.Detail = "manual", ferr.Error()
	case len(changes) == 0:
		st.Status, st.Detail = "ok", "krate fix has nothing to change"
	default:
		st.Status = "action"
		st.Detail = fmt.Sprintf("%d safe rewrite(s) in %d file(s)", len(changes), len(files))
		for i, c := range changes {
			if i >= 8 {
				break
			}
			st.Evidence = append(st.Evidence, fmt.Sprintf("%s %s:%d:%d", c.Code, c.File, c.Line, c.Col))
		}
		st.Fix = map[string]any{"tool": "apply_safe_fixes", "args": map[string]any{"apply": true}}
	}
	if len(skipped) > 0 {
		st.Evidence = append(st.Evidence, fmt.Sprintf("%d file(s) skipped (parse errors)", len(skipped)))
	}
	steps = append(steps, st)

	// 2: Tailwind v4 output.
	if pj, ok := s.readPackageJSON("package.json"); ok {
		tw := pj.Dependencies["tailwindcss"] + pj.DevDependencies["tailwindcss"]
		if tw != "" {
			steps = append(steps, upgradeStep{ID: "tailwind", Title: "Tailwind output follows v4", Status: "manual",
				Detail:   "rings, shadows, blurs and colours render differently and a CSS reset is added; rename the classes or set tailwind.preflight: false",
				Evidence: []string{"tailwindcss " + tw}})
		}
	}

	ops := s.opsFiles()
	// 3: dev server host binding.
	hits := s.grepProjectFiles([]*regexp.Regexp{regexp.MustCompile(`krate["',\s]+dev\b`)}, []string{"Dockerfile", "docker-compose.yml", "docker-compose.yaml", ".devcontainer/devcontainer.json"}, 5)
	if len(hits) > 0 {
		steps = append(steps, upgradeStep{ID: "dev-host", Title: "krate dev binds 127.0.0.1", Status: "manual",
			Detail: "a container or devcontainer runs krate dev: it is no longer reachable from outside; add --host 0.0.0.0", Evidence: hits})
	}
	// 4: krate start no longer builds.
	hits = s.grepProjectFiles([]*regexp.Regexp{regexp.MustCompile(`krate["',\s]+start\b`)}, ops, 8)
	if len(hits) > 0 {
		steps = append(steps, upgradeStep{ID: "start-no-build", Title: "krate start no longer builds", Status: "manual",
			Detail: "run krate build first, or use krate serve (build then serve)", Evidence: hits})
	}
	// 5: tailwind config sandbox.
	for _, n := range []string{"tailwind.config.ts", "tailwind.config.js", "tailwind.config.mjs", "tailwind.config.cjs"} {
		if h := s.grepProjectFiles([]*regexp.Regexp{regexp.MustCompile(`require\(|from ['"]node:|from ['"](fs|path|os|child_process)['"]|process\.`)}, []string{n}, 5); len(h) > 0 {
			steps = append(steps, upgradeStep{ID: "tailwind-sandbox", Title: "tailwind.config runs in the config sandbox", Status: "manual",
				Detail: "the config uses Node APIs the sandbox may not provide; check the API subset in the upgrade guide", Evidence: h})
		}
	}
	// 6: output layout.
	hits = s.grepProjectFiles([]*regexp.Regexp{regexp.MustCompile(`dist/manifest\.json`)}, ops, 8)
	if len(hits) > 0 {
		steps = append(steps, upgradeStep{ID: "manifest-path", Title: "Manifest moved to dist/.krate/manifest.json", Status: "action",
			Detail: "scripts read dist/manifest.json, which no longer exists", Evidence: hits,
			Fix: map[string]any{"edit": "replace dist/manifest.json with dist/.krate/manifest.json in the files listed"}})
	}
	// 8: bin/krate.
	hits = s.grepProjectFiles([]*regexp.Regexp{regexp.MustCompile(`node_modules/@krate/core/bin/krate`)}, ops, 8)
	if len(hits) > 0 {
		steps = append(steps, upgradeStep{ID: "bin-krate", Title: "@krate/core/bin/krate is gone", Status: "action",
			Detail: "use krate, npx krate or node_modules/.bin/krate", Evidence: hits})
	}

	// 7: new hard errors.
	if runBuild {
		reportProgress(ctx, 0, 1, "building to find new hard errors")
		_, buildErr := s.runBuild(ctx)
		st = upgradeStep{ID: "build-errors", Title: "Errors that 0.4 let through"}
		if buildErr == nil {
			st.Status, st.Detail = "ok", "the project builds"
		} else {
			st.Status = "action"
			ds := build.DiagnosticsFromError(buildErr)
			st.Detail = fmt.Sprintf("the build fails with %d diagnostic(s); use explain_diagnostic on each code", len(ds))
			for i, d := range ds {
				if i >= 8 {
					break
				}
				file := d.File
				if rel := s.relToRoot(file); rel != "" {
					file = rel
				}
				code := string(d.Code)
				if code == "" {
					code = "no-code"
				}
				st.Evidence = append(st.Evidence, fmt.Sprintf("%s %s:%d %s", code, filepath.ToSlash(file), d.Line, firstLine(d.Message)))
			}
			if len(ds) == 0 {
				st.Evidence = append(st.Evidence, firstLine(buildErr.Error()))
			}
			st.Fix = map[string]any{"tool": "get_diagnostics"}
		}
		steps = append(steps, st)
	}
	steps = append(steps, upgradeStep{ID: "server-hardening", Title: "Server hardening changes", Status: "info",
		Detail: "middleware runs on more URLs and proxy headers are read differently; see krate://docs/guides/upgrading (Server behaviour changes)"})

	res := map[string]any{"from": from, "to": to, "steps": steps}
	pending := 0
	for _, stp := range steps {
		if stp.Status == "action" || stp.Status == "manual" {
			pending++
		}
	}
	res["pending"] = pending
	if from != "" && to != "" && versionMinor(from) == to {
		res["note"] = "the installed packages already match this compiler's minor version; the steps below are still worth checking"
	}
	res["guide"] = "krate://docs/guides/upgrading"
	if apply {
		if len(changes) == 0 {
			res["applied"] = "nothing to apply: no safe codemods are pending"
		} else {
			r, rerr := s.toolApplySafeFixes(ctx, map[string]any{"apply": true})
			if rerr != nil {
				return r, rerr
			}
			if r.IsError {
				return r, nil
			}
			res["applied"] = "applied the safe codemods as one transaction (undo restores them)"
			if len(r.Content) > 0 {
				txt, _ := truncateText(r.Content[0].Text, 4000)
				res["applyResult"] = txt
			}
		}
	} else {
		res["dryRun"] = true
		res["hint"] = "pass apply: true to run the safe codemods; dependency and script changes are listed for you to make"
	}
	return JSONResult(res)
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

// --- migrate_nextjs ---

func (s *Service) toolMigrateNextjs(ctx context.Context, args map[string]any) (ToolResult, *rpcError) {
	rep, err := migrate.ScanNextWithOptions(s.root, migrate.ScanOptions{UpdatePackage: argBool(args, "updatePackage", false), KrateVersion: krateversion.Value})
	if err != nil {
		return ErrorResult("scanning for a Next.js project: " + err.Error()), nil
	}
	type file struct {
		Kind       string   `json:"kind"`
		Source     string   `json:"source,omitempty"`
		Target     string   `json:"target"`
		Conflict   bool     `json:"conflict,omitempty"`
		Overwrites bool     `json:"overwrites,omitempty"`
		Rewrites   []string `json:"rewrites,omitempty"`
	}
	files := make([]file, 0, len(rep.Files))
	kinds := map[string]int{}
	overwrites := 0
	for _, f := range rep.Files {
		files = append(files, file{Kind: string(f.Kind), Source: f.Source, Target: f.Target, Conflict: f.Conflict, Overwrites: f.Overwrites, Rewrites: f.Rewrites})
		kinds[string(f.Kind)]++
		if f.Overwrites {
			overwrites++
		}
	}
	fpage, fnext, rerr := paginate(files, args, 60, 400)
	if rerr != nil {
		return ToolResult{}, rerr
	}
	return JSONResult(map[string]any{
		"framework": rep.Framework, "detected": rep.Detected,
		"fileCount": len(files), "byKind": kinds, "overwrites": overwrites,
		"files": fpage, "nextCursor": fnext,
		"routes": rep.Routes, "manual": rep.Manual, "warnings": rep.Warnings,
		"apply": "this tool only plans. Apply with `krate migrate nextjs --apply` (backs up every overwritten file; `krate migrate rollback <dir>` reverts), then run build and get_diagnostics.",
		"guide": "krate://docs/guides/migrating-from-nextjs",
	})
}

func (s *Service) registerQualityTools(srv *Server) {
	s.reg(srv, Tool{
		Name:        "audit_page",
		Description: "Run the compiler-enforced quality gates and return findings filtered by category (a11y, seo, perf, links), severity or route, with file:line:col, rule id and hint, plus counts per category and rule and whether the build would fail. Like check, but scoped and paginated for large sites. Builds first when there is no output.",
		InputSchema: objSchema(map[string]any{
			"category": enumSchema("Category filter (default all)", "all", "a11y", "seo", "perf", "links"),
			"severity": enumSchema("Severity filter (default all)", "all", "error", "warning"),
			"route":    strSchema("Only findings for this route"),
			"limit":    numSchema("Findings per page (default 30, max 300)"),
			"cursor":   strSchema("Opaque cursor from a previous result"),
		}),
		Annotations: rebuildToolAnnotations("Audit Page"),
		Handler:     s.toolAuditPage,
	})
	s.reg(srv, Tool{
		Name:        "explain_diagnostic",
		Description: "Explain a Krate diagnostic code (KR1xxx-KR9xxx): title, why it is reported, what to do instead, and whether apply_safe_fixes can repair it. Pass code for an exact lookup or query (words) to search all codes. Use it on any code from get_errors, get_diagnostics or build output.",
		InputSchema: objSchema(map[string]any{
			"code":   strSchema("Diagnostic code, e.g. KR7115"),
			"query":  strSchema("Words to search in code titles and explanations"),
			"limit":  numSchema("Matches per page for query (default 10, max 50)"),
			"cursor": strSchema("Opaque cursor from a previous result"),
		}),
		Annotations: readOnlyAnnotations("Explain Diagnostic"),
		Handler:     s.toolExplainDiagnostic,
	})
	s.reg(srv, Tool{
		Name:        "typecheck",
		Description: "Type-check the project with its own TypeScript compiler after generating route types, and return structured diagnostics (file, line, column, TS code, message) with tier-decorator noise filtered out. Needs \"typecheck\" in allowTasks and typescript installed. Prefer this over run_task typecheck when you need file:line results.",
		InputSchema: objSchema(map[string]any{
			"file":   strSchema("Only diagnostics in this project-relative file"),
			"limit":  numSchema("Diagnostics per page (default 50, max 500)"),
			"cursor": strSchema("Opaque cursor from a previous result"),
		}),
		Annotations: rebuildToolAnnotations("Typecheck"),
		Handler:     s.toolTypecheck,
	})
	s.reg(srv, Tool{
		Name:        "upgrade",
		Description: "Guided 0.4 to 0.5 upgrade of this project: checks package versions, runs the krate fix codemods (tsconfig baseUrl and more), scans scripts, Dockerfiles and CI for the breaking changes (krate start, dev host binding, manifest path, bin/krate, Tailwind v4, config sandbox) and builds to surface errors 0.4 let through. Returns an ordered step list with status and evidence. Dry-run by default; apply: true runs the safe codemods as one undoable transaction. Dependency and script edits are listed, never made.",
		InputSchema: objSchema(map[string]any{
			"apply": boolSchema("Run the safe codemods (default false: report only)"),
			"build": boolSchema("Build to find new hard errors (default true)"),
		}),
		Annotations: destructiveToolAnnotations("Upgrade Project"),
		Handler:     s.toolUpgrade,
	})
	s.reg(srv, Tool{
		Name:        "migrate_nextjs",
		Description: "Plan a Next.js to Krate migration for this project without writing anything: detected app/pages routers, every file that would be copied or generated (with the rewrites each gets), the route inventory, conflicts and overwrites, and the manual decisions left to you (intercepting routes, unsupported APIs). Apply with `krate migrate nextjs --apply`.",
		InputSchema: objSchema(map[string]any{
			"updatePackage": boolSchema("Also plan a package.json update with Krate scripts and dependencies (default false)"),
			"limit":         numSchema("Files per page (default 60, max 400)"),
			"cursor":        strSchema("Opaque cursor from a previous result"),
		}),
		Annotations: readOnlyAnnotations("Plan Next.js Migration"),
		Handler:     s.toolMigrateNextjs,
	})
}
