package css

import (
	"strings"
	"testing"
)

func scanSet(src string) map[string]bool {
	m := map[string]bool{}
	for _, c := range candidateTokens(src) {
		m[c] = true
	}
	return m
}

func wantScanned(t *testing.T, src string, want ...string) {
	t.Helper()
	got := scanSet(src)
	for _, w := range want {
		if !got[w] {
			t.Errorf("scanner lost %q from %q; got %v", w, src, got)
		}
	}
}

func TestScanArbitraryValues(t *testing.T) {
	toks := []string{
		"data-[state=open]:p-4", "aria-[sort=ascending]:underline", "[&_p]:mt-2",
		"[&:nth-child(3)]:underline", "[&>svg]:size-4", "has-[>img]:p-2",
		"nth-[2n+1]:bg-red-500", "content-['x']", "bg-[url('/a.png')]",
		"bg-(--brand)", "hover:bg-(--brand)/50", "w-[calc(100%-2rem)]",
		"[mask-type:alpha]", "@[400px]:grid", "supports-[display:grid]:grid",
		"content-[')']", "-mt-2", "w-1/2", "p-0.5", "!font-bold", "font-bold!",
	}
	wantScanned(t, `<div class="`+strings.Join(toks, " ")+`">`, toks...)
	// Each token also survives when quoted and adjacent to punctuation.
	for _, tok := range toks {
		wantScanned(t, `cn("`+tok+`", cond && 'p-9')`, tok, "p-9")
	}
}
