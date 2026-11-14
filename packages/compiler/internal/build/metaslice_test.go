package build

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSliceModuleKeepsTheClosureOnly(t *testing.T) {
	write := func(src string) string {
		p := filepath.Join(t.TempDir(), "page.tsx")
		if err := os.WriteFile(p, []byte(src), 0o644); err != nil {
			t.Fatal(err)
		}
		return p
	}
	sl, ok := sliceModule(write(`import { seo } from './seo';
import { Chart } from './chart';
import type { Metadata } from 'next';
const base = { site: 'S' };
const hero = <Chart />;
export const metadata: Metadata = seo(base.site, { x: 1 });
export default function Page() { return <main>{hero}</main>; }
`), []string{"metadata"})
	if !ok {
		t.Fatal("not sliced")
	}
	for _, want := range []string{`import { seo } from "./seo"`, "const base", "export const metadata"} {
		if !strings.Contains(sl.Source, want) {
			t.Errorf("slice lacks %q:\n%s", want, sl.Source)
		}
	}
	for _, bad := range []string{"Chart", "hero", "Page"} {
		if strings.Contains(sl.Source, bad) {
			t.Errorf("slice keeps %q:\n%s", bad, sl.Source)
		}
	}

	// Top-level code that changes what the metadata reads cannot be cut away.
	if _, ok := sliceModule(write("let title = 'a';\ntitle = title + 'b';\nexport const metadata = { title };\n"), []string{"metadata"}); ok {
		t.Error("a let reassigned by top-level code was sliced")
	}
	if _, ok := sliceModule(write("const base = { title: 'a' };\nregister(base);\nexport const metadata = base;\n"), []string{"metadata"}); ok {
		t.Error("a binding used by a top-level call was sliced")
	}
}
