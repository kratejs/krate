package gohost

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
)

// Init sets up the Go workspace for editor intellisense (gopls): it vendors the
// helper module, writes a project go.mod (when absent) and go.work, and adds
// `.krate/types` to the tsconfig include. Safe to run repeatedly.
func Init(root string) ([]string, error) {
	var notes []string
	if err := WriteGoextModule(root); err != nil {
		return nil, err
	}

	goModPath := filepath.Join(root, "go.mod")
	if _, err := os.Stat(goModPath); os.IsNotExist(err) {
		mod := moduleName(root)
		rel := filepath.ToSlash(filepath.Join(KrateDirName, GoextModuleName))
		content := "module " + mod + "\n\ngo " + goVersionShort() + "\n\n" +
			"require " + GoextModulePath + " v0.0.0\n\n" +
			"replace " + GoextModulePath + " => " + rel + "\n"
		if werr := os.WriteFile(goModPath, []byte(content), 0644); werr != nil {
			return nil, werr
		}
		notes = append(notes, "wrote go.mod (module "+mod+")")
	} else {
		notes = append(notes, "kept existing go.mod")
	}

	writeGoWork(root)
	notes = append(notes, "wrote "+GoWorkName)

	tsPath := filepath.Join(root, "tsconfig.json")
	if _, err := os.Stat(tsPath); err == nil {
		if added, err := ensureTsconfigInclude(tsPath, ".krate/types"); err != nil {
			notes = append(notes, "tsconfig: "+err.Error())
		} else if added {
			notes = append(notes, "added .krate/types to tsconfig include")
		} else {
			notes = append(notes, "tsconfig already includes .krate/types")
		}
	}
	return notes, nil
}

var moduleNameSanitizeRe = regexp.MustCompile(`[^a-z0-9._/-]+`)

func moduleName(root string) string {
	base := strings.ToLower(filepath.Base(filepath.Clean(root)))
	base = moduleNameSanitizeRe.ReplaceAllString(base, "-")
	base = strings.Trim(base, "-./")
	if base == "" {
		base = "krate-app"
	}
	return base
}

func goVersionShort() string {
	v := strings.TrimPrefix(runtime.Version(), "go")
	if v == "" || strings.ContainsAny(v, "devel ") {
		return "1.21"
	}
	return v
}

func ensureTsconfigInclude(path, dir string) (bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return false, err
	}
	var doc map[string]any
	if err := json.Unmarshal(data, &doc); err != nil {
		return false, fmt.Errorf("not plain JSON; add %q to include manually", dir)
	}
	var include []string
	if raw, ok := doc["include"].([]any); ok {
		for _, v := range raw {
			if s, ok := v.(string); ok {
				if s == dir {
					return false, nil
				}
				include = append(include, s)
			}
		}
	}
	include = append(include, dir)
	arr := make([]any, len(include))
	for i, s := range include {
		arr[i] = s
	}
	doc["include"] = arr
	out, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return false, err
	}
	out = append(out, '\n')
	return true, os.WriteFile(path, out, 0644)
}
