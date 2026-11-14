package syntaxhighlight

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
)

type advResult struct {
	Name    string  `json:"name"`
	Len     int     `json:"len"`
	MS      float64 `json:"ms"`
	Timeout bool    `json:"timeout"`
	Error   string  `json:"error"`
}

// advBudgetMS is the most a hostile snippet of the size cap may take. The
// tokenizer runs on the main thread, so this is how long a page can stall.
const advBudgetMS = 1500

// advLangs are the languages TestAdversarialInputs checks by default: the
// ones with the most complex patterns, and markdown, whose bundle carries
// every shipped lexer. KRATE_HL_CORPUS=full checks every language.
var advLangs = map[string]bool{"ts": true, "markdown": true, "bash": true, "html": true}

// TestAdversarialInputs feeds hostile text at the size the runtime accepts
// (see HIGHLIGHT_MAX_LENGTH; 20000 here, KRATE_HL_ADV_SIZE=65536 and
// HL_ADV_FULL=1 run the full set): runs of every delimiter, unterminated
// strings and comments, deep nesting, long words and lines. A regex that
// backtracks catastrophically on any of them fails the test.
func TestAdversarialInputs(t *testing.T) {
	root := repoRoot(t)
	needNode(t, root)
	size := 20000
	if os.Getenv("KRATE_HL_ADV_SIZE") != "" {
		size, _ = strconv.Atoi(os.Getenv("KRATE_HL_ADV_SIZE"))
	}
	for _, spec := range corpusSpecs {
		spec := spec
		lang := spec.langs[0]
		if lang == "tsx" || lang == "jsx" {
			continue
		}
		if !advLangs[lang] && os.Getenv("KRATE_HL_CORPUS") != "full" {
			continue
		}
		t.Run(lang, func(t *testing.T) {
			langs := []string{spec.lexer}
			if spec.lexer == "markdown" {
				langs = ShippedLanguages
			}
			b, err := BuildBundle(langs)
			if err != nil {
				t.Fatal(err)
			}
			dir := t.TempDir()
			bundle := filepath.Join(dir, "bundle.json")
			out := filepath.Join(dir, "out.json")
			if err := os.WriteFile(bundle, b.JSON(), 0o644); err != nil {
				t.Fatal(err)
			}
			script, _ := filepath.Abs(filepath.Join("testdata", "hl_adversarial.mjs"))
			cmd := exec.Command("node", script, root, bundle, lang, out, strconv.Itoa(advBudgetMS*3), strconv.Itoa(size))
			cmd.Env = os.Environ()
			if o, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("adversarial run failed: %v\n%s", err, o)
			}
			raw, err := os.ReadFile(out)
			if err != nil {
				t.Fatal(err)
			}
			var res []advResult
			if err := json.Unmarshal(raw, &res); err != nil {
				t.Fatal(err)
			}
			worst := advResult{}
			for _, r := range res {
				switch {
				case r.Error != "":
					t.Errorf("%s: input %s: %s", lang, r.Name, r.Error)
				case r.Timeout:
					t.Errorf("%s: input %s (%d chars) did not finish in %d ms", lang, r.Name, r.Len, advBudgetMS*3)
				case r.MS > advBudgetMS:
					t.Errorf("%s: input %s (%d chars) took %.0f ms (budget %d)", lang, r.Name, r.Len, r.MS, advBudgetMS)
				}
				if r.MS > worst.MS {
					worst = r
				}
			}
			t.Logf("%s: %d hostile inputs of %d chars, slowest %s at %.0f ms", lang, len(res), size, worst.Name, worst.MS)
		})
	}
}
