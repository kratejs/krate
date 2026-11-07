package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/kratejs/krate/packages/compiler/internal/diag"
)

// tsOpts is the subset of a tsconfig that Krate honours, merged across the
// `extends` chain (the child wins).
type tsOpts struct {
	experimentalDecorators  *bool
	useDefineForClassFields *bool
	baseURL                 string // absolute, from the config that set it
	hasBaseURL              bool
	paths                   map[string][]string
	pathsDir                string // directory of the config that declared paths
}

// StripJSONC removes comments, a leading BOM and trailing commas from
// JSON-with-comments (tsconfig.json). String literals are left untouched.
func StripJSONC(src []byte) []byte {
	if len(src) >= 3 && src[0] == 0xEF && src[1] == 0xBB && src[2] == 0xBF {
		src = src[3:]
	}
	out := make([]byte, 0, len(src))
	for i := 0; i < len(src); i++ {
		c := src[i]
		switch {
		case c == '"':
			out = append(out, c)
			for i++; i < len(src); i++ {
				out = append(out, src[i])
				if src[i] == '\\' && i+1 < len(src) {
					i++
					out = append(out, src[i])
					continue
				}
				if src[i] == '"' {
					break
				}
			}
		case c == '/' && i+1 < len(src) && src[i+1] == '/':
			for i < len(src) && src[i] != '\n' {
				i++
			}
			if i < len(src) {
				out = append(out, '\n')
			}
		case c == '/' && i+1 < len(src) && src[i+1] == '*':
			i += 2
			for i+1 < len(src) && !(src[i] == '*' && src[i+1] == '/') {
				if src[i] == '\n' {
					out = append(out, '\n')
				}
				i++
			}
			i++
		default:
			out = append(out, c)
		}
	}
	return dropTrailingCommas(out)
}

func dropTrailingCommas(src []byte) []byte {
	out := make([]byte, 0, len(src))
	for i := 0; i < len(src); i++ {
		c := src[i]
		if c == '"' {
			out = append(out, c)
			for i++; i < len(src); i++ {
				out = append(out, src[i])
				if src[i] == '\\' && i+1 < len(src) {
					i++
					out = append(out, src[i])
					continue
				}
				if src[i] == '"' {
					break
				}
			}
			continue
		}
		if c == ',' {
			j := i + 1
			for j < len(src) && (src[j] == ' ' || src[j] == '\t' || src[j] == '\n' || src[j] == '\r') {
				j++
			}
			if j < len(src) && (src[j] == '}' || src[j] == ']') {
				continue
			}
		}
		out = append(out, c)
	}
	return out
}

// loadTSOpts reads one tsconfig file and its `extends` chain.
func loadTSOpts(path string, seen map[string]bool, warn func(string)) (tsOpts, bool) {
	var res tsOpts
	abs, err := filepath.Abs(path)
	if err != nil {
		abs = path
	}
	if seen[abs] {
		warn(fmt.Sprintf("tsconfig extends cycle at %s", abs))
		return res, false
	}
	seen[abs] = true
	data, err := os.ReadFile(abs)
	if err != nil {
		warn(fmt.Sprintf("tsconfig: cannot read %s: %v", abs, err))
		return res, false
	}
	var raw struct {
		Extends         json.RawMessage `json:"extends"`
		CompilerOptions map[string]any  `json:"compilerOptions"`
	}
	if err := json.Unmarshal(StripJSONC(data), &raw); err != nil {
		warn(fmt.Sprintf("tsconfig: could not parse %s: %v; path aliases and compiler options from it are ignored", abs, err))
		return res, false
	}
	dir := filepath.Dir(abs)

	var specs []string
	if len(raw.Extends) > 0 {
		var one string
		var many []string
		if json.Unmarshal(raw.Extends, &one) == nil && one != "" {
			specs = []string{one}
		} else if json.Unmarshal(raw.Extends, &many) == nil {
			specs = many
		}
	}
	for _, spec := range specs {
		p := resolveTSExtends(dir, spec)
		if p == "" {
			warn(fmt.Sprintf("tsconfig: extends %q in %s was not found; its path aliases are ignored", spec, abs))
			continue
		}
		if base, ok := loadTSOpts(p, seen, warn); ok {
			res = mergeTSOpts(res, base)
		}
	}

	co := raw.CompilerOptions
	own := tsOpts{}
	if v, ok := co["experimentalDecorators"].(bool); ok {
		own.experimentalDecorators = &v
	}
	if v, ok := co["useDefineForClassFields"].(bool); ok {
		own.useDefineForClassFields = &v
	}
	if b, ok := co["baseUrl"].(string); ok && b != "" {
		own.hasBaseURL = true
		own.baseURL = filepath.Join(dir, filepath.FromSlash(tsSlashes(b)))
	}
	if pm, ok := co["paths"].(map[string]any); ok {
		own.paths = map[string][]string{}
		own.pathsDir = dir
		for prefix, tv := range pm {
			var targets []string
			switch v := tv.(type) {
			case []any:
				for _, t := range v {
					if s, ok := t.(string); ok {
						targets = append(targets, tsSlashes(s))
					}
				}
			case string:
				targets = []string{tsSlashes(v)}
			}
			if len(targets) > 0 {
				own.paths[prefix] = targets
			}
		}
	}
	return mergeTSOpts(res, own), true
}

// tsSlashes normalizes a tsconfig path to forward slashes. A path written with
// backslashes resolves on Windows, where they are separators, so it must
// resolve the same on Linux and macOS, where they are filename characters.
func tsSlashes(p string) string {
	return strings.ReplaceAll(p, `\`, "/")
}

// mergeTSOpts overlays child on base. A child `paths` replaces the base map
// wholesale, as tsc does.
func mergeTSOpts(base, child tsOpts) tsOpts {
	out := base
	if child.experimentalDecorators != nil {
		out.experimentalDecorators = child.experimentalDecorators
	}
	if child.useDefineForClassFields != nil {
		out.useDefineForClassFields = child.useDefineForClassFields
	}
	if child.hasBaseURL {
		out.hasBaseURL, out.baseURL = true, child.baseURL
	}
	if child.paths != nil {
		out.paths, out.pathsDir = child.paths, child.pathsDir
	}
	return out
}

// resolveTSExtends resolves an `extends` specifier relative to dir, or as a
// package under node_modules.
func resolveTSExtends(dir, spec string) string {
	try := func(base string) string {
		for _, c := range []string{base, base + ".json", filepath.Join(base, "tsconfig.json")} {
			if fi, err := os.Stat(c); err == nil && !fi.IsDir() {
				return c
			}
		}
		return ""
	}
	if strings.HasPrefix(spec, ".") || filepath.IsAbs(spec) {
		if filepath.IsAbs(spec) {
			return try(spec)
		}
		return try(filepath.Join(dir, filepath.FromSlash(spec)))
	}
	for d := dir; ; d = filepath.Dir(d) {
		if p := try(filepath.Join(d, "node_modules", filepath.FromSlash(spec))); p != "" {
			return p
		}
		if filepath.Dir(d) == d {
			return ""
		}
	}
}

// LoadTSConfigPaths reads tsconfig.json (JSONC, following `extends`) and
// extracts compilerOptions.paths and compilerOptions.baseUrl into the Config's
// PathAliases and TSBaseDir fields, plus the decorator/class-field options
// into TSCompilerOptions. Problems are reported through Warnings.
func (c *Config) LoadTSConfigPaths(root string) {
	tsconfigPath := filepath.Join(root, "tsconfig.json")
	if _, err := os.Stat(tsconfigPath); err != nil {
		return
	}
	warn := func(msg string) { Warnings = append(Warnings, msg) }
	o, ok := loadTSOpts(tsconfigPath, map[string]bool{}, warn)
	if !ok {
		return
	}
	if o.experimentalDecorators != nil {
		c.TSCompilerOptions.ExperimentalDecorators = *o.experimentalDecorators
	}
	if o.useDefineForClassFields != nil {
		c.TSCompilerOptions.UseDefineForClassFields = o.useDefineForClassFields
	}

	baseDir := root
	if o.hasBaseURL {
		baseDir = o.baseURL
	}
	c.TSBaseDir = baseDir
	if o.hasBaseURL {
		c.TSBaseURL = baseDir
	}

	prefixes := make([]string, 0, len(o.paths))
	for p := range o.paths {
		prefixes = append(prefixes, p)
	}
	// tsc takes the pattern with the longest literal prefix; keep that order
	// so overlapping aliases are deterministic.
	sort.Slice(prefixes, func(i, j int) bool {
		li, lj := literalPrefixLen(prefixes[i]), literalPrefixLen(prefixes[j])
		if li != lj {
			return li > lj
		}
		return prefixes[i] < prefixes[j]
	})
	var shim rootShim
	for _, prefix := range prefixes {
		targets := o.paths[prefix]
		if !o.hasBaseURL && o.pathsDir != "" && o.pathsDir != root {
			// Without baseUrl, targets are relative to the config that
			// declared them.
			abs := make([]string, len(targets))
			for i, t := range targets {
				if filepath.IsAbs(t) {
					abs[i] = t
				} else {
					abs[i] = filepath.Join(o.pathsDir, filepath.FromSlash(t))
				}
			}
			targets = abs
		}
		if o.hasBaseURL {
			targets = rootRelativeTargets(root, baseDir, targets, &shim)
		}
		c.PathAliases = append(c.PathAliases, PathAlias{Prefix: prefix, Targets: targets})
	}
	if shim.count > 0 {
		warn(shim.message(root, baseDir))
	}
}

// rootShim tallies the alias targets read relative to the project root.
type rootShim struct {
	count     int
	repeating int // targets that start with the baseUrl directory
	example   string
}

// rootRelativeTargets keeps a target that exists under baseUrl. One that does
// not, but exists when read relative to the project root, is the 0.4 template
// ("baseUrl": "./src" with "./src/*" targets): it is returned as an absolute
// root path so it resolves, and recorded for one KR7125 warning. A target that
// exists nowhere is left alone and fails as a normal unresolved import.
func rootRelativeTargets(root, baseDir string, targets []string, shim *rootShim) []string {
	rel, err := filepath.Rel(root, baseDir)
	if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return targets
	}
	rel = filepath.ToSlash(rel)
	out := make([]string, len(targets))
	for i, t := range targets {
		out[i] = t
		if filepath.IsAbs(t) || aliasTargetExists(baseDir, t) || !aliasTargetExists(root, t) {
			continue
		}
		out[i] = filepath.ToSlash(filepath.Join(root, filepath.FromSlash(t)))
		shim.count++
		clean := strings.TrimPrefix(t, "./")
		if clean == rel || strings.HasPrefix(clean, rel+"/") {
			shim.repeating++
		}
		if shim.example == "" {
			shim.example = t
		}
	}
	return out
}

func (s rootShim) message(root, baseDir string) string {
	rel, _ := filepath.Rel(root, baseDir)
	msg := "[" + string(diag.CodeTSConfigBaseURL) + "] tsconfig.json: paths target " + strconv.Quote(s.example) +
		" does not exist under baseUrl " + strconv.Quote("./"+filepath.ToSlash(rel)) +
		" but does under the project root; Krate resolves it there, tsc does not. "
	if s.repeating == s.count {
		return msg + "Run `krate fix --apply` to set baseUrl to \".\""
	}
	return msg + "Set baseUrl to \".\" or write the targets relative to baseUrl"
}

// aliasTargetExists reports whether the literal part of a paths target (the
// text before any "*") names a file or directory under dir, with or without a
// source extension.
func aliasTargetExists(dir, target string) bool {
	lit := target
	if i := strings.IndexByte(lit, '*'); i >= 0 {
		lit = lit[:i]
	}
	lit = strings.TrimRight(lit, "/")
	if lit == "" || lit == "." {
		return true
	}
	p := filepath.Join(dir, filepath.FromSlash(lit))
	if _, err := os.Stat(p); err == nil {
		return true
	}
	for _, ext := range []string{".ts", ".tsx", ".mts", ".js", ".jsx", ".mjs", ".cjs", ".json", ".css", ".md", ".mdx"} {
		if fi, err := os.Stat(p + ext); err == nil && !fi.IsDir() {
			return true
		}
	}
	return false
}

func literalPrefixLen(p string) int {
	if i := strings.IndexByte(p, '*'); i >= 0 {
		return i
	}
	return len(p) + 1
}
