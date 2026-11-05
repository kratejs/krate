package config

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/environ"
)

func siteName(t *testing.T, src string) string {
	t.Helper()
	cfg, err := loadCfgSrc(t, src)
	if err != nil {
		t.Fatalf("%v\nsource: %s", err, src)
	}
	return cfg.Site.Name
}

func TestConfigSeesShellEnvironment(t *testing.T) {
	old := environ.Current
	t.Cleanup(func() { environ.Current = old })
	environ.Current = map[string]string{"KRATE_T_FILE": "fromfile"}
	t.Setenv("KRATE_T_SHELL", "fromshell")
	t.Setenv("NODE_ENV", "")
	got := siteName(t, "export default { site: { name: `${process.env.KRATE_T_FILE}|${process.env.KRATE_T_SHELL}|${process.env.NODE_ENV}` } };")
	if got != "fromfile|fromshell|production" {
		t.Errorf("site.name = %q", got)
	}
}

func TestConfigFsExistsSync(t *testing.T) {
	got := siteName(t, "import fs from 'node:fs';\nexport default { site: { name: [fs.existsSync('./missing.json'), fs.existsSync('krate.config.ts'), fs.existsSync('.'), fs.statSync('.').isDirectory()].join() } };")
	if got != "false,true,true,true" {
		t.Errorf("site.name = %q", got)
	}
}

func TestConfigProcessAndOSGlobals(t *testing.T) {
	got := siteName(t, "import os from 'node:os';\nexport default { site: { name: [process.cwd() !== '', process.platform === os.platform(), process.argv.length > 0, typeof os.homedir(), typeof os.tmpdir()].join() } };")
	if got != "true,true,true,string,string" {
		t.Errorf("site.name = %q", got)
	}
	want := runtime.GOOS
	if want == "windows" {
		want = "win32"
	}
	if got := siteName(t, "export default { site: { name: process.platform } };"); got != want {
		t.Errorf("process.platform = %q, want %q", got, want)
	}
}

func TestConfigBuffer(t *testing.T) {
	got := siteName(t, "export default { site: { name: Buffer.from('h\u00e9llo \u20ac').toString('base64') + '|' + Buffer.from('aGk=', 'base64').toString() + '|' + Buffer.from('hi').toString('hex') } };")
	if got != "aMOpbGxvIOKCrA==|hi|6869" {
		t.Errorf("site.name = %q", got)
	}
}

func TestConfigDirnameAndModuleExports(t *testing.T) {
	dir := t.TempDir()
	write := func(name, body string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.MkdirAll(filepath.Join(dir, "lib"), 0o755); err != nil {
		t.Fatal(err)
	}
	write("lib/where.ts", "export const here = __dirname.split('/').pop() + ':' + __filename.split('/').pop();\n")
	write("krate.config.ts", "import { here } from './lib/where';\nimport path from 'node:path';\nmodule.exports = { site: { name: path.basename(__dirname) + '|' + here } };\n")
	cfg, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Base(dir) + "|lib:where.ts"; cfg.Site.Name != want {
		t.Errorf("site.name = %q, want %q", cfg.Site.Name, want)
	}

	// A module that declares its own __dirname (the ESM idiom) keeps it.
	write("krate.config.ts", "import { fileURLToPath } from 'node:url';\nimport { dirname, basename } from 'node:path';\nconst __dirname = dirname(fileURLToPath(import.meta.url));\nexport default { site: { name: basename(__dirname) } };\n")
	cfg, err = Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Site.Name != filepath.Base(dir) {
		t.Errorf("site.name = %q", cfg.Site.Name)
	}
	if strings.Contains(cfg.Site.Name, "undefined") {
		t.Errorf("site.name = %q", cfg.Site.Name)
	}
}
