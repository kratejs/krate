package build

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"

	esbuild "github.com/evanw/esbuild/pkg/api"

	"github.com/kratejs/krate/packages/compiler/internal/config"
)

// Environment variables `krate native test` sets for the test processes; the
// driver (@krate/app/driver) reads them in launch().
const (
	TestShellEnv = "KRATE_TEST_SHELL"
	TestAppEnv   = "KRATE_TEST_APP"
)

// nativeTestSuffixes are the file names `krate native test` runs by default.
var nativeTestSuffixes = []string{".native.test.ts", ".native.test.mts", ".native.test.js", ".native.test.mjs"}

// NativeTestOptions configures RunNativeTest.
type NativeTestOptions struct {
	// LoadConfig loads the project, as the dev command does.
	LoadConfig func() (root string, cfg *config.Config, env map[string]string, err error)
	// Patterns select test files; see FindNativeTests.
	Patterns []string
	// Shell is the --shell override.
	Shell string
	// Grep runs only the tests whose name matches (a regular expression).
	Grep string
	// Workers is how many test files run at once (default 1: each test
	// opens real windows).
	Workers int
	// List prints the test files and stops.
	List bool
	// Node is the node executable (default: node on PATH).
	Node string
	// Target is the Go host platform, as for native dev.
	Target  string
	Verbose bool
	Stdout  io.Writer
	Stderr  io.Writer
	Ctx     context.Context
}

// RunNativeTest builds the app the way native dev does, bundles the test
// files and runs them with Node's test runner. Each launch() in a test starts
// its own shell on the built app. It returns the exit code of the runner (0
// when every test passed) or an error when the run could not start.
func RunNativeTest(o NativeTestOptions) (int, error) {
	if o.Stdout == nil {
		o.Stdout = os.Stdout
	}
	if o.Stderr == nil {
		o.Stderr = os.Stderr
	}
	if o.Ctx == nil {
		o.Ctx = context.Background()
	}
	root, cfg, env, err := o.LoadConfig()
	if err != nil {
		return 1, err
	}
	files, err := FindNativeTests(root, o.Patterns)
	if err != nil {
		return 1, err
	}
	if o.List {
		for _, f := range files {
			rel, _ := filepath.Rel(root, f)
			fmt.Fprintln(o.Stdout, filepath.ToSlash(rel))
		}
		return 0, nil
	}
	if err := checkTestDisplay(runtime.GOOS, os.Getenv); err != nil {
		return 1, err
	}
	node, major, minor, err := locateNode(o.Node)
	if err != nil {
		return 1, err
	}
	if major < 20 || (major == 20 && minor < 10) {
		return 1, fmt.Errorf("krate native test needs Node.js 20.10 or newer for its test runner; %s is v%d.%d. Install a newer Node.js", node, major, minor)
	}
	if o.Grep != "" {
		if _, err := regexp.Compile(o.Grep); err != nil {
			return 1, fmt.Errorf("--grep %q is not a regular expression: %v", o.Grep, err)
		}
	}
	if _, err := os.Stat(filepath.Join(root, "node_modules", "@krate", "app", "dist", "driver.js")); err != nil {
		return 1, fmt.Errorf("@krate/app has no test driver in this project (node_modules/@krate/app/dist/driver.js is missing); install a @krate/app version that ships @krate/app/driver")
	}

	b := New(root, cfg)
	b.Verbose = o.Verbose
	b.Env = env
	b.DevMode = true
	b.SetContext(o.Ctx)
	fmt.Fprintf(o.Stdout, "  Building native app for test...\n")
	res, err := b.BuildNativeApp(NativeAppOptions{OutDir: filepath.Join(root, ".krate", "native", "test"), Target: o.Target, Dev: true})
	if err != nil {
		return 1, fmt.Errorf("the app did not build: %s", BuildErrorSummary(err))
	}
	for _, n := range res.Notes {
		fmt.Fprintf(o.Stdout, "  ! %s\n", n)
	}
	goos, goarch, _ := parseNativeTarget(o.Target)
	shellPath := ""
	if names := b.NativePluginNames(); len(names) > 0 {
		if o.Shell != "" || os.Getenv(ShellEnv) != "" {
			return 1, errors.New("the app uses native plugins, which are linked into a shell built for it; drop --shell and $" + ShellEnv)
		}
		if shellPath, err = b.PluginShell(goos, goarch); err != nil {
			return 1, err
		}
	} else if shellPath, err = LocateNativeShell(root, o.Shell); err != nil {
		return 1, err
	}

	outDir := filepath.Join(root, ".krate", "native-test", "run")
	bundled, err := bundleNativeTests(root, outDir, files)
	if err != nil {
		return 1, err
	}

	workers := o.Workers
	if workers < 1 {
		workers = 1
	}
	args := []string{"--test", "--enable-source-maps", "--test-reporter=spec", "--test-concurrency=" + strconv.Itoa(workers)}
	if o.Grep != "" {
		args = append(args, "--test-name-pattern="+o.Grep)
	}
	args = append(args, bundled...)
	fmt.Fprintf(o.Stdout, "  Running %d test file(s)...\n\n", len(files))
	cmd := exec.CommandContext(o.Ctx, node, args...)
	cmd.Dir = root
	cmd.Stdout, cmd.Stderr = o.Stdout, o.Stderr
	cmd.Env = append(os.Environ(), TestShellEnv+"="+shellPath, TestAppEnv+"="+res.AppDir, "KRATE_TEST=1")
	if err := cmd.Run(); err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			return ee.ExitCode(), nil
		}
		return 1, fmt.Errorf("running node: %v", err)
	}
	return 0, nil
}

// checkTestDisplay reports a Linux session with no display, where the shell
// cannot open a window, before the app is built.
func checkTestDisplay(goos string, getenv func(string) string) error {
	if goos != "linux" || getenv("DISPLAY") != "" || getenv("WAYLAND_DISPLAY") != "" {
		return nil
	}
	return errors.New("no display: the tests open real windows and this session has neither $DISPLAY nor $WAYLAND_DISPLAY. On a headless machine run: xvfb-run -a krate native test")
}

// locateNode finds node and its version.
func locateNode(override string) (path string, major, minor int, err error) {
	path = override
	if path == "" {
		if path, err = exec.LookPath("node"); err != nil {
			return "", 0, 0, errors.New("krate native test runs tests with Node.js (20.10 or newer), which is not on PATH. Install Node.js")
		}
	}
	out, err := exec.Command(path, "--version").Output()
	if err != nil {
		return "", 0, 0, fmt.Errorf("%s --version: %v", path, err)
	}
	major, minor, ok := parseNodeVersion(string(out))
	if !ok {
		return "", 0, 0, fmt.Errorf("cannot read the Node.js version from %q", strings.TrimSpace(string(out)))
	}
	return path, major, minor, nil
}

func parseNodeVersion(s string) (major, minor int, ok bool) {
	s = strings.TrimPrefix(strings.TrimSpace(s), "v")
	parts := strings.SplitN(s, ".", 3)
	if len(parts) < 2 {
		return 0, 0, false
	}
	major, e1 := strconv.Atoi(parts[0])
	minor, e2 := strconv.Atoi(parts[1])
	return major, minor, e1 == nil && e2 == nil
}

// bundleNativeTests compiles the test files with esbuild: TypeScript is
// stripped and relative imports are bundled, while packages (node:test,
// @krate/app/driver, anything in node_modules) stay imports that Node
// resolves. The outputs keep the sources' layout under outDir.
func bundleNativeTests(root, outDir string, files []string) ([]string, error) {
	if err := os.RemoveAll(outDir); err != nil {
		return nil, err
	}
	res := esbuild.Build(esbuild.BuildOptions{
		EntryPoints:   files,
		Outdir:        outDir,
		Outbase:       root,
		AbsWorkingDir: root,
		Bundle:        true,
		Write:         true,
		Platform:      esbuild.PlatformNode,
		Format:        esbuild.FormatESModule,
		Target:        esbuild.ES2022,
		Packages:      esbuild.PackagesExternal,
		Sourcemap:     esbuild.SourceMapInline,
		OutExtension:  map[string]string{".js": ".mjs"},
		LogLevel:      esbuild.LogLevelSilent,
	})
	if len(res.Errors) > 0 {
		var sb strings.Builder
		sb.WriteString("the test files did not compile:")
		for _, m := range res.Errors {
			sb.WriteString("\n  ")
			if m.Location != nil {
				fmt.Fprintf(&sb, "%s:%d:%d: ", m.Location.File, m.Location.Line, m.Location.Column+1)
			}
			sb.WriteString(m.Text)
		}
		return nil, errors.New(sb.String())
	}
	var out []string
	for _, f := range files {
		rel, err := filepath.Rel(root, f)
		if err != nil {
			return nil, err
		}
		rel = strings.TrimSuffix(rel, filepath.Ext(rel)) + ".mjs"
		out = append(out, filepath.Join(outDir, rel))
	}
	return out, nil
}

// FindNativeTests returns the absolute paths of the test files to run, sorted.
// With no patterns it is every *.native.test.{ts,mts,js,mjs} file under root
// (skipping node_modules, dist output and dot directories). A pattern is, in
// this order: an existing file; an existing directory (the test files in
// it); a glob with * ? and ** matched against the path relative to root (a
// glob without a slash matches file names anywhere); or any other text, which
// keeps the discovered files whose relative path contains it.
func FindNativeTests(root string, patterns []string) ([]string, error) {
	var all []string
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			name := d.Name()
			if p != root && (strings.HasPrefix(name, ".") || name == "node_modules" || name == "dist" || name == "dist-native") {
				return filepath.SkipDir
			}
			return nil
		}
		if isNativeTestFile(d.Name()) {
			all = append(all, p)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	rel := func(p string) string {
		r, _ := filepath.Rel(root, p)
		return filepath.ToSlash(r)
	}
	chosen := map[string]bool{}
	if len(patterns) == 0 {
		for _, p := range all {
			chosen[p] = true
		}
	}
	for _, pat := range patterns {
		matched := false
		abs := pat
		if !filepath.IsAbs(abs) {
			abs = filepath.Join(root, pat)
		}
		if st, err := os.Stat(abs); err == nil {
			if st.IsDir() {
				for _, p := range all {
					if strings.HasPrefix(p, filepath.Clean(abs)+string(filepath.Separator)) {
						chosen[p], matched = true, true
					}
				}
			} else {
				chosen[abs], matched = true, true
			}
		} else if strings.ContainsAny(pat, "*?") {
			re, err := globRegexp(filepath.ToSlash(pat))
			if err != nil {
				return nil, fmt.Errorf("test pattern %q: %v", pat, err)
			}
			for _, p := range all {
				target := rel(p)
				if !strings.Contains(pat, "/") {
					target = filepath.Base(p)
				}
				if re.MatchString(target) {
					chosen[p], matched = true, true
				}
			}
		} else {
			for _, p := range all {
				if strings.Contains(rel(p), filepath.ToSlash(pat)) {
					chosen[p], matched = true, true
				}
			}
		}
		if !matched {
			return nil, fmt.Errorf("no test file matches %q. Test files are named *.native.test.ts (or .mts, .js, .mjs); run `krate native test --list` to see what is found", pat)
		}
	}
	if len(chosen) == 0 {
		return nil, errors.New("no test files found. Create one named *.native.test.ts, for example tests/app.native.test.ts (see https://krate.js.org/docs/native/testing/)")
	}
	out := make([]string, 0, len(chosen))
	for p := range chosen {
		out = append(out, p)
	}
	sort.Strings(out)
	return out, nil
}

func isNativeTestFile(name string) bool {
	for _, s := range nativeTestSuffixes {
		if strings.HasSuffix(name, s) {
			return true
		}
	}
	return false
}

// globRegexp turns a glob with *, ? and ** into an anchored regular
// expression. * and ? stay inside one path segment; ** crosses segments, and
// "**/" also matches no directory at all.
func globRegexp(glob string) (*regexp.Regexp, error) {
	var sb strings.Builder
	sb.WriteString("^")
	for i := 0; i < len(glob); i++ {
		switch c := glob[i]; c {
		case '*':
			if i+1 < len(glob) && glob[i+1] == '*' {
				i++
				if i+1 < len(glob) && glob[i+1] == '/' {
					i++
					sb.WriteString("(?:.*/)?")
				} else {
					sb.WriteString(".*")
				}
			} else {
				sb.WriteString("[^/]*")
			}
		case '?':
			sb.WriteString("[^/]")
		default:
			sb.WriteString(regexp.QuoteMeta(string(c)))
		}
	}
	sb.WriteString("$")
	return regexp.Compile(sb.String())
}
