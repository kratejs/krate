package build

import (
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/config"
)

func TestRouteListAndPageDetail(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, root, "src/pages/index.tsx", `export default function Page() { return <h1>Hi</h1>; }`)
	writeTestFile(t, root, "src/pages/about.tsx", `export default function About() { return <h1>About</h1>; }`)
	writeTestFile(t, root, "src/pages/video/[id].tsx", `export default function Video() { return <h1>Video</h1>; }`)
	writeTestFile(t, root, "src/pages/404.tsx", `export default function NotFound() { return <h1>404</h1>; }`)

	cfg := config.Default()
	cfg.Resolve(root)
	b := New(root, cfg)

	routes, err := b.RouteList()
	if err != nil {
		t.Fatalf("RouteList: %v", err)
	}
	byRoute := map[string]RouteSummary{}
	for _, r := range routes {
		byRoute[r.Route] = r
	}
	if _, ok := byRoute["/about"]; !ok {
		t.Errorf("missing /about in %+v", routes)
	}
	if _, ok := byRoute["/"]; !ok {
		t.Errorf("missing root route in %+v", routes)
	}
	if v, ok := byRoute["/video/[id]"]; !ok || len(v.Params) != 1 || v.Params[0] != "id" {
		t.Errorf("expected /video/[id] with param id, got %+v", v)
	}
	// 404 must not register as the root route.
	if byRoute["/"].Source == "src/pages/404.tsx" {
		t.Errorf("404 page leaked into the root route: %+v", byRoute["/"])
	}

	detail, err := b.PageDetail("/about")
	if err != nil {
		t.Fatalf("PageDetail: %v", err)
	}
	if !strings.HasSuffix(detail.Source, "about.tsx") {
		t.Errorf("unexpected source: %s", detail.Source)
	}
	if detail.LossyTypes {
		t.Errorf("plain JSX should not be lossy")
	}
	if len(detail.AST) == 0 || !strings.Contains(string(detail.AST), `"Program"`) {
		t.Errorf("expected AST document, got %s", string(detail.AST))
	}
}

func TestPageDetailLossyTypes(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, root, "src/pages/typed.tsx", `interface Props { title: string }
export default function Page(props: Props) { return <h1>{props.title}</h1>; }`)

	cfg := config.Default()
	cfg.Resolve(root)
	b := New(root, cfg)

	detail, err := b.PageDetail("/typed")
	if err != nil {
		t.Fatalf("PageDetail: %v", err)
	}
	if !detail.LossyTypes {
		t.Fatal("expected LossyTypes true for a page with interfaces/annotations")
	}
}

func TestPageDetailBySourcePath(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, root, "src/pages/about.tsx", `export default function About() { return <h1>About</h1>; }`)

	cfg := config.Default()
	cfg.Resolve(root)
	b := New(root, cfg)

	detail, err := b.PageDetail("src/pages/about.tsx")
	if err != nil {
		t.Fatalf("PageDetail by source: %v", err)
	}
	if detail.Route != "/about" {
		t.Errorf("expected /about, got %s", detail.Route)
	}
}

func TestPageDetailFormats(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, root, "src/pages/about.tsx", `export default function About() { return <h1>About</h1>; }`)
	writeTestFile(t, root, "src/pages/typed.tsx", `interface Props { title: string }
export default function Page(props: Props) { return <h1>{props.title}</h1>; }`)

	cfg := config.Default()
	cfg.Resolve(root)
	b := New(root, cfg)

	// Source-only: raw text, no AST work.
	d, err := b.PageDetailFor("/about", FormatSource)
	if err != nil {
		t.Fatalf("PageDetailFor source: %v", err)
	}
	if d.Content == "" || !strings.Contains(d.Content, "About") {
		t.Errorf("expected source content, got %q", d.Content)
	}
	if len(d.AST) != 0 {
		t.Errorf("source format must not compute the AST, got %s", d.AST)
	}

	// AST: document but no source content.
	d, err = b.PageDetailFor("/about", FormatAST)
	if err != nil {
		t.Fatalf("PageDetailFor ast: %v", err)
	}
	if len(d.AST) == 0 || !strings.Contains(string(d.AST), `"Program"`) {
		t.Errorf("expected AST document, got %s", d.AST)
	}
	if d.Content != "" {
		t.Errorf("ast format must not carry source content, got %q", d.Content)
	}

	// All: everything, including lossyTypes for typed source.
	d, err = b.PageDetailFor("/typed", FormatAll)
	if err != nil {
		t.Fatalf("PageDetailFor all: %v", err)
	}
	if !d.LossyTypes {
		t.Errorf("expected LossyTypes true for /typed")
	}
	if d.Content == "" || len(d.AST) == 0 {
		t.Errorf("all format should include source and AST")
	}
}
