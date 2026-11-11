package sdk

import (
	"bytes"
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

var updateIDs = flag.Bool("update-ids", false, "rewrite ids.json")

// idsEntry is one row of ids.json, the SDK id table as the compiler reads it
// (capability analysis, see packages/compiler/internal/native/capability).
type idsEntry struct {
	ID         string `json:"id"`
	Permission string `json:"permission,omitempty"`
	// Area is the SDK area (Go package sdk/<area>).
	Area string `json:"area"`
	// Module is the @krate/app subpath ("@krate/app/<module>") that wraps it.
	Module string `json:"module"`
	// Exports are the names the module exports that reach this id.
	Exports []string `json:"exports"`
}

var (
	declRe        = regexp.MustCompile(`(?m)^(export\s+)?(?:async\s+)?(?:function|class|const|let)\s+([A-Za-z_$][\w$]*)`)
	exportListRe  = regexp.MustCompile(`export\s*\{([^}]*)\}`)
	lineCommentR  = regexp.MustCompile(`(?m)//[^\n]*`)
	blockCommentR = regexp.MustCompile(`(?s)/\*.*?\*/`)
)

// tsDecls maps each top-level value of a TS module source to its body text
// without comments, and lists which of them are exported.
func tsDecls(src string) (bodies map[string]string, exported map[string]bool) {
	src = blockCommentR.ReplaceAllString(src, "")
	src = lineCommentR.ReplaceAllString(src, "")
	locs := declRe.FindAllStringSubmatchIndex(src, -1)
	bodies, exported = map[string]string{}, map[string]bool{}
	for i, l := range locs {
		end := len(src)
		if i+1 < len(locs) {
			end = locs[i+1][0]
		}
		name := src[l[4]:l[5]]
		bodies[name] = src[l[0]:end]
		if l[2] >= 0 {
			exported[name] = true
		}
	}
	// export { del as delete }
	for _, m := range exportListRe.FindAllStringSubmatch(src, -1) {
		for _, item := range strings.Split(m[1], ",") {
			local, remote, _ := strings.Cut(strings.TrimSpace(item), " as ")
			local, remote = strings.TrimSpace(local), strings.TrimSpace(remote)
			if remote == "" {
				remote = local
			}
			if body, ok := bodies[local]; ok && local != "" {
				bodies[remote] = body
				exported[remote] = true
			}
		}
	}
	return
}

// buildIDsJSON derives the export names of every id from packages/app/src.
func buildIDsJSON(t *testing.T) []byte {
	t.Helper()
	appSrc := filepath.Join("..", "..", "app", "src")
	type tsMod struct {
		bodies   map[string]string
		exported map[string]bool
	}
	srcs := map[string]*tsMod{} // ts file -> declarations
	var rows []idsEntry
	for _, id := range IDs {
		area, op, _ := strings.Cut(strings.TrimPrefix(id.Name, "krate/"), ".")
		mod := id.TS
		if mod == "lifecycle" {
			mod = "app"
		}
		if srcs[id.TS] == nil {
			b, err := os.ReadFile(filepath.Join(appSrc, id.TS+".ts"))
			if err != nil {
				t.Fatalf("reading the wrapper of %s: %v", id.Name, err)
			}
			bodies, exported := tsDecls(string(b))
			srcs[id.TS] = &tsMod{bodies, exported}
		}
		exports := srcs[id.TS].bodies
		reach := map[string]bool{}
		for name, body := range exports {
			if strings.Contains(body, `"`+id.Name+`"`) ||
				(strings.Contains(body, "krate/"+area+".${") && strings.Contains(body, `"`+op+`"`)) {
				reach[name] = true
			}
		}
		// A declaration that calls (or constructs) a reaching one of the same
		// module reaches the id too (open() builds a Window whose methods call
		// the ids; an exported function may use a private helper).
		for changed := true; changed; {
			changed = false
			for name, body := range exports {
				if reach[name] {
					continue
				}
				for other := range reach {
					if regexp.MustCompile(`(?:\bnew\s+|\b)` + regexp.QuoteMeta(other) + `\s*\(`).MatchString(body) {
						reach[name] = true
						changed = true
						break
					}
				}
			}
		}
		e := idsEntry{ID: id.Name, Permission: id.Permission, Area: area, Module: mod, Exports: []string{}}
		for n := range reach {
			if srcs[id.TS].exported[n] {
				e.Exports = append(e.Exports, n)
			}
		}
		sort.Strings(e.Exports)
		rows = append(rows, e)
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetIndent("", "  ")
	if err := enc.Encode(map[string]any{"ids": rows}); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// TestIDsJSON keeps ids.json, the compiler's copy of the id table, in step
// with ids.go and the @krate/app wrappers. Refresh it with
// "go test ./sdk -run IDsJSON -update-ids" and copy it to
// packages/compiler/internal/native/capability/ids.json.
func TestIDsJSON(t *testing.T) {
	want := buildIDsJSON(t)
	if *updateIDs {
		if err := os.WriteFile("ids.json", want, 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	got, err := os.ReadFile("ids.json")
	if err != nil {
		t.Fatalf("ids.json: %v (run: go test ./sdk -run IDsJSON -update-ids)", err)
	}
	if !bytes.Equal(got, want) {
		t.Fatal("ids.json is stale; run: go test ./sdk -run IDsJSON -update-ids")
	}
	// Every id is reachable from some export: a wrapper that no longer names
	// its id would silently drop its permission from the analysis.
	var doc struct{ IDs []idsEntry }
	if err := json.Unmarshal(got, &doc); err != nil {
		t.Fatal(err)
	}
	for _, e := range doc.IDs {
		if len(e.Exports) == 0 {
			t.Errorf("%s is reached by no export of @krate/app/%s", e.ID, e.Module)
		}
	}
}
