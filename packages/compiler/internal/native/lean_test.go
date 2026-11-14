package native

import (
	"bytes"
	"image"
	"image/png"
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/config"
	"github.com/kratejs/krate/packages/compiler/internal/diag"
)

func pngBytes(t *testing.T, w, h int) string {
	t.Helper()
	var b bytes.Buffer
	if err := png.Encode(&b, image.NewRGBA(image.Rect(0, 0, w, h))); err != nil {
		t.Fatal(err)
	}
	return b.String()
}

func leanOn(t *testing.T, files map[string]string, mut func(c *config.Config)) []diag.Diagnostic {
	t.Helper()
	root := project(t, files)
	cfg := config.Default()
	cfg.PagesDir = "src/pages"
	cfg.Native = &config.NativeConfig{}
	if mut != nil {
		mut(cfg)
	}
	cfg.Resolve(root)
	pl, err := Analyze(root, cfg)
	if err != nil {
		t.Fatal(err)
	}
	ds, err := CheckLean(root, cfg, pl)
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range ds {
		if !d.IsWarning() {
			t.Errorf("lean diagnostics must be warnings: %v", d)
		}
		if d.Hint == "" {
			t.Errorf("lean diagnostic without a hint: %v", d)
		}
	}
	return ds
}

func codes(ds []diag.Diagnostic) string {
	var c []string
	for _, d := range ds {
		c = append(c, string(d.Code))
	}
	return strings.Join(c, ",")
}

func TestLeanOversizedImage(t *testing.T) {
	big := pngBytes(t, 1200, 800)
	files := map[string]string{
		"public/photo.png":   big,
		"public/icon.png":    pngBytes(t, 96, 96),
		"public/exact.png":   pngBytes(t, 400, 400),
		"src/pages/a.tsx":    `export default () => <img src="/photo.png" width="120" height="80" />`,
		"src/pages/b.tsx":    `export default () => <img src="/icon.png" width={48} height={48} />`,
		"src/pages/c.tsx":    `export default () => <img src="/exact.png" style={{ width: 200 }} />`,
		"src/pages/d.tsx":    `export default () => <img src="/photo.png" />`,
		"src/pages/e.tsx":    `export default () => <img src="https://example.com/x.png" width={10} />`,
		"src/pages/f.tsx":    `import hero from "../hero.png"; export default () => <img src={hero} width={100} />`,
		"src/hero.png":       big,
		"src/pages/index.go": "",
	}
	ds := leanOn(t, files, nil)
	var got []string
	for _, d := range ds {
		if d.Code != diag.CodeLeanImageOversize {
			t.Errorf("unexpected %v", d)
			continue
		}
		got = append(got, d.File)
	}
	want := "src/pages/a.tsx,src/pages/f.tsx"
	if strings.Join(got, ",") != want {
		t.Errorf("oversized images in %v, want %s", got, want)
	}
	if !strings.Contains(ds[0].Message, "1200x800") || !strings.Contains(ds[0].Message, "120x80") {
		t.Errorf("message should give both sizes: %s", ds[0].Message)
	}
	// 2x is allowed by default; a ratio of 1 reports the 400px image at 200.
	ds = leanOn(t, files, func(c *config.Config) { c.Native.Lean.ImageRatio = 1 })
	found := false
	for _, d := range ds {
		if d.File == "src/pages/c.tsx" {
			found = true
		}
	}
	if !found {
		t.Errorf("native.lean.imageRatio = 1 should report c.tsx: %s", codes(ds))
	}
}
