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
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/evanw/esbuild/pkg/api"

	"github.com/kratejs/krate/packages/compiler/internal/config"
	"github.com/kratejs/krate/packages/compiler/internal/diag"
)

// Browser differential against React 19 (react-dom/client).
//
// Each fixture testdata/reactbrowser/<area>/<name>.tsx exports a default
// component; the sibling <name>.steps.json holds an interaction script. The
// fixture is rendered (a) by real React 19 with createRoot, bundled here with
// esbuild (automatic JSX, production React from packages/react-oracle), and
// (b) by the real Krate build pipeline as a page. testdata/reactbrowser/run.mjs
// drives both sides with Playwright and records a snapshot after page load and
// after every step: normalized DOM of #root plus body-level portal content,
// form control state, the focused element, new window.__log entries (fixtures
// log through testdata/reactbrowser/lib/log.ts) and new uncaught errors. The
// snapshots are compared step by step here.
//
// DOM normalization (see capture() in run.mjs) drops comment nodes, <script>,
// <style>, <link>, <template> (Krate keeps hidden branches in templates) and
// Krate attributes (data-k, data-k?, data-kattr-*, data-krate-*); attributes
// compare as sets, class tokens and inline style declarations are sorted,
// adjacent text nodes are merged. Reflected form attributes (value, checked,
// selected) are left to the form-state channel. The javascript: placeholder
// React writes into action/formaction for function actions is dropped.
//
// Spec keys (<name>.steps.json):
//
//	steps                  interaction script (ops listed in reactBrowserOps)
//	expectBuildDiagnostic  "KR2001" or a range like "KR2xxx": the Krate build
//	                       must fail or warn with a matching code; the fixture
//	                       is then not compared with React
//	server                 run the Krate side under the real server so "use
//	                       server" modules go through POST /__krate/action/<id>
//	captureHead            also compare document.title, <meta> and some <link>s
//	settleMs               extra wait after each step (default 30)
//
// Known divergences live in testdata/reactbrowser/divergences.json as
// fixture -> {severity, reason}. P0 is a crash, uncaught error, lost or wrong
// user data/state or wrong DOM after an interaction; P1 is wrong ordering or
// timing observable by app logic, or a missing diagnostic for an unsupported
// pattern; P2 is cosmetic. An unlisted divergence fails the test, a listed
// fixture that now matches is logged as FIXED. Environment:
//
//	KRATE_BROWSER=chromium|firefox|webkit  run the browser differential
//	KRATE_REACTBROWSER_UPDATE=1            rewrite divergences.json (keeps
//	                                       reasons; new entries get a guessed
//	                                       severity and "TODO: classify")
//	KRATE_REACTBROWSER_RUN=<regexp>        only fixtures whose name matches
//	KRATE_REACTBROWSER_REPORT=<file>       write every divergence diff to file
//	KRATE_REACTBROWSER_JOBS=<n>            parallel fixtures (default 6 on
//	                                       Chromium, 3 on Firefox/WebKit)
//	KRATE_REACTBROWSER_KEEP=<dir>          build into dir and keep it
//	KRATE_REACTBROWSER_DRIVER=<command>    run run.mjs with this command
//	                                       instead of node, e.g. a docker run
//	                                       of the Playwright image (mount the
//	                                       repo and the temp dir at the same
//	                                       paths, use --network host)
//	KRATE_REACT_EXIT=1                     fail while any P0/P1 entry remains
//	                                       (the R2/R3 exit criterion)

const reactBrowserDir = "testdata/reactbrowser"

var reactBrowserOps = map[string][]string{
	"click":    {"sel"},
	"dblclick": {"sel"},
	"hover":    {"sel"},
	"type":     {"sel", "text"},
	"fill":     {"sel", "text"},
	"clear":    {"sel"},
	"press":    {"key"},
	"select":   {"sel"},
	"check":    {"sel"},
	"uncheck":  {"sel"},
	"focus":    {"sel"},
	"blur":     {"sel"},
	"submit":   {"sel"},
	"reset":    {"sel"},
	"waitFor":  {},
	"wait":     {"ms"},
	"dispatch": {"sel", "event"},
	"hash":     {"value"},
	"eval":     {"js"},
}

type rbSpec struct {
	Steps                 []map[string]any `json:"steps"`
	ExpectBuildDiagnostic string           `json:"expectBuildDiagnostic,omitempty"`
	Server                bool             `json:"server,omitempty"`
	CaptureHead           bool             `json:"captureHead,omitempty"`
	SettleMs              *int             `json:"settleMs,omitempty"`
	Notes                 string           `json:"notes,omitempty"`
}

type rbFixture struct {
	Name string // "<area>/<base>"
	Path string // absolute path of the .tsx file
	Spec rbSpec
}

type rbDivergence struct {
	Severity string `json:"severity"`
	Reason   string `json:"reason"`
}

func loadReactBrowserFixtures(t *testing.T) []rbFixture {
	t.Helper()
	entries, err := filepath.Glob(filepath.Join(reactBrowserDir, "*", "*.tsx"))
	if err != nil {
		t.Fatal(err)
	}
	var out []rbFixture
	for _, e := range entries {
		area := filepath.Base(filepath.Dir(e))
		if area == "lib" || area == "actions" {
			continue
		}
		abs, err := filepath.Abs(e)
		if err != nil {
			t.Fatal(err)
		}
		base := strings.TrimSuffix(filepath.Base(e), ".tsx")
		fx := rbFixture{Name: area + "/" + base, Path: abs}
		specPath := strings.TrimSuffix(abs, ".tsx") + ".steps.json"
		b, err := os.ReadFile(specPath)
		if err != nil {
			t.Errorf("%s: missing %s", fx.Name, filepath.Base(specPath))
			continue
		}
		dec := json.NewDecoder(bytes.NewReader(b))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&fx.Spec); err != nil {
			t.Errorf("%s: parse %s: %v", fx.Name, filepath.Base(specPath), err)
			continue
		}
		for i, st := range fx.Spec.Steps {
			op, _ := st["op"].(string)
			req, ok := reactBrowserOps[op]
			if !ok {
				t.Errorf("%s: step %d: unknown op %q", fx.Name, i, op)
				continue
			}
			for _, k := range req {
				if _, ok := st[k]; !ok {
					t.Errorf("%s: step %d (%s): missing %q", fx.Name, i, op, k)
				}
			}
		}
		if fx.Spec.ExpectBuildDiagnostic != "" && !diagPatternRe.MatchString(fx.Spec.ExpectBuildDiagnostic) {
			t.Errorf("%s: expectBuildDiagnostic %q must look like KR2001 or KR2xxx", fx.Name, fx.Spec.ExpectBuildDiagnostic)
		}
		out = append(out, fx)
	}
	for _, e := range mustGlob(t, filepath.Join(reactBrowserDir, "*", "*.steps.json")) {
		if _, err := os.Stat(strings.TrimSuffix(e, ".steps.json") + ".tsx"); err != nil {
			t.Errorf("%s has no fixture .tsx", e)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	if len(out) == 0 {
		t.Fatalf("no fixtures in %s", reactBrowserDir)
	}
	return out
}

func mustGlob(t *testing.T, pattern string) []string {
	t.Helper()
	m, err := filepath.Glob(pattern)
	if err != nil {
		t.Fatal(err)
	}
	return m
}

var diagPatternRe = regexp.MustCompile(`^KR[1-8][0-9x]{3}$`)

// diagMatcher turns "KR2xxx" into a regexp matching any code in the range.
func diagMatcher(pattern string) *regexp.Regexp {
	return regexp.MustCompile(strings.ReplaceAll(pattern, "x", "[0-9]"))
}

func rbDivergencePath() string { return filepath.Join(reactBrowserDir, "divergences.json") }

func loadReactBrowserDivergences(t *testing.T) map[string]rbDivergence {
	t.Helper()
	known := map[string]rbDivergence{}
	b, err := os.ReadFile(rbDivergencePath())
	if err != nil {
		if os.IsNotExist(err) {
			return known
		}
		t.Fatal(err)
	}
	if err := json.Unmarshal(b, &known); err != nil {
		t.Fatalf("parse %s: %v", rbDivergencePath(), err)
	}
	return known
}

func writeReactBrowserDivergences(t *testing.T, m map[string]rbDivergence) {
	t.Helper()
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(m); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(rbDivergencePath(), buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
}

func rbFilter(t *testing.T, fixtures []rbFixture) ([]rbFixture, bool) {
	pat := os.Getenv("KRATE_REACTBROWSER_RUN")
	if pat == "" {
		return fixtures, false
	}
	re, err := regexp.Compile(pat)
	if err != nil {
		t.Fatalf("KRATE_REACTBROWSER_RUN: %v", err)
	}
	var out []rbFixture
	for _, fx := range fixtures {
		if re.MatchString(fx.Name) {
			out = append(out, fx)
		}
	}
	return out, true
}

// captureOutput runs fn with os.Stdout and os.Stderr redirected and returns
// what was written. Build warnings are printed, not returned, so this is how
// the harness sees diagnostic codes. Tests in this package do not run in
// parallel, so swapping the globals is safe.
func captureOutput(fn func() error) (string, error) {
	r, w, err := os.Pipe()
	if err != nil {
		return "", fn()
	}
	oldOut, oldErr := os.Stdout, os.Stderr
	os.Stdout, os.Stderr = w, w
	done := make(chan string, 1)
	go func() {
		b, _ := io.ReadAll(r)
		done <- string(b)
	}()
	func() {
		defer func() {
			os.Stdout, os.Stderr = oldOut, oldErr
		}()
		err = fn()
	}()
	w.Close()
	out := <-done
	r.Close()
	return out, err
}

// buildKrateFixture builds fx as the index page of a fresh project under
// projDir, writing output to outDir. basePath is the URL prefix the output
// is served under ("" for root).
func buildKrateFixture(fx rbFixture, projDir, outDir, basePath string) (*config.Config, string, error) {
	pagesDir := filepath.Join(projDir, "src", "pages")
	if err := os.MkdirAll(pagesDir, 0o755); err != nil {
		return nil, "", err
	}
	src, err := os.ReadFile(fx.Path)
	if err != nil {
		return nil, "", err
	}
	if err := os.WriteFile(filepath.Join(pagesDir, "index.tsx"), src, 0o644); err != nil {
		return nil, "", err
	}
	for _, dir := range []string{"lib", "actions"} {
		if dir == "actions" && !fx.Spec.Server {
			continue
		}
		if err := copyDirFlat(filepath.Join(reactBrowserDir, dir), filepath.Join(projDir, "src", dir)); err != nil {
			return nil, "", err
		}
	}
	cfg := config.Default()
	cfg.PagesDir = pagesDir
	cfg.OutDir = outDir
	cfg.Minify = false
	cfg.BasePath = basePath
	out, err := captureOutput(func() error { return New(projDir, cfg).BuildAll() })
	return cfg, out, err
}

func copyDirFlat(src, dst string) error {
	entries, err := os.ReadDir(src)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if err := os.MkdirAll(dst, 0o755); err != nil {
		return err
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		b, err := os.ReadFile(filepath.Join(src, e.Name()))
		if err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(dst, e.Name()), b, 0o644); err != nil {
			return err
		}
	}
	return nil
}

// checkBuildDiagnostic builds a fixture that declares expectBuildDiagnostic.
// It returns "" when the build failed or warned with a matching code, or a
// description of the divergence.
func checkBuildDiagnostic(t *testing.T, fx rbFixture, work string) string {
	t.Helper()
	key := strings.ReplaceAll(fx.Name, "/", "__")
	_, out, err := buildKrateFixture(fx, filepath.Join(work, "diag", key), filepath.Join(work, "diag", key, "dist"), "")
	text := out
	if err != nil {
		text += "\n" + err.Error()
	}
	if diagMatcher(fx.Spec.ExpectBuildDiagnostic).MatchString(text) {
		return ""
	}
	status := "build succeeded"
	if err != nil {
		status = "build failed without the code: " + oneLine(err.Error())
	}
	return fmt.Sprintf("  expected a %s diagnostic; %s\n%s", fx.Spec.ExpectBuildDiagnostic, status, indentText(strings.TrimSpace(stripANSI(out)), "    | "))
}

// buildWarnings extracts warning blocks (and their indented detail lines)
// from captured build output.
func buildWarnings(out string) string {
	var keep []string
	inWarning := false
	for _, line := range strings.Split(stripANSI(out), "\n") {
		trimmed := strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(trimmed, "Warning") || strings.Contains(trimmed, "⚠"):
			inWarning = true
			keep = append(keep, trimmed)
		case inWarning && strings.HasPrefix(line, "    ") && trimmed != "":
			keep = append(keep, trimmed)
		default:
			inWarning = false
		}
	}
	return strings.Join(keep, "\n")
}

var ansiRe = regexp.MustCompile("\x1b\\[[0-9;]*m")

func stripANSI(s string) string { return ansiRe.ReplaceAllString(s, "") }

func oneLine(s string) string {
	s = strings.Join(strings.Fields(stripANSI(s)), " ")
	if len(s) > 300 {
		s = s[:300] + "..."
	}
	return s
}

func indentText(s, prefix string) string {
	if s == "" {
		return ""
	}
	lines := strings.Split(s, "\n")
	if len(lines) > 30 {
		lines = append(lines[:30], "...")
	}
	return prefix + strings.Join(lines, "\n"+prefix) + "\n"
}

// bundleReactFixture writes <outDir>/index.html and app.js rendering fx with
// React 19 createRoot (production build).
func bundleReactFixture(oracle string, fx rbFixture, outDir string) error {
	entry := fmt.Sprintf(`import App from %s;
import { createElement } from "react";
import { createRoot } from "react-dom/client";
createRoot(document.getElementById("root")).render(createElement(App));
`, strconv.Quote(filepath.ToSlash(fx.Path)))
	reactPkg := regexp.MustCompile(`^(react|react-dom|scheduler)(/.*)?$`)
	res := api.Build(api.BuildOptions{
		Stdin: &api.StdinOptions{
			Contents:   entry,
			ResolveDir: filepath.Dir(fx.Path),
			Loader:     api.LoaderTSX,
			Sourcefile: "entry.tsx",
		},
		Bundle:          true,
		Write:           false,
		Format:          api.FormatIIFE,
		Platform:        api.PlatformBrowser,
		Target:          api.ES2020,
		JSX:             api.JSXAutomatic,
		JSXImportSource: "react",
		Define:          map[string]string{"process.env.NODE_ENV": `"production"`},
		LogLevel:        api.LogLevelSilent,
		Outfile:         filepath.Join(outDir, "app.js"),
		Plugins: []api.Plugin{{
			Name: "react-oracle",
			Setup: func(pb api.PluginBuild) {
				pb.OnResolve(api.OnResolveOptions{Filter: reactPkg.String()}, func(args api.OnResolveArgs) (api.OnResolveResult, error) {
					// Packages resolve their own dependencies (pnpm layout).
					if args.PluginData == "oracle" || strings.Contains(filepath.ToSlash(args.Importer), "/node_modules/") {
						return api.OnResolveResult{}, nil
					}
					r := pb.Resolve(args.Path, api.ResolveOptions{ResolveDir: oracle, Kind: args.Kind, PluginData: "oracle"})
					if len(r.Errors) > 0 {
						return api.OnResolveResult{}, fmt.Errorf("resolve %s from react-oracle: %s", args.Path, r.Errors[0].Text)
					}
					return api.OnResolveResult{Path: r.Path}, nil
				})
			},
		}},
	})
	if len(res.Errors) > 0 {
		e := res.Errors[0]
		loc := ""
		if e.Location != nil {
			loc = fmt.Sprintf("%s:%d: ", e.Location.File, e.Location.Line)
		}
		return fmt.Errorf("esbuild: %s%s", loc, e.Text)
	}
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return err
	}
	for _, f := range res.OutputFiles {
		if err := os.WriteFile(f.Path, f.Contents, 0o644); err != nil {
			return err
		}
	}
	page := `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<title>Krate App</title>
</head>
<body>
<div id="root"></div>
<script src="./app.js"></script>
</body>
</html>
`
	return os.WriteFile(filepath.Join(outDir, "index.html"), []byte(page), 0o644)
}

func reactOracleReady(t *testing.T) string {
	t.Helper()
	oracle := reactOracleDir(t)
	if _, err := os.Stat(filepath.Join(oracle, "node_modules", "react-dom", "package.json")); err != nil {
		requireE2E(t, "react-dom not installed in %s (run pnpm install): %v", oracle, err)
		return ""
	}
	return oracle
}

// TestReactBrowserCorpus validates the corpus without a browser: specs parse,
// every fixture bundles with React, divergences.json is well formed and
// classified, and expectBuildDiagnostic fixtures get their diagnostic (or are
// listed). With KRATE_REACT_EXIT=1 it fails while any P0/P1 entry remains.
func TestReactBrowserCorpus(t *testing.T) {
	fixtures := loadReactBrowserFixtures(t)
	known := loadReactBrowserDivergences(t)
	byName := map[string]rbFixture{}
	for _, fx := range fixtures {
		byName[fx.Name] = fx
	}
	var blocking []string
	for name, d := range known {
		if _, ok := byName[name]; !ok {
			t.Errorf("divergences.json lists unknown fixture %q", name)
		}
		switch d.Severity {
		case "P0", "P1", "P2":
		default:
			t.Errorf("divergences.json: %s has severity %q (want P0, P1 or P2)", name, d.Severity)
		}
		if strings.TrimSpace(d.Reason) == "" || strings.HasPrefix(d.Reason, "TODO") {
			t.Errorf("divergences.json: %s is not classified (reason %q)", name, d.Reason)
		}
		if d.Severity == "P0" || d.Severity == "P1" {
			blocking = append(blocking, fmt.Sprintf("%s [%s] %s", name, d.Severity, d.Reason))
		}
	}
	sort.Strings(blocking)
	if os.Getenv("KRATE_REACT_EXIT") != "" && len(blocking) > 0 {
		t.Errorf("R2/R3 exit criteria not met: %d P0/P1 divergences remain:\n  %s", len(blocking), strings.Join(blocking, "\n  "))
	}

	if oracle := reactOracleReady(t); oracle != "" {
		work := t.TempDir()
		for _, fx := range fixtures {
			if fx.Spec.ExpectBuildDiagnostic != "" {
				continue
			}
			if err := bundleReactFixture(oracle, fx, filepath.Join(work, strings.ReplaceAll(fx.Name, "/", "__"))); err != nil {
				t.Errorf("%s: React bundle failed (fixture bug): %v", fx.Name, err)
			}
		}
	}

	work := t.TempDir()
	for _, fx := range fixtures {
		if fx.Spec.ExpectBuildDiagnostic == "" {
			continue
		}
		diff := checkBuildDiagnostic(t, fx, work)
		_, listed := known[fx.Name]
		switch {
		case diff != "" && !listed:
			t.Errorf("%s: diagnostic expectation not met and not in divergences.json:\n%s", fx.Name, diff)
		case diff == "" && listed:
			t.Logf("FIXED: %s now reports %s; remove it from divergences.json", fx.Name, fx.Spec.ExpectBuildDiagnostic)
		}
	}
	t.Logf("react browser corpus: %d fixtures, %d listed divergences (%d P0/P1)", len(fixtures), len(known), len(blocking))
}

// rbSnap mirrors one snapshot recorded by run.mjs.
type rbSnap struct {
	Step      string   `json:"step"`
	StepError string   `json:"stepError"`
	DOM       []string `json:"dom"`
	Extra     []string `json:"extra"`
	Forms     []string `json:"forms"`
	Focus     string   `json:"focus"`
	Head      []string `json:"head"`
	Log       []string `json:"log"`
	Errors    []string `json:"errors"`
}

type rbSide struct {
	Snaps []rbSnap `json:"snaps"`
	Fatal string   `json:"fatal"`
}

type rbResult struct {
	React rbSide `json:"react"`
	Krate rbSide `json:"krate"`
}

type rbChannel struct {
	name  string
	get   func(s rbSnap) []string
	sev   string
	label string
}

var rbChannels = []rbChannel{
	{"errors", func(s rbSnap) []string { return s.Errors }, "P0", "uncaught errors"},
	{"dom", func(s rbSnap) []string { return s.DOM }, "P0", "DOM of #root"},
	{"extra", func(s rbSnap) []string { return s.Extra }, "P0", "body-level (portal) DOM"},
	{"forms", func(s rbSnap) []string { return s.Forms }, "P0", "form control state"},
	{"log", func(s rbSnap) []string { return s.Log }, "P1", "event log"},
	{"focus", func(s rbSnap) []string { return []string{s.Focus} }, "P1", "focused element"},
	{"stepError", func(s rbSnap) []string { return rbOptional(s.StepError) }, "P1", "step outcome"},
	{"head", func(s rbSnap) []string { return s.Head }, "P1", "document head"},
}

func rbOptional(s string) []string {
	if s == "" {
		return nil
	}
	return []string{s}
}

// compareSides returns a readable diff and the most severe channel that
// differs ("" when the sides match).
func compareSides(r rbResult) (string, string) {
	var sb strings.Builder
	worst := ""
	bump := func(sev string) {
		if worst == "" || sev < worst {
			worst = sev
		}
	}
	if r.Krate.Fatal != "" {
		bump("P0")
		fmt.Fprintf(&sb, "  krate page failed: %s\n", r.Krate.Fatal)
	}
	n := len(r.React.Snaps)
	if len(r.Krate.Snaps) < n {
		n = len(r.Krate.Snaps)
	}
	var later []string
	shown := 0
	for i := 0; i < n; i++ {
		rs, ks := r.React.Snaps[i], r.Krate.Snaps[i]
		var parts []string
		for _, ch := range rbChannels {
			a, b := ch.get(rs), ch.get(ks)
			if strings.Join(a, "\n") == strings.Join(b, "\n") {
				continue
			}
			bump(ch.sev)
			parts = append(parts, fmt.Sprintf("    %s:\n%s", ch.label, indentText(strings.TrimRight(lineDiff(a, b), "\n"), "    ")))
		}
		if len(parts) == 0 {
			continue
		}
		if shown < 2 {
			fmt.Fprintf(&sb, "  after step %d (%s):\n%s", i, rs.Step, strings.Join(parts, ""))
			shown++
		} else {
			later = append(later, fmt.Sprintf("%d (%s)", i, rs.Step))
		}
	}
	if len(later) > 0 {
		fmt.Fprintf(&sb, "  also differs after steps: %s\n", strings.Join(later, ", "))
	}
	if len(r.Krate.Snaps) != len(r.React.Snaps) && r.Krate.Fatal == "" {
		bump("P0")
		fmt.Fprintf(&sb, "  snapshot count: react %d, krate %d\n", len(r.React.Snaps), len(r.Krate.Snaps))
	}
	return sb.String(), worst
}

// TestReactBrowserDifferential runs the corpus in a real browser against
// React 19. Requires KRATE_BROWSER (like TestBrowserConformance).
func TestReactBrowserDifferential(t *testing.T) {
	browser := strings.ToLower(strings.TrimSpace(os.Getenv("KRATE_BROWSER")))
	if browser == "" {
		t.Skip("set KRATE_BROWSER=chromium|firefox|webkit to run the React browser differential")
	}
	switch browser {
	case "chromium", "firefox", "webkit":
	default:
		t.Fatalf("unsupported KRATE_BROWSER %q", browser)
	}
	if _, err := exec.LookPath("node"); err != nil {
		requireE2E(t, "node is not installed")
		return
	}
	if err := exec.Command("node", "-e", "require.resolve('playwright')").Run(); err != nil {
		requireE2E(t, "playwright is not installed (run pnpm install)")
		return
	}
	oracle := reactOracleReady(t)
	if oracle == "" {
		return
	}
	all := loadReactBrowserFixtures(t)
	fixtures, filtered := rbFilter(t, all)
	known := loadReactBrowserDivergences(t)
	update := os.Getenv("KRATE_REACTBROWSER_UPDATE") != ""

	work := t.TempDir()
	if keep := os.Getenv("KRATE_REACTBROWSER_KEEP"); keep != "" {
		// Keep built pages (www/r, www/k) for inspection.
		work = keep
		_ = os.RemoveAll(work)
		if err := os.MkdirAll(work, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	provideRuntime(t, work)
	www := filepath.Join(work, "www")
	diverged := map[string]string{} // fixture -> diff
	guess := map[string]string{}    // fixture -> severity guess
	skipped := map[string]bool{}
	warnings := map[string]string{} // fixture -> build warnings

	type planFixture struct {
		Name        string           `json:"name"`
		React       string           `json:"react"`
		Krate       string           `json:"krate"`
		Steps       []map[string]any `json:"steps"`
		CaptureHead bool             `json:"captureHead"`
		SettleMs    *int             `json:"settleMs,omitempty"`
	}
	plan := []planFixture{}
	for _, fx := range fixtures {
		if fx.Spec.ExpectBuildDiagnostic != "" {
			if diff := checkBuildDiagnostic(t, fx, work); diff != "" {
				diverged[fx.Name] = diff
				guess[fx.Name] = "P1"
			}
			continue
		}
		key := strings.ReplaceAll(fx.Name, "/", "__")
		if err := bundleReactFixture(oracle, fx, filepath.Join(www, "r", fx.Name)); err != nil {
			t.Errorf("%s: React bundle failed (fixture bug): %v", fx.Name, err)
			continue
		}
		pf := planFixture{
			Name:        fx.Name,
			React:       "/r/" + fx.Name + "/",
			Krate:       "/k/" + fx.Name + "/",
			Steps:       fx.Spec.Steps,
			CaptureHead: fx.Spec.CaptureHead,
			SettleMs:    fx.Spec.SettleMs,
		}
		if pf.Steps == nil {
			pf.Steps = []map[string]any{}
		}
		proj := filepath.Join(work, "proj", key)
		if fx.Spec.Server {
			if !canStopInProcessServer {
				t.Logf("%s: skipped (server fixtures need a unix host)", fx.Name)
				skipped[fx.Name] = true
				continue
			}
			cfg, out, err := buildKrateFixture(fx, proj, filepath.Join(proj, "dist"), "")
			if err != nil {
				diverged[fx.Name] = "  krate build failed: " + oneLine(err.Error()) + "\n" + indentText(strings.TrimSpace(stripANSI(out)), "    | ")
				guess[fx.Name] = "P0"
				continue
			}
			base, err := startKrateServer(t, proj, cfg)
			if err != nil {
				t.Errorf("%s: krate serve: %v", fx.Name, err)
				continue
			}
			pf.Krate = base + "/"
			warnings[fx.Name] = buildWarnings(out)
		} else {
			_, out, err := buildKrateFixture(fx, proj, filepath.Join(www, "k", fx.Name), "/k/"+fx.Name)
			if err != nil {
				diverged[fx.Name] = "  krate build failed: " + oneLine(err.Error()) + "\n" + indentText(strings.TrimSpace(stripANSI(out)), "    | ")
				guess[fx.Name] = "P0"
				continue
			}
			warnings[fx.Name] = buildWarnings(out)
		}
		plan = append(plan, pf)
	}

	jobs := 6
	if browser != "chromium" {
		jobs = 3
	}
	if v, err := strconv.Atoi(os.Getenv("KRATE_REACTBROWSER_JOBS")); err == nil && v > 0 {
		jobs = v
	}
	planPath := filepath.Join(work, "plan.json")
	outPath := filepath.Join(work, "out.json")
	pb, _ := json.Marshal(map[string]any{"browser": browser, "root": www, "concurrency": jobs, "fixtures": plan})
	if err := os.WriteFile(planPath, pb, 0o644); err != nil {
		t.Fatal(err)
	}
	script, err := filepath.Abs(filepath.Join(reactBrowserDir, "run.mjs"))
	if err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	driver := []string{"node"}
	if d := strings.Fields(os.Getenv("KRATE_REACTBROWSER_DRIVER")); len(d) > 0 {
		driver = d
	}
	cmd := exec.Command(driver[0], append(driver[1:], script, planPath, outPath)...)
	runOut, err := cmd.CombinedOutput()
	if err != nil {
		if strings.Contains(string(runOut), "Executable doesn't exist") || strings.Contains(string(runOut), "browserType.launch") {
			requireE2E(t, "%s browser is not installed: %s", browser, runOut)
			return
		}
		t.Fatalf("run.mjs failed: %v\n%s", err, runOut)
	}
	t.Logf("browser run (%s): %d fixtures in %s", browser, len(plan), time.Since(start).Round(time.Millisecond))
	var results map[string]rbResult
	rb, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(rb, &results); err != nil {
		t.Fatalf("decode run.mjs output: %v", err)
	}
	for _, pf := range plan {
		r, ok := results[pf.Name]
		if !ok {
			t.Errorf("%s: no result from run.mjs", pf.Name)
			continue
		}
		if r.React.Fatal != "" {
			t.Errorf("%s: React side failed (fixture bug): %s", pf.Name, r.React.Fatal)
			continue
		}
		for i, s := range r.React.Snaps {
			if s.StepError != "" {
				t.Errorf("%s: React side step %d (%s) failed (fixture bug): %s", pf.Name, i, s.Step, s.StepError)
			}
		}
		if diff, sev := compareSides(r); sev != "" {
			if w := warnings[pf.Name]; w != "" {
				diff += "  krate build warnings:\n" + indentText(w, "    | ")
			}
			diverged[pf.Name] = diff
			guess[pf.Name] = sev
		}
	}

	if path := os.Getenv("KRATE_REACTBROWSER_REPORT"); path != "" {
		var sb strings.Builder
		var names []string
		for n := range diverged {
			names = append(names, n)
		}
		sort.Strings(names)
		for _, n := range names {
			fmt.Fprintf(&sb, "=== %s (guess %s, listed %q)\n%s\n", n, guess[n], known[n].Severity, diverged[n])
		}
		if err := os.WriteFile(path, []byte(sb.String()), 0o644); err != nil {
			t.Errorf("write report: %v", err)
		}
	}

	if update {
		next := map[string]rbDivergence{}
		for name, d := range known {
			next[name] = d
		}
		for _, fx := range fixtures {
			if skipped[fx.Name] {
				continue
			}
			if _, bad := diverged[fx.Name]; !bad {
				delete(next, fx.Name)
				continue
			}
			if _, ok := next[fx.Name]; !ok {
				next[fx.Name] = rbDivergence{Severity: guess[fx.Name], Reason: "TODO: classify"}
			}
		}
		writeReactBrowserDivergences(t, next)
		t.Logf("updated %s: %d divergences", rbDivergencePath(), len(next))
		return
	}

	counts := map[string]int{}
	for _, fx := range fixtures {
		if skipped[fx.Name] {
			continue
		}
		diff, bad := diverged[fx.Name]
		d, listed := known[fx.Name]
		switch {
		case bad && !listed:
			t.Errorf("%s diverges from React (guess %s) and is not in divergences.json:\n%s", fx.Name, guess[fx.Name], diff)
		case !bad && listed:
			t.Logf("FIXED: %s now matches React; remove it from divergences.json (or run with KRATE_REACTBROWSER_UPDATE=1)", fx.Name)
		case bad:
			counts[d.Severity]++
		default:
			counts["match"]++
		}
	}
	if !filtered {
		for name := range known {
			found := false
			for _, fx := range all {
				found = found || fx.Name == name
			}
			if !found {
				t.Errorf("divergences.json lists unknown fixture %q", name)
			}
		}
	}
	t.Logf("react browser differential (%s): %d fixtures, %d match, P0 %d, P1 %d, P2 %d",
		browser, len(fixtures)-len(skipped), counts["match"], counts["P0"], counts["P1"], counts["P2"])
}

// provideRuntime makes the built @krate/runtime bundles visible to fixture
// projects under work. The build probes <ancestor>/packages/runtime/dist from
// the project root, so the bundles are copied to work/packages/runtime/dist.
func provideRuntime(t *testing.T, work string) {
	t.Helper()
	repo, err := repoRootPath()
	if err != nil {
		t.Fatal(err)
	}
	src := filepath.Join(repo, "packages", "runtime", "dist")
	names := []string{"krate-runtime.js", "krate-hydrate.js", "krate-hydrate-lite.js", "krate-dev.js"}
	dst := filepath.Join(work, "packages", "runtime", "dist")
	if err := os.MkdirAll(dst, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, n := range names {
		b, err := os.ReadFile(filepath.Join(src, n))
		if err != nil {
			requireE2E(t, "built runtime missing (run pnpm -r build): %v", err)
			return
		}
		if err := os.WriteFile(filepath.Join(dst, n), b, 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// startKrateServer serves a built project in-process with the real Krate
// server (static pages, server actions through the Node sidecar) and returns
// its base URL. The server is stopped when the test binary signals itself at
// cleanup; see stopInProcessServers.
func startKrateServer(t *testing.T, root string, cfg *config.Config) (string, error) {
	t.Helper()
	port := freePort(t)
	cfg.Server.Port = port
	errCh := make(chan error, 1)
	go func() {
		errCh <- Serve(root, cfg, time.Now())
	}()
	base := fmt.Sprintf("http://127.0.0.1:%d", port)
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case err := <-errCh:
			return "", fmt.Errorf("server exited: %v", err)
		default:
		}
		resp, err := http.Get(base + "/")
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode < 500 {
				// Serve installs its signal handler before it answers HTTP, so
				// signaling at cleanup cannot kill the test binary.
				registerServerStop(t, errCh)
				break
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	if hasActionManifest(root) {
		// The action sidecar listens on port+1.
		for time.Now().Before(deadline) {
			c, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port+1), 200*time.Millisecond)
			if err == nil {
				c.Close()
				return base, nil
			}
			time.Sleep(100 * time.Millisecond)
		}
		return "", fmt.Errorf("action sidecar on port %d did not start", port+1)
	}
	return base, nil
}

// Diagnostic registry sanity: every code an expectation names exactly must be
// registered, so a typo does not silently pass as "missing diagnostic".
func TestReactBrowserDiagnosticCodesRegistered(t *testing.T) {
	for _, fx := range loadReactBrowserFixtures(t) {
		code := fx.Spec.ExpectBuildDiagnostic
		if code == "" || strings.Contains(code, "x") {
			continue
		}
		if _, ok := diag.Lookup(diag.Code(code)); !ok {
			t.Errorf("%s: expectBuildDiagnostic %s is not a registered code", fx.Name, code)
		}
	}
}
