package build

import (
	"bytes"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/config"
)

func TestExpandStaticParams(t *testing.T) {
	sets := func(m ...map[string]string) []map[string]string { return m }
	p := func(kv ...string) map[string]string {
		out := map[string]string{}
		for i := 0; i+1 < len(kv); i += 2 {
			out[kv[i]] = kv[i+1]
		}
		return out
	}

	pages, errs := expandStaticParams("/x/blog/[slug].tsx", "blog/[slug].tsx",
		sets(p("slug", "a"), p("slug", "b"), p("slug", "a"), p("slug", "c", "extra", "ok")))
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	var got []string
	for _, pg := range pages {
		got = append(got, pg.OutPath)
	}
	if strings.Join(got, ",") != "blog/a,blog/b,blog/c" {
		t.Errorf("paths = %v, want the unique entries in order", got)
	}

	// Catch-all and optional catch-all values are several segments.
	pages, errs = expandStaticParams("/x/docs/[...path].tsx", "docs/[...path].tsx", sets(p("path", "a/b/c")))
	if len(errs) != 0 || len(pages) != 1 || pages[0].OutPath != "docs/a/b/c" {
		t.Errorf("catch-all: pages=%+v errs=%v", pages, errs)
	}
	pages, errs = expandStaticParams("/x/docs/[[...path]].tsx", "docs/[[...path]].tsx", sets(p("path", "")))
	if len(errs) != 0 || len(pages) != 1 || pages[0].OutPath != "docs" {
		t.Errorf("optional catch-all: pages=%+v errs=%v", pages, errs)
	}

	bad := []struct {
		name, route string
		sets        []map[string]string
		want        string
	}{
		{"missing param", "blog/[slug].tsx", sets(p("slug", "a"), p("id", "x")), `entry #2 {"id":"x"} has no value for [slug]`},
		{"empty value", "blog/[slug].tsx", sets(p("slug", "")), "empty value for [slug]"},
		{"traversal", "blog/[slug].tsx", sets(p("slug", "..")), "not a plain path segment"},
		{"slash in single segment", "blog/[slug].tsx", sets(p("slug", "a/b")), `containing "/"`},
		{"traversal in catch-all", "docs/[...p].tsx", sets(p("p", "a/../../b")), "not a plain path segment"},
		{"backslash", "blog/[slug].tsx", sets(p("slug", `a\b`)), "backslash"},
		{"conflicting entries", "blog/[slug].tsx", sets(p("slug", "a"), p("slug", "a", "lang", "fr")), "same page as entry #1"},
	}
	for _, tc := range bad {
		t.Run(tc.name, func(t *testing.T) {
			pages, errs := expandStaticParams("/x/"+tc.route, tc.route, tc.sets)
			if len(errs) == 0 {
				t.Fatalf("no error; pages = %+v", pages)
			}
			if !strings.Contains(errs[0], tc.want) || !strings.Contains(errs[0], "KR7142") {
				t.Errorf("error = %q, want it to contain %q and KR7142", errs[0], tc.want)
			}
			for _, pg := range pages {
				if strings.Contains(pg.OutPath, "[") || strings.Contains(pg.OutPath, "..") {
					t.Errorf("unsafe output path %q was planned", pg.OutPath)
				}
			}
		})
	}
}

func buildStaticParamsProject(t *testing.T, gsp string) (root string, cfg *config.Config, out string, err error) {
	t.Helper()
	root = t.TempDir()
	writeFileRel(t, root, "src/pages/index.tsx", "export default function Page() { return <p>home</p>; }\n")
	writeFileRel(t, root, "src/pages/blog/[slug].tsx",
		"export function generateStaticParams() { return "+gsp+"; }\n"+
			"export const dynamicParams = false;\n"+
			"export default function Post({ params }: { params: { slug: string } }) { return <h1>{params.slug}</h1>; }\n")
	cfg = config.Default()
	cfg.PagesDir = filepath.Join(root, "src", "pages")
	cfg.OutDir = filepath.Join(root, "dist")
	var buf bytes.Buffer
	SetOutput(&buf)
	defer SetOutput(os.Stdout)
	err = New(root, cfg).BuildAll()
	return root, cfg, buf.String(), err
}

// An entry without the route's param used to be written to the literal path
// blog/[slug]/index.html while the log still counted it.
func TestBuildFailsOnStaticParamsEntryWithoutTheRouteParam(t *testing.T) {
	_, cfg, _, err := buildStaticParamsProject(t, `[{ slug: "a" }, { id: "x" }]`)
	if err == nil {
		t.Fatal("BuildAll succeeded with a generateStaticParams entry that has no slug")
	}
	if msg := err.Error(); !strings.Contains(msg, "entry #2") || !strings.Contains(msg, `{"id":"x"}`) || !strings.Contains(msg, "KR7142") {
		t.Errorf("error does not name the entry:\n%s", msg)
	}
	if _, serr := os.Stat(filepath.Join(cfg.OutDir, "blog", "[slug]", "index.html")); serr == nil {
		t.Error("the literal blog/[slug]/index.html was written")
	}
}

// Duplicate entries are built once, and the logged count is the number of
// pages written.
func TestStaticParamsLogCountMatchesFilesWritten(t *testing.T) {
	_, cfg, out, err := buildStaticParamsProject(t, `[{ slug: "a" }, { slug: "b" }, { slug: "c" }, { slug: "a" }, { slug: "b" }, { slug: 3 }]`)
	if err != nil {
		t.Fatalf("BuildAll: %v", err)
	}
	m := regexp.MustCompile(`Building (\d+) statically generated pages`).FindStringSubmatch(out)
	if m == nil {
		t.Fatalf("no count in the log:\n%s", out)
	}
	files, _ := filepath.Glob(filepath.Join(cfg.OutDir, "blog", "*", "index.html"))
	if len(files) != 4 || m[1] != "4" {
		t.Errorf("logged %s pages, wrote %d files %v; want 4 and 4", m[1], len(files), files)
	}
	for _, f := range files {
		if strings.Contains(f, "[") {
			t.Errorf("literal bracket path written: %s", f)
		}
	}
}

// A numeric value used to make the string-map decode fail halfway, and the
// fallback then appended to the half-filled result: two entries became four,
// some of them empty (which wrote the literal [slug] path).
func TestParseParamSetsWithNonStringValues(t *testing.T) {
	sets, err := parseParamSets([]byte(`[{"slug":"a"},{"slug":3},{"slug":true},{"path":["a","b",4]}]`))
	if err != nil {
		t.Fatal(err)
	}
	want := []map[string]string{{"slug": "a"}, {"slug": "3"}, {"slug": "true"}, {"path": "a/b/4"}}
	if len(sets) != len(want) {
		t.Fatalf("got %d entries %v, want %d", len(sets), sets, len(want))
	}
	for i := range want {
		if !sameParams(sets[i], want[i]) {
			t.Errorf("entry %d = %v, want %v", i+1, sets[i], want[i])
		}
	}
	if _, err := parseParamSets([]byte(`[{"slug":{"a":1}}]`)); err == nil || !strings.Contains(err.Error(), "entry #1") {
		t.Errorf("an object value must be an error naming the entry, got %v", err)
	}
	if _, err := parseParamSets([]byte(`{"slug":"a"}`)); err == nil {
		t.Error("a non-array result must be an error")
	}
}
