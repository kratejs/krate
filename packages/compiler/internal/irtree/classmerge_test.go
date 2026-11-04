package irtree_test

import (
	"encoding/json"
	"fmt"
	"html"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

type classMergeVector struct {
	Name     string            `json:"name"`
	Fn       string            `json:"fn"`
	Args     []json.RawMessage `json:"args"`
	Src      string            `json:"src"`
	Base     json.RawMessage   `json:"base"`
	Config   json.RawMessage   `json:"config"`
	Select   json.RawMessage   `json:"selection"`
	Expected string            `json:"expected"`
}

func loadClassMergeVectors(t *testing.T) []classMergeVector {
	t.Helper()
	path := filepath.Join("..", "..", "..", "runtime", "src", "testdata", "class-merge-vectors.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read shared vectors: %v", err)
	}
	var vectors []classMergeVector
	if err := json.Unmarshal(data, &vectors); err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	if len(vectors) < 100 {
		t.Fatalf("only %d vectors in %s", len(vectors), path)
	}
	return vectors
}

var classAttrRe = regexp.MustCompile(`class="([^"]*)"`)

// TestClassMergeSharedVectors checks that statically folded cn/twMerge/clsx
// calls match the runtime on the vectors shared with the Vitest suite.
func TestClassMergeSharedVectors(t *testing.T) {
	for i, v := range loadClassMergeVectors(t) {
		t.Run(fmt.Sprintf("%03d_%s", i, v.Fn), func(t *testing.T) {
			args := make([]string, len(v.Args))
			for j, a := range v.Args {
				args[j] = string(a)
			}
			argSrc := strings.Join(args, ", ")
			if v.Src != "" {
				argSrc = v.Src
			}
			src := fmt.Sprintf("export default function App() { return <div class={%s(%s)}>x</div>; }", v.Fn, argSrc)
			if v.Fn == "cva" {
				decl := string(v.Base)
				if len(v.Config) > 0 {
					decl += ", " + string(v.Config)
				}
				src = fmt.Sprintf("const variants = cva(%s);\nexport default function App() { return <div class={variants(%s)}>x</div>; }", decl, v.Select)
			}
			tree := buildStaticTreeWithJS(t, src)
			got := renderStaticHTML(tree.Root.Children)
			want := v.Expected
			var have string
			if m := classAttrRe.FindStringSubmatch(got); m != nil {
				have = html.UnescapeString(m[1])
			} else if want != "" {
				t.Fatalf("%s: no class attribute in %q, want %q", v.Name, got, want)
			}
			if have != want {
				t.Fatalf("%s: class = %q, want %q (html %q)", v.Name, have, want, got)
			}
		})
	}
}

func TestStaticCvaMergesClassOverride(t *testing.T) {
	src := `const button = cva("px-4 py-2 bg-blue-500", {
  variants: { size: { sm: "px-2 text-sm", lg: "px-8 text-lg" } },
  defaultVariants: { size: "sm" },
});
export default function App() { return <button class={button({ class: "px-10 bg-green-500" })}>x</button>; }`
	tree := buildStaticTreeWithJS(t, src)
	want := `<button class="py-2 text-sm px-10 bg-green-500">x</button>`
	if got := renderStaticHTML(tree.Root.Children); got != want {
		t.Errorf("HTML = %q, want %q", got, want)
	}
}

func TestStaticCnMergesConflictingClasses(t *testing.T) {
	cases := []struct{ src, want string }{
		{`<div class={cn("p-2", "p-4")}>x</div>`, `<div class="p-4">x</div>`},
		{`<div className={cn("p-2", "p-4")}>x</div>`, `<div class="p-4">x</div>`},
		{`<div class={twMerge("duration-700", "duration-500")}>x</div>`, `<div class="duration-500">x</div>`},
		{`<div class={clsx("p-2", "p-4")}>x</div>`, `<div class="p-2 p-4">x</div>`},
	}
	for _, tc := range cases {
		tree := buildStaticTreeWithJS(t, "export default function App() { return "+tc.src+"; }")
		if got := renderStaticHTML(tree.Root.Children); got != tc.want {
			t.Errorf("%s: HTML = %q, want %q", tc.src, got, tc.want)
		}
	}
}
