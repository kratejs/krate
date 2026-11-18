package config

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// tsTypesPath is @krate/core's config type file, relative to this package.
var tsTypesPath = filepath.Join("..", "..", "..", "core", "src", "config.ts")

var (
	tsInterfaceRe = regexp.MustCompile(`(?m)^export interface (\w+)(?:<[^>]*>)?\s*\{`)
	tsPropRe      = regexp.MustCompile(`^\s*"?([A-Za-z_$][\w$]*)"?\??\s*:\s*(.*)$`)
)

// tsBlock returns the text between the brace at open and its match.
func tsBlock(src string, open int) string {
	depth := 0
	for i := open; i < len(src); i++ {
		switch src[i] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return src[open+1 : i]
			}
		}
	}
	return src[open+1:]
}

type tsProp struct {
	typ    string
	inline string // body of an inline object type, if any
}

// tsProps lists the direct members of an interface or inline object body,
// skipping comments and nested object bodies.
func tsProps(body string) map[string]tsProp {
	out := map[string]tsProp{}
	depth := 0
	inComment := false
	lines := strings.Split(body, "\n")
	offset := 0
	for _, line := range lines {
		lineStart := offset
		offset += len(line) + 1
		trimmed := strings.TrimSpace(line)
		if inComment {
			if strings.Contains(trimmed, "*/") {
				inComment = false
			}
			continue
		}
		if strings.HasPrefix(trimmed, "/*") {
			if !strings.Contains(trimmed, "*/") {
				inComment = true
			}
			continue
		}
		if strings.HasPrefix(trimmed, "//") {
			continue
		}
		if depth == 0 {
			if m := tsPropRe.FindStringSubmatch(line); m != nil {
				p := tsProp{typ: strings.TrimSpace(m[2])}
				if idx := strings.Index(line, "{"); idx >= 0 && strings.Contains(m[2], "{") {
					p.inline = tsBlock(body, lineStart+idx)
				}
				out[m[1]] = p
			}
		}
		depth += strings.Count(line, "{") - strings.Count(line, "}")
	}
	return out
}

// TestTSConfigTypesCoverGoKeys fails when krate.config.ts accepts a key (per
// knownTopLevelKeys/knownNestedKeys) that @krate/core's KrateConfig type does
// not declare, so a valid config never fails to typecheck.
func TestTSConfigTypesCoverGoKeys(t *testing.T) {
	data, err := os.ReadFile(tsTypesPath)
	if err != nil {
		t.Fatalf("read %s: %v", tsTypesPath, err)
	}
	src := string(data)
	interfaces := map[string]string{}
	for _, m := range tsInterfaceRe.FindAllStringSubmatchIndex(src, -1) {
		name := src[m[2]:m[3]]
		interfaces[name] = tsBlock(src, m[1]-1)
	}
	root, ok := interfaces["KrateConfig"]
	if !ok {
		t.Fatal("KrateConfig interface not found")
	}
	top := tsProps(root)
	identRe := regexp.MustCompile(`^(\w+)`)
	nestedOf := func(p tsProp) map[string]tsProp {
		if p.inline != "" {
			return tsProps(p.inline)
		}
		merged := map[string]tsProp{}
		for _, part := range strings.Split(strings.TrimSuffix(p.typ, ";"), "|") {
			m := identRe.FindStringSubmatch(strings.TrimSpace(part))
			if m == nil {
				continue
			}
			if body, ok := interfaces[m[1]]; ok {
				for k, v := range tsProps(body) {
					merged[k] = v
				}
			}
		}
		return merged
	}

	var missing []string
	for k := range knownTopLevelKeys {
		if _, ok := top[k]; !ok {
			missing = append(missing, k)
		}
	}
	for k, nested := range knownNestedKeys {
		p, ok := top[k]
		if !ok {
			continue
		}
		have := nestedOf(p)
		if len(have) == 0 {
			// Typed loosely (Record, string[] union and similar).
			continue
		}
		for n := range nested {
			if _, ok := have[n]; !ok {
				missing = append(missing, k+"."+n)
			}
		}
	}
	if sc, ok := nestedOf(top["api"])["sidecar"]; ok {
		have := nestedOf(sc)
		for n := range knownSidecarKeys {
			if _, ok := have[n]; !ok {
				missing = append(missing, "api.sidecar."+n)
			}
		}
	} else {
		missing = append(missing, "api.sidecar")
	}
	sort.Strings(missing)
	if len(missing) > 0 {
		t.Errorf("config keys the Go config accepts but %s does not type:\n  %s", tsTypesPath, strings.Join(missing, "\n  "))
	}
}
