package build

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/config"
	"github.com/kratejs/krate/packages/compiler/internal/css"
)

func pages(idx ...int) pageSet {
	var s pageSet
	for _, i := range idx {
		s.add(i)
	}
	return s
}

func TestPlanCSSSheets(t *testing.T) {
	all := pages(0, 1, 2, 3, 4, 5, 6, 7, 8, 9)
	cases := []struct {
		name  string
		gz    []int
		users []pageSet
		want  []int
	}{
		{
			// A theme on every page and the page's own CSS stay apart.
			name: "shared then own", gz: []int{8000, 3000}, users: []pageSet{all, pages(0)}, want: []int{1, 2},
		},
		{
			// Sources always used together are one file.
			name: "same pages", gz: []int{300, 400, 500, 8000}, users: []pageSet{pages(0, 1), pages(0, 1), pages(0, 1), all}, want: []int{3, 4},
		},
		{
			// A small source used on fewer pages is not folded into the
			// theme every page shares: it stays a file of its own.
			name: "small stays out of the shared theme", gz: []int{600, 8000}, users: []pageSet{pages(0, 1, 2, 3), all}, want: []int{1, 2},
		},
		{
			// A small source used on nearly the same pages as its neighbour
			// joins it.
			name: "small joins alike neighbour", gz: []int{1000, 600, 8000}, users: []pageSet{pages(0, 1, 2, 3), pages(0, 1, 2, 3, 4), all}, want: []int{2, 3},
		},
		{
			// Never more than maxPageSheets files.
			name: "cap", gz: []int{5000, 5000, 5000, 5000, 5000, 5000, 5000},
			users: []pageSet{pages(0), pages(1, 0), pages(2, 0), pages(3, 0), pages(4, 0), pages(5, 0), pages(6, 0)},
			want:  nil,
		},
	}
	for _, c := range cases {
		got := planCSSSheets(c.gz, c.users)
		if c.want == nil {
			if len(got) != maxPageSheets || got[len(got)-1] != len(c.gz) {
				t.Errorf("%s: got %v, want %d groups ending at %d", c.name, got, maxPageSheets, len(c.gz))
			}
			continue
		}
		if !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
		}
	}
}

// bigCSS returns n distinct rules, enough to be over cssRequestCost gzipped.
func bigCSS(prefix string, n int) string {
	var sb strings.Builder
	x := uint32(2463534242)
	next := func() uint32 { x ^= x << 13; x ^= x >> 17; x ^= x << 5; return x }
	for i := 0; i < n; i++ {
		fmt.Fprintf(&sb, ".%s-%d{color:#%06x;margin:%dpx %dpx}\n", prefix, i, next()%0xffffff, next()%97, next()%89)
	}
	return sb.String()
}

var sheetLinkRe = regexp.MustCompile(`href="?/(styles\.[a-z0-9]+\.css)`)

func linkedSheets(t *testing.T, path string) []string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, m := range sheetLinkRe.FindAllStringSubmatch(string(data), -1) {
		out = append(out, m[1])
	}
	return out
}

// TestPageCSSSplitBySource: two pages importing the same stylesheet link one
// shared file for it, followed by a file with their own CSS, in import order.
func TestPageCSSSplitBySource(t *testing.T) {
	root := t.TempDir()
	writeProjectFile(t, root, "src/lib/shared.css", bigCSS("shared", 400))
	writeProjectFile(t, root, "src/pages/a.css", ".own-a{color:red}")
	writeProjectFile(t, root, "src/pages/b.css", ".own-b{color:blue}")
	writePage(t, root, "a.tsx", "import '../lib/shared.css';\nimport './a.css';\nexport default function A() { return <h1 class=\"own-a shared-1\">A</h1>; }")
	writePage(t, root, "b.tsx", "import '../lib/shared.css';\nimport './b.css';\nexport default function B() { return <h1 class=\"own-b shared-2\">B</h1>; }")
	cfg := config.Default()
	cfg.Resolve(root)
	cfg.Tailwind.Enabled = false
	if err := New(root, cfg).BuildAll(); err != nil {
		t.Fatal(err)
	}
	a := linkedSheets(t, filepath.Join(cfg.OutDir, "a", "index.html"))
	b := linkedSheets(t, filepath.Join(cfg.OutDir, "b", "index.html"))
	if len(a) != 2 || len(b) != 2 {
		t.Fatalf("want 2 stylesheets per page, got a=%v b=%v", a, b)
	}
	if a[0] != b[0] {
		t.Fatalf("the shared stylesheet should be one file: a=%v b=%v", a, b)
	}
	read := func(f string) string {
		data, err := os.ReadFile(filepath.Join(cfg.OutDir, f))
		if err != nil {
			t.Fatal(err)
		}
		return string(data)
	}
	if s := read(a[0]); !strings.Contains(s, ".shared-399") || strings.Contains(s, "own-a") || strings.Contains(s, "krate:css-part") {
		t.Fatalf("shared sheet has the wrong content: %.200q", s)
	}
	if s := read(a[1]); !strings.Contains(s, ".own-a") || strings.Contains(s, "shared-") {
		t.Fatalf("page sheet has the wrong content: %.200q", s)
	}
}

// TestPageCSSDevModeOneSheet: a dev build keeps one stylesheet per page, in
// the same source order.
func TestPageCSSDevModeOneSheet(t *testing.T) {
	root := t.TempDir()
	cfg := config.Default()
	cfg.Resolve(root)
	cfg.Minify = false
	b := New(root, cfg)
	b.DevMode = true
	if err := os.MkdirAll(cfg.OutDir, 0o755); err != nil {
		t.Fatal(err)
	}
	raw := css.PartMarker("/shared.css") + bigCSS("shared", 200) + css.PartMarker("") + ".own{color:red}"
	r := &PageResult{Page: "a.tsx", OutName: "a", CSS: raw}
	if _, err := b.writePageCSS([]*PageResult{r}); err != nil {
		t.Fatal(err)
	}
	if len(r.CSSFiles) != 1 {
		t.Fatalf("dev build: want one sheet, got %v", r.CSSFiles)
	}
	data, err := os.ReadFile(filepath.Join(cfg.OutDir, r.CSSFiles[0]))
	if err != nil {
		t.Fatal(err)
	}
	s := string(data)
	if strings.Contains(s, "krate:css-part") || strings.Index(s, ".shared-0") > strings.Index(s, ".own") {
		t.Fatalf("dev sheet lost the source order or kept a marker: %.200q", s)
	}
}
