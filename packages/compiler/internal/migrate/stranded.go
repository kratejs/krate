package migrate

import (
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// strandedSpecifier matches the module string of an import, export-from,
// dynamic import, require call or a vi/jest module mock.
var strandedSpecifier = regexp.MustCompile(`(\bfrom\s*|\bimport\s*\(\s*|\bimport\s+|\brequire\s*\(\s*|\b(?:vi|jest)\.(?:mock|doMock|unmock|importActual|requireActual)\s*\(\s*)(["'])([^"'\r\n]+)(["'])`)

var (
	strandedScriptExts = map[string]bool{".ts": true, ".tsx": true, ".js": true, ".jsx": true, ".mjs": true, ".cjs": true, ".mts": true, ".cts": true}
	importsProxyName   = regexp.MustCompile(`\bproxy\b`)
	importsConfigName  = regexp.MustCompile(`\bconfig\b`)
)

// rewriteStrandedImports fixes project files that stay in place but import a
// file the migration moves aside (a Next route file, or proxy.ts, which becomes
// middleware.ts). A direct replacement gets the import pointed at it; anything
// else becomes a manual item naming the importer and the missing import. It
// runs after planMoveAside, because that decides what moves.
func rewriteStrandedImports(abs string, rep *Report) {
	if len(rep.MoveAside) == 0 {
		return
	}
	moved := func(rel string) bool {
		for _, tree := range rep.MoveAside {
			if rel == tree || strings.HasPrefix(rel, tree+"/") {
				return true
			}
		}
		return false
	}
	kept := map[string]bool{}
	planned := map[string]bool{}
	replacement := map[string]int{}
	for i := range rep.Files {
		f := &rep.Files[i]
		if f.Kind == KindStub {
			kept[f.Target] = true
			continue
		}
		if f.Source == "" {
			continue
		}
		planned[f.Source] = true
		if _, ok := replacement[f.Source]; !ok && f.Source != f.Target {
			replacement[f.Source] = i
		}
	}
	aliases := loadTSAliases(abs)

	// resolve maps an import specifier in dir to the project file it names.
	resolve := func(dir, spec string) string {
		var bases []string
		if strings.HasPrefix(spec, "./") || strings.HasPrefix(spec, "../") {
			bases = []string{path.Join(dir, spec)}
		} else {
			bases, _ = aliases.candidates(spec)
		}
		for _, base := range bases {
			base = path.Clean(base)
			if base == "." || base == ".." || strings.HasPrefix(base, "../") {
				continue
			}
			tries := []string{base}
			for _, e := range moduleExts {
				tries = append(tries, base+e)
			}
			for _, e := range moduleExts {
				tries = append(tries, base+"/index"+e)
			}
			if e := path.Ext(base); e == ".js" || e == ".jsx" || e == ".mjs" {
				stem := strings.TrimSuffix(base, e)
				for _, x := range []string{".ts", ".tsx", ".mts"} {
					tries = append(tries, stem+x)
				}
			}
			for _, t := range tries {
				if st, err := os.Stat(filepath.Join(abs, filepath.FromSlash(t))); err == nil && st.Mode().IsRegular() {
					return t
				}
			}
		}
		return ""
	}

	var importers []string
	_ = filepath.WalkDir(abs, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		rel, rerr := filepath.Rel(abs, p)
		if rerr != nil {
			return nil
		}
		rel = filepath.ToSlash(rel)
		if d.IsDir() {
			if skipDir(d.Name()) || (rel != "." && moved(rel)) {
				return filepath.SkipDir
			}
			return nil
		}
		if strandedScriptExts[strings.ToLower(filepath.Ext(rel))] && !planned[rel] && !moved(rel) {
			importers = append(importers, rel)
		}
		return nil
	})
	sort.Strings(importers)

	aliasProxy := map[int]bool{}
	for _, importer := range importers {
		data, err := os.ReadFile(filepath.Join(abs, filepath.FromSlash(importer)))
		if err != nil {
			continue
		}
		src := string(data)
		dir := path.Dir(importer)
		var rewrites []string
		var manual []ManualItem
		var out strings.Builder
		last := 0
		for _, m := range strandedSpecifier.FindAllStringSubmatchIndex(src, -1) {
			spec := src[m[6]:m[7]]
			hit := resolve(dir, spec)
			if hit == "" || !moved(hit) || kept[hit] {
				continue
			}
			idx, ok := replacement[hit]
			if !ok {
				manual = append(manual, ManualItem{
					Source: importer,
					Reason: importer + " imports " + spec + ", which is not migrated and moves to the backup (" + hit + ")",
					Hint:   "restore the file from the backup or point the import at a file the migrated app has",
				})
				continue
			}
			target := rep.Files[idx]
			newSpec := relativeSpecifier(dir, target.Target, spec)
			if newSpec == spec {
				continue
			}
			if target.Kind == KindMiddleware {
				clause := importClause(src, m[0])
				if importsProxyName.MatchString(clause) && strings.HasPrefix(path.Base(hit), "proxy.") {
					aliasProxy[idx] = true
				}
				if importsConfigName.MatchString(clause) {
					manual = append(manual, ManualItem{
						Source: importer,
						Reason: importer + " imports config from " + spec + ", but the generated " + target.Target + " applies the matcher itself and does not export config",
						Hint:   "drop the config import, or read the matcher from the original file in the backup",
					})
				}
			}
			rewrites = append(rewrites, "import "+spec+" -> "+newSpec+" ("+hit+" moved to "+target.Target+")")
			out.WriteString(src[last:m[6]])
			out.WriteString(newSpec)
			last = m[7]
		}
		rep.Manual = append(rep.Manual, manual...)
		if len(rewrites) == 0 {
			continue
		}
		out.WriteString(src[last:])
		rep.Files = append(rep.Files, PlannedFile{Kind: KindModule, Source: importer, Target: importer, Content: []byte(out.String()), orig: data, Rewrites: rewrites})
		rep.Info = append(rep.Info, fmt.Sprintf("%s: %d import(s) of moved files now point at their new location", importer, len(rewrites)))
	}
	for idx := range aliasProxy {
		mw := &rep.Files[idx]
		if strings.Contains(string(mw.Content), "export { middleware as proxy }") {
			continue
		}
		mw.Content = append(mw.Content, []byte("\n// Kept so files that import proxy from the old proxy module keep working.\nexport { middleware as proxy };\n")...)
		mw.Rewrites = append(mw.Rewrites, "exported middleware under the name proxy for importers of the old proxy file")
	}
}

// importClause returns the text from the import keyword before pos up to pos,
// which holds the imported names.
func importClause(src string, pos int) string {
	start := strings.LastIndex(src[:pos], "import")
	if start < 0 || pos-start > 600 {
		return ""
	}
	return src[start:pos]
}
