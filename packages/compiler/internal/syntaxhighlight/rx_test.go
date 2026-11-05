package syntaxhighlight

import (
	"encoding/json"
	"math/rand"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/dlclark/regexp2/v2"
)

// TestTranspiledRegexesMatchRegexp2 runs patterns through regexp2 (the engine
// chroma uses) and through their JavaScript translation, on random inputs, and
// requires the same match and the same capture groups. The patterns cover what
// differs between .NET and JavaScript regular expressions.
func TestTranspiledRegexesMatchRegexp2(t *testing.T) {
	root := repoRoot(t)
	needNode(t, root)
	patterns := []struct {
		pattern, flags string
		alphabet       string
	}{
		{`[^\W\d]\w*`, "m", "a1_ \u00e9\u0661"}, // regexp2 treats this as \w
		{`[^\W\d]+`, "m", "a1_ -"},
		{`\w+\b`, "m", "ab1_ -\u00e9"},
		{`\d+`, "m", "12\u0661\u0966x"},
		{`\s+`, "m", " \t\n\r\u00a0\u0085\u2028\ufeffx"},
		{`^\s*#`, "m", " \n#a"},
		{`a$`, "m", "a\n\rb"},
		{`a$`, "", "a\n\rb"},
		{`^a`, "", "a\n\rb"},
		{`(?i)abc(?-i)d`, "m", "abcdABCDaBcD\u212a"},
		{`k+`, "mi", "kK\u212aK"},
		{`.+`, "m", "a\n\r\u2028b"},
		{`.+`, "ms", "a\n\r\u2028b"},
		{`[]a]+`, "m", "]a-"},
		{`[^]a]+`, "m", "]ab-"},
		{`(\*|_)((?:(?!\1).)*)(\1)`, "m", "*_ab"},
		{`(a)|b`, "m", "ab"},
		{`(?<=a)b`, "m", "ab"},
		{`(?<!a)b`, "m", "ab"},
		{`[a-c-[b]]+`, "m", "abcd"},
		{`(\p{L}+)(\p{Nd}*)`, "m", "a1\u00e9\u0661-"},
		{`\b\B`, "m", "a b"},
		{`a{2,3}?`, "m", "aaaa"},
		{`(?:a|ab)(?:c|bcd)`, "m", "abcd"},
		{`'(\\['"\\abfnrtv]|\\x[0-9a-fA-F]{2}|[^\\])'`, "m", "'a\\x4'\\"},
	}
	var cases []map[string]interface{}
	var inputs [][]string
	rng := rand.New(rand.NewSource(7))
	for _, p := range patterns {
		res, err := transpileRule(p.pattern, p.flags)
		if err != nil {
			t.Fatalf("%q: %v", p.pattern, err)
		}
		rs := []rune(p.alphabet)
		var ins []string
		for i := 0; i < 400; i++ {
			n := rng.Intn(9)
			b := make([]rune, n)
			for j := range b {
				b[j] = rs[rng.Intn(len(rs))]
			}
			ins = append(ins, string(b))
		}
		cases = append(cases, map[string]interface{}{"src": res.Src, "inputs": ins})
		inputs = append(inputs, ins)
	}
	dir := t.TempDir()
	in, out := filepath.Join(dir, "in.json"), filepath.Join(dir, "out.json")
	data, _ := json.Marshal(cases)
	if err := os.WriteFile(in, data, 0o644); err != nil {
		t.Fatal(err)
	}
	script, _ := filepath.Abs(filepath.Join("testdata", "rx_run.mjs"))
	if o, err := exec.Command("node", script, in, out).CombinedOutput(); err != nil {
		t.Fatalf("node: %v\n%s", err, o)
	}
	raw, _ := os.ReadFile(out)
	var results [][][]interface{}
	if err := json.Unmarshal(raw, &results); err != nil {
		t.Fatal(err)
	}
	for i, p := range patterns {
		full := `\G(?:` + p.pattern + `)`
		if p.flags != "" {
			full = `\G(?` + p.flags + `)(?:` + p.pattern + `)`
		}
		re, err := regexp2.Compile(full, regexp2.None)
		if err != nil {
			t.Fatal(err)
		}
		re.MatchTimeout = time.Second
		for j, text := range inputs[i] {
			m, err := re.FindStringMatch(text)
			if err != nil {
				t.Fatalf("%q on %q: %v", p.pattern, text, err)
			}
			var want []interface{}
			if m != nil && m.RuneIndex == 0 {
				for _, g := range m.Groups() {
					if len(g.Captures) == 0 {
						want = append(want, nil)
					} else {
						want = append(want, g.String())
					}
				}
			}
			got := results[i][j]
			if want == nil && got == nil {
				continue
			}
			if !reflect.DeepEqual(got, want) {
				t.Errorf("%q (flags %q) on %q: JavaScript %v, regexp2 %v\n  translated: %s", p.pattern, p.flags, text, got, want, cases[i]["src"])
				break
			}
		}
	}
}
