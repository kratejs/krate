package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/diag"
)

func writeCfg(t *testing.T, name, src string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func wantSyntaxDiag(t *testing.T, err error, file string) diag.Diagnostic {
	t.Helper()
	if err == nil {
		t.Fatal("expected a syntax error, config loaded")
	}
	var d diag.Diagnostic
	if !errors.As(err, &d) {
		t.Fatalf("error is not a diag.Diagnostic: %v", err)
	}
	if filepath.Base(d.File) != file || d.Line < 1 || d.Col < 1 {
		t.Errorf("want %s:line:col, got %s:%d:%d", file, d.File, d.Line, d.Col)
	}
	if d.Hint == "" {
		t.Errorf("want a hint, got none for %v", d)
	}
	return d
}

func TestConfigSyntaxErrorFailsWithDiagnostic(t *testing.T) {
	cases := map[string]string{
		"trailing garbage":  "export default { outDir: 'dist' }\nsyntax error here\n",
		"stray operators":   "export default { outDir: 'dist' } +++ ;;; {{{\n",
		"unclosed object":   "export default { outDir: 'dist',\n",
		"double comma":      "export default { outDir: 'dist',, }\n",
		"bad statement":     "const x = ;\nexport default { outDir: 'dist' }\n",
		"module and syntax": "import path from 'node:path';\nexport default { outDir: path.join('a') \n",
	}
	for name, src := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := Load(writeCfg(t, "krate.config.ts", src))
			_ = wantSyntaxDiag(t, err, "krate.config.ts")
		})
	}
}

func TestConfigSyntaxErrorPosition(t *testing.T) {
	_, err := Load(writeCfg(t, "krate.config.ts", "export default { outDir: 'dist' }\nsyntax error here\n"))
	d := wantSyntaxDiag(t, err, "krate.config.ts")
	if d.Line != 2 || d.Col != 8 {
		t.Errorf("position = %d:%d, want 2:8", d.Line, d.Col)
	}
}

func TestConfigSyntaxErrorInExtendedBase(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "base.ts"), []byte("export default { outDir: 'x' \n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "krate.config.ts"), []byte("export default { extends: './base.ts' }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := Load(dir)
	_ = wantSyntaxDiag(t, err, "base.ts")
}

func TestConfigJSONSyntaxErrorFailsWithDiagnostic(t *testing.T) {
	_, err := Load(writeCfg(t, "krate.config.json", "{\n  \"outDir\": \"dist\",\n  \"minify\": tru\n}\n"))
	d := wantSyntaxDiag(t, err, "krate.config.json")
	if d.Line != 3 {
		t.Errorf("line = %d, want 3", d.Line)
	}
}

// When the evaluator cannot run at all, a plain literal config must still load
// through the static parser; only real syntax errors are fatal.
func TestConfigStaticFallbackWhenEvaluatorCannotRun(t *testing.T) {
	old := evaluateTSConfig
	evaluateTSConfig = func(string, LoadOptions) ([]byte, error) { return nil, fmt.Errorf("runtime unavailable") }
	t.Cleanup(func() { evaluateTSConfig = old })

	cfg, err := Load(writeCfg(t, "krate.config.ts", "export default { outDir: 'viastatic', server: { port: 4321 } }\n"))
	if err != nil {
		t.Fatalf("static fallback failed: %v", err)
	}
	if !strings.HasSuffix(filepath.ToSlash(cfg.OutDir), "/viastatic") || cfg.Server.Port != 4321 {
		t.Errorf("outDir=%q port=%d", cfg.OutDir, cfg.Server.Port)
	}
	if !warned(Warnings, "parsed statically") {
		t.Errorf("want a static-parse warning, got %v", Warnings)
	}

	// A syntax error is still fatal even though the evaluator is down.
	_, err = Load(writeCfg(t, "krate.config.ts", "export default { outDir: 'a' }\nsyntax error here\n"))
	_ = wantSyntaxDiag(t, err, "krate.config.ts")

	// A valid config the static parser cannot read reports the evaluator cause.
	_, err = Load(writeCfg(t, "krate.config.ts", "const c = { outDir: 'a' };\nexport default c;\n"))
	if err == nil || !strings.Contains(err.Error(), "runtime unavailable") {
		t.Errorf("want the evaluator cause in the error, got %v", err)
	}

	// A module config keeps its dedicated message.
	_, err = Load(writeCfg(t, "krate.config.ts", "import x from 'nope-not-installed';\nexport default { outDir: x };\n"))
	if err == nil || !strings.Contains(err.Error(), "imports/requires") {
		t.Errorf("module config error = %v", err)
	}
}

func warned(ws []string, sub string) bool {
	for _, w := range ws {
		if strings.Contains(w, sub) {
			return true
		}
	}
	return false
}

func staticLoad(t *testing.T, src string) (*Config, error) {
	t.Helper()
	old := evaluateTSConfig
	evaluateTSConfig = func(string, LoadOptions) ([]byte, error) { return nil, fmt.Errorf("runtime unavailable") }
	t.Cleanup(func() { evaluateTSConfig = old })
	return Load(writeCfg(t, "krate.config.ts", src))
}

func TestStaticParserRejectsMistypedNestedValue(t *testing.T) {
	cases := []struct{ src, want string }{
		{"export default { server: { port: '8080' } }", "server.port must be a number"},
		{"export default { devServer: { open: 'yes' } }", "devServer.open must be a boolean"},
		{"export default { compress: { gzip: 'on' } }", "compress.gzip must be a boolean"},
		{"export default { compat: { mode: 3 } }", "compat.mode must be a string"},
		{"export default { server: { host: ['a'] } }", "server.host must be a string"},
	}
	for _, c := range cases {
		_, err := staticLoad(t, c.src)
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: err = %v, want %q", c.src, err, c.want)
		}
	}
}

// Every typed nested key must reject a value of the wrong type when the static
// parser reads the config, matching the evaluated path.
func TestStaticParserRejectsEveryMistypedNestedKey(t *testing.T) {
	var checked int
	ct := reflect.TypeOf(Config{})
	for i := 0; i < ct.NumField(); i++ {
		top, ok := schemaFieldName(ct.Field(i))
		if !ok {
			continue
		}
		st := ct.Field(i).Type
		for st.Kind() == reflect.Ptr {
			st = st.Elem()
		}
		if st.Kind() != reflect.Struct || hasCustomDecoder(st) {
			continue
		}
		for j := 0; j < st.NumField(); j++ {
			f := st.Field(j)
			nested, ok := schemaFieldName(f)
			if !ok || !f.IsExported() {
				continue
			}
			bad, ok := wrongLiteral(f.Type)
			if !ok {
				continue
			}
			src := fmt.Sprintf("export default { %s: { %s: %s } }", top, nested, bad)
			_, err := staticLoad(t, src)
			if err == nil {
				t.Errorf("%s: static parser accepted a mistyped value", src)
			}
			checked++
		}
	}
	if checked < 40 {
		t.Fatalf("only %d nested keys checked; the schema walk is broken", checked)
	}
}

// wrongLiteral returns a literal that cannot decode into t, or false when t
// accepts any JSON value or has a custom decoder.
func wrongLiteral(t reflect.Type) (string, bool) {
	for t.Kind() == reflect.Ptr {
		t = t.Elem()
	}
	if hasCustomDecoder(t) {
		return "", false
	}
	switch t.Kind() {
	case reflect.String:
		return "123", true
	case reflect.Bool, reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Float32, reflect.Float64:
		return "'wrong'", true
	case reflect.Slice, reflect.Array, reflect.Struct, reflect.Map:
		return "'wrong'", true
	}
	return "", false
}

func hasCustomDecoder(t reflect.Type) bool {
	return reflect.PointerTo(t).Implements(reflect.TypeOf((*interface{ UnmarshalJSON([]byte) error })(nil)).Elem())
}

// Quoted keys must lose their quotes in the static parser; otherwise a quoted
// top-level key is dropped as unknown and nested map keys keep stray quotes.
func TestStaticParserQuotedKeys(t *testing.T) {
	cfg, err := staticLoad(t, `export default {
  "outDir": "quoted-out",
  headers: [{ source: "/about", headers: { "X-Custom": "about" } }],
  devServer: { proxy: { "/api": "http://localhost:8080" } },
}`)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(filepath.ToSlash(cfg.OutDir), "/quoted-out") {
		t.Errorf("outDir = %q, want the quoted key applied", cfg.OutDir)
	}
	if len(cfg.Headers) != 1 || cfg.Headers[0].Headers["X-Custom"] != "about" {
		t.Errorf("headers = %+v", cfg.Headers)
	}
	if cfg.DevServer.Proxy["/api"] != "http://localhost:8080" {
		t.Errorf("proxy = %+v", cfg.DevServer.Proxy)
	}
	if warned(Warnings, "unknown config key") {
		t.Errorf("unexpected unknown-key warning: %v", Warnings)
	}
}

func TestLoadExplicitConfigMustExist(t *testing.T) {
	dir := writeCfg(t, "krate.config.ts", "export default { outDir: 'from-default-file' }\n")

	for _, p := range []string{"typo.config.ts", "sub/missing.config.ts", filepath.Join(dir, "nope.config.ts")} {
		_, err := Load(dir, p)
		if err == nil {
			t.Errorf("--config %s: want an error for a missing file, got none", p)
			continue
		}
		if !strings.Contains(err.Error(), p) || !strings.Contains(err.Error(), "not found") {
			t.Errorf("--config %s: error should name the path, got %v", p, err)
		}
	}

	if err := os.Mkdir(filepath.Join(dir, "adir"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(dir, "adir"); err == nil || !strings.Contains(err.Error(), "directory") {
		t.Errorf("--config on a directory: err = %v", err)
	}
}

func TestLoadExplicitConfigIsUsed(t *testing.T) {
	dir := writeCfg(t, "krate.config.ts", "export default { outDir: 'default-out' }\n")
	if err := os.WriteFile(filepath.Join(dir, "other.config.ts"), []byte("export default { outDir: 'other-out' }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{"other.config.ts", filepath.Join(dir, "other.config.ts")} {
		cfg, err := Load(dir, p)
		if err != nil {
			t.Fatalf("--config %s: %v", p, err)
		}
		if !strings.HasSuffix(filepath.ToSlash(cfg.OutDir), "/other-out") {
			t.Errorf("--config %s: outDir = %q, want other-out", p, cfg.OutDir)
		}
	}
}

// Without --config, a project with no config file at all is valid.
func TestLoadWithoutConfigFileUsesDefaults(t *testing.T) {
	dir := t.TempDir()
	cfg, err := Load(dir)
	if err != nil {
		t.Fatalf("no config file: %v", err)
	}
	if !strings.HasSuffix(filepath.ToSlash(cfg.OutDir), "/dist") {
		t.Errorf("outDir = %q, want the default", cfg.OutDir)
	}
}

// A syntax error in a local file the config imports is the user's bug in that
// file. It must not read as a missing dependency or dump bundler text.
func TestConfigSyntaxErrorInImportedFile(t *testing.T) {
	cases := map[string]struct{ file, src string }{
		"ts helper":     {"helper.ts", "export const title = 'a';\nexport const broken = ;\n"},
		"js helper":     {"helper.js", "export const title = 'a'\nexport const broken = );\n"},
		"nested helper": {"lib/helper.ts", "export const title = 'a';\nconst x = (1 + ;\n"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			if err := os.MkdirAll(filepath.Dir(filepath.Join(dir, tc.file)), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dir, tc.file), []byte(tc.src), 0o644); err != nil {
				t.Fatal(err)
			}
			cfg := "import { title } from './" + tc.file + "';\nexport default { outDir: title };\n"
			if err := os.WriteFile(filepath.Join(dir, "krate.config.ts"), []byte(cfg), 0o644); err != nil {
				t.Fatal(err)
			}
			_, err := Load(dir)
			d := wantSyntaxDiag(t, err, filepath.Base(tc.file))
			if d.Line != 2 {
				t.Errorf("line = %d, want 2", d.Line)
			}
			if strings.TrimSpace(d.Source) == "" {
				t.Errorf("want the source line, got %q", d.Source)
			}
			msg := err.Error()
			for _, bad := range []string{"isn't installed", "bundling entry", "Underlying error"} {
				if strings.Contains(msg, bad) {
					t.Errorf("message contains %q:\n%s", bad, msg)
				}
			}
			if !strings.Contains(d.Message, "imported by") {
				t.Errorf("message should say the file is imported by the config: %q", d.Message)
			}
		})
	}
}

// A missing package keeps the install hint; only syntax errors change.
func TestConfigMissingPackageKeepsInstallHint(t *testing.T) {
	_, err := Load(writeCfg(t, "krate.config.ts", "import x from 'no-such-package-krate';\nexport default { outDir: String(x) };\n"))
	if err == nil || !strings.Contains(err.Error(), "isn't installed") {
		t.Fatalf("err = %v", err)
	}
}
