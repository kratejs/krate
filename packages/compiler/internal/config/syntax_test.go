package config

import (
	"errors"
	"os"
	"path/filepath"
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
