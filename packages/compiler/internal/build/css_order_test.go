package build

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/config"
)

func TestCSPCoversMinifiedInlineStylesAndAttrs(t *testing.T) {
	root := t.TempDir()
	writeFileRel(t, root, "src/pages/index.tsx", "export default function P() {\n"+
		" return <div style={{color: 'red', 'margin-top': '3px'}}><style>{`.a  {  color : blue ;  }\n\n .b{color:green}`}</style><p>x</p></div>;\n}\n")
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
	meta := doc[strings.Index(doc, "Content-Security-Policy"):]
	meta = meta[:strings.Index(meta, ">")]
	for _, inline := range extractInlineContent(doc, "style") {
		if !strings.Contains(meta, sha256Base64(inline)) {
			t.Errorf("style block %q not hashed in %s", inline, meta)
		}
	}
	attrs, _ := inlineStyleAttrsAndSpecRules(doc, "")
	if len(attrs) == 0 {
		t.Fatalf("no style attribute found in %s", doc)
	}
	if !strings.Contains(meta, "'unsafe-hashes'") {
		t.Errorf("missing unsafe-hashes: %s", meta)
	}
	for _, h := range attrs {
		if !strings.Contains(meta, h) {
			t.Errorf("style attr hash %s missing in %s", h, meta)
		}
	}
}
