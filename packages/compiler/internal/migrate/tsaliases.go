package migrate

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

// tsAliases holds the import aliases of tsconfig.json (or jsconfig.json):
// compilerOptions.paths and baseUrl, following relative `extends` chains.
type tsAliases struct {
	root    string
	baseURL string // absolute, empty when unset
	pathsAt string // absolute directory paths targets are relative to
	paths   map[string][]string
}

type tsConfigLayer struct {
	baseURL string
	paths   map[string][]string
	dir     string
}

func loadTSAliases(root string) tsAliases {
	a := tsAliases{root: root}
	for _, name := range []string{"tsconfig.json", "jsconfig.json"} {
		path := filepath.Join(root, name)
		if !fileExists(path) {
			continue
		}
		var baseURL, pathsDir string
		var paths map[string][]string
		for _, layer := range readTSChain(path, 0) {
			if layer.baseURL != "" {
				baseURL = layer.baseURL
			}
			if layer.paths != nil {
				paths = layer.paths
				pathsDir = layer.dir
			}
		}
		a.baseURL = baseURL
		a.paths = paths
		a.pathsAt = pathsDir
		if baseURL != "" {
			a.pathsAt = baseURL
		}
		return a
	}
	return a
}

// readTSChain returns the config layers from the farthest base to path.
func readTSChain(path string, depth int) []tsConfigLayer {
	if depth > 5 {
		return nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var cfg struct {
		Extends         json.RawMessage `json:"extends"`
		CompilerOptions struct {
			BaseURL string              `json:"baseUrl"`
			Paths   map[string][]string `json:"paths"`
		} `json:"compilerOptions"`
	}
	if err := json.Unmarshal([]byte(stripJSONC(string(data))), &cfg); err != nil {
		return nil
	}
	dir := filepath.Dir(path)
	var out []tsConfigLayer
	var parents []string
	if len(cfg.Extends) > 0 {
		var one string
		var many []string
		if json.Unmarshal(cfg.Extends, &one) == nil {
			parents = []string{one}
		} else if json.Unmarshal(cfg.Extends, &many) == nil {
			parents = many
		}
	}
	for _, p := range parents {
		if !strings.HasPrefix(p, ".") {
			continue
		}
		ref := filepath.Join(dir, filepath.FromSlash(p))
		if filepath.Ext(ref) != ".json" {
			ref += ".json"
		}
		out = append(out, readTSChain(ref, depth+1)...)
	}
	layer := tsConfigLayer{dir: dir, paths: cfg.CompilerOptions.Paths}
	if cfg.CompilerOptions.BaseURL != "" {
		layer.baseURL = filepath.Join(dir, filepath.FromSlash(cfg.CompilerOptions.BaseURL))
	}
	return append(out, layer)
}

// candidates returns project-relative import bases for spec, best match first.
// outside reports that a matching alias points beyond the project root.
func (a tsAliases) candidates(spec string) (out []string, outside bool) {
	add := func(abs string) {
		rel, err := filepath.Rel(a.root, abs)
		if err != nil {
			return
		}
		rel = filepath.ToSlash(rel)
		if rel == ".." || strings.HasPrefix(rel, "../") {
			outside = true
			return
		}
		out = append(out, rel)
	}
	bestKey, bestStar, bestLen := "", "", -1
	for key := range a.paths {
		star := strings.Index(key, "*")
		switch {
		case star < 0:
			if key == spec && len(key) > bestLen {
				bestKey, bestStar, bestLen = key, "", len(key)
			}
		default:
			pre, suf := key[:star], key[star+1:]
			if len(spec) >= len(pre)+len(suf) && strings.HasPrefix(spec, pre) && strings.HasSuffix(spec, suf) && len(pre) > bestLen {
				bestKey, bestStar, bestLen = key, spec[len(pre):len(spec)-len(suf)], len(pre)
			}
		}
	}
	if bestLen >= 0 {
		for _, target := range a.paths[bestKey] {
			add(filepath.Join(a.pathsAt, filepath.FromSlash(strings.Replace(target, "*", bestStar, 1))))
		}
	}
	if a.baseURL != "" {
		add(filepath.Join(a.baseURL, filepath.FromSlash(spec)))
	}
	return out, outside
}
