package bundler

import (
	"os"
	"path/filepath"
	"regexp"
	"testing"
)

func TestAliasFilterCoversResolveAlias(t *testing.T) {
	dir := t.TempDir()
	for _, f := range []string{"src/a/b.ts", "src/lib.ts", "src/index.ts", "shared/util.ts"} {
		p := filepath.Join(dir, filepath.FromSlash(f))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("export {}\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	r := NewImportResolver(
		[]string{"@/*", "~lib", "#shared/*", "app.v2/*", "./rel/*", "/rooted/*", "*"},
		[][]string{{"src/*"}, {"src/lib.ts"}, {"shared/*"}, {"src/*"}, {"src/*"}, {"src/*"}, {"src/*"}},
		dir,
	)
	re := regexp.MustCompile(r.AliasFilter())
	cases := []string{
		"@/a/b", "@/lib", "@", "@/", "~lib", "~lib/x", "#shared/util", "#shared", "app.v2/lib", "appxv2/lib",
		"./rel/lib", "rel/lib", "/rooted/lib", "react", "@scope/pkg", "../x", "node:fs", "*", "", `C:\x`,
	}
	for _, spec := range cases {
		resolved := r.ResolveAlias(spec) != ""
		matched := re.MatchString(spec)
		rooted := regexp.MustCompile(`^[./]`).MatchString(spec) // never reached the plugin: the old filter was ^[^./]
		if resolved && !matched && !rooted {
			t.Errorf("filter excludes %q, which ResolveAlias resolves", spec)
		}
	}
	for _, spec := range []string{"@/a/b", "@/lib", "~lib", "#shared/util", "app.v2/lib"} {
		if r.ResolveAlias(spec) == "" {
			t.Fatalf("ResolveAlias(%q) is empty; fixture is wrong", spec)
		}
	}
	for _, spec := range []string{"react", "@scope/pkg", "~libx", "#sharedx/util", "./rel/lib", "/rooted/lib", "node:fs"} {
		if re.MatchString(spec) {
			t.Errorf("filter matches %q, which no alias covers", spec)
		}
	}
}

func TestAliasFilterWithoutAliasesMatchesNothing(t *testing.T) {
	dir := t.TempDir()
	for _, r := range []*ImportResolver{nil, {}, NewImportResolver(nil, nil, dir), NewImportResolver([]string{"@/*"}, [][]string{{"src/*"}}, "")} {
		re := regexp.MustCompile(r.AliasFilter())
		for _, s := range []string{"", "react", "@/x", "@scope/pkg", "~lib", "\x00"} {
			if re.MatchString(s) {
				t.Errorf("filter of a resolver without usable aliases matched %q", s)
			}
		}
	}
}
