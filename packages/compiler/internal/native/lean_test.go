package native

import (
	"bytes"
	"encoding/base64"
	"image"
	"image/png"
	"strconv"
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

func TestLeanDataURIThreshold(t *testing.T) {
	payload := base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{7}, 40<<10))
	small := base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{7}, 2<<10))
	files := map[string]string{
		"src/pages/a.tsx": "export default () => <img alt=\"\" src=\"data:image/png;base64," + payload + "\" />",
		"src/pages/b.tsx": "export default () => <img alt=\"\" src=\"data:image/png;base64," + small + "\" />",
		"src/style.css":   "\n.hero { background: url(data:image/png;base64," + payload + "); }\n",
	}
	ds := leanOn(t, files, nil)
	if codes(ds) != "KR7271,KR7271" {
		t.Fatalf("got %s", codes(ds))
	}
	if ds[0].File != "src/pages/a.tsx" || ds[1].File != "src/style.css" || ds[1].Line != 2 {
		t.Errorf("positions: %v %v", ds[0], ds[1])
	}
	if !strings.Contains(ds[0].Message, "40 KiB") {
		t.Errorf("message: %s", ds[0].Message)
	}
	ds = leanOn(t, files, func(c *config.Config) { c.Native.Lean.DataURIKB = 1 })
	if len(ds) != 3 {
		t.Errorf("threshold 1 KiB should report all three, got %s", codes(ds))
	}
	ds = leanOn(t, files, func(c *config.Config) { c.Native.Lean.DataURIKB = 64 })
	if len(ds) != 0 {
		t.Errorf("threshold 64 KiB should report none, got %s", codes(ds))
	}
}

func TestLeanWebFonts(t *testing.T) {
	files := map[string]string{
		"src/fonts.css": `@font-face { font-family: "Inter"; src: url(/inter.woff2) format("woff2"); }
@font-face { font-family: "Inter"; font-weight: 700; src: url(/inter-b.woff2); }
@font-face { font-family: "MyIcons"; src: url(/icons.woff2); }
@font-face { font-family: "Local"; src: local("Arial"); }
body { font-family: system-ui, sans-serif; }
`,
		"src/pages/a.tsx": `import "@fontsource/roboto"; export default () => <p/>`,
		"src/pages/b.tsx": `export default () => <link rel="stylesheet" href="https://fonts.googleapis.com/css2?family=Lato" />`,
	}
	ds := leanOn(t, files, nil)
	if codes(ds) != "KR7272,KR7272,KR7272" {
		t.Fatalf("got %s: %v", codes(ds), ds)
	}
	if ds[0].File != "src/fonts.css" || ds[0].Line != 1 || !strings.Contains(ds[0].Message, "Inter") {
		t.Errorf("first: %v", ds[0])
	}
	ds = leanOn(t, files, func(c *config.Config) { c.Native.Lean.Ignore = []string{"KR7272"} })
	if len(ds) != 0 {
		t.Errorf("ignore should silence KR7272: %v", ds)
	}
}

func TestLeanCompositing(t *testing.T) {
	files := map[string]string{
		"src/a.css": `.glass { backdrop-filter: blur(8px); -webkit-backdrop-filter: blur(8px); }
.fine { backdrop-filter: none; box-shadow: 0 1px 3px rgba(0,0,0,.2); filter: blur(2px); }
.haze { filter: blur(32px); }
@keyframes pulse { from { filter: blur(0px) } to { filter: blur(6px) } }
li { box-shadow: 0 10px 60px rgba(0,0,0,.4); }
.card { box-shadow: 0 10px 60px rgba(0,0,0,.4); }
* { will-change: transform; }
.one { will-change: transform; }
`,
		"src/pages/a.tsx": `export default () => <div className="p-4 backdrop-blur-md"><i style={{ backdropFilter: "blur(4px)" }} /><b className="blur-sm backdrop-blur-none"/></div>`,
	}
	ds := leanOn(t, files, nil)
	var lines []string
	for _, d := range ds {
		if d.Code != diag.CodeLeanCompositing {
			t.Errorf("unexpected %v", d)
		}
		lines = append(lines, d.File+":"+strconv.Itoa(d.Line))
	}
	want := "src/a.css:1,src/a.css:3,src/a.css:4,src/a.css:5,src/a.css:7,src/pages/a.tsx:1,src/pages/a.tsx:1"
	if strings.Join(lines, ",") != want {
		t.Errorf("compositing findings\n got %s\nwant %s", strings.Join(lines, ","), want)
	}
}

func TestLeanWillChangeEverywhere(t *testing.T) {
	var css strings.Builder
	for i := 0; i < 6; i++ {
		css.WriteString(".c" + strconv.Itoa(i) + " { will-change: transform; }\n")
	}
	ds := leanOn(t, map[string]string{"src/a.css": css.String()}, nil)
	if codes(ds) != "KR7273" || !strings.Contains(ds[0].Message, "6 places") {
		t.Errorf("got %v", ds)
	}
	ds = leanOn(t, map[string]string{"src/a.css": ".c { will-change: transform; } .d { will-change: opacity }"}, nil)
	if len(ds) != 0 {
		t.Errorf("a couple of will-change rules are fine: %v", ds)
	}
}

func TestLeanLongLists(t *testing.T) {
	var items strings.Builder
	for i := 0; i < 150; i++ {
		items.WriteString(strconv.Itoa(i) + ",")
	}
	files := map[string]string{
		"src/data.ts": "export const rows = [" + items.String() + "];",
		"src/pages/a.tsx": `const rows = [` + items.String() + `];
export default () => <ul>{rows.map((r) => <li>{r}</li>)}</ul>`,
		"src/pages/b.tsx": `export default () => <ul>{Array.from({ length: 500 }).map((_, i) => <li>{i}</li>)}</ul>`,
		"src/pages/c.tsx": `import { List } from "../lib/tasks.go";
export default function P() { const tasks = List(); return <ul>{tasks.map((t) => <li>{t.title}</li>)}</ul>; }`,
		"src/pages/d.tsx": `const rows = [1,2,3];
export default () => <ul>{rows.map((r) => <li>{r}</li>)}{Array.from({ length: 20 }).map((_, i) => <li>{i}</li>)}</ul>`,
		"src/pages/e.tsx": `import { List } from "../lib/tasks.go";
export default function P() { return <ul>{List().slice(0, 50).map((t) => <li>{t.title}</li>)}</ul>; }`,
		"src/pages/f.tsx": `import { VirtualList } from "react-virtuoso";
import { List } from "../lib/tasks.go";
export default function P() { return <ul>{List().map((t) => <li>{t.title}</li>)}</ul>; }`,
		"src/pages/g.tsx": `export default () => <p>{[1,2,3].map((n) => n * 2).join(",")}</p>`,
		"src/pages/h.tsx": `import { hostSignal } from "@krate/app/host";
export default function P() { const items = hostSignal([]); return <ul>{items.get().map((t) => <li>{t}</li>)}</ul>; }`,
		"src/lib/tasks.go": "package lib\n\nfunc List() []string { return nil }\n",
	}
	ds := leanOn(t, files, nil)
	var got []string
	for _, d := range ds {
		if d.Code != diag.CodeLeanLongList {
			t.Errorf("unexpected %v", d)
			continue
		}
		got = append(got, d.File)
	}
	want := "src/pages/a.tsx,src/pages/b.tsx,src/pages/c.tsx,src/pages/h.tsx"
	if strings.Join(got, ",") != want {
		t.Errorf("long lists in %v, want %s", got, want)
	}
	ds = leanOn(t, files, func(c *config.Config) { c.Native.Lean.ListItems = 1000 })
	if got := codes(ds); got != "KR7274,KR7274" {
		t.Errorf("with a 1000 item limit only the host lists remain, got %s", got)
	}
}

func TestLeanCodesAreRegistered(t *testing.T) {
	for _, c := range []diag.Code{diag.CodeLeanImageOversize, diag.CodeLeanDataURI, diag.CodeLeanWebFont, diag.CodeLeanCompositing, diag.CodeLeanLongList} {
		if _, ok := diag.Lookup(c); !ok {
			t.Errorf("%s is not registered", c)
		}
	}
}
