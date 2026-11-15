package migrate

import "testing"

func TestStaticEnvDirForms(t *testing.T) {
	for src, want := range map[string]string{
		`'../..')`: "../..",
		`path.resolve(__dirname, '../..'), true, undefined, true)`:          "../..",
		`path.resolve(import.meta.dirname, '../..'))`:                       "../..",
		`join(process.cwd(), '..', '..'))`:                                  "../..",
		`fileURLToPath(new URL('../..', import.meta.url)))`:                 "../..",
		`path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..'))`: "..",
		`process.cwd())`: "",
	} {
		got, ok := staticEnvDir(src)
		if !ok || got != want {
			t.Errorf("staticEnvDir(%q) = %q, %v; want %q", src, got, ok, want)
		}
	}
	for _, src := range []string{`process.env.ROOT)`, `'/abs/dir')`, `resolveRoot())`, `path.resolve(__dirname, dir))`} {
		if got, ok := staticEnvDir(src); ok {
			t.Errorf("staticEnvDir(%q) = %q, want unresolved", src, got)
		}
	}
}
