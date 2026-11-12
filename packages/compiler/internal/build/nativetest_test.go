package build

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/config"
)

func writeTestTree(t *testing.T, files ...string) string {
	t.Helper()
	root := t.TempDir()
	for _, f := range files {
		p := filepath.Join(root, filepath.FromSlash(f))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("// test\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func relList(t *testing.T, root string, abs []string) []string {
	t.Helper()
	var out []string
	for _, p := range abs {
		r, err := filepath.Rel(root, p)
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, filepath.ToSlash(r))
	}
	return out
}

func TestFindNativeTests(t *testing.T) {
	root := writeTestTree(t,
		"tests/a.native.test.ts",
		"tests/sub/b.native.test.mjs",
		"c.native.test.js",
		"src/lib.test.ts", // not a native test
		"tests/plain.ts",
		"node_modules/pkg/x.native.test.ts",
		"dist/y.native.test.ts",
		".krate/z.native.test.ts",
	)
	cases := []struct {
		name     string
		patterns []string
		want     []string
	}{
		{"default", nil, []string{"c.native.test.js", "tests/a.native.test.ts", "tests/sub/b.native.test.mjs"}},
		{"file", []string{"tests/a.native.test.ts"}, []string{"tests/a.native.test.ts"}},
		{"directory", []string{"tests/sub"}, []string{"tests/sub/b.native.test.mjs"}},
		{"glob with a slash", []string{"tests/**/*.native.test.*"}, []string{"tests/a.native.test.ts", "tests/sub/b.native.test.mjs"}},
		{"glob with no slash matches the name anywhere", []string{"*.native.test.mjs"}, []string{"tests/sub/b.native.test.mjs"}},
		{"fragment", []string{"sub/b"}, []string{"tests/sub/b.native.test.mjs"}},
		{"two patterns", []string{"c.native", "tests/a"}, []string{"c.native.test.js", "tests/a.native.test.ts"}},
	}
	for _, c := range cases {
		got, err := FindNativeTests(root, c.patterns)
		if err != nil {
			t.Errorf("%s: %v", c.name, err)
			continue
		}
		if r := relList(t, root, got); !reflect.DeepEqual(r, c.want) {
			t.Errorf("%s: got %v, want %v", c.name, r, c.want)
		}
	}
}

func TestFindNativeTestsErrors(t *testing.T) {
	root := writeTestTree(t, "tests/a.native.test.ts")
	if _, err := FindNativeTests(root, []string{"nope"}); err == nil || !strings.Contains(err.Error(), `no test file matches "nope"`) {
		t.Errorf("unmatched pattern: %v", err)
	}
	empty := t.TempDir()
	if _, err := FindNativeTests(empty, nil); err == nil || !strings.Contains(err.Error(), "*.native.test.ts") {
		t.Errorf("no tests: %v", err)
	}
}

func TestGlobRegexp(t *testing.T) {
	for _, c := range []struct {
		glob, path string
		ok         bool
	}{
		{"**/*.test.ts", "a.test.ts", true},
		{"**/*.test.ts", "a/b/c.test.ts", true},
		{"*.test.ts", "a/c.test.ts", false},
		{"tests/*.ts", "tests/a.ts", true},
		{"tests/*.ts", "tests/x/a.ts", false},
		{"tests/**", "tests/x/a.ts", true},
		{"a?.ts", "ab.ts", true},
		{"a?.ts", "a/.ts", false},
		{"a.b", "axb", false},
	} {
		re, err := globRegexp(c.glob)
		if err != nil {
			t.Fatal(err)
		}
		if got := re.MatchString(c.path); got != c.ok {
			t.Errorf("%q vs %q: got %v, want %v", c.glob, c.path, got, c.ok)
		}
	}
}

func TestParseNodeVersion(t *testing.T) {
	for in, want := range map[string][2]int{"v20.10.0\n": {20, 10}, "v24.22.2": {24, 22}, "22.1.0": {22, 1}} {
		maj, min, ok := parseNodeVersion(in)
		if !ok || maj != want[0] || min != want[1] {
			t.Errorf("%q: %d.%d %v", in, maj, min, ok)
		}
	}
	if _, _, ok := parseNodeVersion("nonsense"); ok {
		t.Error("nonsense parsed")
	}
}

func TestCheckTestDisplay(t *testing.T) {
	env := func(m map[string]string) func(string) string { return func(k string) string { return m[k] } }
	if err := checkTestDisplay("linux", env(nil)); err == nil || !strings.Contains(err.Error(), "xvfb-run -a krate native test") {
		t.Errorf("headless linux: %v", err)
	}
	for _, ok := range []error{
		checkTestDisplay("linux", env(map[string]string{"DISPLAY": ":1"})),
		checkTestDisplay("linux", env(map[string]string{"WAYLAND_DISPLAY": "wayland-0"})),
		checkTestDisplay("darwin", env(nil)),
		checkTestDisplay("windows", env(nil)),
	} {
		if ok != nil {
			t.Errorf("unexpected: %v", ok)
		}
	}
}

func TestBundleNativeTests(t *testing.T) {
	root := writeTestTree(t)
	write := func(rel, src string) string {
		p := filepath.Join(root, filepath.FromSlash(rel))
		_ = os.MkdirAll(filepath.Dir(p), 0o755)
		if err := os.WriteFile(p, []byte(src), 0o644); err != nil {
			t.Fatal(err)
		}
		return p
	}
	write("tests/helper.ts", "export const n: number = 41;\n")
	f := write("tests/a.native.test.ts", "import { n } from './helper';\nimport { test } from 'node:test';\nimport { launch } from '@krate/app/driver';\nconst x: string = 'v' + (n + 1);\ntest('t', () => { void launch; console.log(x); });\n")
	out := filepath.Join(root, ".krate", "native-test", "run")
	got, err := bundleNativeTests(root, out, []string{f})
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(out, "tests", "a.native.test.mjs")
	if len(got) != 1 || got[0] != want {
		t.Fatalf("got %v, want %v", got, want)
	}
	src, err := os.ReadFile(want)
	if err != nil {
		t.Fatal(err)
	}
	s := string(src)
	if strings.Contains(s, ": number") || strings.Contains(s, "./helper") {
		t.Errorf("TypeScript not stripped or helper not bundled:\n%s", s)
	}
	if !strings.Contains(s, `from "node:test"`) || !strings.Contains(s, `from "@krate/app/driver"`) {
		t.Errorf("packages must stay imports:\n%s", s)
	}

	bad := write("tests/bad.native.test.ts", "const = ;\n")
	if _, err := bundleNativeTests(root, out, []string{bad}); err == nil || !strings.Contains(err.Error(), "bad.native.test.ts:1") {
		t.Errorf("syntax error: %v", err)
	}
}

// A fake node and shell show what RunNativeTest passes on without a window.
func TestRunNativeTestRunsNodeWithEnvironment(t *testing.T) {
	if _, err := exec.LookPath("sh"); err != nil || runtime.GOOS == "windows" {
		t.Skip("needs sh")
	}
	if testing.Short() {
		t.Skip("builds an app")
	}
	ex := e2eRequireDeps(t)
	// A copy of nothing: use the example, but stop before launching anything
	// by pointing node at a script that records its arguments.
	work := t.TempDir()
	rec := filepath.Join(work, "args.txt")
	node := filepath.Join(work, "node")
	script := "#!/bin/sh\nif [ \"$1\" = \"--version\" ]; then echo v22.1.0; exit 0; fi\n" +
		"echo \"$@\" > " + rec + "\necho \"$KRATE_TEST_SHELL|$KRATE_TEST_APP|$KRATE_TEST\" >> " + rec + "\nexit 3\n"
	if err := os.WriteFile(node, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	shell := e2eBuildShell(t, work)
	t.Setenv("DISPLAY", ":0")
	var out bytes.Buffer
	code, err := RunNativeTest(NativeTestOptions{
		LoadConfig: func() (string, *config.Config, map[string]string, error) {
			cfg, err := config.Load(ex)
			return ex, cfg, nil, err
		},
		Patterns: []string{"tasks"},
		Shell:    shell,
		Grep:     "adds",
		Workers:  2,
		Node:     node,
		Stdout:   &out,
		Stderr:   &out,
	})
	if err != nil {
		t.Fatal(err)
	}
	if code != 3 {
		t.Errorf("exit code %d, want node's 3\n%s", code, out.String())
	}
	b, err := os.ReadFile(rec)
	if err != nil {
		t.Fatal(err)
	}
	got := string(b)
	for _, want := range []string{"--test", "--test-concurrency=2", "--test-name-pattern=adds", "tasks.native.test.mjs", shell + "|", filepath.Join("native", "test", "app") + "|1"} {
		if !strings.Contains(got, want) {
			t.Errorf("node was run as:\n%s\nmissing %q", got, want)
		}
	}
}

func TestRunNativeTestOldNode(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("needs sh")
	}
	work := t.TempDir()
	node := filepath.Join(work, "node")
	if err := os.WriteFile(node, []byte("#!/bin/sh\necho v18.19.0\n"), 0o755); err != nil {
		t.Skip(err)
	}
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("needs sh")
	}
	root := writeTestTree(t, "t.native.test.ts")
	t.Setenv("DISPLAY", ":0")
	_, err := RunNativeTest(NativeTestOptions{
		LoadConfig: func() (string, *config.Config, map[string]string, error) { return root, &config.Config{}, nil, nil },
		Node:       node,
	})
	if err == nil || !strings.Contains(err.Error(), "Node.js 20.10 or newer") {
		t.Errorf("old node: %v", err)
	}
}
