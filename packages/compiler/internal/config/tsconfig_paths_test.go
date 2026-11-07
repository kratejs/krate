package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeTS(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestTSConfigWithCommentsKeepsAliases(t *testing.T) {
	dir := t.TempDir()
	writeTS(t, filepath.Join(dir, "tsconfig.json"), `{
  // strict mode
  "compilerOptions": {
    /* block */
    "baseUrl": ".",
    "paths": { "@/*": ["./src/*",], "url": ["a//b"] },
  },
}`)
	Warnings = nil
	cfg := &Config{}
	cfg.LoadTSConfigPaths(dir)
	if len(cfg.PathAliases) != 2 {
		t.Fatalf("aliases = %+v, warnings=%v", cfg.PathAliases, Warnings)
	}
	for _, a := range cfg.PathAliases {
		if a.Prefix == "url" && a.Targets[0] != "a//b" {
			t.Errorf("comment stripping touched a string: %v", a.Targets)
		}
	}
	if len(Warnings) != 0 {
		t.Errorf("unexpected warnings %v", Warnings)
	}
}

func TestTSConfigExtendsRelativeAndPackage(t *testing.T) {
	dir := t.TempDir()
	writeTS(t, filepath.Join(dir, "base", "tsconfig.base.json"), `{"compilerOptions":{"paths":{"@lib/*":["./lib/*"]},"experimentalDecorators":true}}`)
	writeTS(t, filepath.Join(dir, "node_modules", "pkg-preset", "tsconfig.json"), `{"compilerOptions":{"baseUrl":"./src","paths":{"~/*":["./*"]}}}`)
	writeTS(t, filepath.Join(dir, "a", "tsconfig.json"), `{"extends":"../base/tsconfig.base.json"}`)
	writeTS(t, filepath.Join(dir, "b", "tsconfig.json"), `{"extends":"pkg-preset"}`)
	writeTS(t, filepath.Join(dir, "node_modules", "pkg-preset", "src", "x.ts"), ``)

	cfg := &Config{}
	cfg.LoadTSConfigPaths(filepath.Join(dir, "a"))
	if len(cfg.PathAliases) != 1 || cfg.PathAliases[0].Targets[0] != filepath.Join(dir, "base", "lib", "*") {
		t.Fatalf("relative extends: %+v", cfg.PathAliases)
	}
	if !cfg.TSCompilerOptions.ExperimentalDecorators {
		t.Error("experimentalDecorators not inherited")
	}

	cfg = &Config{}
	cfg.LoadTSConfigPaths(filepath.Join(dir, "b"))
	if len(cfg.PathAliases) != 1 || cfg.TSBaseDir != filepath.Join(dir, "node_modules", "pkg-preset", "src") {
		t.Fatalf("package extends: %+v base=%s", cfg.PathAliases, cfg.TSBaseDir)
	}
}

func TestTSConfigParseFailureWarns(t *testing.T) {
	dir := t.TempDir()
	writeTS(t, filepath.Join(dir, "tsconfig.json"), `{ "compilerOptions": { `)
	Warnings = nil
	cfg := &Config{}
	cfg.LoadTSConfigPaths(dir)
	if len(Warnings) != 1 || !strings.Contains(Warnings[0], "could not parse") {
		t.Fatalf("warnings = %v", Warnings)
	}
}

func TestTSConfigMissingExtendsWarns(t *testing.T) {
	dir := t.TempDir()
	writeTS(t, filepath.Join(dir, "tsconfig.json"), `{"extends":"./nope.json"}`)
	Warnings = nil
	cfg := &Config{}
	cfg.LoadTSConfigPaths(dir)
	if len(Warnings) != 1 || !strings.Contains(Warnings[0], "was not found") {
		t.Fatalf("warnings = %v", Warnings)
	}
}

func TestTSConfigAliasOrderDeterministic(t *testing.T) {
	dir := t.TempDir()
	writeTS(t, filepath.Join(dir, "tsconfig.json"), `{"compilerOptions":{"paths":{"@/*":["a/*"],"@/ui/*":["b/*"],"@/ui/x":["c"]}}}`)
	for i := 0; i < 20; i++ {
		cfg := &Config{}
		cfg.LoadTSConfigPaths(dir)
		got := cfg.PathAliases[0].Prefix + "," + cfg.PathAliases[1].Prefix + "," + cfg.PathAliases[2].Prefix
		if got != "@/ui/x,@/ui/*,@/*" {
			t.Fatalf("order = %s", got)
		}
	}
}

func TestTSConfigBackslashPathsAreNormalized(t *testing.T) {
	dir := t.TempDir()
	writeTS(t, filepath.Join(dir, "tsconfig.json"), `{
  "compilerOptions": {
    "baseUrl": ".\\app",
    "paths": { "@/*": [".\\src\\*"], "one": "lib\\one.ts" }
  }
}`)
	c := &Config{}
	c.LoadTSConfigPaths(dir)
	got := map[string]string{}
	for _, a := range c.PathAliases {
		got[a.Prefix] = strings.Join(a.Targets, ",")
	}
	if got["@/*"] != "./src/*" || got["one"] != "lib/one.ts" {
		t.Fatalf("targets = %v", got)
	}
	if want := filepath.Join(dir, "app"); c.TSBaseDir != want {
		t.Fatalf("TSBaseDir = %q, want %q", c.TSBaseDir, want)
	}
}

func TestTSBaseURLOnlySetWhenTsconfigDeclaresIt(t *testing.T) {
	dir := t.TempDir()
	writeTS(t, filepath.Join(dir, "tsconfig.json"), `{"compilerOptions":{"baseUrl":"src"}}`)
	cfg := &Config{}
	cfg.LoadTSConfigPaths(dir)
	if want := filepath.Join(dir, "src"); cfg.TSBaseURL != want || cfg.TSBaseDir != want {
		t.Errorf("TSBaseURL = %q, TSBaseDir = %q, want %q", cfg.TSBaseURL, cfg.TSBaseDir, want)
	}

	dir = t.TempDir()
	writeTS(t, filepath.Join(dir, "tsconfig.json"), `{"compilerOptions":{"paths":{"@/*":["./src/*"]}}}`)
	cfg = &Config{}
	cfg.LoadTSConfigPaths(dir)
	if cfg.TSBaseURL != "" {
		t.Errorf("TSBaseURL = %q without a baseUrl", cfg.TSBaseURL)
	}
}

func shimWarnings(t *testing.T) []string {
	t.Helper()
	var out []string
	for _, w := range Warnings {
		if strings.Contains(w, "KR7125") {
			out = append(out, w)
		}
	}
	return out
}

// The 0.4 scaffold ("baseUrl": "./src" with "./src/*" targets) names directories
// that only exist under the project root: resolve them there and warn once.
func TestTSConfigRootRelativeTargetsResolveWithOneWarning(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "src", "components"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeTS(t, filepath.Join(dir, "tsconfig.json"), `{
  "compilerOptions": {
    "baseUrl": "./src",
    "paths": { "@/*": ["./src/*"], "@c/*": ["./src/components/*"] }
  }
}`)
	Warnings = nil
	c := &Config{}
	c.LoadTSConfigPaths(dir)
	if want := filepath.Join(dir, "src"); c.TSBaseDir != want {
		t.Fatalf("TSBaseDir = %q, want %q", c.TSBaseDir, want)
	}
	got := map[string]string{}
	for _, a := range c.PathAliases {
		got[a.Prefix] = strings.Join(a.Targets, ",")
	}
	if want := filepath.ToSlash(filepath.Join(dir, "src")) + "/*"; got["@/*"] != want {
		t.Errorf("@/* targets = %q, want the root path %q", got["@/*"], want)
	}
	ws := shimWarnings(t)
	if len(ws) != 1 || !strings.Contains(ws[0], "krate fix --apply") {
		t.Fatalf("warnings = %q, want exactly one KR7125 naming `krate fix --apply`", ws)
	}
}

// Targets that exist under baseUrl, or nowhere, are left exactly as written.
func TestTSConfigAliasTargetsThatAreNotRootRelativeAreUntouched(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "src", "lib"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeTS(t, filepath.Join(dir, "tsconfig.json"), `{
  "compilerOptions": {
    "baseUrl": "./src",
    "paths": { "@/*": ["./*"], "~/*": ["./missing/*"] }
  }
}`)
	Warnings = nil
	c := &Config{}
	c.LoadTSConfigPaths(dir)
	got := map[string]string{}
	for _, a := range c.PathAliases {
		got[a.Prefix] = strings.Join(a.Targets, ",")
	}
	if got["@/*"] != "./*" || got["~/*"] != "./missing/*" {
		t.Errorf("targets were rewritten: %v", got)
	}
	if ws := shimWarnings(t); len(ws) != 0 {
		t.Errorf("unexpected warnings: %q", ws)
	}
}

// A root-relative target that krate fix cannot rewrite is still resolved, but
// the warning must not promise a fix that will not happen.
func TestTSConfigRootRelativeWarningOnlyNamesFixWhenItApplies(t *testing.T) {
	dir := t.TempDir()
	for _, d := range []string{"src", "lib"} {
		if err := os.MkdirAll(filepath.Join(dir, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	writeTS(t, filepath.Join(dir, "tsconfig.json"), `{"compilerOptions":{"baseUrl":"./src","paths":{"@lib/*":["./lib/*"]}}}`)
	Warnings = nil
	c := &Config{}
	c.LoadTSConfigPaths(dir)
	ws := shimWarnings(t)
	if len(ws) != 1 || strings.Contains(ws[0], "krate fix") {
		t.Fatalf("warnings = %q, want one KR7125 without a krate fix promise", ws)
	}
}
