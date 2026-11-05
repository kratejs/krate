package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func loadCfgSrc(t *testing.T, src string) (*Config, error) {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "krate.config.ts"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	return Load(dir)
}

func TestConfigThrownErrorIsReported(t *testing.T) {
	_, err := loadCfgSrc(t, "const v = (() => { throw new Error('MISSING_ENV_NOPE'); })();\nexport default { entry: 'src/pages/index.tsx' + v };\n")
	if err == nil || !strings.Contains(err.Error(), "MISSING_ENV_NOPE") {
		t.Fatalf("err = %v", err)
	}
	if strings.Contains(err.Error(), "isn't installed") || strings.Contains(err.Error(), "__krateEvalResult") {
		t.Errorf("misleading message: %v", err)
	}
}

func TestConfigValidateThrowReported(t *testing.T) {
	_, err := loadCfgSrc(t, "export default { outDir: 'dist', validate() { throw new Error('bad outDir') } };\n")
	if err == nil || !strings.Contains(err.Error(), "bad outDir") {
		t.Fatalf("err = %v", err)
	}
}

func TestConfigRejectedAsyncConfigReported(t *testing.T) {
	_, err := loadCfgSrc(t, "export default async () => { await null; throw new Error('ASYNC_BOOM'); };\n")
	if err == nil || !strings.Contains(err.Error(), "ASYNC_BOOM") {
		t.Fatalf("err = %v", err)
	}
}

func TestConfigUnsupportedNodeBuiltinIsPrecise(t *testing.T) {
	_, err := loadCfgSrc(t, "import cp from 'node:child_process';\nexport default { outDir: String(typeof cp) };\n")
	if err == nil || !strings.Contains(err.Error(), "node:child_process") || !strings.Contains(err.Error(), "does not provide") {
		t.Fatalf("err = %v", err)
	}
	if strings.Contains(err.Error(), "isn't installed") {
		t.Errorf("misleading dependency hint: %v", err)
	}
}

// The prefix is optional in Node, so a bare built-in the config runtime does
// not provide must be reported the same way, not as a missing package.
func TestConfigUnsupportedBareBuiltinIsPrecise(t *testing.T) {
	for _, spec := range []string{"child_process", "crypto", "fs/promises", "zlib"} {
		_, err := loadCfgSrc(t, "import m from '"+spec+"';\nexport default { outDir: String(typeof m) };\n")
		if err == nil || !strings.Contains(err.Error(), "node:"+spec) || !strings.Contains(err.Error(), "does not provide") {
			t.Errorf("%s: err = %v", spec, err)
		} else if strings.Contains(err.Error(), "isn't installed") {
			t.Errorf("%s: misleading dependency hint: %v", spec, err)
		}
	}
	cfg, err := loadCfgSrc(t, "import path from 'path';\nexport default { outDir: path.join('a', 'b') };\n")
	if err != nil || !strings.HasSuffix(filepath.ToSlash(cfg.OutDir), "a/b") {
		t.Fatalf("bare path: %v, %v", cfg, err)
	}
}

func TestConfigTopLevelAwait(t *testing.T) {
	cfg, err := loadCfgSrc(t, "const v = await Promise.resolve('built');\nexport default { outDir: v };\n")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(filepath.ToSlash(cfg.OutDir), "/built") {
		t.Errorf("outDir = %q", cfg.OutDir)
	}
}

func TestConfigAsyncDefineConfig(t *testing.T) {
	cfg, err := loadCfgSrc(t, "export default async () => { await null; return { outDir: 'asyncout' }; };\n")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(filepath.ToSlash(cfg.OutDir), "/asyncout") {
		t.Errorf("outDir = %q", cfg.OutDir)
	}
}

func TestConfigNodeBuiltinImport(t *testing.T) {
	cfg, err := loadCfgSrc(t, "import path from 'node:path';\nimport { readFileSync } from 'node:fs';\nexport default { outDir: path.join('a', 'b'), site: { name: typeof readFileSync } };\n")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(filepath.ToSlash(cfg.OutDir), "/a/b") {
		t.Errorf("outDir = %q", cfg.OutDir)
	}
}
