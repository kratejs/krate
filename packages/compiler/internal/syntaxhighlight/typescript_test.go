package syntaxhighlight

import (
	"strings"
	"testing"

	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/lexers"
)

func TestTypeScriptGenericsHaveNoErrorTokens(t *testing.T) {
	t.Parallel()
	cases := []string{
		"const [tasks, setTasks] = createSignal<Task[]>([]);\n",
		"const [s, setS] = useState<string[]>([]);\n",
		"let x: Array<Task[]> = [];\n",
		"foo<T[]>();\n",
		"const id = <T,>(x: T[]) => x;\n",
		"const f = <T extends Item[]>(x: T) => x;\n",
		"const m: Map<string, Task[]> = new Map();\n",
		"const el = useMemo<Task[]>(() => <ul><li>{items.length}</li></ul>, []);\n",
	}
	for _, lang := range []string{"tsx", "ts", "typescript"} {
		for _, code := range cases {
			lex := lexers.Get(lang)
			it, err := lex.Tokenise(nil, code)
			if err != nil {
				t.Fatal(err)
			}
			for _, tok := range it.Tokens() {
				if tok.Type == chroma.Error {
					t.Errorf("%s: error token %q in %q", lang, tok.Value, code)
				}
			}
			if out := HighlightTheme(code, lang, DefaultTheme); strings.Contains(out, `class="err"`) {
				t.Errorf("%s: err span in output for %q", lang, code)
			}
		}
	}
}

func TestTypeScriptJSXAfterGenericCall(t *testing.T) {
	t.Parallel()
	code := "const v = useMemo<Task[]>(() => <Row id={1}>hi</Row>, []);\n"
	it, err := lexers.Get("tsx").Tokenise(nil, code)
	if err != nil {
		t.Fatal(err)
	}
	tags := map[string]bool{}
	for _, tok := range it.Tokens() {
		if tok.Type == chroma.NameTag {
			tags[tok.Value] = true
		}
		if tok.Type == chroma.NameTag && tok.Value == "Task" {
			t.Errorf("type argument Task lexed as a JSX tag")
		}
	}
	if !tags["Row"] {
		t.Errorf("JSX tag Row not recognised; tags: %v", tags)
	}
}
