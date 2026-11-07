package build

import (
	"os"
	"path/filepath"
	"sort"
)

// ActionInfo describes one server action discovered from "use server" files.
type ActionInfo struct {
	ID     string `json:"id"`
	Export string `json:"export"`
	Source string `json:"source"`
}

// ListActions scans the project for "use server" modules and returns every
// exported action with its stable ID, without compiling anything. Used by the
// MCP list_actions and get_server_action_by_id tools.
func (b *Builder) ListActions() ([]ActionInfo, error) {
	files, err := discoverActionFiles(b.Root, b.Cfg.OutDir)
	if err != nil {
		return nil, err
	}
	var out []ActionInfo
	for _, abs := range files {
		rel, err := filepath.Rel(b.Root, abs)
		if err != nil {
			continue
		}
		rel = filepath.ToSlash(rel)
		data, err := os.ReadFile(abs)
		if err != nil {
			continue
		}
		exports, err := parseActionExports(string(data), rel)
		if err != nil {
			return nil, err
		}
		for name, id := range exports {
			out = append(out, ActionInfo{ID: id, Export: name, Source: rel})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Source != out[j].Source {
			return out[i].Source < out[j].Source
		}
		return out[i].Export < out[j].Export
	})
	return out, nil
}
