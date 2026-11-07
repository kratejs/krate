package mcp

import (
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
)

// importSpecRe matches the module specifier of an import, export or require.
var importSpecRe = regexp.MustCompile(`(\bfrom\s+|\bimport\s+|\bimport\(\s*|\brequire\(\s*)(['"])([^'"\n]+)(['"])`)

var moduleFileExts = map[string]bool{".ts": true, ".tsx": true, ".js": true, ".jsx": true, ".mjs": true, ".mdx": true}

// moduleFiles lists the project's source modules, project-relative.
func (s *Service) moduleFiles() []string {
	var files []string
	_ = filepath.WalkDir(s.root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			switch d.Name() {
			case "node_modules", ".git", ".krate", "dist", "out":
				return filepath.SkipDir
			}
			return nil
		}
		if !moduleFileExts[path.Ext(d.Name())] {
			return nil
		}
		rel, err := filepath.Rel(s.root, p)
		if err != nil {
			return nil
		}
		rel = filepath.ToSlash(rel)
		if s.policy == nil || !s.policy.denied(rel) {
			files = append(files, rel)
		}
		return nil
	})
	return files
}

// importersOf returns the modules that import target.
func (s *Service) importersOf(target string) []string {
	var out []string
	for _, file := range s.moduleFiles() {
		if file == target {
			continue
		}
		data, err := os.ReadFile(filepath.Join(s.root, filepath.FromSlash(file)))
		if err != nil {
			continue
		}
		for _, m := range importSpecRe.FindAllStringSubmatch(string(data), -1) {
			if _, resolved := s.resolveImport(file, m[3]); resolved == target {
				out = append(out, file)
				break
			}
		}
	}
	return out
}

// rewritesForMove returns the new text of every file a move changes, keyed by
// path after the move. moved is the current text of the file being moved.
func (s *Service) rewritesForMove(from, to, moved string) map[string]string {
	out := map[string]string{}
	for _, file := range s.moduleFiles() {
		if file == from {
			continue
		}
		data, err := os.ReadFile(filepath.Join(s.root, filepath.FromSlash(file)))
		if err != nil {
			continue
		}
		text := string(data)
		next := importSpecRe.ReplaceAllStringFunc(text, func(match string) string {
			m := importSpecRe.FindStringSubmatch(match)
			if _, resolved := s.resolveImport(file, m[3]); resolved != from {
				return match
			}
			return m[1] + m[2] + s.specFor(file, m[3], from, to) + m[4]
		})
		if next != text {
			out[file] = next
		}
	}
	// The moved file's own relative imports change with its directory.
	if path.Dir(from) != path.Dir(to) {
		next := importSpecRe.ReplaceAllStringFunc(moved, func(match string) string {
			m := importSpecRe.FindStringSubmatch(match)
			if !strings.HasPrefix(m[3], ".") {
				return match
			}
			_, resolved := s.resolveImport(from, m[3])
			if resolved == "" {
				return match
			}
			return m[1] + m[2] + s.specFor(to, m[3], resolved, resolved) + m[4]
		})
		if next != moved {
			out[to] = next
		}
	}
	return out
}

// specFor writes the specifier importer needs for target, in the style of the
// old one: alias stays alias when target is still under it, otherwise relative.
func (s *Service) specFor(importer, old, was, target string) string {
	spec, query, hasQuery := strings.Cut(old, "?")
	keepExt := path.Ext(spec) != "" && strings.HasSuffix(was, path.Ext(spec))
	bare := strings.TrimSuffix(target, path.Ext(target))
	wasIndex := strings.HasSuffix(strings.TrimSuffix(was, path.Ext(was)), "/index") && !strings.HasSuffix(spec, "/index") && !keepExt
	bare = strings.TrimSuffix(bare, ifThen(wasIndex, "/index", ""))
	if keepExt {
		bare = target
	}
	out := ""
	if !strings.HasPrefix(spec, ".") {
		out = s.aliasSpec(spec, bare)
	}
	if out == "" {
		rel, err := filepath.Rel(filepath.FromSlash(path.Dir(importer)), filepath.FromSlash(bare))
		if err != nil {
			return old
		}
		out = filepath.ToSlash(rel)
		if !strings.HasPrefix(out, ".") {
			out = "./" + out
		}
	}
	if hasQuery {
		out += "?" + query
	}
	return out
}

// aliasSpec spells bare through the tsconfig alias that spec used.
func (s *Service) aliasSpec(spec, bare string) string {
	baseDir := s.root
	if s.cfg.TSBaseURL != "" {
		baseDir = s.cfg.TSBaseURL
	}
	for _, a := range s.cfg.PathAliases {
		prefix := strings.TrimSuffix(a.Prefix, "*")
		if prefix == "" || !strings.HasPrefix(spec, prefix) {
			continue
		}
		for _, t := range a.Targets {
			head, _, wild := strings.Cut(t, "*")
			if !wild {
				continue
			}
			abs := head
			if !filepath.IsAbs(head) {
				abs = filepath.Join(baseDir, filepath.FromSlash(head))
			}
			root := s.relToRoot(abs)
			if root == "." {
				return prefix + bare
			}
			if rest, ok := strings.CutPrefix(bare, strings.TrimSuffix(root, "/")+"/"); ok {
				return prefix + rest
			}
		}
	}
	return ""
}

func ifThen(cond bool, yes, no string) string {
	if cond {
		return yes
	}
	return no
}
