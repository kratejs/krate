package build

import (
	"math/rand"
	"strconv"
	"strings"
	"testing"
)

// reachesReExportByWalk is the per-entry walk reachesReExport replaced.
func reachesReExportByWalk(texts map[string]string, entry string) bool {
	seen := map[string]bool{}
	queue := []string{entry}
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		if seen[cur] {
			continue
		}
		seen[cur] = true
		text := texts[cur]
		if strings.Contains(text, "__reExport") {
			return true
		}
		for base := range texts {
			if !seen[base] && strings.Contains(text, "./"+base) {
				queue = append(queue, base)
			}
		}
	}
	return false
}

// The graph-wide pass gives every file the answer of the per-entry walk,
// on random graphs with cycles, names that are prefixes of other names
// ("x1.js" in "x1.jsx"), self references and stray "./" text.
func TestReachesReExportMatchesWalk(t *testing.T) {
	rng := rand.New(rand.NewSource(1))
	for round := 0; round < 300; round++ {
		n := 1 + rng.Intn(14)
		names := make([]string, n)
		for i := range names {
			switch rng.Intn(3) {
			case 0:
				names[i] = "x" + strconv.Itoa(i) + ".js"
			case 1:
				names[i] = "chunk-" + strconv.Itoa(i) + ".js"
			default:
				names[i] = "x" + strconv.Itoa(i) + ".jsx"
			}
		}
		texts := map[string]string{}
		for _, name := range names {
			var b strings.Builder
			for k := rng.Intn(5); k > 0; k-- {
				switch rng.Intn(5) {
				case 0:
					b.WriteString(`import "./` + names[rng.Intn(n)] + `";` + "\n")
				case 1:
					b.WriteString(`const s = "a./` + names[rng.Intn(n)] + `m";` + "\n")
				case 2:
					b.WriteString("var y = ./ ./.\n")
				case 3:
					b.WriteString(`import "./x` + strconv.Itoa(rng.Intn(n)) + `.js";` + "\n")
				default:
					b.WriteString("noise();\n")
				}
			}
			if rng.Intn(6) == 0 {
				b.WriteString("__reExport(a, b);\n")
			}
			texts[name] = b.String()
		}
		got := reachesReExport(texts)
		for _, name := range names {
			if want := reachesReExportByWalk(texts, name); got[name] != want {
				t.Fatalf("round %d: %s: got %v, want %v\n%v", round, name, got[name], want, texts)
			}
		}
	}
}
