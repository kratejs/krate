package conformance

import (
	"encoding/json"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/evanw/esbuild/pkg/api"

	"github.com/kratejs/krate/packages/compiler/internal/astprint"
)

var update = flag.Bool("update", false, "rewrite testdata/baseline.json from the current results")

const baselinePath = "testdata/baseline.json"

// Case is one corpus entry.
type Case struct {
	ID       string
	Suite    string // e.g. "test262/positive"
	Src      string
	JSX      bool
	Negative bool
}

type status int

const (
	statusPass status = iota
	statusFail
	statusSkip
)

type result struct {
	Case   Case
	Status status
	Reason string
}

// ---------------------------------------------------------------- loading

func loadCases(t *testing.T) []Case {
	var cases []Case
	cases = append(cases, loadTest262(t)...)
	cases = append(cases, loadBabel(t)...)
	cases = append(cases, loadHandwritten(t, "testdata/handwritten/jsx.txt", "jsx-handwritten", true)...)
	cases = append(cases, loadHandwritten(t, "testdata/handwritten/es.txt", "es-handwritten", false)...)
	sort.Slice(cases, func(i, j int) bool { return cases[i].ID < cases[j].ID })
	return cases
}

func walkFiles(t *testing.T, root string, match func(path string) bool) []string {
	var out []string
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && match(p) {
			out = append(out, p)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", root, err)
	}
	sort.Strings(out)
	return out
}

func loadTest262(t *testing.T) []Case {
	root := "testdata/test262"
	var cases []Case
	for _, p := range walkFiles(t, root, func(p string) bool { return strings.HasSuffix(p, ".js") }) {
		b, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		src := string(b)
		meta := ""
		if i := strings.Index(src, "/*---"); i >= 0 {
			if j := strings.Index(src[i:], "---*/"); j >= 0 {
				meta = src[i+5 : i+j]
			}
		}
		neg := strings.Contains(meta, "negative:")
		if strings.Contains(meta, "onlyStrict") {
			src = "\"use strict\";\n" + src
		}
		rel, _ := filepath.Rel(root, p)
		suite := "test262/positive"
		if neg {
			suite = "test262/negative"
		}
		cases = append(cases, Case{
			ID:       "test262:" + filepath.ToSlash(rel),
			Suite:    suite,
			Src:      src,
			JSX:      false,
			Negative: neg,
		})
	}
	return cases
}

func loadBabel(t *testing.T) []Case {
	root := "testdata/babel-jsx"
	var cases []Case
	for _, p := range walkFiles(t, root, func(p string) bool { return filepath.Base(p) == "input.js" }) {
		b, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		rel, _ := filepath.Rel(root, filepath.Dir(p))
		rel = filepath.ToSlash(rel)
		neg := strings.HasPrefix(rel, "errors/")
		suite := "babel-jsx/positive"
		if neg {
			suite = "babel-jsx/negative"
		}
		cases = append(cases, Case{ID: "babel-jsx:" + rel, Suite: suite, Src: string(b), JSX: true, Negative: neg})
	}
	return cases
}

// loadHandwritten reads a file of "=== name" or "=== name !" headers, each
// followed by the source of one case. A trailing "!" marks a must-error case.
func loadHandwritten(t *testing.T, path, suite string, jsx bool) []Case {
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var cases []Case
	var cur *Case
	var body []string
	flush := func() {
		if cur != nil {
			cur.Src = strings.Join(body, "\n") + "\n"
			cases = append(cases, *cur)
		}
	}
	for _, line := range strings.Split(strings.TrimRight(string(b), "\n"), "\n") {
		if strings.HasPrefix(line, "=== ") {
			flush()
			name := strings.TrimSpace(strings.TrimPrefix(line, "=== "))
			neg := false
			if strings.HasSuffix(name, " !") {
				neg = true
				name = strings.TrimSpace(strings.TrimSuffix(name, "!"))
			}
			s := suite + "/positive"
			if neg {
				s = suite + "/negative"
			}
			cur = &Case{ID: suite + ":" + name, Suite: s, JSX: jsx, Negative: neg}
			body = nil
			continue
		}
		body = append(body, line)
	}
	flush()
	return cases
}

// ---------------------------------------------------------------- oracle

// normalize runs src through esbuild so that two sources with the same
// syntax tree print identically (comments, quotes, parens, whitespace and
// JSX are all canonicalized). With minifySyntax set, esbuild also folds
// semantically neutral sugar (redundant braces, empty statements, key
// quoting) so printer style choices do not count as failures.
func normalize(src string, jsx, minifySyntax bool) (string, error) {
	loader := api.LoaderJS
	if jsx {
		loader = api.LoaderJSX
	}
	res := api.Transform(src, api.TransformOptions{
		Loader:            loader,
		JSX:               api.JSXTransform,
		JSXFactory:        "h",
		JSXFragment:       "F",
		MinifyWhitespace:  true,
		MinifySyntax:      minifySyntax,
		MinifyIdentifiers: false,
		Target:            api.ESNext,
		LegalComments:     api.LegalCommentsNone,
		Charset:           api.CharsetUTF8,
		LogLevel:          api.LogLevelSilent,
		Sourcefile:        "input.js",
	})
	if len(res.Errors) > 0 {
		return "", fmt.Errorf("%s", res.Errors[0].Text)
	}
	return strings.TrimSpace(string(res.Code)), nil
}

type parsed struct {
	printed string
	errs    []error
	panic   any
}

func krateParse(src string) (out parsed) {
	defer func() {
		if r := recover(); r != nil {
			out.panic = r
		}
	}()
	prog, errs, _ := astprint.Parse(src)
	out.errs = errs
	if len(errs) == 0 {
		out.printed = astprint.Print(prog)
	}
	return out
}

// parseWithTimeout guards against a parser that never terminates.
func parseWithTimeout(src string) (parsed, bool) {
	ch := make(chan parsed, 1)
	go func() { ch <- krateParse(src) }()
	select {
	case p := <-ch:
		return p, true
	case <-time.After(10 * time.Second):
		return parsed{}, false
	}
}

func short(s string, n int) string {
	s = strings.ReplaceAll(s, "\n", "\\n")
	if len(s) > n {
		return s[:n] + "..."
	}
	return s
}

// firstDiff describes where two strings first differ.
func firstDiff(a, b string) string {
	i := 0
	for i < len(a) && i < len(b) && a[i] == b[i] {
		i++
	}
	lo := i - 20
	if lo < 0 {
		lo = 0
	}
	cut := func(s string) string {
		hi := i + 40
		if hi > len(s) {
			hi = len(s)
		}
		if lo > len(s) {
			return ""
		}
		return s[lo:hi]
	}
	return fmt.Sprintf("want ...%s... got ...%s...", cut(a), cut(b))
}

func runCase(c Case) result {
	r := result{Case: c}
	p, ok := parseWithTimeout(c.Src)
	switch {
	case !ok:
		r.Status, r.Reason = statusFail, "timeout"
		return r
	case p.panic != nil:
		r.Status, r.Reason = statusFail, fmt.Sprintf("panic: %v", p.panic)
		return r
	}
	if c.Negative {
		if len(p.errs) > 0 {
			r.Status = statusPass
		} else {
			r.Status, r.Reason = statusFail, "accepted invalid syntax"
		}
		return r
	}
	want, err := normalize(c.Src, c.JSX, false)
	if err != nil {
		r.Status, r.Reason = statusSkip, "esbuild rejects original: "+short(err.Error(), 80)
		return r
	}
	if len(p.errs) > 0 {
		r.Status, r.Reason = statusFail, "parse error: "+short(p.errs[0].Error(), 160)
		return r
	}
	got, err := normalize(p.printed, c.JSX, false)
	if err != nil {
		r.Status, r.Reason = statusFail, "printed output invalid: "+short(err.Error(), 80)
		return r
	}
	if got != want {
		// Retry with esbuild's syntax normalization: braces, empty
		// statements and key quoting are style, not a miscompile.
		w2, e1 := normalize(c.Src, c.JSX, true)
		g2, e2 := normalize(p.printed, c.JSX, true)
		if e1 != nil || e2 != nil || w2 != g2 {
			r.Status, r.Reason = statusFail, "shape mismatch: "+firstDiff(want, got)
			return r
		}
	}
	r.Status = statusPass
	return r
}

// ---------------------------------------------------------------- reporting

type suiteStats struct {
	Pass int `json:"pass"`
	Fail int `json:"fail"`
	Skip int `json:"skip"`
}

func (s suiteStats) rate() float64 {
	if s.Pass+s.Fail == 0 {
		return 0
	}
	return 100 * float64(s.Pass) / float64(s.Pass+s.Fail)
}

type failure struct {
	ID     string `json:"id"`
	Reason string `json:"reason"`
}

type summary struct {
	Suites   map[string]suiteStats `json:"suites"`
	Total    suiteStats            `json:"total"`
	PassRate float64               `json:"passRate"`
	Failures []failure             `json:"failures"`
}

func buildSummary(results []result) summary {
	s := summary{Suites: map[string]suiteStats{}}
	for _, r := range results {
		st := s.Suites[r.Case.Suite]
		switch r.Status {
		case statusPass:
			st.Pass++
			s.Total.Pass++
		case statusFail:
			st.Fail++
			s.Total.Fail++
			s.Failures = append(s.Failures, failure{r.Case.ID, r.Reason})
		case statusSkip:
			st.Skip++
			s.Total.Skip++
		}
		s.Suites[r.Case.Suite] = st
	}
	s.PassRate = s.Total.rate()
	return s
}

func (s summary) table() string {
	names := make([]string, 0, len(s.Suites))
	for n := range s.Suites {
		names = append(names, n)
	}
	sort.Strings(names)
	var b strings.Builder
	b.WriteString("| Suite | Pass | Fail | Skip | Pass rate |\n|---|---:|---:|---:|---:|\n")
	for _, n := range names {
		st := s.Suites[n]
		fmt.Fprintf(&b, "| %s | %d | %d | %d | %.1f%% |\n", n, st.Pass, st.Fail, st.Skip, st.rate())
	}
	fmt.Fprintf(&b, "| **total** | %d | %d | %d | %.1f%% |\n", s.Total.Pass, s.Total.Fail, s.Total.Skip, s.PassRate)
	return b.String()
}

type baseline struct {
	Passing []string `json:"passing"`
}

func readBaseline(t *testing.T) map[string]bool {
	b, err := os.ReadFile(baselinePath)
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]bool{}
		}
		t.Fatal(err)
	}
	var bl baseline
	if err := json.Unmarshal(b, &bl); err != nil {
		t.Fatalf("parse %s: %v", baselinePath, err)
	}
	m := make(map[string]bool, len(bl.Passing))
	for _, id := range bl.Passing {
		m[id] = true
	}
	return m
}

func writeBaseline(t *testing.T, results []result) {
	var bl baseline
	for _, r := range results {
		if r.Status == statusPass {
			bl.Passing = append(bl.Passing, r.Case.ID)
		}
	}
	sort.Strings(bl.Passing)
	b, err := json.MarshalIndent(bl, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(baselinePath, append(b, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
}

// ---------------------------------------------------------------- test

func TestConformance(t *testing.T) {
	cases := loadCases(t)
	if len(cases) == 0 {
		t.Fatal("empty corpus")
	}
	results := make([]result, len(cases))
	var wg sync.WaitGroup
	jobs := make(chan int)
	for w := 0; w < runtime.GOMAXPROCS(0); w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range jobs {
				results[i] = runCase(cases[i])
			}
		}()
	}
	for i := range cases {
		jobs <- i
	}
	close(jobs)
	wg.Wait()

	if sub := os.Getenv("KRATE_CONFORMANCE_SHOW"); sub != "" {
		for _, c := range cases {
			if strings.Contains(c.ID, sub) {
				p := krateParse(c.Src)
				t.Logf("case %s\n--- source\n%s\n--- errors: %v\n--- printed\n%s", c.ID, c.Src, p.errs, p.printed)
			}
		}
	}

	sum := buildSummary(results)
	table := sum.table()
	t.Logf("parser conformance (skips excluded from pass rate)\n%s", table)

	if p := os.Getenv("KRATE_CONFORMANCE_SUMMARY"); p != "" {
		b, err := json.MarshalIndent(sum, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, append(b, '\n'), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if p := os.Getenv("KRATE_CONFORMANCE_MARKDOWN"); p != "" {
		md := "### Parser conformance\n\n" + table
		if err := os.WriteFile(p, []byte(md), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	if *update || os.Getenv("KRATE_CONFORMANCE_UPDATE") == "1" {
		writeBaseline(t, results)
		t.Logf("baseline rewritten: %d passing", sum.Total.Pass)
		return
	}

	base := readBaseline(t)
	byID := make(map[string]result, len(results))
	for _, r := range results {
		byID[r.Case.ID] = r
	}
	var regress, gone []string
	for id := range base {
		r, ok := byID[id]
		switch {
		case !ok:
			gone = append(gone, id)
		case r.Status != statusPass:
			regress = append(regress, fmt.Sprintf("%s: %s", id, r.Reason))
		}
	}
	sort.Strings(regress)
	sort.Strings(gone)
	for _, s := range regress {
		t.Errorf("regression: %s", s)
	}
	for _, id := range gone {
		t.Errorf("baseline case missing from corpus: %s (re-baseline with -update)", id)
	}
	var fixed []string
	for _, r := range results {
		if r.Status == statusPass && !base[r.Case.ID] {
			fixed = append(fixed, r.Case.ID)
		}
	}
	if len(fixed) > 0 {
		t.Logf("%d newly passing case(s); run with -update to record them:\n  %s", len(fixed), strings.Join(fixed, "\n  "))
	}
}
