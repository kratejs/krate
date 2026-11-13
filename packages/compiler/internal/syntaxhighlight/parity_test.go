package syntaxhighlight

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"unicode/utf8"
)

func repoRoot(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Clean(filepath.Join(wd, "..", "..", "..", ".."))
	if _, err := os.Stat(filepath.Join(root, "packages", "runtime", "src", "highlight.ts")); err != nil {
		t.Fatalf("repo root not found at %s", root)
	}
	return root
}

func needNode(t *testing.T, root string) {
	t.Helper()
	if _, err := exec.LookPath("node"); err != nil {
		skipOrFail(t, "node is not installed")
	}
	if _, err := os.Stat(filepath.Join(root, "packages", "runtime", "node_modules", "esbuild")); err != nil {
		skipOrFail(t, "esbuild is not installed (run pnpm install)")
	}
}

func skipOrFail(t *testing.T, msg string) {
	t.Helper()
	if os.Getenv("KRATE_REQUIRE_E2E") != "" {
		t.Fatal(msg)
	}
	t.Skip(msg)
}

type parityCase struct {
	ID    string `json:"id"`
	Lang  string `json:"lang"`
	File  string `json:"file"`
	Lines string `json:"lines,omitempty"`
}

type parityOut struct {
	ID    string  `json:"id"`
	HTML  *string `json:"html"`
	MS    float64 `json:"ms"`
	Error string  `json:"error"`
}

// runTokenizer tokenizes the cases with the runtime's tokenizer and returns
// the HTML by case id.
func runTokenizer(t *testing.T, root string, b *Bundle, cases []parityCase) map[string]parityOut {
	t.Helper()
	dir := t.TempDir()
	bundle := filepath.Join(dir, "bundle.json")
	casesFile := filepath.Join(dir, "cases.json")
	outFile := filepath.Join(dir, "out.json")
	if err := os.WriteFile(bundle, b.JSON(), 0o644); err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(cases)
	if err := os.WriteFile(casesFile, data, 0o644); err != nil {
		t.Fatal(err)
	}
	script, _ := filepath.Abs(filepath.Join("testdata", "hl_run.mjs"))
	cmd := exec.Command("node", "--max-old-space-size=4096", script, root, bundle, casesFile, outFile)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("tokenizer run failed: %v\n%s", err, out)
	}
	raw, err := os.ReadFile(outFile)
	if err != nil {
		t.Fatal(err)
	}
	var outs []parityOut
	if err := json.Unmarshal(raw, &outs); err != nil {
		t.Fatal(err)
	}
	m := map[string]parityOut{}
	for _, o := range outs {
		m[o.ID] = o
	}
	return m
}

// firstDiff describes where two strings first differ.
func firstDiff(got, want string) string {
	n := len(got)
	if len(want) < n {
		n = len(want)
	}
	i := 0
	for i < n && got[i] == want[i] {
		i++
	}
	lo := i - 80
	if lo < 0 {
		lo = 0
	}
	hiG, hiW := i+120, i+120
	if hiG > len(got) {
		hiG = len(got)
	}
	if hiW > len(want) {
		hiW = len(want)
	}
	return fmt.Sprintf("at byte %d\n  got:  ...%s\n  want: ...%s", i, strings.ReplaceAll(got[lo:hiG], "\n", `\n`), strings.ReplaceAll(want[lo:hiW], "\n", `\n`))
}

// referenceMu runs the rechecks of recheckReference one at a time.
var referenceMu sync.Mutex

// recheckReference recomputes chroma's output for a case that differs. chroma
// gives each rule's regexp a 250ms match timeout, and a match that times out
// (a loaded runner with the languages tokenized in parallel) becomes an error
// token, so one reference can be wrong. A real difference stays.
func recheckReference(t *testing.T, c parityCase, got, want string) string {
	t.Helper()
	referenceMu.Lock()
	defer referenceMu.Unlock()
	for i := 0; i < 2 && got != want; i++ {
		want = expectedHTML(t, c.Lang, c.File, c.Lines)
	}
	return want
}

func expectedHTML(t *testing.T, lang, file, lines string) string {
	t.Helper()
	raw, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	return HighlightThemeLines(strings.TrimSpace(string(raw)), NormalizeLanguage(lang), DefaultTheme, lines)
}

// langStats is what a parity run measured for one language.
type langStats struct {
	Lexer      string  `json:"lexer"`
	Lang       string  `json:"lang"`
	Files      int     `json:"files"`
	Variants   int     `json:"variants"`
	SourceKB   int     `json:"sourceKB"`
	Tokens     int     `json:"tokens"`
	Identical  int     `json:"identical"`
	Declined   int     `json:"declined"`
	TableGzip  int     `json:"tableGzipBytes"`
	WithDeps   int     `json:"tableWithDepsGzipBytes"`
	TokenizeMS float64 `json:"tokenizerMs"`
}

var (
	statsMu  sync.Mutex
	allStats []langStats
)

var lineSpecs = []string{"1,3-5", "2-4,3-8,12", "9,1", "0,abc,5-3,2-2,7", "1-1000"}

// mutations writes mutated copies of a file: cut off part-way (an unterminated
// string, comment or block), with CRLF line endings, and without the final
// newline. They reach the lexers' error and recovery paths.
func mutations(t *testing.T, dir string, src string, seq int) []string {
	t.Helper()
	data, err := os.ReadFile(src)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	var out []string
	write := func(name, content string) {
		p := filepath.Join(dir, fmt.Sprintf("%d_%s%s", seq, name, filepath.Ext(src)))
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		out = append(out, p)
	}
	cut := func(frac float64) string {
		n := int(float64(len(text)) * frac)
		for n < len(text) && n > 0 && !utf8.RuneStart(text[n]) {
			n++
		}
		return text[:n]
	}
	write("cut1", cut(0.37))
	write("cut2", cut(0.71))
	write("cut3", cut(0.93))
	write("crlf", strings.ReplaceAll(strings.ReplaceAll(text, "\r\n", "\n"), "\n", "\r\n"))
	write("nonl", strings.TrimRight(text, "\n"))
	return out
}

func gzipSize(b []byte) int {
	var buf bytes.Buffer
	w, _ := gzip.NewWriterLevel(&buf, gzip.BestCompression)
	_, _ = w.Write(b)
	_ = w.Close()
	return buf.Len()
}

func countTokens(html string) int {
	return strings.Count(html, `<span class="`) - 2*strings.Count(html, `<span class="line`)
}

// TestParityCorpus compares the runtime tokenizer with chroma, token for
// token, on real-world code for every shipped language: this repository, the
// Go modules it depends on, chroma's own samples and a vendored corpus, plus
// truncated, CRLF and newline-less copies and line-highlight specs. Any
// difference fails the test, including one that appears when chroma is
// upgraded.
func TestParityCorpus(t *testing.T) {
	root := repoRoot(t)
	needNode(t, root)
	for _, spec := range corpusSpecs {
		for _, lang := range spec.langs {
			spec, lang := spec, lang
			t.Run(lang, func(t *testing.T) {
				t.Parallel()
				vendored := vendoredFiles(t, spec)
				files := corpusFor(t, spec, vendored)
				files = append(files, chromaSamples(t, spec.lexer)...)
				if len(files) == 0 {
					t.Fatalf("no sample files found for %s", lang)
				}
				langs := []string{spec.lexer}
				if spec.lexer == "markdown" {
					langs = ShippedLanguages
				}
				bundle, err := BuildBundle(langs)
				if err != nil {
					t.Fatal(err)
				}
				var cases []parityCase
				id := 0
				add := func(f, lines string) {
					cases = append(cases, parityCase{ID: strconv.Itoa(id), Lang: lang, File: f, Lines: lines})
					id++
				}
				sourceBytes := 0
				for _, f := range files {
					add(f, "")
					if st, err := os.Stat(f); err == nil {
						sourceBytes += int(st.Size())
					}
				}
				nSource := len(cases)
				mdir := t.TempDir()
				used := 0
				sorted := append([]string(nil), files...)
				sort.Slice(sorted, func(i, j int) bool { return pathKey(sorted[i]) < pathKey(sorted[j]) })
				for i, f := range sorted {
					st, err := os.Stat(f)
					if err != nil {
						continue
					}
					if used >= variantBudget {
						break
					}
					used += int(st.Size())
					for _, m := range mutations(t, mdir, f, i) {
						add(m, "")
					}
				}
				for i, f := range sorted {
					if i >= len(lineSpecs) {
						break
					}
					add(f, lineSpecs[i])
				}
				got := runTokenizer(t, root, bundle, cases)
				st := langStats{Lexer: spec.lexer, Lang: lang, Files: nSource, Variants: len(cases) - nSource, SourceKB: sourceBytes >> 10}
				failures := 0
				for _, c := range cases {
					o := got[c.ID]
					switch {
					case o.Error != "":
						failures++
						t.Errorf("%s: tokenizer error: %.300s", c.File, o.Error)
					case o.HTML == nil:
						// Only markdown may decline (a fence names a lexer outside the
						// shipped set); everything else must highlight.
						if spec.lexer != "markdown" {
							failures++
							t.Errorf("%s: tokenizer declined", c.File)
						}
						st.Declined++
					default:
						want := expectedHTML(t, c.Lang, c.File, c.Lines)
						if *o.HTML != want {
							want = recheckReference(t, c, *o.HTML, want)
						}
						st.TokenizeMS += o.MS
						if *o.HTML != want {
							failures++
							if failures <= 3 {
								t.Errorf("%s (lines %q) differs from chroma %s", c.File, c.Lines, firstDiff(*o.HTML, want))
							}
						} else {
							st.Identical++
							st.Tokens += countTokens(want)
						}
					}
					if failures > 12 {
						break
					}
				}
				one, _ := ExportLexer(spec.lexer)
				st.TableGzip = gzipSize(one.JSON())
				st.WithDeps = gzipSize(bundleJSONFor(t, spec.lexer))
				statsMu.Lock()
				allStats = append(allStats, st)
				statsMu.Unlock()
				t.Logf("%s: %d files + %d variants, %d KB source, %d tokens identical, %d declined, table %d B gz (%d B with deps)",
					lang, st.Files, st.Variants, st.SourceKB, st.Tokens, st.Declined, st.TableGzip, st.WithDeps)
			})
		}
	}
}

func bundleJSONFor(t *testing.T, lexer string) []byte {
	b, err := BuildBundle([]string{lexer})
	if err != nil {
		t.Fatal(err)
	}
	return b.JSON()
}

func TestMain(m *testing.M) {
	code := m.Run()
	if path := os.Getenv("KRATE_HL_REPORT"); path != "" && len(allStats) > 0 {
		sort.Slice(allStats, func(i, j int) bool { return allStats[i].Lang < allStats[j].Lang })
		data, _ := json.MarshalIndent(allStats, "", "  ")
		_ = os.WriteFile(path, data, 0o644)
	}
	os.Exit(code)
}
