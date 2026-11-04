package css

import "testing"

func TestAbsolutizeSourcePaths(t *testing.T) {
	in := `@import "tailwindcss" source("../app"); @source "../x"; @source not '../y'; @source inline("p-1"); @source "/abs";`
	want := `@import "tailwindcss" source("/r/a/app"); @source "/r/a/x"; @source not '/r/a/y'; @source inline("p-1"); @source "/abs";`
	if got := AbsolutizeSourcePaths(in, "/r/a/b"); got != want {
		t.Errorf("got  %s\nwant %s", got, want)
	}
}

func TestImportTailwindModes(t *testing.T) {
	src := `@import "tailwindcss"; @import "tailwindcss/preflight"; a{b:c}`
	var warns []string
	opts := ImportOptions{Warn: func(m string) { warns = append(warns, m) }, Tailwind: TailwindKeep}
	out := InlineImportsWith(src, t.TempDir(), opts)
	if len(warns) != 0 || out != src {
		t.Errorf("keep: warns=%v out=%q", warns, out)
	}
	opts.Tailwind = TailwindOff
	InlineImportsWith(src, t.TempDir(), opts)
	if len(warns) != 2 {
		t.Errorf("off: want 2 warnings, got %v", warns)
	}
}
