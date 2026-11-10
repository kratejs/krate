package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// packageManagerFor names the package manager a project uses: the
// packageManager field of package.json, else a lockfile, else "npm". The
// search walks up from root so a workspace package finds the workspace lockfile.
func packageManagerFor(root string) string {
	if data, err := os.ReadFile(filepath.Join(root, "package.json")); err == nil {
		var pkg struct {
			PackageManager string `json:"packageManager"`
		}
		if json.Unmarshal(data, &pkg) == nil && pkg.PackageManager != "" {
			name, _, _ := strings.Cut(pkg.PackageManager, "@")
			switch name {
			case "npm", "pnpm", "yarn", "bun":
				return name
			}
		}
	}
	locks := []struct{ file, pm string }{
		{"pnpm-lock.yaml", "pnpm"},
		{"bun.lock", "bun"},
		{"bun.lockb", "bun"},
		{"yarn.lock", "yarn"},
		{"package-lock.json", "npm"},
		{"npm-shrinkwrap.json", "npm"},
	}
	for dir := root; ; {
		for _, l := range locks {
			if _, err := os.Stat(filepath.Join(dir, l.file)); err == nil {
				return l.pm
			}
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return "npm"
}

// installedKratePackage is one @krate/* package found in node_modules.
type installedKratePackage struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

// installedKratePackages lists the @krate/* packages installed under
// root/node_modules, sorted by name.
func installedKratePackages(root string) []installedKratePackage {
	var out []installedKratePackage
	scope := filepath.Join(root, "node_modules", "@krate")
	entries, err := os.ReadDir(scope)
	if err != nil {
		return nil
	}
	for _, e := range entries {
		data, err := os.ReadFile(filepath.Join(scope, e.Name(), "package.json"))
		if err != nil {
			continue
		}
		var pkg struct {
			Name    string `json:"name"`
			Version string `json:"version"`
		}
		if json.Unmarshal(data, &pkg) != nil || pkg.Version == "" {
			continue
		}
		name := pkg.Name
		if name == "" {
			name = "@krate/" + e.Name()
		}
		out = append(out, installedKratePackage{Name: name, Version: pkg.Version})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}
