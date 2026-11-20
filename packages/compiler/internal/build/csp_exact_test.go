package build

import (
	"crypto/sha256"
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/config"
)

func rawHash(s string) string {
	sum := sha256.Sum256([]byte(s))
	return "'sha256-" + base64.StdEncoding.EncodeToString(sum[:]) + "'"
}

func TestCSPHashesExactBytes(t *testing.T) {
	t.Parallel()
	script := "\n  var a = 1;\n"
	style := " .a { color: red } "
	doc := "<script>" + script + "</script><style>" + style + "</style>"
	meta := generateCSPMeta(doc, doc, "", "")
	if !strings.Contains(meta, rawHash(script)) {
		t.Errorf("script hash must cover the exact bytes including whitespace: %s", meta)
	}
	if !strings.Contains(meta, rawHash(style)) {
		t.Errorf("style hash must cover the exact bytes including whitespace: %s", meta)
	}
	trimmed := sha256Base64(strings.TrimSpace(script))
	if strings.Contains(meta, trimmed) {
		t.Errorf("the trimmed script hash must not be allow-listed: %s", meta)
	}
}

func TestCSPAllowsJsDelivrOnlyForLoaders(t *testing.T) {
	t.Parallel()
	loader := `<script>import('https://cdn.jsdelivr.net/npm/mermaid@11/dist/m.mjs')</script>`
	with := generateCSPMeta(loader, "", "", "")
	for _, d := range []string{"script-src 'self' https://cdn.jsdelivr.net", "style-src 'self' https://cdn.jsdelivr.net", "font-src 'self' https://cdn.jsdelivr.net"} {
		if !strings.Contains(with, d) {
			t.Errorf("missing %q in %s", d, with)
		}
	}
	without := generateCSPMeta(`<script>var a=1</script>`, "", "", "")
	if strings.Contains(without, "jsdelivr") {
		t.Errorf("no CDN origin without a loader: %s", without)
	}
}

func TestInsertCSPMetaAfterCharset(t *testing.T) {
	t.Parallel()
	doc := `<!DOCTYPE html><html><head><meta charset=UTF-8><script>x()</script></head><body></body></html>`
	got := insertCSPMeta(doc, "<meta http-equiv=\"Content-Security-Policy\" content=\"a\">")
	csp := strings.Index(got, "Content-Security-Policy")
	if csp < strings.Index(got, "charset") || csp > strings.Index(got, "<script>") {
		t.Errorf("policy must sit between charset and the first script: %s", got)
	}
}

func TestBuiltPageCSPPrecedesInlineAssetsAndMatchesBytes(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeFileRel(t, root, "src/pages/index.tsx", "export default function P() {\n"+
		" return <div><style>{`.a  {  color : blue ;  }`}</style><p>x</p></div>;\n}\n")
	cfg, err := config.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	cfg.CSP.Enabled = true
	if err := New(root, cfg).BuildAll(); err != nil {
		t.Fatalf("BuildAll: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(cfg.OutDir, "index.html"))
	if err != nil {
		t.Fatal(err)
	}
	doc := string(data)
	csp := strings.Index(doc, "Content-Security-Policy")
	if csp < 0 {
		t.Fatalf("no CSP meta in %s", doc)
	}
	for _, tag := range []string{"<script", "<style"} {
		if i := strings.Index(doc, tag); i >= 0 && i < csp {
			t.Errorf("%s at %d precedes the CSP meta at %d:\n%s", tag, i, csp, doc)
		}
	}
	meta := doc[csp:]
	meta = meta[:strings.Index(meta, ">")]
	for _, tag := range []string{"script", "style"} {
		for _, inline := range extractInlineContent(doc, tag) {
			if !strings.Contains(meta, rawHash(inline)) {
				t.Errorf("%s block %q is not hashed byte for byte in %s", tag, inline, meta)
			}
		}
	}
}
