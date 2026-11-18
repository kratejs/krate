package deploy

import (
	"encoding/json"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/kratejs/krate/packages/compiler/internal/config"
)

// ImmutableCacheControl is the policy for content-hashed assets: the name
// changes with the content, so a cached copy never goes stale.
const ImmutableCacheControl = "public, max-age=31536000, immutable"

// LoadHashedAssets returns the content-hashed files the build emitted, as URL
// paths relative to the output directory (for example "/index.ab12cd34.js"),
// from the build manifest. `krate serve` and the deploy adapters both read this
// list, so they agree on what is immutable. A missing or unreadable manifest
// yields nil: nothing is guessed from file names.
func LoadHashedAssets(outDir string) []string {
	for _, p := range []string{
		filepath.Join(outDir, ".krate", "manifest.json"),
		filepath.Join(outDir, "manifest.json"),
	} {
		data, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		var m struct {
			HashedAssets []string `json:"hashedAssets"`
		}
		if json.Unmarshal(data, &m) != nil {
			return nil
		}
		return m.HashedAssets
	}
	return nil
}

// immutableRules turns the hashed asset list into header rules, ahead of the
// user's own rules. Sources are relative to the site base path, like the
// user's. A directory whose files are all hashed becomes one "/dir/*" rule;
// everything else is listed by path. A rule is dropped (or a directory rule
// expanded) where a user rule sets Cache-Control for the same files, so the
// user's policy wins.
func immutableRules(cfg *config.Config, outDir string, hashed []string) autoHeaders {
	if len(hashed) == 0 {
		return autoHeaders{}
	}
	set := make(map[string]bool, len(hashed))
	for _, h := range hashed {
		if strings.HasPrefix(h, "/") && !strings.ContainsAny(h, "*:?#") {
			set[h] = true
		}
	}
	// A compressed sibling is served under its source's URL, so it counts as
	// the source for the all-hashed test.
	isHashed := func(url string) bool {
		if set[url] {
			return true
		}
		for _, ext := range []string{".br", ".gz"} {
			if strings.HasSuffix(url, ext) && set[strings.TrimSuffix(url, ext)] {
				return true
			}
		}
		return false
	}

	type dirStat struct{ total, unhashed int }
	dirs := map[string]*dirStat{}
	_ = filepath.WalkDir(outDir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		rel, rerr := filepath.Rel(outDir, p)
		if rerr != nil || rel == "." {
			return nil
		}
		rel = filepath.ToSlash(rel)
		if d.IsDir() {
			if rel == ".krate" {
				return filepath.SkipDir
			}
			return nil
		}
		url := "/" + rel
		bad := 0
		if !isHashed(url) {
			bad = 1
		}
		// Count the file in every ancestor directory below the root.
		for dir := path.Dir(url); dir != "/" && dir != "."; dir = path.Dir(dir) {
			st := dirs[dir]
			if st == nil {
				st = &dirStat{}
				dirs[dir] = st
			}
			st.total++
			st.unhashed += bad
		}
		return nil
	})

	userCC := userCacheControlMatchers(cfg)
	covered := func(url string) bool { // a user rule sets Cache-Control here
		for _, m := range userCC {
			if m.re.MatchString(url) {
				return true
			}
		}
		return false
	}
	overlaps := func(dir string) bool { // a user rule could match inside dir
		for _, m := range userCC {
			if m.re.MatchString(dir+"/x") || strings.HasPrefix(m.prefix, dir+"/") || strings.HasPrefix(dir+"/", m.prefix) {
				return true
			}
		}
		return false
	}

	// Top-most all-hashed directories.
	var splat []string
	for dir, st := range dirs {
		if st.total > 0 && st.unhashed == 0 && !overlaps(dir) {
			splat = append(splat, dir)
		}
	}
	sort.Strings(splat)
	var roots []string
	for _, d := range splat {
		if len(roots) > 0 && strings.HasPrefix(d, roots[len(roots)-1]+"/") {
			continue
		}
		roots = append(roots, d)
	}
	under := func(url string) bool {
		for _, r := range roots {
			if strings.HasPrefix(url, r+"/") {
				return true
			}
		}
		return false
	}

	var sources []string
	for _, r := range roots {
		sources = append(sources, r+"/*")
	}
	exact := make([]string, 0, len(set))
	for url := range set {
		if !under(url) && !covered(url) {
			exact = append(exact, url)
		}
	}
	sort.Strings(exact)
	sources = append(sources, exact...)

	rules := make([]config.HeaderRule, 0, len(sources))
	for _, s := range sources {
		rules = append(rules, config.HeaderRule{Source: s, Headers: map[string]string{"Cache-Control": ImmutableCacheControl}})
	}
	return autoHeaders{rules: rules}
}

type ccMatcher struct {
	re     *regexp.Regexp
	prefix string // literal text before the first wildcard
}

func userCacheControlMatchers(cfg *config.Config) []ccMatcher {
	var out []ccMatcher
	for _, r := range cfg.Headers {
		if r.Source == "" {
			continue
		}
		has := false
		for k := range r.Headers {
			if strings.EqualFold(k, "Cache-Control") {
				has = true
			}
		}
		if !has {
			continue
		}
		re, err := regexp.Compile(headerSourceRegex(r.Source))
		if err != nil {
			continue
		}
		prefix := r.Source
		if i := strings.IndexAny(prefix, "*:"); i >= 0 {
			prefix = prefix[:i]
		}
		out = append(out, ccMatcher{re: re, prefix: prefix})
	}
	return out
}

// autoHeaders are the header rules krate adds ahead of the user's.
type autoHeaders struct {
	rules   []config.HeaderRule
	omitted int // hashed files left out to stay under a host's rule limit
}

// cloudflareMaxHeaderRules is the number of rules Cloudflare Pages reads from a
// _headers file; later ones are ignored.
const cloudflareMaxHeaderRules = 100

// capped keeps at most limit rules in total with the user's n rules, dropping
// the smallest files first (directory rules always stay).
func (a autoHeaders) capped(outDir string, userRules, limit int) autoHeaders {
	room := limit - userRules
	if room < 0 {
		room = 0
	}
	if len(a.rules) <= room {
		return a
	}
	type sized struct {
		r    config.HeaderRule
		size int64
	}
	var dirs []config.HeaderRule
	var files []sized
	for _, r := range a.rules {
		if strings.HasSuffix(r.Source, "/*") {
			dirs = append(dirs, r)
			continue
		}
		var size int64
		if fi, err := os.Stat(filepath.Join(outDir, filepath.FromSlash(r.Source))); err == nil {
			size = fi.Size()
		}
		files = append(files, sized{r, size})
	}
	sort.SliceStable(files, func(i, j int) bool { return files[i].size > files[j].size })
	keep := room - len(dirs)
	if keep < 0 {
		keep = 0
		dirs = dirs[:min(len(dirs), room)]
	}
	out := autoHeaders{rules: dirs, omitted: len(a.rules) - len(dirs)}
	if keep > len(files) {
		keep = len(files)
	}
	for _, f := range files[:keep] {
		out.rules = append(out.rules, f.r)
	}
	out.omitted = len(a.rules) - len(out.rules)
	return out
}

// withImmutable puts the automatic rules ahead of the user's, so a later user
// rule overrides them on hosts that apply rules in order.
func withImmutable(cfg *config.Config, auto autoHeaders) []config.HeaderRule {
	if len(auto.rules) == 0 {
		return cfg.Headers
	}
	out := make([]config.HeaderRule, 0, len(auto.rules)+len(cfg.Headers))
	out = append(out, auto.rules...)
	return append(out, cfg.Headers...)
}
