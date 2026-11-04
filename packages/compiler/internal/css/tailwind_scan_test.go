package css

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
)

func scanSet(src string) map[string]bool {
	m := map[string]bool{}
	for _, c := range candidateTokens(src) {
		m[c] = true
	}
	return m
}

func wantScanned(t *testing.T, src string, want ...string) {
	t.Helper()
	got := scanSet(src)
	for _, w := range want {
		if !got[w] {
			t.Errorf("scanner lost %q from %q; got %v", w, src, got)
		}
	}
}

func TestScanApostropheInProse(t *testing.T) {
	wantScanned(t, `<p>Don't panic</p><div class="mx-8 p-6 transition text-red-500">`, "mx-8", "p-6", "transition", "text-red-500")
	wantScanned(t, "It's a doc.\n\n<div className=\"flex gap-4\">", "flex", "gap-4")
}

func TestScanRegexLiteral(t *testing.T) {
	wantScanned(t, `const re = /["']/g; const a = "mt-2 p-4"; <b class='flex-1 gap-2'/>`, "mt-2", "p-4", "flex-1", "gap-2")
}

func TestScanTemplateInterpolation(t *testing.T) {
	wantScanned(t, "`flex ${open ? \"block\" : \"hidden\"} p-4`", "flex", "block", "hidden", "p-4")
}

func TestScanArbitraryValues(t *testing.T) {
	toks := []string{
		"data-[state=open]:p-4", "aria-[sort=ascending]:underline", "[&_p]:mt-2",
		"[&:nth-child(3)]:underline", "[&>svg]:size-4", "has-[>img]:p-2",
		"nth-[2n+1]:bg-red-500", "content-['x']", "bg-[url('/a.png')]",
		"bg-(--brand)", "hover:bg-(--brand)/50", "w-[calc(100%-2rem)]",
		"[mask-type:alpha]", "@[400px]:grid", "supports-[display:grid]:grid",
		"content-[')']", "-mt-2", "w-1/2", "p-0.5", "!font-bold", "font-bold!",
	}
	wantScanned(t, `<div class="`+strings.Join(toks, " ")+`">`, toks...)
	// Each token also survives when quoted and adjacent to punctuation.
	for _, tok := range toks {
		wantScanned(t, `cn("`+tok+`", cond && 'p-9')`, tok, "p-9")
	}
}

func TestScanCodeArguments(t *testing.T) {
	wantScanned(t, `cn("flex","block",cond&&"hidden") + ["gap-2","gap-4"]`, "flex", "block", "hidden", "gap-2", "gap-4")
	wantScanned(t, `cva("inline-flex", { variants: { size: { sm: "h-8 px-3", lg: "h-12" } } })`, "inline-flex", "h-8", "px-3", "h-12")
}

func TestScanDynamicPrefixGeneratesNothing(t *testing.T) {
	got := scanSet("`bg-${color}` `text-${x}-500` 'p-' + n")
	for _, bad := range []string{"bg-", "text-", "p-", "-500"} {
		if got[bad] {
			t.Errorf("dynamic prefix %q emitted", bad)
		}
	}
}

func TestScanRejectsProseAndIdentifiers(t *testing.T) {
	got := scanSet(`const message = "this is a normal sentence of prose"; arr[0]; foo.bar(baz)`)
	for _, bad := range []string{"normal", "sentence", "prose", "arr[0]", "foo.bar", "message"} {
		if got[bad] {
			t.Errorf("emitted non-utility %q", bad)
		}
	}
}

func TestScanEscapedNewlineInJSString(t *testing.T) {
	wantScanned(t, `"flex\nblock\thidden"`, "flex", "block", "hidden")
}

// Every single-word utility the generator supports must survive the scanner.
func TestScannerKeepsEveryBareUtility(t *testing.T) {
	words := map[string]bool{}
	files, _ := filepath.Glob("*.go")
	wordRe := regexp.MustCompile(`^[a-z]+$`)
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		fset := token.NewFileSet()
		af, err := parser.ParseFile(fset, f, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		ast.Inspect(af, func(n ast.Node) bool {
			if bl, ok := n.(*ast.BasicLit); ok && bl.Kind == token.STRING {
				if s, err := strconv.Unquote(bl.Value); err == nil && wordRe.MatchString(s) {
					words[s] = true
				}
			}
			return true
		})
	}
	theme := DefaultTailwindTheme()
	var missing []string
	for w := range words {
		// brightness/contrast/saturate are accepted by the generator but are not
		// v4 utilities; keeping them out avoids generating rules from prose.
		if w == "brightness" || w == "contrast" || w == "saturate" {
			continue
		}
		if text, _ := classToRule(w, theme); text != "" && !looksLikeUtility(w) {
			missing = append(missing, w)
		}
	}
	sort.Strings(missing)
	if len(missing) > 0 {
		t.Errorf("generator supports bare utilities the scanner drops: %v", missing)
	}
	// Spot check the known miss and hyphenated keywords.
	for _, w := range []string{"transition", "flex", "hidden", "truncate", "container", "underline"} {
		if !looksLikeUtility(w) {
			t.Errorf("%q dropped", w)
		}
	}
}

func TestScanDeterministicOrder(t *testing.T) {
	src := `<a class="b-1 a-1 c-1 a-1">`
	a := strings.Join(candidateTokens(src), ",")
	b := strings.Join(candidateTokens(src), ",")
	if a != b {
		t.Fatal("non-deterministic")
	}
}
