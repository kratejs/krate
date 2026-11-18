package migrate

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// krateRelease is one row of the generated table in krateversions_gen.go.
type krateRelease struct {
	Name    string
	Dir     string
	Version string
}

func krateReleaseFor(name string) (krateRelease, bool) {
	for _, r := range krateReleases {
		if r.Name == name {
			return r, true
		}
	}
	return krateRelease{}, false
}

// krateSpecs picks the dependency range written for each @krate package.
//
// A release compiler writes ^<version> from the table of published versions,
// so @krate/three and @krate/konva get their own version and not the
// runtime's. A development build has no published version to point at: it
// writes link: ranges to the packages of the Krate checkout it runs from, or
// "latest" (with an info line) when there is none.
type krateSpecs struct {
	version  string
	root     string // the migrated project
	checkout string // a Krate checkout, for development builds
	fallback bool
}

func newKrateSpecs(root, version, checkout string) *krateSpecs {
	s := &krateSpecs{version: strings.TrimPrefix(version, "v"), root: root}
	if s.dev() {
		s.checkout = checkout
		if s.checkout == "" {
			s.checkout = findKrateCheckout(root)
		}
	}
	return s
}

func (s *krateSpecs) dev() bool { return s.version == "" || strings.HasPrefix(s.version, "dev") }

func (s *krateSpecs) spec(name string) string {
	if s == nil {
		return "latest"
	}
	rel, known := krateReleaseFor(name)
	if !s.dev() {
		if known {
			return "^" + rel.Version
		}
		return "^" + s.version
	}
	if known && s.checkout != "" && fileExists(filepath.Join(s.checkout, "packages", rel.Dir, "package.json")) {
		if r, err := filepath.Rel(s.root, filepath.Join(s.checkout, "packages", rel.Dir)); err == nil {
			return "link:" + filepath.ToSlash(r)
		}
	}
	s.fallback = true
	return "latest"
}

// info is the note for a development build that had to write "latest".
func (s *krateSpecs) info() string {
	if s == nil || !s.fallback {
		return ""
	}
	return "this krate is a development build and no Krate checkout was found next to it, so the @krate/* dependencies were written as \"latest\"; pin them to a released version"
}

// findKrateCheckout looks for the Krate repository the running krate belongs
// to: KRATE_BINARY, the executable, or the @krate/core the project resolves.
func findKrateCheckout(root string) string {
	var starts []string
	if p := os.Getenv("KRATE_BINARY"); p != "" {
		starts = append(starts, p)
	}
	if p, err := os.Executable(); err == nil {
		starts = append(starts, p)
	}
	for d := root; ; d = filepath.Dir(d) {
		starts = append(starts, filepath.Join(d, "node_modules", "@krate", "core"))
		if filepath.Dir(d) == d {
			break
		}
	}
	for _, p := range starts {
		if abs, err := filepath.Abs(p); err == nil {
			if real, err := filepath.EvalSymlinks(abs); err == nil {
				abs = real
			}
			for d, i := abs, 0; i < 8 && filepath.Dir(d) != d; d, i = filepath.Dir(d), i+1 {
				if fileExists(filepath.Join(d, "packages", "core", "package.json")) && fileExists(filepath.Join(d, "packages", "compiler", "go.mod")) {
					return d
				}
			}
		}
	}
	return ""
}

// nextPackages are the dependencies the migration removes once nothing
// imports them. react and react-dom stay: islands alias them and other
// dependencies may peer on them.
var nextPackages = []string{"next", "next-auth", "@next/env"}

var nextPackageImport = regexp.MustCompile(`(?:\bfrom\s*|\bimport\s*\(?\s*|\brequire\s*\(\s*|\bdeclare\s+module\s*)["'](next|next-auth|@next/env)(?:/[^"']*)?["']`)

var (
	nextInvocation  = regexp.MustCompile(`^((?:npx|bunx)\s+|(?:pnpm|yarn)\s+(?:exec\s+)?)?next(?:\s+(\S+))?(.*)$`)
	nextBundlerFlag = regexp.MustCompile(`(^|\s)--(?:turbopack|turbo|webpack)\b`)
	nextPortFlag    = regexp.MustCompile(`(^|\s)-p\b`)
	nextHostFlag    = regexp.MustCompile(`(^|\s)-H\b`)
)

var nextStandalone = regexp.MustCompile(`\.next/standalone`)

// nextBin matches a script command that runs the next binary.
var nextBin = regexp.MustCompile(`(?:^|[\s;&|(])(?:npx\s+|pnpm\s+(?:exec\s+)?|yarn\s+|bunx\s+)?next(?:\s|$)`)

// remainingNextImporters maps each of nextPackages to the project files that
// still import it after the migration: planned files by their final content,
// and project files the plan does not touch. Files that move aside do not count.
func remainingNextImporters(root string, rep *Report) map[string][]string {
	out := map[string]map[string]bool{}
	note := func(label, src string) {
		for _, m := range nextPackageImport.FindAllStringSubmatch(maskComments(src), -1) {
			if out[m[1]] == nil {
				out[m[1]] = map[string]bool{}
			}
			out[m[1]][label] = true
		}
	}
	plannedSource := map[string]bool{}
	for _, f := range rep.Files {
		if f.Source != "" {
			plannedSource[filepath.ToSlash(f.Source)] = true
		}
		if f.Kind == KindPackage || f.Kind == KindPublic || len(f.Content) == 0 {
			continue
		}
		note(f.Target, string(f.Content))
	}
	moved := map[string]bool{}
	for _, m := range rep.MoveAside {
		moved[filepath.ToSlash(m)] = true
	}
	count := 0
	_ = filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		rel, _ := filepath.Rel(root, p)
		rel = filepath.ToSlash(rel)
		if d.IsDir() {
			if rel != "." && (skipDir(d.Name()) || moved[rel]) {
				return filepath.SkipDir
			}
			return nil
		}
		if moved[rel] || plannedSource[rel] || !isModuleSource(rel) && !strings.HasSuffix(rel, ".d.ts") {
			return nil
		}
		if base := filepath.Base(rel); base == "next-env.d.ts" || strings.HasPrefix(base, "next.config.") {
			return nil
		}
		if count++; count > 20000 {
			return filepath.SkipAll
		}
		if data, err := os.ReadFile(p); err == nil {
			note(rel, string(data))
		}
		return nil
	})
	res := map[string][]string{}
	for pkg, files := range out {
		for f := range files {
			res[pkg] = append(res[pkg], f)
		}
		sort.Strings(res[pkg])
	}
	return res
}

// rewriteNextScripts maps `next <cmd>` in package scripts to Krate commands.
// It drops `next typegen`, maps dev/build/start, and leaves every other next
// command alone with an info line. It reports whether a script still runs next.
func rewriteNextScripts(scripts map[string]any, node nodeDeploy, rep *Report, rewrites *[]string) (nextStays bool) {
	names := make([]string, 0, len(scripts))
	for n := range scripts {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, name := range names {
		old, ok := scripts[name].(string)
		if !ok {
			continue
		}
		if node.On && name == "start" && nextStandalone.MatchString(old) {
			cmd := node.script("start")
			scripts[name] = cmd
			*rewrites = append(*rewrites, fmt.Sprintf("scripts.%s: %q -> %q (the original is in the backup)", name, old, cmd))
			continue
		}
		if !nextBin.MatchString(old) {
			if strings.Contains(old, ".next/") {
				rep.Info = append(rep.Info, fmt.Sprintf("package.json scripts.%s refers to .next/, which Krate does not write; check it", name))
			}
			continue
		}
		var kept []string
		changed, stays := false, false
		for _, seg := range strings.Split(old, "&&") {
			trimmed := strings.TrimSpace(seg)
			m := nextInvocation.FindStringSubmatch(trimmed)
			if m == nil {
				if nextBin.MatchString(trimmed) {
					stays = true
				}
				kept = append(kept, trimmed)
				continue
			}
			args := strings.TrimSpace(m[3])
			args = strings.Join(strings.Fields(nextBundlerFlag.ReplaceAllString(args, " ")), " ")
			args = nextPortFlag.ReplaceAllString(args, "${1}--port")
			args = nextHostFlag.ReplaceAllString(args, "${1}--host")
			switch m[2] {
			case "typegen":
				changed = true
			case "dev", "build", "start":
				cmd := node.script(m[2])
				if args != "" && strings.HasPrefix(cmd, "krate ") {
					cmd += " " + args
				}
				kept = append(kept, cmd)
				changed = true
			default:
				stays = true
				kept = append(kept, trimmed)
				rep.Info = append(rep.Info, fmt.Sprintf("package.json scripts.%s still runs `next %s`, which Krate has no equivalent for; next stays installed for it", name, m[2]))
			}
		}
		if !changed {
			nextStays = nextStays || stays
			continue
		}
		cmd := strings.Join(kept, " && ")
		scripts[name] = cmd
		*rewrites = append(*rewrites, fmt.Sprintf("scripts.%s: %q -> %q (the original is in the backup)", name, old, cmd))
		nextStays = nextStays || stays
	}
	return nextStays
}

// installCommand is the install command of the package manager the project
// uses, and the directory (relative to root) to run it in: the pnpm workspace
// root when there is one.
func installCommand(root string) (string, string) {
	for d := root; ; d = filepath.Dir(d) {
		mgr := ""
		switch {
		case fileExists(filepath.Join(d, "pnpm-workspace.yaml")), fileExists(filepath.Join(d, "pnpm-lock.yaml")):
			mgr = "pnpm install"
		case fileExists(filepath.Join(d, "yarn.lock")):
			mgr = "yarn install"
		case fileExists(filepath.Join(d, "bun.lock")), fileExists(filepath.Join(d, "bun.lockb")):
			mgr = "bun install"
		case fileExists(filepath.Join(d, "package-lock.json")):
			mgr = "npm install"
		}
		if mgr != "" {
			rel, err := filepath.Rel(root, d)
			if err != nil {
				rel = "."
			}
			return mgr, filepath.ToSlash(rel)
		}
		if filepath.Dir(d) == d {
			break
		}
	}
	return "npm install", "."
}

// krateImportPackage matches an import of a @krate package in module source.
var krateImportPackage = regexp.MustCompile(`(?:\bfrom\s*|\bimport\s*\(?\s*|\brequire\s*\(\s*)["'](@krate/[a-z0-9-]+)(?:/[^"']*)?["']`)

// finishPackage completes the planned package.json once every other pass has
// run: it adds the @krate packages the rewritten imports need, removes next,
// next-auth and @next/env when nothing imports them, and replaces the
// "Next.js dependencies remain" items with the one install command.
func finishPackage(rep *Report, root string, specs *krateSpecs) {
	idx := -1
	for i := range rep.Files {
		if rep.Files[i].Kind == KindPackage {
			idx = i
		}
	}
	if idx < 0 {
		return
	}
	f := &rep.Files[idx]
	var pkg map[string]any
	if json.Unmarshal(f.Content, &pkg) != nil {
		return
	}
	deps, _ := pkg["dependencies"].(map[string]any)
	if deps == nil {
		deps = map[string]any{}
	}
	var added, removed []string
	needed := map[string]bool{}
	for _, pf := range rep.Files {
		if pf.Kind == KindPackage || pf.Kind == KindPublic || len(pf.Content) == 0 {
			continue
		}
		for _, m := range krateImportPackage.FindAllStringSubmatch(string(pf.Content), -1) {
			needed[m[1]] = true
		}
	}
	names := make([]string, 0, len(needed))
	for n := range needed {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		if _, ok := krateReleaseFor(n); !ok || hasDependency(pkg, n) {
			continue
		}
		spec := specs.spec(n)
		deps[n] = spec
		added = append(added, n+"@"+spec)
	}

	importers := remainingNextImporters(root, rep)
	scripts, _ := pkg["scripts"].(map[string]any)
	nextStays := false
	for _, s := range scripts {
		if cmd, ok := s.(string); ok && nextBin.MatchString(cmd) {
			nextStays = true
		}
	}
	for _, name := range nextPackages {
		present := false
		for _, key := range []string{"dependencies", "devDependencies"} {
			if m, ok := pkg[key].(map[string]any); ok {
				if _, ok := m[name]; ok {
					present = true
				}
			}
		}
		if !present {
			continue
		}
		switch {
		case len(importers[name]) > 0:
			rep.Info = append(rep.Info, fmt.Sprintf("kept %s in package.json: %s", name, importerList(importers[name])))
		case name == "next" && nextStays:
		default:
			for _, key := range []string{"dependencies", "devDependencies"} {
				if m, ok := pkg[key].(map[string]any); ok {
					delete(m, name)
				}
			}
			removed = append(removed, name)
		}
	}
	pkg["dependencies"] = deps
	for _, key := range []string{"dependencies", "devDependencies"} {
		if m, ok := pkg[key].(map[string]any); ok && len(m) == 0 && key == "devDependencies" {
			delete(pkg, key)
		}
	}
	if len(added) > 0 {
		f.Rewrites = append(f.Rewrites, "dependencies added: "+strings.Join(added, ", "))
	}
	if len(removed) > 0 {
		f.Rewrites = append(f.Rewrites, "dependencies removed (nothing imports them any more): "+strings.Join(removed, ", "))
	}
	var kept []string
	for _, dep := range []string{"react", "react-dom"} {
		if _, ok := deps[dep]; ok {
			kept = append(kept, dep)
		}
	}
	if len(kept) > 0 {
		f.Rewrites = append(f.Rewrites, "dependencies kept: "+strings.Join(kept, ", ")+" (the Preact islands alias them)")
	}
	if out, err := marshalPackageJSON(f.Content, pkg); err == nil {
		f.Content = out
	}
	if note := specs.info(); note != "" {
		rep.Info = append(rep.Info, note)
	}
	mgr, dir := installCommand(root)
	hint := "run `" + mgr + "`"
	if dir != "." {
		hint += " in " + dir + " (the workspace root)"
	}
	rep.Manual = append(rep.Manual, ManualItem{
		Source: "package.json",
		Reason: "dependencies changed in package.json and are not installed yet",
		Hint:   hint,
	})
}

func importerList(files []string) string {
	shown := files
	more := ""
	if len(shown) > 4 {
		shown, more = shown[:4], fmt.Sprintf(" (+%d more)", len(files)-4)
	}
	return "still imported by " + strings.Join(shown, ", ") + more
}

// hasPlannedPackage reports whether the plan rewrites package.json.
func hasPlannedPackage(rep *Report) bool {
	for _, f := range rep.Files {
		if f.Kind == KindPackage {
			return true
		}
	}
	return false
}

// jsonKeyOrder lists the keys of a JSON object in file order.
func jsonKeyOrder(data []byte) []string {
	dec := json.NewDecoder(bytes.NewReader(data))
	if t, err := dec.Token(); err != nil || t != json.Delim('{') {
		return nil
	}
	var keys []string
	for dec.More() {
		t, err := dec.Token()
		if err != nil {
			return keys
		}
		k, _ := t.(string)
		keys = append(keys, k)
		var skip json.RawMessage
		if dec.Decode(&skip) != nil {
			return keys
		}
	}
	return keys
}

func marshalValue(v any) ([]byte, error) {
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return bytes.TrimRight(b.Bytes(), "\n"), nil
}

// marshalPackageJSON writes pkg the way the file in orig was laid out: the
// top-level keys and the scripts keep their order, new keys go last (new
// dependencies sort among the others), and & < > are not escaped. Diffs of
// the migrated package.json then show only what changed.
func marshalPackageJSON(orig []byte, pkg map[string]any) ([]byte, error) {
	var raw map[string]json.RawMessage
	_ = json.Unmarshal(orig, &raw)
	order := func(m map[string]any, keys []string, sortRest bool) []string {
		seen := map[string]bool{}
		var out []string
		for _, k := range keys {
			if _, ok := m[k]; ok && !seen[k] {
				seen[k] = true
				out = append(out, k)
			}
		}
		var rest []string
		for k := range m {
			if !seen[k] {
				rest = append(rest, k)
			}
		}
		sort.Strings(rest)
		if sortRest && len(out) > 0 {
			// dependency maps are kept sorted
			out = append(out, rest...)
			sort.Strings(out)
			return out
		}
		return append(out, rest...)
	}
	var b bytes.Buffer
	b.WriteString("{\n")
	top := order(pkg, jsonKeyOrder(orig), false)
	for i, k := range top {
		kb, _ := marshalValue(k)
		b.WriteString("  " + string(kb) + ": ")
		if m, ok := pkg[k].(map[string]any); ok && len(m) > 0 {
			keys := jsonKeyOrder(raw[k])
			b.WriteString("{\n")
			sorted := strings.HasSuffix(k, "ependencies") && sort.StringsAreSorted(keys)
			inner := order(m, keys, sorted)
			for j, ik := range inner {
				ikb, _ := marshalValue(ik)
				vb, err := marshalValue(m[ik])
				if err != nil {
					return nil, err
				}
				var ind bytes.Buffer
				if json.Indent(&ind, vb, "    ", "  ") != nil {
					ind.Write(vb)
				}
				b.WriteString("    " + string(ikb) + ": " + ind.String())
				if j < len(inner)-1 {
					b.WriteString(",")
				}
				b.WriteString("\n")
			}
			b.WriteString("  }")
		} else {
			vb, err := marshalValue(pkg[k])
			if err != nil {
				return nil, err
			}
			var ind bytes.Buffer
			if json.Indent(&ind, vb, "  ", "  ") != nil {
				ind.Write(vb)
			}
			b.WriteString(ind.String())
		}
		if i < len(top)-1 {
			b.WriteString(",")
		}
		b.WriteString("\n")
	}
	b.WriteString("}\n")
	return b.Bytes(), nil
}
