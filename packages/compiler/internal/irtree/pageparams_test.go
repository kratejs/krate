package irtree_test

import (
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/annotator"
	"github.com/kratejs/krate/packages/compiler/internal/config"
	"github.com/kratejs/krate/packages/compiler/internal/irtree"
)

func buildPageWithParams(t *testing.T, src string, params map[string]string, dynamic bool) *irtree.ComponentTree {
	t.Helper()
	prog := parseProg(t, src)
	ann := annotator.Annotate(prog, &config.Config{}, "page.tsx", src)
	return irtree.BuildWithOptions(prog, ann, irtree.BuildOptions{
		PageParams:        params,
		PageParamsDynamic: dynamic,
	})
}

func TestPageParamsFoldDestructuredModuleData(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want string
	}{
		{
			name: "object then array destructuring",
			src: `const ROWS = { a: ["Alpha", "x"], b: ["Beta", "y"] };
export default function Page({ params }) {
	const { slug } = params;
	const [out] = ROWS[slug];
	return <h1>{out}</h1>;
}`,
			want: "<h1>Alpha</h1>",
		},
		{
			name: "nested param destructuring",
			src: `const ROWS = { a: ["Alpha", "x"] };
export default function Page({ params: { slug } }) {
	const [out] = ROWS[slug];
	return <h1>{out}</h1>;
}`,
			want: "<h1>Alpha</h1>",
		},
		{
			name: "nested body object destructuring",
			src: `const ITEMS = { a: { title: "Alpha", meta: { tag: "v1" } } };
export default function Page({ params }) {
	const { slug } = params;
	const { title, meta: { tag } } = ITEMS[slug];
	return <h1>{title}:{tag}</h1>;
}`,
			want: "<h1>Alpha:v1</h1>",
		},
		{
			name: "plain props object",
			src: `const ITEMS = { a: { title: "Alpha" } };
export default function Page(props) {
	const { slug } = props.params;
	const title = ITEMS[slug].title;
	return <h1>{title}</h1>;
}`,
			want: "<h1>Alpha</h1>",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tree := buildPageWithParams(t, tc.src, map[string]string{"slug": "a"}, false)
			if tree.Root.Tier != irtree.TierStatic {
				t.Fatalf("tier = %v, want static (PreSignalVars=%v ExtraVars=%v)", tree.Root.Tier, tree.Root.PreSignalVars, tree.Root.ExtraVars)
			}
			if got := renderStaticHTML(tree.Root.Children); got != tc.want {
				t.Fatalf("HTML = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestPageParamsHydrationFallback(t *testing.T) {
	t.Run("concrete page binds params when client code reads it", func(t *testing.T) {
		src := `export default function Page({ params }) {
	const label = Object.values(params).join(",") + ":" + Math.random();
	return <h1>{label}</h1>;
}`
		tree := buildPageWithParams(t, src, map[string]string{"slug": "a"}, false)
		if tree.Root.Tier != irtree.TierClient {
			t.Fatalf("tier = %v, want client", tree.Root.Tier)
		}
		joined := strings.Join(tree.Root.PreSignalVars, ";")
		if !strings.Contains(joined, `var params={slug:'a'}`) {
			t.Fatalf("PreSignalVars = %v, want var params={slug:'a'}", tree.Root.PreSignalVars)
		}
	})

	t.Run("dynamic template binds window.__KRATE_PARAMS__ when client code reads params", func(t *testing.T) {
		src := `const ROWS = { a: ["Alpha", "x"] };
export default function Page({ params }) {
	const { slug } = params;
	const [out] = ROWS[slug];
	return <h1>{out}</h1>;
}`
		tree := buildPageWithParams(t, src, map[string]string{"slug": "__KRATE_PARAM_slug__"}, true)
		if tree.Root.Tier != irtree.TierClient {
			t.Fatalf("tier = %v, want client", tree.Root.Tier)
		}
		joined := strings.Join(tree.Root.PreSignalVars, ";")
		if !strings.Contains(joined, "var params=window.__KRATE_PARAMS__||{}") {
			t.Fatalf("PreSignalVars = %v, want window.__KRATE_PARAMS__ binding", tree.Root.PreSignalVars)
		}
	})

	t.Run("dynamic template stays static when destructured param renders directly", func(t *testing.T) {
		src := `export default function Page({ params }) {
	const { slug } = params;
	return <h1>{slug}</h1>;
}`
		tree := buildPageWithParams(t, src, map[string]string{"slug": "__KRATE_PARAM_slug__"}, true)
		if tree.Root.Tier != irtree.TierStatic {
			t.Fatalf("tier = %v, want static (PreSignalVars=%v)", tree.Root.Tier, tree.Root.PreSignalVars)
		}
		if got := renderStaticHTML(tree.Root.Children); got != "<h1>__KRATE_PARAM_slug__</h1>" {
			t.Fatalf("HTML = %q, want sentinel heading", got)
		}
	})
}
