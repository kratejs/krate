package build

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/config"
)

// buildPlain builds files without Tailwind and returns the index page HTML and
// the captured build log.
func buildPlain(t *testing.T, files map[string]string) (html, logged string) {
	t.Helper()
	_, out, logged := buildFiles(t, files, func(c *config.Config) {
		c.Tailwind.Enabled = false
		c.Tailwind.Strict = false
	})
	b, err := os.ReadFile(filepath.Join(out, "index.html"))
	if err != nil {
		t.Fatal(err)
	}
	return string(b), logged
}

func TestNamedReexportBarrelRendersComponents(t *testing.T) {
	html, logged := buildPlain(t, map[string]string{
		"src/components/Card.tsx": "export function Card() { return <b>CARD</b>; }\nexport default function D() { return <i>DEF</i>; }\n",
		"src/components/index.ts": "export { Card } from './Card';\nexport { default as Dflt } from './Card';\n",
		"src/components/deep.ts":  "export { Card as Deep } from './index';\n",
		"src/pages/index.tsx":     "import { Card, Dflt } from '../components';\nimport { Deep } from '../components/deep';\nexport default function Home() { return <main><Card /><Dflt /><Deep /></main>; }\n",
	})
	if !strings.Contains(html, "<b>CARD</b><i>DEF</i><b>CARD</b>") {
		t.Errorf("barrel components missing:\n%s\nlog:\n%s", html, logged)
	}
	if strings.Contains(logged, "could not be resolved") {
		t.Errorf("unexpected unresolved warning:\n%s", logged)
	}
}

func TestComponentFileBesideSameNamedDirectory(t *testing.T) {
	html, logged := buildPlain(t, map[string]string{
		"src/components/Button.tsx":       "export default function Button() { return <b>BTN</b>; }\n",
		"src/components/Button/style.css": "b { color: red; }\n",
		"src/pages/index.tsx":             "import Button from '../components/Button';\nexport default function Home() { return <main><Button /></main>; }\n",
	})
	if !strings.Contains(html, "<b>BTN</b>") || strings.Contains(logged, "could not be resolved") {
		t.Errorf("Button missing:\n%s\nlog:\n%s", html, logged)
	}
}

func TestReservedNameExportsRenderAndHydrate(t *testing.T) {
	html, logged := buildPlain(t, map[string]string{
		"src/lib/theme.ts": "export const krateTheme = { name: 'dark' };\nexport function h() { return 'H'; }\n",
		"src/pages/index.tsx": "import { createSignal } from '@krate/runtime';\nimport { krateTheme, h } from '../lib/theme';\n" +
			"export default function Home() { const [n, setN] = createSignal(0); return <main><p>{krateTheme.name}</p><p>{h()}</p><button onClick={() => setN(n() + 1)}>{n()}{krateTheme.name}</button></main>; }\n",
	})
	if !strings.Contains(html, "<p>dark</p><p>H</p>") {
		t.Errorf("exports with reserved names did not render:\n%s", html)
	}
	if strings.Contains(logged, "KR8302") {
		t.Errorf("unexpected KR8302:\n%s", logged)
	}
}

func buildErrText(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for p, c := range files {
		full := filepath.Join(root, p)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(c), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	cfg := config.Default()
	cfg.PagesDir = filepath.Join(root, "src", "pages")
	cfg.OutDir = filepath.Join(root, "dist")
	cfg.Tailwind.Enabled = false
	if err := New(root, cfg).BuildAll(); err != nil {
		return err.Error()
	}
	return ""
}

func TestScssImportIsABuildError(t *testing.T) {
	got := buildErrText(t, map[string]string{
		"src/pages/index.tsx": "import './a.scss';\nexport default function P() { return <p>x</p>; }\n",
		"src/pages/a.scss":    "a { b { color: red } }\n",
	})
	if !strings.Contains(got, "KR7120") || !strings.Contains(got, "a.scss") || !strings.Contains(got, "Sass") {
		t.Errorf("build error = %q", got)
	}
}

func TestMemberAssignedFunctionComponents(t *testing.T) {
	html, logged := buildPlain(t, map[string]string{
		"src/components/Table.tsx": "export function Table(props) { return <table>{props.children}</table>; }\n" +
			"Table.Cell = function (props) { return <td class=\"c\">{props.children}</td>; };\n" +
			"Table.Head = (props) => <th>{props.children}</th>;\n" +
			"Table.Named = function Inner(props) { return <td class=\"n\">{props.children}</td>; };\n",
		"src/pages/index.tsx": "import { Table } from '../components/Table';\n" +
			"function Local(props) { return <i>{props.children}</i>; }\nLocal.Part = function (props) { return <b>{props.children}</b>; };\n" +
			"export default function Home() { return <Table><tr><Table.Head>h</Table.Head><Table.Cell>x</Table.Cell><Table.Named>y</Table.Named></tr><Local><Local.Part>z</Local.Part></Local></Table>; }\n",
	})
	want := `<tr><th>h</th><td class=c>x</td><td class=n>y</td></tr><i><b>z</b></i>`
	if !strings.Contains(html, want) || strings.Contains(logged, "could not be resolved") {
		t.Errorf("member components missing:\n%s\nlog:\n%s", html, logged)
	}
}

func TestSameNamedExportsRenderEachOwnComponent(t *testing.T) {
	html, logged := buildPlain(t, map[string]string{
		"src/ui/a.tsx": "export function Card(props) { return <p class=\"a\">A{props.label}</p>; }\n",
		"src/ui/b.tsx": "export function Card(props) { return <b class=\"b\">B{props.label}</b>; }\n",
		"src/pages/index.tsx": "import { Card as A } from '../ui/a';\nimport { Card as B } from '../ui/b';\n" +
			"export default function Home() { return <main><A label=\"1\" /><B label=\"2\" /></main>; }\n",
	})
	if !strings.Contains(html, `<p class=a>A1</p><b class=b>B2</b>`) {
		t.Errorf("each Card should render its own module's component:\n%s", html)
	}
	if strings.Contains(logged, "KR7116") {
		t.Errorf("collision should be resolved:\n%s", logged)
	}
}

func TestComponentWithPlainParamAndModuleConst(t *testing.T) {
	html, logged := buildPlain(t, map[string]string{
		"src/pages/index.tsx": "const data = 'O';\nconst obj = { p: 'OBJ' };\n" +
			"function Box(p) { return <div class={p.cls} title={obj.p}>{p.title}{p.p}{p.children}</div>; }\n" +
			"const Arrow = (q) => <em>{q.title}{data}</em>;\n" +
			"export default function A() { return <section><Box title=\"x\" cls=\"c\" p=\"P\">kid</Box><Arrow title=\"y\" />{data}</section>; }\n",
	})
	want := `<section><div class=c title=OBJ>xPkid</div><em>yO</em>O</section>`
	if !strings.Contains(html, want) {
		t.Errorf("want %s in:\n%s\nlog:\n%s", want, html, logged)
	}
}

func TestProjectComponentsNamedLikeBuiltinsWin(t *testing.T) {
	html, logged := buildPlain(t, map[string]string{
		"src/ui/Image.tsx": "export default function Image(p) { return <figure>{p.alt}</figure>; }\n",
		"src/ui/parts.tsx": "export function Head(p) { return <header>{p.children}</header>; }\n",
		"src/pages/index.tsx": "import Image from '../ui/Image';\nimport { Head } from '../ui/parts';\nconst sib = 5;\n" +
			"function Link(p) { return <i class=\"mine\">{p.children}</i>; }\n" +
			"function Icon(p) { return <u>{p.name}{sib}</u>; }\n" +
			"export default function A() { return <main><Link>l</Link><Head>h</Head><Image alt=\"a\" src=\"/missing.png\" /><Icon name=\"i\" /></main>; }\n",
	})
	want := `<main><i class=mine>l</i><header>h</header><figure>a</figure><u>i5</u></main>`
	if !strings.Contains(html, want) {
		t.Errorf("want %s in:\n%s\nlog:\n%s", want, html, logged)
	}
}

func TestRuntimeLinkStillBuiltIn(t *testing.T) {
	html, _ := buildPlain(t, map[string]string{
		"src/pages/index.tsx": "import { Link } from '@krate/runtime';\nexport default function A() { return <main><Link href=\"/about\">go</Link></main>; }\n",
	})
	if !strings.Contains(html, "data-krate-link") {
		t.Errorf("the built-in <Link> was lost:\n%s", html)
	}
}

func TestAliasBarrelAndRenamedDefaultResolve(t *testing.T) {
	root := t.TempDir()
	files := map[string]string{
		"tsconfig.json":           `{"compilerOptions":{"baseUrl":".","paths":{"@/*":["./src/*"]}}}`,
		"src/assets/p.png":        "PNG",
		"src/components/Card.tsx": "export function Card() { return <b>CARD</b>; }\nexport default function D() { return <i>DEF</i>; }\n",
		"src/components/index.ts": "export { Card } from './Card';\nexport { default as Dflt } from './Card';\n",
		"src/pages/index.tsx": "import Foo from '@/components/Card';\nimport { Card, Dflt } from '@/components';\nimport logo from '@/assets/p.png';\n" +
			"export default function Home() { return <main><Foo /><Card /><Dflt /><img src={logo} alt=\"\" /></main>; }\n",
	}
	for p, c := range files {
		full := filepath.Join(root, p)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(c), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	cfg := config.Default()
	cfg.PagesDir = filepath.Join(root, "src", "pages")
	cfg.OutDir = filepath.Join(root, "dist")
	cfg.Tailwind.Enabled = false
	cfg.LoadTSConfigPaths(root)
	if err := New(root, cfg).BuildAll(); err != nil {
		t.Fatal(err)
	}
	out, err := os.ReadFile(filepath.Join(cfg.OutDir, "index.html"))
	if err != nil {
		t.Fatal(err)
	}
	html := string(out)
	if !strings.Contains(html, "<main><i>DEF</i><b>CARD</b><i>DEF</i><img src=/assets/p-") {
		t.Errorf("aliased imports did not resolve:\n%s", html)
	}
}
