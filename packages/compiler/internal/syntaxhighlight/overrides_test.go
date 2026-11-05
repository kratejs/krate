package syntaxhighlight

import (
	"math/rand"
	"testing"
	"time"

	"github.com/dlclark/regexp2/v2"
)

// TestPatternOverrides checks that each replacement pattern matches exactly the
// text its original does, using regexp2 (the engine chroma runs) on random
// strings built from the characters the patterns care about.
func TestPatternOverrides(t *testing.T) {
	alphabet := []rune("ABZ09_ab -r#\u00e9.")
	for lexer, m := range patternOverrides {
		for orig, repl := range m {
			compile := func(p string) *regexp2.Regexp {
				re, err := regexp2.Compile(`\G(?m)(?:`+p+`)`, regexp2.None)
				if err != nil {
					t.Fatalf("%s: %q: %v", lexer, p, err)
				}
				re.MatchTimeout = 2 * time.Second
				return re
			}
			a, b := compile(orig), compile(repl)
			rng := rand.New(rand.NewSource(1))
			for i := 0; i < 200000; i++ {
				n := rng.Intn(11)
				rs := make([]rune, n)
				for j := range rs {
					rs[j] = alphabet[rng.Intn(len(alphabet))]
				}
				text := rs
				ma, errA := a.FindRunesMatch(text)
				mb, errB := b.FindRunesMatch(text)
				if errA != nil || errB != nil {
					t.Fatalf("%s: match error on %q: %v %v", lexer, string(text), errA, errB)
				}
				if (ma == nil) != (mb == nil) || (ma != nil && (ma.RuneIndex != mb.RuneIndex || ma.RuneLength != mb.RuneLength)) {
					t.Fatalf("%s: %q and %q disagree on %q: %v vs %v", lexer, orig, repl, string(text), ma, mb)
				}
			}
		}
	}
}
