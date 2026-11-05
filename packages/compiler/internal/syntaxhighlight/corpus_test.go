package syntaxhighlight

import (
	"crypto/sha1"
	"encoding/hex"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
)

// corpusSpec says which real-world files exercise a lexer.
type corpusSpec struct {
	lexer string   // canonical chroma name, as in ShippedLanguages
	langs []string // language strings to run (each file set is run under each)
	exts  []string // file extensions
}

var corpusSpecs = []corpusSpec{
	{"TypeScript", []string{"ts"}, []string{".ts"}},
	{"TypeScript", []string{"tsx"}, []string{".tsx"}},
	{"JavaScript", []string{"js"}, []string{".js", ".mjs", ".cjs"}},
	{"react", []string{"jsx"}, []string{".jsx", ".js"}},
	{"JSON", []string{"json"}, []string{".json"}},
	{"CSS", []string{"css"}, []string{".css"}},
	{"HTML", []string{"html"}, []string{".html", ".htm"}},
	{"Bash", []string{"bash"}, []string{".sh", ".bash"}},
	{"Go", []string{"go"}, []string{".go"}},
	{"Python", []string{"python"}, []string{".py"}},
	{"Rust", []string{"rust"}, []string{".rs"}},
	{"YAML", []string{"yaml"}, []string{".yml", ".yaml"}},
	{"markdown", []string{"markdown"}, []string{".md", ".markdown"}},
	{"TOML", []string{"toml"}, []string{".toml"}},
	{"XML", []string{"xml"}, []string{".xml", ".svg"}},
	{"C", []string{"c"}, []string{".c", ".h"}},
}

const (
	minFileBytes  = 200
	maxFileBytes  = 300 << 10
	corpusBudget  = 1536 << 10 // bytes of sampled files per language
	variantBudget = 192 << 10  // bytes of mutated copies per language
)

var (
	corpusOnce  sync.Once
	corpusFiles map[string][]string // extension -> candidate files, in stable order
	corpusErr   error
)

// gatherCorpus lists candidate files by extension from the repository, the Go
// modules it depends on, chroma's own samples and the vendored corpus.
func gatherCorpus(t *testing.T) map[string][]string {
	t.Helper()
	corpusOnce.Do(func() {
		root := repoRoot(t)
		roots := []string{root}
		if out, err := exec.Command("go", "env", "GOROOT").Output(); err == nil {
			if gr := strings.TrimSpace(string(out)); gr != "" {
				roots = append(roots, filepath.Join(gr, "src"), filepath.Join(gr, "misc"), filepath.Join(gr, "lib"))
			}
		}
		if out, err := exec.Command("go", "list", "-m", "-f", "{{.Dir}}", "all").Output(); err == nil {
			for _, d := range strings.Fields(string(out)) {
				if d != "" {
					roots = append(roots, d)
				}
			}
		}
		skip := map[string]bool{"node_modules": true, ".git": true, "dist": true, ".krate": true, "docs": false, ".next": true, "target": true}
		byExt := map[string][]string{}
		for _, r := range roots {
			_ = filepath.WalkDir(r, func(p string, d fs.DirEntry, err error) error {
				if err != nil {
					return nil
				}
				if d.IsDir() {
					if skip[d.Name()] {
						return filepath.SkipDir
					}
					// The synced docs copy and this corpus are not independent samples.
					if strings.HasSuffix(filepath.ToSlash(p), "internal/kratedocs/docs") {
						return filepath.SkipDir
					}
					return nil
				}
				info, err := d.Info()
				if err != nil || info.Size() < minFileBytes || info.Size() > maxFileBytes {
					return nil
				}
				ext := strings.ToLower(filepath.Ext(p))
				byExt[ext] = append(byExt[ext], p)
				return nil
			})
		}
		corpusFiles = byExt
	})
	if corpusErr != nil {
		t.Fatal(corpusErr)
	}
	return corpusFiles
}

func pathKey(p string) string {
	h := sha1.Sum([]byte(filepath.ToSlash(p)))
	return hex.EncodeToString(h[:])
}

// corpusFor returns the files to test for a spec: every vendored file, then a
// stable pseudo-random sample of the rest up to the byte budget.
func corpusFor(t *testing.T, spec corpusSpec, vendored []string) []string {
	t.Helper()
	all := gatherCorpus(t)
	seen := map[string]bool{}
	var out []string
	total := 0
	for _, f := range vendored {
		out = append(out, f)
		if st, err := os.Stat(f); err == nil {
			total += int(st.Size())
		}
		seen[f] = true
	}
	var cand []string
	for _, e := range spec.exts {
		for _, f := range all[e] {
			if !seen[f] && !strings.Contains(filepath.ToSlash(f), "/testdata/corpus/") {
				cand = append(cand, f)
			}
		}
	}
	sort.Slice(cand, func(i, j int) bool { return pathKey(cand[i]) < pathKey(cand[j]) })
	full := os.Getenv("KRATE_HL_CORPUS") == "full"
	for _, f := range cand {
		if !full && total >= corpusBudget {
			break
		}
		st, err := os.Stat(f)
		if err != nil {
			continue
		}
		out = append(out, f)
		total += int(st.Size())
	}
	return out
}

func vendoredFiles(t *testing.T, spec corpusSpec) []string {
	t.Helper()
	var out []string
	for _, l := range []string{strings.ToLower(spec.lexer)} {
		dir := filepath.Join("testdata", "corpus", l)
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			out = append(out, filepath.Join(dir, e.Name()))
		}
	}
	return out
}

// chromaSamples lists chroma's own sample files for a lexer.
func chromaSamples(t *testing.T, lexer string) []string {
	t.Helper()
	out, err := exec.Command("go", "list", "-m", "-f", "{{.Dir}}", "github.com/alecthomas/chroma/v2").Output()
	if err != nil {
		return nil
	}
	dir := filepath.Join(strings.TrimSpace(string(out)), "lexers", "testdata")
	var files []string
	for _, pat := range []string{strings.ToLower(lexer) + ".actual", strings.ToLower(lexer) + "_*.actual"} {
		m, _ := filepath.Glob(filepath.Join(dir, pat))
		files = append(files, m...)
	}
	return files
}
