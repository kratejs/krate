package astjson

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"testing"
)

// TestASTTypesInSyncWithRegistry guards the JS plugin SDK's ASTTypes object
// (packages/plugin/src/ast.ts) against drifting from the Go node registry.
// Every node kind the encoder can emit must appear as a value in ASTTypes, and
// ASTTypes must not advertise a kind the decoder would reject.
func TestASTTypesInSyncWithRegistry(t *testing.T) {
	src, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "packages", "plugin", "src", "ast.ts"))
	if err != nil {
		t.Skipf("ast.ts not found (running outside the monorepo?): %v", err)
	}

	// Match the `ASTTypes` const block and pull out its string values.
	block := regexp.MustCompile(`(?s)export const ASTTypes = Object\.freeze\(\{(.*?)\} as const\);`).FindSubmatch(src)
	if block == nil {
		t.Fatal("could not locate ASTTypes const block in ast.ts")
	}
	valueRe := regexp.MustCompile(`:\s*"([A-Za-z]+)"`)
	tsKinds := map[string]bool{}
	for _, m := range valueRe.FindAllSubmatch(block[1], -1) {
		tsKinds[string(m[1])] = true
	}

	goKinds := map[string]bool{}
	for name := range nodeTypes {
		goKinds[name] = true
	}

	var missing, extra []string
	for name := range goKinds {
		if !tsKinds[name] {
			missing = append(missing, name)
		}
	}
	for name := range tsKinds {
		if !goKinds[name] {
			extra = append(extra, name)
		}
	}
	sort.Strings(missing)
	sort.Strings(extra)
	if len(missing) > 0 {
		t.Errorf("ASTTypes is missing Go node kinds: %v", missing)
	}
	if len(extra) > 0 {
		t.Errorf("ASTTypes lists kinds not in the Go registry: %v", extra)
	}
}
