package migrate

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

// tsconfigPlan generates or merges the TypeScript config Krate needs while
// preserving path aliases and other user settings.
func tsconfigPlan(root string, rep *Report) (PlannedFile, bool, string) {
	path := filepath.Join(root, "tsconfig.json")
	if fileExists(path) {
		data, err := os.ReadFile(path)
		if err != nil {
			return PlannedFile{}, false, err.Error()
		}
		clean := stripJSONC(string(data))
		var cfg map[string]any
		if err := json.Unmarshal([]byte(clean), &cfg); err != nil {
			return PlannedFile{}, false, "tsconfig.json could not be parsed: " + err.Error()
		}
		opts, _ := cfg["compilerOptions"].(map[string]any)
		if opts == nil {
			opts = map[string]any{}
		}
		opts["jsx"] = "preserve"
		opts["jsxImportSource"] = "@krate/runtime"
		opts["moduleResolution"] = "bundler"
		opts["allowArbitraryExtensions"] = true
		opts["resolveJsonModule"] = true
		cfg["compilerOptions"] = opts
		cfg["include"] = krateIncludes(cfg["include"])
		out, err := json.MarshalIndent(cfg, "", "  ")
		if err != nil {
			return PlannedFile{}, false, err.Error()
		}
		return PlannedFile{Kind: KindConfig, Source: "tsconfig.json", Target: "tsconfig.json", Content: append(out, '\n')}, true, ""
	}

	// Only generate for projects that actually contain TypeScript.
	hasTS := false
	for _, f := range rep.Files {
		ext := strings.ToLower(filepath.Ext(f.Source))
		if ext == ".ts" || ext == ".tsx" {
			hasTS = true
			break
		}
	}
	if !hasTS {
		return PlannedFile{}, false, ""
	}
	cfg := map[string]any{
		"compilerOptions": map[string]any{
			"target":                   "ES2022",
			"module":                   "ESNext",
			"moduleResolution":         "bundler",
			"jsx":                      "preserve",
			"jsxImportSource":          "@krate/runtime",
			"allowArbitraryExtensions": true,
			"strict":                   true,
			"esModuleInterop":          true,
			"skipLibCheck":             true,
			"resolveJsonModule":        true,
			"baseUrl":                  ".",
			"paths":                    map[string]any{"@/*": []any{"./src/*"}},
		},
		"include": []any{"src", ".krate/types"},
	}
	out, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return PlannedFile{}, false, err.Error()
	}
	return PlannedFile{Kind: KindConfig, Target: "tsconfig.json", Content: append(out, '\n')}, true, ""
}

func krateIncludes(v any) []any {
	var out []any
	seen := map[string]bool{}
	add := func(item string) {
		if item == "" || seen[item] {
			return
		}
		seen[item] = true
		out = append(out, item)
	}
	arr, _ := v.([]any)
	for _, item := range arr {
		s, ok := item.(string)
		if !ok {
			out = append(out, item)
			continue
		}
		if strings.Contains(s, "next-env") || strings.Contains(s, ".next/") {
			continue
		}
		add(s)
	}
	add("src")
	add(".krate/types")
	return out
}

// stripJSONC removes // and /* */ comments and trailing commas outside strings.
func stripJSONC(src string) string {
	var b strings.Builder
	inString, escape := false, false
	for i := 0; i < len(src); i++ {
		c := src[i]
		if inString {
			b.WriteByte(c)
			if escape {
				escape = false
			} else if c == '\\' {
				escape = true
			} else if c == '"' {
				inString = false
			}
			continue
		}
		if c == '"' {
			inString = true
			b.WriteByte(c)
			continue
		}
		if c == '/' && i+1 < len(src) {
			if src[i+1] == '/' {
				for i < len(src) && src[i] != '\n' {
					i++
				}
				if i < len(src) {
					b.WriteByte('\n')
				}
				continue
			}
			if src[i+1] == '*' {
				i += 2
				for i+1 < len(src) && !(src[i] == '*' && src[i+1] == '/') {
					i++
				}
				i++
				continue
			}
		}
		if c == ',' {
			j := i + 1
			for j < len(src) && (src[j] == ' ' || src[j] == '\t' || src[j] == '\r' || src[j] == '\n') {
				j++
			}
			if j < len(src) && (src[j] == '}' || src[j] == ']') {
				continue
			}
		}
		b.WriteByte(c)
	}
	return b.String()
}
