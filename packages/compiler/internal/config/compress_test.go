package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCompressDefaultsOnAndHonoursFalse(t *testing.T) {
	load := func(src string) *Config {
		t.Helper()
		root := t.TempDir()
		if err := os.WriteFile(filepath.Join(root, "krate.config.ts"), []byte(src), 0644); err != nil {
			t.Fatal(err)
		}
		cfg, err := Load(root)
		if err != nil {
			t.Fatal(err)
		}
		return cfg
	}
	cases := []struct {
		name, src          string
		brotli, gzip, want bool
	}{
		{"unset", `export default {};`, true, true, true},
		{"empty object", `export default { compress: {} };`, true, true, true},
		{"gzip off", `export default { compress: { gzip: false } };`, true, false, true},
		{"brotli off", `export default { compress: { brotli: false } };`, false, true, true},
		{"both off", `export default { compress: { brotli: false, gzip: false } };`, false, false, false},
		{"explicit on", `export default { compress: { brotli: true } };`, true, true, true},
	}
	for _, c := range cases {
		cfg := load(c.src)
		if cfg.Compress.BrotliOn() != c.brotli || cfg.Compress.GzipOn() != c.gzip || cfg.ShouldPrecompress() != c.want {
			t.Errorf("%s: brotli=%v gzip=%v precompress=%v, want %v %v %v", c.name,
				cfg.Compress.BrotliOn(), cfg.Compress.GzipOn(), cfg.ShouldPrecompress(), c.brotli, c.gzip, c.want)
		}
	}
	if !Default().ShouldPrecompress() {
		t.Error("Default() must precompress")
	}
}
