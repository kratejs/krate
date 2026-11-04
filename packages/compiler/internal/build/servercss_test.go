package build

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func TestSSRServerBundleUsesKrateCSSModuleNames(t *testing.T) {
	root := t.TempDir()
	writeFileRel(t, root, "src/pages/W.module.css", ".w{color:rgb(9,9,9)} .card-title{color:red}")
	writeFileRel(t, root, "src/pages/plain.css", ".plain{color:blue}")
	writeFileRel(t, root, "node_modules/csspkg/index.js", "export const x = 1;")
	writeFileRel(t, root, "node_modules/csspkg/package.json", `{"name":"csspkg","main":"index.js"}`)
	writeFileRel(t, root, "node_modules/csspkg/style.css", ".pkg{color:green}")
	writeFileRel(t, root, "src/pages/ssr.tsx", `import styles from './W.module.css';
import './plain.css';
import 'csspkg/style.css';
export const config = { ssr: true };
export default function Page() { return <div class={styles.w}><i class={styles["card-title"]}>x</i></div>; }
`)
	cfg := cssTestConfig(root)
	if err := New(root, cfg).BuildAll(); err != nil {
		t.Fatalf("BuildAll: %v", err)
	}
	bundleDir := filepath.Join(cfg.OutDir, ".krate", "server-bundles")
	data, err := os.ReadFile(filepath.Join(bundleDir, "ssr.server.mjs"))
	if err != nil {
		t.Fatal(err)
	}
	out := string(data)
	if strings.Contains(out, "W_w") {
		t.Errorf("server bundle uses esbuild's class names:\n%s", out)
	}
	m := regexp.MustCompile(`"w":\s*"(w_[a-z0-9]+)"`).FindStringSubmatch(out)
	if m == nil {
		t.Fatalf("Krate mapping missing from the server bundle:\n%s", out)
	}
	if strings.Contains(out, "import \"csspkg/style.css\"") || strings.Contains(out, "import 'csspkg/style.css'") {
		t.Errorf("package stylesheet import left in the server bundle:\n%s", out)
	}
	if css, _ := filepath.Glob(filepath.Join(bundleDir, "*.css")); len(css) > 0 {
		t.Errorf("stray server css written: %v", css)
	}
	// The same class name must be the one the page stylesheet defines.
	var allCSS string
	files, _ := filepath.Glob(filepath.Join(cfg.OutDir, "styles.*.css"))
	for _, f := range files {
		d, _ := os.ReadFile(f)
		allCSS += string(d)
	}
	if !strings.Contains(allCSS, "."+m[1]) {
		t.Errorf("stylesheet does not define .%s:\n%s", m[1], allCSS)
	}
}
